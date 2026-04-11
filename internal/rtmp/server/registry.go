package server

// Stream Registry
// ----------------
// Thread-safe registry that tracks active publish streams keyed by the full
// stream key ("app/stream"). Publish/play handlers register one publisher
// and multiple subscribers per stream. The registry also supports media
// broadcast, codec detection, and subscriber removal.
//
// Concurrency model: sync.RWMutex guards the map. Per-stream mutable slices
// are guarded by the stream's own mutex (so that subscriber operations do not
// serialize across different streams).

import (
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
	"github.com/alxayo/go-rtmp/internal/rtmp/media"
	"github.com/alxayo/go-rtmp/internal/rtmp/metrics"
)

// ErrPublisherExists is returned when trying to set a second publisher.
var ErrPublisherExists = errors.New("publisher already registered for stream")

// Registry holds all active streams keyed by stream key.
type Registry struct {
	mu      sync.RWMutex
	streams map[string]*Stream
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry { return &Registry{streams: make(map[string]*Stream)} }

// Stream represents a published live stream with its publisher, subscribers,
// codec info, and optional FLV recorder.
//
// When a client publishes media, the stream caches the first audio and video
// "sequence headers" (codec configuration data). When a new subscriber joins
// mid-stream, these cached headers are sent immediately so the subscriber's
// decoder can initialize without waiting for the next keyframe.
type Stream struct {
	Key         string             // unique identifier: "app/streamName" (e.g. "live/mystream")
	Publisher   interface{}        // the connection that is publishing media to this stream
	Subscribers []media.Subscriber // connections that are playing/watching this stream
	VideoCodec  string             // detected video codec (e.g. "H264", "HEVC")
	AudioCodec  string             // detected audio codec (e.g. "AAC", "MP3")
	StartTime   time.Time          // when the stream was created
	Recorder    media.MediaWriter  // optional media file recorder (nil if not recording)
	RecordDir   string             // non-empty when recording is requested; used for lazy recorder init

	// Cached sequence headers for late-joining subscribers.
	// Sequence headers contain codec configuration (H.264 SPS/PPS, AAC AudioSpecificConfig)
	// that decoders need before they can process media frames.
	AudioSequenceHeader *chunk.Message
	VideoSequenceHeader *chunk.Message

	mu sync.RWMutex // protects concurrent access to Subscribers and Publisher
}

// CreateStream returns the existing stream if present or creates a new one.
// The boolean indicates whether a new stream was created.
func (r *Registry) CreateStream(key string) (*Stream, bool) {
	if key == "" {
		return nil, false
	}
	// Fast path read
	r.mu.RLock()
	if s, ok := r.streams[key]; ok {
		r.mu.RUnlock()
		return s, false
	}
	r.mu.RUnlock()

	// Upgrade to write lock
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.streams[key]; ok { // double‑check
		return s, false
	}
	s := &Stream{Key: key, StartTime: time.Now(), Subscribers: make([]media.Subscriber, 0)}
	r.streams[key] = s
	metrics.StreamsActive.Add(1)
	return s, true
}

// GetStream returns the stream for key or nil if absent.
func (r *Registry) GetStream(key string) *Stream {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.streams[key]
}

// DeleteStream removes the stream (if present) and returns true if deleted.
func (r *Registry) DeleteStream(key string) bool {
	if key == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.streams[key]; ok {
		delete(r.streams, key)
		metrics.StreamsActive.Add(-1)
		return true
	}
	return false
}

// SetPublisher sets the publisher if empty else returns ErrPublisherExists.
func (s *Stream) SetPublisher(pub interface{}) error {
	if s == nil || pub == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Publisher != nil {
		return ErrPublisherExists
	}
	s.Publisher = pub
	metrics.PublishersActive.Add(1)
	metrics.PublishersTotal.Add(1)
	return nil
}

// EvictPublisher forcibly replaces the current publisher with a new one and
// returns the old publisher (if any). This is used when a new client tries
// to publish on a stream key that is still occupied by a stale/zombie
// connection. The caller is responsible for closing the old connection.
//
// Unlike SetPublisher (which rejects duplicates), EvictPublisher always
// succeeds. The old publisher's disconnect handler will fire when its
// connection is closed, but the identity check in PublisherDisconnected
// (s.Publisher == pub) will correctly see that the publisher has changed
// and skip cleanup — so there is no double-free risk.
func (s *Stream) EvictPublisher(newPub interface{}) (oldPub interface{}) {
	if s == nil || newPub == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	oldPub = s.Publisher
	s.Publisher = newPub
	if oldPub == nil {
		// No previous publisher — this is equivalent to a fresh SetPublisher.
		metrics.PublishersActive.Add(1)
	}
	// If oldPub != nil, the active count stays the same (one out, one in).
	// PublishersTotal tracks every successful publish attempt.
	metrics.PublishersTotal.Add(1)
	return oldPub
}

// AddSubscriber adds a subscriber (ignoring nil) in a thread‑safe manner.
func (s *Stream) AddSubscriber(sub media.Subscriber) {
	if s == nil || sub == nil {
		return
	}
	s.mu.Lock()
	s.Subscribers = append(s.Subscribers, sub)
	metrics.SubscribersActive.Add(1)
	metrics.SubscribersTotal.Add(1)
	s.mu.Unlock()
}

// RemoveSubscriber removes the first matching subscriber reference (identity
// comparison) from the slice. This helper is added by T050 (play handler) so
// tests can simulate disconnect without a full connection lifecycle yet.
func (s *Stream) RemoveSubscriber(sub media.Subscriber) {
	if s == nil || sub == nil {
		return
	}
	s.mu.Lock()
	for i, existing := range s.Subscribers {
		if existing == sub {
			// Remove without preserving order (swap delete) since order is
			// not semantically relevant.
			last := len(s.Subscribers) - 1
			s.Subscribers[i] = s.Subscribers[last]
			s.Subscribers[last] = nil
			s.Subscribers = s.Subscribers[:last]
			metrics.SubscribersActive.Add(-1)
			break
		}
	}
	s.mu.Unlock()
}

// SubscriberCount returns a snapshot count of subscribers.
func (s *Stream) SubscriberCount() int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.Subscribers)
}

// --- CodecStore interface implementation (required for relay/codec detection) ---

// SetAudioCodec sets the audio codec name in a thread-safe manner.
func (s *Stream) SetAudioCodec(codec string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.AudioCodec = codec
	s.mu.Unlock()
}

// SetVideoCodec sets the video codec name in a thread-safe manner.
func (s *Stream) SetVideoCodec(codec string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.VideoCodec = codec
	s.mu.Unlock()
}

// GetAudioCodec returns the current audio codec in a thread-safe manner.
func (s *Stream) GetAudioCodec() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.AudioCodec
}

// GetVideoCodec returns the current video codec in a thread-safe manner.
func (s *Stream) GetVideoCodec() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.VideoCodec
}

// GetRecorder returns the current recorder in a thread-safe manner.
// Returns nil if no recorder is active.
func (s *Stream) GetRecorder() media.MediaWriter {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Recorder
}

// StreamKey returns the stream's key (required by CodecStore interface).
func (s *Stream) StreamKey() string {
	if s == nil {
		return ""
	}
	return s.Key
}

// BroadcastMessage relays a publisher's media message to all current subscribers.
// It also performs one-shot codec detection on the first audio/video frames.
// This implementation mirrors media.Stream.BroadcastMessage but operates on
// server.Stream which has additional fields for recording, metadata, etc.
func (s *Stream) BroadcastMessage(detector *media.CodecDetector, msg *chunk.Message, logger *slog.Logger) {
	if s == nil || msg == nil || logger == nil {
		return
	}

	// Codec detection (first frame logic handled inside detector via empty codec check).
	if msg.TypeID == 8 || msg.TypeID == 9 {
		if detector == nil {
			detector = &media.CodecDetector{}
		}
		detector.Process(msg.TypeID, msg.Payload, s, logger)
	}

	// Cache sequence headers for late-joining subscribers.
	// Uses media.IsVideoSequenceHeader / media.IsAudioSequenceHeader helpers
	// which support both legacy (AVC/AAC) and Enhanced RTMP (FourCC) formats.
	if msg.TypeID == 9 && media.IsVideoSequenceHeader(msg.Payload) {
		s.mu.Lock()
		s.VideoSequenceHeader = &chunk.Message{
			CSID:            msg.CSID,
			TypeID:          msg.TypeID,
			Timestamp:       msg.Timestamp,
			MessageStreamID: msg.MessageStreamID,
			MessageLength:   msg.MessageLength,
			Payload:         make([]byte, len(msg.Payload)),
		}
		copy(s.VideoSequenceHeader.Payload, msg.Payload)
		s.mu.Unlock()
		logger.Info("Cached video sequence header", "stream_key", s.Key, "size", len(msg.Payload))
	} else if msg.TypeID == 8 && media.IsAudioSequenceHeader(msg.Payload) {
		s.mu.Lock()
		s.AudioSequenceHeader = &chunk.Message{
			CSID:            msg.CSID,
			TypeID:          msg.TypeID,
			Timestamp:       msg.Timestamp,
			MessageStreamID: msg.MessageStreamID,
			MessageLength:   msg.MessageLength,
			Payload:         make([]byte, len(msg.Payload)),
		}
		copy(s.AudioSequenceHeader.Payload, msg.Payload)
		s.mu.Unlock()
		logger.Info("Cached audio sequence header", "stream_key", s.Key, "size", len(msg.Payload))
	}

	// DIAGNOSTIC: Log parsed video packet details for debugging.
	if msg.TypeID == 9 && len(msg.Payload) > 0 {
		if vm, err := media.ParseVideoMessage(msg.Payload); err == nil {
			logger.Debug("Video packet",
				"enhanced", vm.Enhanced,
				"codec", vm.Codec,
				"frame_type", vm.FrameType,
				"packet_type", vm.PacketType,
				"fourcc", vm.FourCC,
				"payload_len", len(msg.Payload))
		}
	}

	// Snapshot subscribers under read lock to avoid holding lock during I/O.
	s.mu.RLock()
	subs := make([]media.Subscriber, len(s.Subscribers))
	copy(subs, s.Subscribers)
	s.mu.RUnlock()

	// Send to each subscriber with backpressure handling.
	// CRITICAL FIX: Clone message payload for each subscriber to prevent
	// shared slice corruption between publisher and subscriber connections.
	for _, sub := range subs {
		if sub == nil {
			continue
		}

		// Create independent copy of message to prevent payload sharing issues
		relayMsg := &chunk.Message{
			CSID:            msg.CSID,
			TypeID:          msg.TypeID,
			Timestamp:       msg.Timestamp,
			MessageStreamID: msg.MessageStreamID,
			MessageLength:   msg.MessageLength,
			Payload:         make([]byte, len(msg.Payload)),
		}
		copy(relayMsg.Payload, msg.Payload)

		// Non-blocking path if available (TrySendMessage interface).
		if ts, ok := sub.(media.TrySendMessage); ok {
			if ok := ts.TrySendMessage(relayMsg); !ok {
				logger.Debug("Dropped media message (slow subscriber)", "stream_key", s.Key)
				continue
			}
			continue
		}
		// Fallback: best effort send (assumes timeout handling in SendMessage).
		_ = sub.SendMessage(relayMsg)
	}
}

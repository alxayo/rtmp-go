package server

// Stream Registry (Task T048)
// ---------------------------
// Thread‑safe registry that tracks active publish streams keyed by the full
// stream key ("app/stream"). This will be used by publish/play handlers so
// they can register one publisher and multiple subscribers. At this stage we
// only implement the minimal API required by the task; more helper methods
// (broadcast, removal hooks etc.) can be layered in future tasks.
//
// Concurrency model: sync.RWMutex guards the map. Per‑stream mutable slices
// are guarded by the stream's own mutex (so that subscriber operations do not
// serialize across different streams).

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
	"github.com/alxayo/go-rtmp/internal/rtmp/media"
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

// Stream represents a server side stream (superset of media.Stream fields).
// Publisher will point to a connection object in later tasks; we keep it as
// interface{} for now so tests can inject a stub. Subscribers re‑use the media
// package's Subscriber interface so the media relay can broadcast to them.
// Recorder is optional (may be nil) and provided by T045.
type Stream struct {
	Key         string
	Publisher   interface{}
	Subscribers []media.Subscriber
	Metadata    map[string]interface{}
	VideoCodec  string
	AudioCodec  string
	StartTime   time.Time
	Recorder    *media.Recorder

	// Cached sequence headers for late-joining subscribers
	AudioSequenceHeader *chunk.Message
	VideoSequenceHeader *chunk.Message

	mu sync.RWMutex // protects Subscribers & Publisher mutation
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
	s := &Stream{Key: key, StartTime: time.Now(), Metadata: make(map[string]interface{}), Subscribers: make([]media.Subscriber, 0)}
	r.streams[key] = s
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
	return nil
}

// AddSubscriber adds a subscriber (ignoring nil) in a thread‑safe manner.
func (s *Stream) AddSubscriber(sub media.Subscriber) {
	if s == nil || sub == nil {
		return
	}
	s.mu.Lock()
	s.Subscribers = append(s.Subscribers, sub)
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

	// Cache sequence headers for late-joining subscribers
	// Video: type_id=9, avc_packet_type=0 (byte offset 1)
	// Audio: type_id=8, aac_packet_type=0 (high nibble of byte 0 == 0xAF for AAC)
	if msg.TypeID == 9 && len(msg.Payload) >= 2 && msg.Payload[1] == 0 {
		// Video sequence header (AVC sequence header with SPS/PPS)
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
	} else if msg.TypeID == 8 && len(msg.Payload) >= 2 && (msg.Payload[0]>>4) == 0x0A && msg.Payload[1] == 0 {
		// Audio sequence header (AAC sequence header with AudioSpecificConfig)
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

	// DIAGNOSTIC: Log video packet structure to verify FLV format integrity
	if msg.TypeID == 9 && len(msg.Payload) >= 5 {
		frameType := (msg.Payload[0] >> 4) & 0x0F
		codecID := msg.Payload[0] & 0x0F
		avcPacketType := msg.Payload[1]
		logger.Debug("Video packet structure before relay",
			"frame_type", frameType,
			"codec_id", codecID,
			"avc_packet_type", avcPacketType,
			"payload_len", len(msg.Payload),
			"first_10_bytes", fmt.Sprintf("%02X %02X %02X %02X %02X %02X %02X %02X %02X %02X",
				msg.Payload[0], msg.Payload[1], msg.Payload[2], msg.Payload[3], msg.Payload[4],
				msg.Payload[5], msg.Payload[6], msg.Payload[7], msg.Payload[8], msg.Payload[9]))

		if codecID != 7 {
			logger.Warn("Invalid AVC codec ID in video packet", "codec_id", codecID, "expected", 7)
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

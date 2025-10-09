package media

import (
	"io"
	"log/slog"
	"sync"

	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// NOTE: The full Stream entity will ultimately live in internal/rtmp/server (see data-model.md).
// For the relay task (T044) we implement a minimal, package‑local stream that satisfies
// the CodecStore interface (for codec detection) and exposes subscriber management
// plus BroadcastMessage. Later tasks can replace usages with the server.Stream by
// keeping the same method and interface surface.
//
// Concurrency model: Add/Remove operations take the write lock. Broadcast takes the
// read lock, copies the current subscriber slice, then releases the lock before
// delivering messages to avoid holding the lock across potentially slow sends.
//
// Backpressure strategy: We attempt a non‑blocking send when the subscriber implements
// TrySendMessage(*chunk.Message) bool. If it returns false (queue full) we drop the
// message (as required) and continue. If the interface is not implemented we fall
// back to the blocking SendMessage(*chunk.Message) error which is assumed to handle
// its own timeout (future connection implementation will provide TrySendMessage).

type Subscriber interface {
	SendMessage(*chunk.Message) error
}

// TrySendMessage is an optional interface for non‑blocking enqueue semantics.
type TrySendMessage interface {
	TrySendMessage(*chunk.Message) bool
}

// Stream is a minimal implementation used only for media relay tests. It purposely
// only includes fields required for codec detection + broadcasting.
type Stream struct {
	key        string
	videoCodec string
	audioCodec string
	mu         sync.RWMutex
	subs       []Subscriber
}

func NewStream(key string) *Stream { return &Stream{key: key, subs: make([]Subscriber, 0)} }

// --- CodecStore implementation ---
func (s *Stream) SetAudioCodec(c string) { s.audioCodec = c }
func (s *Stream) SetVideoCodec(c string) { s.videoCodec = c }
func (s *Stream) GetAudioCodec() string  { return s.audioCodec }
func (s *Stream) GetVideoCodec() string  { return s.videoCodec }
func (s *Stream) StreamKey() string      { return s.key }

// AddSubscriber appends a subscriber safely.
func (s *Stream) AddSubscriber(sub Subscriber) {
	if sub == nil {
		return
	}
	s.mu.Lock()
	s.subs = append(s.subs, sub)
	s.mu.Unlock()
}

// Subscribers snapshot (used in tests only).
func (s *Stream) Subscribers() []Subscriber {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Subscriber, len(s.subs))
	copy(out, s.subs)
	return out
}

// BroadcastMessage relays a publisher's media message to all current subscribers.
// It also performs one‑shot codec detection on the first audio/video frames.
func (s *Stream) BroadcastMessage(detector *CodecDetector, msg *chunk.Message, logger *slog.Logger) {
	if s == nil || msg == nil || logger == nil {
		return
	}

	// Codec detection (first frame logic handled inside detector via empty codec check).
	if msg.TypeID == 8 || msg.TypeID == 9 {
		if detector == nil {
			detector = &CodecDetector{}
		}
		detector.Process(msg.TypeID, msg.Payload, s, logger)
	}

	// Snapshot subscribers under read lock.
	s.mu.RLock()
	subs := make([]Subscriber, len(s.subs))
	copy(subs, s.subs)
	s.mu.RUnlock()

	for _, sub := range subs {
		if sub == nil {
			continue
		}
		// Non‑blocking path if available.
		if ts, ok := sub.(TrySendMessage); ok {
			if ok := ts.TrySendMessage(msg); !ok {
				logger.Debug("Dropped media message (slow subscriber)", "stream_key", s.key)
				continue
			}
			continue
		}
		// Fallback: best effort send.
		_ = sub.SendMessage(msg)
	}
}

// NullLogger is a helper returning a no‑op slog.Logger for tests when caller
// doesn't care about output.
func NullLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

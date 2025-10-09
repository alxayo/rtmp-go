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
	"sync"
	"time"

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

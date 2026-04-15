package ingress

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// Manager coordinates the publish lifecycle across protocols.
//
// When a publisher (RTMP or SRT) wants to start streaming, it calls
// BeginPublish() which validates uniqueness, creates the stream session,
// and returns a PublishSession handle. The publisher then calls
// PushMedia() for each media message and EndPublish() when done.
//
// Manager is safe for concurrent use by multiple goroutines; all access
// to the sessions map is protected by a read-write mutex.
type Manager struct {
	// mu guards the sessions map so concurrent publishers can safely
	// register and unregister without data races.
	mu sync.RWMutex

	// sessions maps each stream key (e.g. "live/mystream") to its
	// active PublishSession. A stream key can have at most one active
	// session at any time.
	sessions map[string]*PublishSession

	// log is the structured logger for this manager. All log entries
	// include the "component" field set to "ingress" so operators can
	// filter ingress-related messages.
	log *slog.Logger
}

// NewManager creates a new ingress Manager.
// The logger is tagged with component=ingress so all output from this
// manager is easy to identify in the server logs.
func NewManager(log *slog.Logger) *Manager {
	return &Manager{
		sessions: make(map[string]*PublishSession),
		log:      log.With("component", "ingress"),
	}
}

// BeginPublish starts a new publish session for the given publisher.
//
// It validates that no other publisher is already streaming to the same
// stream key. If the key is free, it creates a PublishSession, stores it
// in the sessions map, and returns the session handle. The caller should
// then set session.MediaHandler and start calling PushMedia().
//
// Returns an error if the stream key is already in use.
func (m *Manager) BeginPublish(pub Publisher) (*PublishSession, error) {
	// Extract the stream key once — this is the map key.
	key := pub.StreamKey()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Reject duplicate publishers: only one source per stream key.
	if _, exists := m.sessions[key]; exists {
		return nil, fmt.Errorf("stream key %q already in use", key)
	}

	// Build the session with a logger that carries useful context fields
	// so every subsequent log line shows the stream key, publisher ID,
	// and protocol without repeating the values manually.
	session := &PublishSession{
		publisher: pub,
		streamKey: key,
		manager:   m,
		log: m.log.With(
			"stream_key", key,
			"publisher_id", pub.ID(),
			"protocol", pub.Protocol(),
		),
	}

	// Register the session so future BeginPublish calls for the same
	// key will be rejected, and GetSession can find it.
	m.sessions[key] = session
	session.log.Info("publish session started", "remote_addr", pub.RemoteAddr())

	return session, nil
}

// EndPublish cleans up the publish session for the given stream key.
// It removes the session from the map so another publisher can take
// over the stream key in the future. It is safe to call EndPublish for
// a key that has already been removed (it is a no-op).
//
// Note: This removes by key unconditionally — use for force-eviction.
// For normal session cleanup, use PublishSession.EndPublish() which
// checks identity to avoid removing a replacement session.
func (m *Manager) EndPublish(key string) {
	m.mu.Lock()
	// Look up and delete in one critical section to avoid races.
	session, exists := m.sessions[key]
	if exists {
		delete(m.sessions, key)
	}
	m.mu.Unlock()

	// Log outside the lock to avoid holding it while doing I/O.
	if exists && session != nil {
		session.log.Info("publish session ended")
	}
}

// endPublishIfCurrent removes the session only if it's still the active
// one for its stream key. If another session has replaced it (via
// force-eviction + re-registration), this is a no-op so the new
// session is not accidentally removed.
func (m *Manager) endPublishIfCurrent(key string, sess *PublishSession) {
	m.mu.Lock()
	current, exists := m.sessions[key]
	if exists && current == sess {
		delete(m.sessions, key)
	}
	m.mu.Unlock()

	if exists && current == sess {
		sess.log.Info("publish session ended")
	}
}

// GetSession returns the active publish session for a stream key.
// The second return value is false if no session exists for that key.
func (m *Manager) GetSession(key string) (*PublishSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[key]
	return s, ok
}

// ActiveSessions returns the number of currently active publish sessions.
// This is useful for metrics and status pages.
func (m *Manager) ActiveSessions() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// ---------------------------------------------------------------------------
// PublishSession
// ---------------------------------------------------------------------------

// PublishSession manages a single active publish session.
//
// It holds a reference to the Publisher that created it and provides the
// PushMedia method for feeding audio/video data into the server's media
// pipeline. The session is created by Manager.BeginPublish and cleaned
// up by EndPublish (either on the session or the manager).
type PublishSession struct {
	// publisher is the protocol-specific source that owns this session.
	publisher Publisher

	// streamKey is the routing key for this session (e.g. "live/mystream").
	streamKey string

	// manager is a back-reference to the Manager that created this session.
	// It is used by the convenience EndPublish() method on the session.
	manager *Manager

	// log is a structured logger pre-filled with stream_key, publisher_id,
	// and protocol fields for consistent, context-rich log output.
	log *slog.Logger

	// MediaHandler is called for each media message pushed into this
	// session. The server integration layer sets this callback to route
	// messages into the stream registry and broadcast to subscribers.
	//
	// If MediaHandler is nil, PushMedia silently drops the message.
	// This allows tests and startup code to create sessions before the
	// media pipeline is fully wired.
	MediaHandler func(msg *chunk.Message)
}

// Publisher returns the Publisher that owns this session.
func (s *PublishSession) Publisher() Publisher { return s.publisher }

// StreamKey returns the stream key for this session.
func (s *PublishSession) StreamKey() string { return s.streamKey }

// PushMedia routes a single media message through the pipeline.
//
// The message should have TypeID 8 (audio) or 9 (video) with properly
// formatted RTMP-style payload data. If no MediaHandler has been set,
// the message is silently dropped — this prevents panics during early
// session setup before the handler is wired.
func (s *PublishSession) PushMedia(msg *chunk.Message) {
	if s.MediaHandler != nil {
		s.MediaHandler(msg)
	}
}

// EndPublish cleans up this publish session by delegating to the
// Manager. Only removes the session if it's still the active one for
// the stream key — if a new session has taken over (via eviction),
// this is a safe no-op. This prevents an old session's deferred cleanup
// from accidentally removing a newly registered session.
func (s *PublishSession) EndPublish() {
	s.manager.endPublishIfCurrent(s.streamKey, s)
}

package ingress

import (
	"fmt"
	"log/slog"
	"sync"
	"testing"

	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// ---------------------------------------------------------------------------
// mockPublisher — a trivial Publisher implementation for tests.
// ---------------------------------------------------------------------------

// mockPublisher satisfies the Publisher interface with hard-coded values.
// Tests create one with the fields they need and pass it to Manager methods.
type mockPublisher struct {
	id       string // unique publisher identifier
	protocol string // protocol name ("rtmp", "srt", …)
	addr     string // remote network address
	key      string // stream key for routing
}

func (m *mockPublisher) ID() string         { return m.id }
func (m *mockPublisher) Protocol() string   { return m.protocol }
func (m *mockPublisher) RemoteAddr() string { return m.addr }
func (m *mockPublisher) StreamKey() string  { return m.key }
func (m *mockPublisher) Close() error       { return nil }

// newTestManager builds a Manager with a no-op logger so test output
// stays clean.
func newTestManager() *Manager {
	return NewManager(slog.Default())
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestNewManager verifies that NewManager produces a valid, empty manager.
func TestNewManager(t *testing.T) {
	m := newTestManager()
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
	if m.sessions == nil {
		t.Fatal("sessions map is nil")
	}
	if m.ActiveSessions() != 0 {
		t.Fatalf("expected 0 active sessions, got %d", m.ActiveSessions())
	}
}

// TestBeginPublish checks that a new session can be created and is
// returned with the correct stream key and publisher reference.
func TestBeginPublish(t *testing.T) {
	m := newTestManager()
	pub := &mockPublisher{id: "srt-1", protocol: "srt", addr: "10.0.0.1:5000", key: "live/test"}

	session, err := m.BeginPublish(pub)
	if err != nil {
		t.Fatalf("BeginPublish failed: %v", err)
	}
	if session == nil {
		t.Fatal("session is nil")
	}
	if session.StreamKey() != "live/test" {
		t.Fatalf("expected stream key %q, got %q", "live/test", session.StreamKey())
	}
	if session.Publisher().ID() != "srt-1" {
		t.Fatalf("expected publisher ID %q, got %q", "srt-1", session.Publisher().ID())
	}
}

// TestBeginPublishDuplicate ensures that two publishers cannot use the
// same stream key at the same time.
func TestBeginPublishDuplicate(t *testing.T) {
	m := newTestManager()
	pub1 := &mockPublisher{id: "srt-1", protocol: "srt", addr: "10.0.0.1:5000", key: "live/dup"}
	pub2 := &mockPublisher{id: "srt-2", protocol: "srt", addr: "10.0.0.2:5001", key: "live/dup"}

	// First publish should succeed.
	if _, err := m.BeginPublish(pub1); err != nil {
		t.Fatalf("first BeginPublish failed: %v", err)
	}

	// Second publish to the same key should fail.
	_, err := m.BeginPublish(pub2)
	if err == nil {
		t.Fatal("expected error for duplicate stream key, got nil")
	}
}

// TestEndPublish verifies that ending a session removes it from the
// manager, allowing a new publisher to reuse the stream key.
func TestEndPublish(t *testing.T) {
	m := newTestManager()
	pub := &mockPublisher{id: "srt-1", protocol: "srt", addr: "10.0.0.1:5000", key: "live/end"}

	session, err := m.BeginPublish(pub)
	if err != nil {
		t.Fatalf("BeginPublish failed: %v", err)
	}

	// End the session.
	session.EndPublish()

	// The manager should no longer have the session.
	if m.ActiveSessions() != 0 {
		t.Fatalf("expected 0 active sessions after EndPublish, got %d", m.ActiveSessions())
	}

	// Re-publishing to the same key should now succeed.
	if _, err := m.BeginPublish(pub); err != nil {
		t.Fatalf("re-publish after EndPublish failed: %v", err)
	}
}

// TestGetSession checks that GetSession returns the correct session
// for an active stream key.
func TestGetSession(t *testing.T) {
	m := newTestManager()
	pub := &mockPublisher{id: "srt-1", protocol: "srt", addr: "10.0.0.1:5000", key: "live/get"}

	session, _ := m.BeginPublish(pub)

	got, ok := m.GetSession("live/get")
	if !ok {
		t.Fatal("GetSession returned false for active key")
	}
	if got != session {
		t.Fatal("GetSession returned a different session object")
	}
}

// TestGetSessionMissing verifies that GetSession returns false for a
// stream key that has no active session.
func TestGetSessionMissing(t *testing.T) {
	m := newTestManager()

	_, ok := m.GetSession("live/nonexistent")
	if ok {
		t.Fatal("GetSession returned true for non-existent key")
	}
}

// TestActiveSessions confirms the count reflects adds and removes.
func TestActiveSessions(t *testing.T) {
	m := newTestManager()

	pub1 := &mockPublisher{id: "srt-1", protocol: "srt", addr: "10.0.0.1:5000", key: "live/a"}
	pub2 := &mockPublisher{id: "srt-2", protocol: "srt", addr: "10.0.0.2:5001", key: "live/b"}

	if m.ActiveSessions() != 0 {
		t.Fatalf("expected 0, got %d", m.ActiveSessions())
	}

	s1, _ := m.BeginPublish(pub1)
	if m.ActiveSessions() != 1 {
		t.Fatalf("expected 1, got %d", m.ActiveSessions())
	}

	m.BeginPublish(pub2)
	if m.ActiveSessions() != 2 {
		t.Fatalf("expected 2, got %d", m.ActiveSessions())
	}

	s1.EndPublish()
	if m.ActiveSessions() != 1 {
		t.Fatalf("expected 1 after EndPublish, got %d", m.ActiveSessions())
	}
}

// TestPushMediaWithHandler ensures PushMedia invokes the MediaHandler
// callback with the supplied message.
func TestPushMediaWithHandler(t *testing.T) {
	m := newTestManager()
	pub := &mockPublisher{id: "srt-1", protocol: "srt", addr: "10.0.0.1:5000", key: "live/media"}

	session, _ := m.BeginPublish(pub)

	// Track whether the handler was called and with which message.
	var received *chunk.Message
	session.MediaHandler = func(msg *chunk.Message) {
		received = msg
	}

	// Build a minimal video message.
	msg := &chunk.Message{
		TypeID:  9, // video
		Payload: []byte{0x17, 0x00}, // dummy video payload
	}

	session.PushMedia(msg)

	if received == nil {
		t.Fatal("MediaHandler was not called")
	}
	if received.TypeID != 9 {
		t.Fatalf("expected TypeID 9, got %d", received.TypeID)
	}
}

// TestPushMediaNilHandler ensures PushMedia does not panic when no
// MediaHandler has been set.
func TestPushMediaNilHandler(t *testing.T) {
	m := newTestManager()
	pub := &mockPublisher{id: "srt-1", protocol: "srt", addr: "10.0.0.1:5000", key: "live/nil"}

	session, _ := m.BeginPublish(pub)

	// MediaHandler is nil by default — PushMedia should be a safe no-op.
	msg := &chunk.Message{TypeID: 8, Payload: []byte{0xAF, 0x00}}
	session.PushMedia(msg) // must not panic
}

// TestConcurrentPublishers verifies that multiple goroutines can safely
// register and unregister different stream keys at the same time.
func TestConcurrentPublishers(t *testing.T) {
	m := newTestManager()

	// Number of concurrent publishers to simulate.
	const n = 50

	var wg sync.WaitGroup
	wg.Add(n)

	// Each goroutine publishes to a unique stream key, then ends its session.
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			pub := &mockPublisher{
				id:       fmt.Sprintf("srt-%d", idx),
				protocol: "srt",
				addr:     fmt.Sprintf("10.0.0.%d:5000", idx),
				key:      fmt.Sprintf("live/concurrent-%d", idx),
			}

			session, err := m.BeginPublish(pub)
			if err != nil {
				t.Errorf("goroutine %d: BeginPublish failed: %v", idx, err)
				return
			}

			// Push one message to exercise the handler path.
			session.PushMedia(&chunk.Message{TypeID: 9, Payload: []byte{0x00}})

			session.EndPublish()
		}(i)
	}

	wg.Wait()

	// All sessions should be cleaned up.
	if m.ActiveSessions() != 0 {
		t.Fatalf("expected 0 active sessions after concurrent test, got %d", m.ActiveSessions())
	}
}

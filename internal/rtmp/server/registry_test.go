// registry_test.go – tests for the stream registry (pub/sub map).
//
// The Registry maps stream keys ("app/name") to Stream objects. It
// supports create-or-get semantics, publisher/subscriber management,
// and deletion.
//
// Key Go concepts:
//   - Interface compliance check: var _ media.Subscriber = (*stubSubscriber)(nil)
//     ensures stubSubscriber implements the interface at compile time.
package server

import (
	"testing"

	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
	"github.com/alxayo/go-rtmp/internal/rtmp/media"
)

// stubSubscriber is a no-op Subscriber used to test subscriber counting.
type stubSubscriber struct{}

func (s *stubSubscriber) SendMessage(_ *chunk.Message) error { return nil }

// Compile-time check: stubSubscriber must implement media.Subscriber.
var _ media.Subscriber = (*stubSubscriber)(nil)

// TestRegistryCreateAndGet verifies that CreateStream returns (stream, true)
// for a new key, (stream, false) for a duplicate, and GetStream returns nil
// for a missing key.
func TestRegistryCreateAndGet(t *testing.T) {
	r := NewRegistry()
	if s, ok := r.CreateStream("app/stream1"); !ok || s == nil {
		t.Fatalf("expected new stream to be created")
	}
	// idempotent create
	if _, ok := r.CreateStream("app/stream1"); ok {
		t.Fatalf("expected existing stream, not newly created")
	}
	if r.GetStream("missing") != nil {
		t.Fatalf("expected nil for missing stream")
	}
}

// TestRegistryPublisher verifies that only one publisher can be set per
// stream – the second SetPublisher call must return an error.
func TestRegistryPublisher(t *testing.T) {
	r := NewRegistry()
	s, _ := r.CreateStream("app/stream2")
	if err := s.SetPublisher("pub1"); err != nil {
		t.Fatalf("unexpected error setting publisher: %v", err)
	}
	if err := s.SetPublisher("pub2"); err == nil {
		t.Fatalf("expected error on second publisher")
	}
}

// TestRegistrySubscribers adds two subscribers and verifies the count.
func TestRegistrySubscribers(t *testing.T) {
	r := NewRegistry()
	s, _ := r.CreateStream("app/stream3")
	s.AddSubscriber(&stubSubscriber{})
	s.AddSubscriber(&stubSubscriber{})
	if c := s.SubscriberCount(); c != 2 {
		t.Fatalf("expected 2 subscribers, got %d", c)
	}
}

// TestRegistryDelete verifies stream deletion: first delete succeeds,
// second returns false, and GetStream returns nil afterwards.
func TestRegistryDelete(t *testing.T) {
	r := NewRegistry()
	r.CreateStream("app/stream4")
	if !r.DeleteStream("app/stream4") {
		t.Fatalf("expected delete to succeed")
	}
	if r.GetStream("app/stream4") != nil {
		t.Fatalf("expected stream to be gone")
	}
	if r.DeleteStream("app/stream4") { // second delete
		t.Fatalf("expected second delete to be false")
	}
}

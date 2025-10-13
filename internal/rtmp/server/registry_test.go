package server

import (
	"testing"

	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
	"github.com/alxayo/go-rtmp/internal/rtmp/media"
)

// stubSubscriber implements media.Subscriber with a no‑op SendMessage.
type stubSubscriber struct{}

func (s *stubSubscriber) SendMessage(_ *chunk.Message) error { return nil }

// Ensure stub implements the right interface expected (from media package we imported earlier).
var _ media.Subscriber = (*stubSubscriber)(nil)

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

func TestRegistrySubscribers(t *testing.T) {
	r := NewRegistry()
	s, _ := r.CreateStream("app/stream3")
	s.AddSubscriber(&stubSubscriber{})
	s.AddSubscriber(&stubSubscriber{})
	if c := s.SubscriberCount(); c != 2 {
		t.Fatalf("expected 2 subscribers, got %d", c)
	}
}

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

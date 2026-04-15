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
	"io"
	"testing"

	"github.com/alxayo/go-rtmp/internal/logger"
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

// TestStreamCodecCaching verifies Set/Get for audio and video codec names.
func TestStreamCodecCaching(t *testing.T) {
	r := NewRegistry()
	s, _ := r.CreateStream("app/codec_test")

	// Initially empty
	if s.GetAudioCodec() != "" {
		t.Fatalf("expected empty audio codec, got %q", s.GetAudioCodec())
	}
	if s.GetVideoCodec() != "" {
		t.Fatalf("expected empty video codec, got %q", s.GetVideoCodec())
	}

	s.SetAudioCodec("AAC")
	s.SetVideoCodec("H264")

	if s.GetAudioCodec() != "AAC" {
		t.Fatalf("expected AAC, got %q", s.GetAudioCodec())
	}
	if s.GetVideoCodec() != "H264" {
		t.Fatalf("expected H264, got %q", s.GetVideoCodec())
	}
}

// identifiableSubscriber is a Subscriber with distinct identity for pointer comparison.
type identifiableSubscriber struct {
	id int
}

func (s *identifiableSubscriber) SendMessage(_ *chunk.Message) error { return nil }

var _ media.Subscriber = (*identifiableSubscriber)(nil)

// TestStreamRemoveSubscriber verifies that removing a subscriber decrements
// the count and that removing a non-existent subscriber is a no-op.
func TestStreamRemoveSubscriber(t *testing.T) {
	r := NewRegistry()
	s, _ := r.CreateStream("app/unsub_test")

	sub1 := &identifiableSubscriber{id: 1}
	sub2 := &identifiableSubscriber{id: 2}
	s.AddSubscriber(sub1)
	s.AddSubscriber(sub2)
	if s.SubscriberCount() != 2 {
		t.Fatalf("expected 2 subscribers, got %d", s.SubscriberCount())
	}

	s.RemoveSubscriber(sub1)
	if s.SubscriberCount() != 1 {
		t.Fatalf("expected 1 subscriber after remove, got %d", s.SubscriberCount())
	}

	// Remove again (no-op)
	s.RemoveSubscriber(sub1)
	if s.SubscriberCount() != 1 {
		t.Fatalf("expected 1 subscriber after duplicate remove, got %d", s.SubscriberCount())
	}
}

// capturingSubscriber records messages for assertion.
type capturingSubscriber struct {
	messages []*chunk.Message
}

func (c *capturingSubscriber) SendMessage(m *chunk.Message) error {
	c.messages = append(c.messages, m)
	return nil
}

var _ media.Subscriber = (*capturingSubscriber)(nil)

// TestBroadcastMessage_RelaysToSubscribers verifies that BroadcastMessage
// delivers a copy of the message to each subscriber.
func TestBroadcastMessage_RelaysToSubscribers(t *testing.T) {
	logger.UseWriter(io.Discard)
	r := NewRegistry()
	s, _ := r.CreateStream("app/broadcast_test")

	sub1 := &capturingSubscriber{}
	sub2 := &capturingSubscriber{}
	s.AddSubscriber(sub1)
	s.AddSubscriber(sub2)

	msg := &chunk.Message{
		CSID: 6, TypeID: 9, Timestamp: 100,
		MessageStreamID: 1, MessageLength: 3,
		Payload: []byte{0x17, 0x01, 0xFF},
	}
	s.BroadcastMessage(nil, msg, logger.Logger())

	if len(sub1.messages) != 1 {
		t.Fatalf("sub1: expected 1 message, got %d", len(sub1.messages))
	}
	if len(sub2.messages) != 1 {
		t.Fatalf("sub2: expected 1 message, got %d", len(sub2.messages))
	}

	// Verify payload is cloned (not shared)
	sub1.messages[0].Payload[0] = 0x00
	if msg.Payload[0] == 0x00 {
		t.Fatal("subscriber message payload shares memory with original")
	}
}

// TestBroadcastMessage_CachesVideoSequenceHeader verifies that a video
// sequence header (TypeID=9, avc_packet_type=0) is cached on the stream.
func TestBroadcastMessage_CachesVideoSequenceHeader(t *testing.T) {
	logger.UseWriter(io.Discard)
	r := NewRegistry()
	s, _ := r.CreateStream("app/seqhdr_test")

	// AVC sequence header: keyframe(0x17) + avc_packet_type=0
	seqHdr := &chunk.Message{
		CSID: 6, TypeID: 9, Timestamp: 0,
		MessageStreamID: 1, MessageLength: 4,
		Payload: []byte{0x17, 0x00, 0x01, 0x02},
	}
	s.BroadcastMessage(nil, seqHdr, logger.Logger())

	if s.VideoSequenceHeader == nil {
		t.Fatal("expected video sequence header to be cached")
	}
	if len(s.VideoSequenceHeader.Payload) != 4 {
		t.Fatalf("expected 4-byte payload, got %d", len(s.VideoSequenceHeader.Payload))
	}
}

// TestBroadcastMessage_CachesAudioSequenceHeader verifies that an AAC
// sequence header (TypeID=8, 0xAF 0x00) is cached on the stream.
func TestBroadcastMessage_CachesAudioSequenceHeader(t *testing.T) {
	logger.UseWriter(io.Discard)
	r := NewRegistry()
	s, _ := r.CreateStream("app/audio_seqhdr")

	// AAC sequence header: sound_format=10(AAC), aac_packet_type=0
	seqHdr := &chunk.Message{
		CSID: 4, TypeID: 8, Timestamp: 0,
		MessageStreamID: 1, MessageLength: 4,
		Payload: []byte{0xAF, 0x00, 0x12, 0x10},
	}
	s.BroadcastMessage(nil, seqHdr, logger.Logger())

	if s.AudioSequenceHeader == nil {
		t.Fatal("expected audio sequence header to be cached")
	}
	if len(s.AudioSequenceHeader.Payload) != 4 {
		t.Fatalf("expected 4-byte payload, got %d", len(s.AudioSequenceHeader.Payload))
	}
}

// TestEvictPublisher_ReplacesExisting verifies that EvictPublisher swaps the
// current publisher with a new one and returns the old publisher. This is
// the core mechanism that allows a reconnecting streamer to take over a
// stream key that is still occupied by a stale/zombie connection.
func TestEvictPublisher_ReplacesExisting(t *testing.T) {
	r := NewRegistry()
	s, _ := r.CreateStream("app/evict_test")

	// Set up an initial publisher.
	oldPub := "old_publisher"
	if err := s.SetPublisher(oldPub); err != nil {
		t.Fatalf("unexpected error setting publisher: %v", err)
	}

	// Evict the old publisher with a new one.
	newPub := "new_publisher"
	gotOld := s.EvictPublisher(newPub)

	// The returned value should be the old publisher.
	if gotOld != oldPub {
		t.Fatalf("expected old publisher %q, got %v", oldPub, gotOld)
	}
	// The stream's publisher should now be the new one.
	s.mu.RLock()
	if s.Publisher != newPub {
		t.Fatalf("expected publisher to be %q, got %v", newPub, s.Publisher)
	}
	s.mu.RUnlock()
}

// TestEvictPublisher_WhenEmpty verifies that EvictPublisher works correctly
// when no publisher is currently set — it should set the new publisher and
// return nil (no old publisher to evict).
func TestEvictPublisher_WhenEmpty(t *testing.T) {
	r := NewRegistry()
	s, _ := r.CreateStream("app/evict_empty")

	newPub := "fresh_publisher"
	gotOld := s.EvictPublisher(newPub)

	if gotOld != nil {
		t.Fatalf("expected nil old publisher, got %v", gotOld)
	}
	s.mu.RLock()
	if s.Publisher != newPub {
		t.Fatalf("expected publisher to be %q, got %v", newPub, s.Publisher)
	}
	s.mu.RUnlock()
}

// TestEvictPublisher_NilStream verifies that calling EvictPublisher on a nil
// stream does not panic — it should return nil safely.
func TestEvictPublisher_NilStream(t *testing.T) {
	var s *Stream
	// Should not panic.
	gotOld := s.EvictPublisher("pub")
	if gotOld != nil {
		t.Fatalf("expected nil from nil stream, got %v", gotOld)
	}
}

// TestEvictPublisher_ThenOldDisconnectIsNoOp verifies the critical safety
// property: after eviction, calling PublisherDisconnected with the OLD
// publisher does NOT clear the new publisher. This simulates what happens
// when the evicted connection's disconnect handler fires.
func TestEvictPublisher_ThenOldDisconnectIsNoOp(t *testing.T) {
	reg := NewRegistry()
	s, _ := reg.CreateStream("app/evict_identity")

	oldConn := &stubConn{}
	newConn := &stubConn{}

	if err := s.SetPublisher(oldConn); err != nil {
		t.Fatalf("set publisher: %v", err)
	}

	// Evict old with new.
	s.EvictPublisher(newConn)

	// Simulate the old connection's disconnect handler firing.
	// This should NOT clear the new publisher because the identity
	// check (s.Publisher == pub) will fail.
	PublisherDisconnected(reg, "app/evict_identity", oldConn)

	s.mu.RLock()
	if s.Publisher != newConn {
		t.Fatalf("new publisher should still be set after old disconnect cleanup")
	}
	s.mu.RUnlock()
}

// mockRecorder is a minimal MediaWriter stub for testing recorder cleanup.
type mockRecorder struct {
	closed bool
}

func (r *mockRecorder) WriteMessage(_ *chunk.Message) { /* no-op */ }
func (r *mockRecorder) Close() error {
	r.closed = true
	return nil
}
func (r *mockRecorder) Disabled() bool { return false }

var _ media.MediaWriter = (*mockRecorder)(nil)

// TestEvictedPublisherCleanupSkipsRecorder verifies that when an evicted
// publisher's cleanup runs, it does NOT close the recorder that belongs
// to the new publisher. This guards against the race where old SRT cleanup
// runs after eviction and could destroy the new publisher's recorder.
func TestEvictedPublisherCleanupSkipsRecorder(t *testing.T) {
	reg := NewRegistry()
	s, _ := reg.CreateStream("app/evict_recorder")

	oldPub := &stubConn{}
	newPub := &stubConn{}

	if err := s.SetPublisher(oldPub); err != nil {
		t.Fatalf("set publisher: %v", err)
	}

	// Simulate recorder created by old publisher.
	oldRec := &mockRecorder{}
	s.mu.Lock()
	s.Recorder = oldRec
	s.mu.Unlock()

	// Evict old publisher with new one.
	s.EvictPublisher(newPub)

	// Simulate new publisher creating a new recorder.
	newRec := &mockRecorder{}
	s.mu.Lock()
	s.Recorder = newRec
	s.mu.Unlock()

	// Simulate old publisher's cleanup running (identity-guarded pattern).
	// This mirrors the fix in srt_accept.go cleanup section.
	s.mu.Lock()
	if s.Publisher == oldPub {
		// This block should NOT execute since publisher was evicted.
		if s.Recorder != nil {
			s.Recorder.Close()
			s.Recorder = nil
		}
		s.Publisher = nil
	}
	s.mu.Unlock()

	// Verify: new publisher and recorder are untouched.
	s.mu.RLock()
	if s.Publisher != newPub {
		t.Fatal("new publisher was incorrectly cleared by old cleanup")
	}
	if s.Recorder != newRec {
		t.Fatal("new recorder was incorrectly closed/cleared by old cleanup")
	}
	s.mu.RUnlock()

	if newRec.closed {
		t.Fatal("new recorder was closed by old publisher's cleanup")
	}
}

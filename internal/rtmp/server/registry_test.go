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

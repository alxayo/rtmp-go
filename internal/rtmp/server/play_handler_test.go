package server

import (
	"testing"

	"github.com/alxayo/go-rtmp/internal/rtmp/amf"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
	"github.com/alxayo/go-rtmp/internal/rtmp/rpc"
)

// capturingConn collects all sent messages for ordering assertions.
type capturingConn struct{ sent []*chunk.Message }

func (c *capturingConn) SendMessage(m *chunk.Message) error { c.sent = append(c.sent, m); return nil }

// buildPlayMessage constructs a minimal AMF0 play command message.
func buildPlayMessage(streamName string) *chunk.Message {
	payload, _ := amf.EncodeAll("play", float64(0), nil, streamName)
	return &chunk.Message{TypeID: rpc.CommandMessageAMF0TypeIDForTest(), Payload: payload, MessageLength: uint32(len(payload)), MessageStreamID: 1}
}

// stubPublisher is a placeholder used to mark a stream as published.
type stubPublisher struct{}

func TestHandlePlaySuccess(t *testing.T) {
	reg := NewRegistry()
	// Prepare an existing stream with a publisher.
	s, _ := reg.CreateStream("app/live1")
	if err := s.SetPublisher(&stubPublisher{}); err != nil {
		t.Fatalf("set publisher: %v", err)
	}

	conn := &capturingConn{}
	msg := buildPlayMessage("live1")
	onStatus, err := HandlePlay(reg, conn, "app", msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if onStatus == nil {
		t.Fatalf("expected onStatus message")
	}
	// Expect two messages sent: StreamBegin control then onStatus Play.Start
	if len(conn.sent) != 2 {
		t.Fatalf("expected 2 messages sent, got %d", len(conn.sent))
	}
	vals, _ := amf.DecodeAll(onStatus.Payload)
	info, _ := vals[3].(map[string]interface{})
	if info["code"] != "NetStream.Play.Start" {
		t.Fatalf("unexpected onStatus code: %v", info["code"])
	}
	// Subscriber should be registered.
	if s.SubscriberCount() != 1 {
		t.Fatalf("expected 1 subscriber, got %d", s.SubscriberCount())
	}
}

func TestHandlePlayStreamNotFound(t *testing.T) {
	reg := NewRegistry() // no streams created
	conn := &capturingConn{}
	msg := buildPlayMessage("missing")
	onStatus, err := HandlePlay(reg, conn, "app", msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conn.sent) != 1 {
		t.Fatalf("expected 1 message (StreamNotFound), got %d", len(conn.sent))
	}
	vals, _ := amf.DecodeAll(onStatus.Payload)
	info, _ := vals[3].(map[string]interface{})
	if info["code"] != "NetStream.Play.StreamNotFound" {
		t.Fatalf("expected StreamNotFound code, got %v", info["code"])
	}
}

func TestSubscriberDisconnected(t *testing.T) {
	reg := NewRegistry()
	s, _ := reg.CreateStream("app/streamX")
	_ = s.SetPublisher(&stubPublisher{})
	conn := &capturingConn{}
	msg := buildPlayMessage("streamX")
	if _, err := HandlePlay(reg, conn, "app", msg); err != nil {
		t.Fatalf("play failed: %v", err)
	}
	if s.SubscriberCount() != 1 {
		t.Fatalf("expected subscriber added")
	}
	SubscriberDisconnected(reg, "app/streamX", conn)
	if s.SubscriberCount() != 0 {
		t.Fatalf("expected subscriber removed on disconnect")
	}
}

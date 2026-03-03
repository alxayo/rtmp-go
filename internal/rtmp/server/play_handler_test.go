// play_handler_test.go – tests for server-side play (subscribe) handling.
//
// When a client sends a "play" command, HandlePlay:
//  1. Parses the command to extract the stream key.
//  2. Looks up the stream in the registry.
//  3. If found: sends StreamBegin control + onStatus Play.Start, adds subscriber.
//  4. If not found: sends onStatus Play.StreamNotFound.
//
// Key Go concepts:
//   - capturingConn: records all sent messages in a slice for ordering assertions.
//   - stubPublisher: minimal type to mark a stream as having a publisher.
package server

import (
	"testing"

	"github.com/alxayo/go-rtmp/internal/rtmp/amf"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
	"github.com/alxayo/go-rtmp/internal/rtmp/rpc"
)

// capturingConn records every message sent via SendMessage.
type capturingConn struct{ sent []*chunk.Message }

func (c *capturingConn) SendMessage(m *chunk.Message) error { c.sent = append(c.sent, m); return nil }

// buildPlayMessage constructs a minimal AMF0 "play" command message.
func buildPlayMessage(streamName string) *chunk.Message {
	payload, _ := amf.EncodeAll("play", float64(0), nil, streamName)
	return &chunk.Message{TypeID: rpc.CommandMessageAMF0TypeIDForTest(), Payload: payload, MessageLength: uint32(len(payload)), MessageStreamID: 1}
}

// stubPublisher is a minimal placeholder to mark a stream as published.
type stubPublisher struct{}

// TestHandlePlaySuccess creates a stream with a publisher, then plays it.
// Expects 2 messages sent (StreamBegin + onStatus Play.Start) and 1 subscriber.
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

// TestHandlePlayStreamNotFound plays a stream that doesn't exist.
// Expects 1 message: onStatus NetStream.Play.StreamNotFound.
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

// TestSubscriberDisconnected verifies that when a subscriber disconnects,
// it is removed from the stream's subscriber list.
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

// TestHandlePlayWithQueryParams verifies that when a stream name contains
// query parameters (e.g. "live1?token=abc"), the query params are stripped
// and the stream is looked up under the clean key.
func TestHandlePlayWithQueryParams(t *testing.T) {
	reg := NewRegistry()
	// Create stream under clean key (no query params)
	s, _ := reg.CreateStream("app/live1")
	if err := s.SetPublisher(&stubPublisher{}); err != nil {
		t.Fatalf("set publisher: %v", err)
	}

	conn := &capturingConn{}
	msg := buildPlayMessage("live1?token=secret123")
	onStatus, err := HandlePlay(reg, conn, "app", msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should find the stream via clean key and send Play.Start
	vals, _ := amf.DecodeAll(onStatus.Payload)
	info, _ := vals[3].(map[string]interface{})
	if info["code"] != "NetStream.Play.Start" {
		t.Fatalf("expected Play.Start, got %v (query params not stripped?)", info["code"])
	}
	if s.SubscriberCount() != 1 {
		t.Fatalf("expected 1 subscriber, got %d", s.SubscriberCount())
	}
}

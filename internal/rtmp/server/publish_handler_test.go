package server

import (
	"testing"

	"github.com/alxayo/go-rtmp/internal/rtmp/amf"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
	"github.com/alxayo/go-rtmp/internal/rtmp/rpc"
)

// stubConn captures the last message sent; it mimics the subset of the
// connection we need (SendMessage). SendMessage always succeeds.
type stubConn struct{ last *chunk.Message }

func (s *stubConn) SendMessage(m *chunk.Message) error { s.last = m; return nil }

// buildPublishMessage builds a minimal AMF0 publish command message for tests.
func buildPublishMessage(streamName string) *chunk.Message {
	payload, _ := amf.EncodeAll("publish", float64(0), nil, streamName, "live")
	return &chunk.Message{TypeID: rpc.CommandMessageAMF0TypeIDForTest(), Payload: payload, MessageLength: uint32(len(payload)), MessageStreamID: 1}
}

func TestHandlePublishSuccess(t *testing.T) {
	reg := NewRegistry()
	sc := &stubConn{}
	msg := buildPublishMessage("testStream")

	onStatus, err := HandlePublish(reg, sc, "app", msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if onStatus == nil || sc.last == nil {
		t.Fatalf("expected onStatus message to be sent")
	}
	s := reg.GetStream("app/testStream")
	if s == nil || s.Publisher == nil {
		t.Fatalf("expected stream and publisher to be registered")
	}
	// Decode payload ensure onStatus code present
	vals, err := amf.DecodeAll(onStatus.Payload)
	if err != nil {
		t.Fatalf("decode onStatus: %v", err)
	}
	if len(vals) < 4 {
		t.Fatalf("expected >=4 AMF values, got %d", len(vals))
	}
	if vals[0] != "onStatus" {
		t.Fatalf("expected command name onStatus, got %v", vals[0])
	}
	info, _ := vals[3].(map[string]interface{})
	if info["code"] != "NetStream.Publish.Start" {
		t.Fatalf("unexpected status code: %v", info["code"])
	}
}

func TestHandlePublishDuplicate(t *testing.T) {
	reg := NewRegistry()
	first := &stubConn{}
	second := &stubConn{}
	msg := buildPublishMessage("dup")
	if _, err := HandlePublish(reg, first, "app", msg); err != nil {
		t.Fatalf("first publish failed: %v", err)
	}
	if _, err := HandlePublish(reg, second, "app", msg); err == nil {
		t.Fatalf("expected duplicate publish error")
	}
}

func TestPublisherDisconnected(t *testing.T) {
	reg := NewRegistry()
	sc := &stubConn{}
	msg := buildPublishMessage("gone")
	if _, err := HandlePublish(reg, sc, "app", msg); err != nil {
		t.Fatalf("publish failed: %v", err)
	}
	PublisherDisconnected(reg, "app/gone", sc)
	if s := reg.GetStream("app/gone"); s == nil || s.Publisher != nil {
		t.Fatalf("expected publisher cleared on disconnect")
	}
}

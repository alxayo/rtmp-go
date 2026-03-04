// publish_handler_test.go – tests for server-side publish handling.
//
// When a client sends a "publish" command, HandlePublish:
//  1. Parses the command to extract the stream key.
//  2. Creates the stream in the registry (or errors on duplicate).
//  3. Sets the publisher on the stream.
//  4. Sends an "onStatus" NetStream.Publish.Start response.
//
// Key Go concepts:
//   - stubConn (helpers_test.go): captures the last message sent.
//   - AMF decode of the onStatus payload to verify the response code.
package server

import (
	"testing"

	"github.com/alxayo/go-rtmp/internal/rtmp/amf"
)

// TestHandlePublishSuccess publishes a stream and verifies:
// the stream is registered, the publisher is set, and the onStatus
// response contains NetStream.Publish.Start.
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

// TestHandlePublishDuplicate attempts to publish to the same stream key
// twice and expects an error on the second attempt.
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

// TestPublisherDisconnected verifies that when a publisher disconnects,
// the stream's publisher is cleared (set to nil) but the stream itself
// remains in the registry.
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

// TestHandlePublishWithQueryParams verifies that when a stream name
// contains query parameters (e.g. "stream?token=abc"), the query params
// are stripped and the stream is registered under the clean key.
func TestHandlePublishWithQueryParams(t *testing.T) {
	reg := NewRegistry()
	sc := &stubConn{}
	msg := buildPublishMessage("mystream?token=secret123")

	onStatus, err := HandlePublish(reg, sc, "live", msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if onStatus == nil {
		t.Fatalf("expected onStatus message")
	}

	// Stream should be registered under the clean key (no query params)
	if s := reg.GetStream("live/mystream"); s == nil {
		t.Fatalf("expected stream registered as 'live/mystream', not found")
	}
	// Verify it's NOT registered with the query string in the key
	if s := reg.GetStream("live/mystream?token=secret123"); s != nil {
		t.Fatalf("stream should NOT be registered with query params in key")
	}
}

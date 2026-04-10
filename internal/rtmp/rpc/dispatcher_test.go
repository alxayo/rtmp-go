// dispatcher_test.go – tests for the RTMP command dispatcher.
//
// The Dispatcher routes incoming AMF0 command messages to registered
// handler callbacks based on the command name (connect, createStream,
// publish, play). Unknown commands are logged and ignored.
//
// Key Go concepts:
//   - Closures as callbacks: each On* handler captures test state.
//   - Struct literal with bool fields to track which handlers were invoked.
//   - Logger output capture via bytes.Buffer for verifying log messages.
package rpc

import (
	"bytes"
	"strings"
	"testing"

	"github.com/alxayo/go-rtmp/internal/logger"
	"github.com/alxayo/go-rtmp/internal/rtmp/amf"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// buildCmd is a helper that AMF0-encodes a list of values into a
// *chunk.Message with TypeID 20 (AMF0 command).
func buildCmd(t *testing.T, values ...interface{}) *chunk.Message {
	t.Helper()
	p, err := amf.EncodeAll(values...)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return &chunk.Message{TypeID: commandMessageAMF0TypeID, Payload: p, MessageLength: uint32(len(p)), MessageStreamID: 0}
}

// TestDispatcher_DispatchKnownCommands registers handlers for all four
// command types, dispatches them in sequence, and verifies every handler
// was called with correct values.
func TestDispatcher_DispatchKnownCommands(t *testing.T) {
	var got struct {
		connect bool
		create  bool
		publish bool
		play    bool
	}
	app := "live"
	d := NewDispatcher(func() string { return app })
	d.OnConnect = func(c *ConnectCommand, _ *chunk.Message) error {
		got.connect = true
		if c.App == "" {
			t.Errorf("connect app empty")
		}
		app = c.App
		return nil
	}
	d.OnCreateStream = func(cs *CreateStreamCommand, _ *chunk.Message) error {
		got.create = true
		if cs.TransactionID != 2 {
			t.Errorf("want trx=2 got %v", cs.TransactionID)
		}
		return nil
	}
	d.OnPublish = func(p *PublishCommand, _ *chunk.Message) error {
		got.publish = true
		if p.StreamKey != app+"/foo" {
			t.Errorf("bad stream key %s", p.StreamKey)
		}
		return nil
	}
	d.OnPlay = func(p *PlayCommand, _ *chunk.Message) error {
		got.play = true
		if p.StreamKey != app+"/bar" {
			t.Errorf("bad play key %s", p.StreamKey)
		}
		return nil
	}

	// connect
	if err := d.Dispatch(buildCmd(t, "connect", 1.0, map[string]interface{}{"app": app, "objectEncoding": 0.0})); err != nil {
		t.Fatalf("dispatch connect: %v", err)
	}
	// createStream
	if err := d.Dispatch(buildCmd(t, "createStream", 2.0, nil)); err != nil {
		t.Fatalf("dispatch createStream: %v", err)
	}
	// publish
	if err := d.Dispatch(buildCmd(t, "publish", 0.0, nil, "foo", "live")); err != nil {
		t.Fatalf("dispatch publish: %v", err)
	}
	// play
	if err := d.Dispatch(buildCmd(t, "play", 0.0, nil, "bar")); err != nil {
		t.Fatalf("dispatch play: %v", err)
	}

	if !got.connect || !got.create || !got.publish || !got.play {
		t.Fatalf("handlers not all invoked: %+v", got)
	}
}

// TestDispatcher_UnknownCommand dispatches a command name the dispatcher
// doesn't recognize ("someWeirdCommand") and verifies it doesn't error
// but does log a warning containing "unknown command".
func TestDispatcher_UnknownCommand(t *testing.T) {
	buf := &bytes.Buffer{}
	logger.UseWriter(buf)
	d := NewDispatcher(nil)
	if err := d.Dispatch(buildCmd(t, "someWeirdCommand", 1.0)); err != nil {
		t.Fatalf("unknown command should not error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "unknown command") {
		t.Fatalf("expected log to contain 'unknown command', got %s", out)
	}
}

// TestDispatcher_NoHandlerRegistered tests that dispatching a known
// command (publish) without registering its handler returns an error.
func TestDispatcher_NoHandlerRegistered(t *testing.T) {
	d := NewDispatcher(nil)
	// Only register connect handler to show missing publish handler errors.
	d.OnConnect = func(c *ConnectCommand, _ *chunk.Message) error { return nil }
	if err := d.Dispatch(buildCmd(t, "publish", 0.0, nil, "foo", "live")); err == nil {
		t.Fatalf("expected error for missing publish handler")
	}
}

// TestDispatcher_DeleteStream verifies that the deleteStream command is
// dispatched to the registered handler with the raw AMF0 values.
func TestDispatcher_DeleteStream(t *testing.T) {
	var called bool
	d := NewDispatcher(nil)
	d.OnDeleteStream = func(vals []interface{}, msg *chunk.Message) error {
		called = true
		// The first value in the decoded AMF0 payload should be the command
		// name string "deleteStream".
		if len(vals) < 1 {
			t.Fatal("expected at least 1 value")
		}
		name, ok := vals[0].(string)
		if !ok || name != "deleteStream" {
			t.Fatalf("expected command name 'deleteStream', got %v", vals[0])
		}
		return nil
	}
	// deleteStream payload: name="deleteStream", txnID=0, null, streamID=1
	if err := d.Dispatch(buildCmd(t, "deleteStream", 0.0, nil, 1.0)); err != nil {
		t.Fatalf("dispatch deleteStream: %v", err)
	}
	if !called {
		t.Fatal("deleteStream handler was not invoked")
	}
}

// TestDispatcher_DeleteStream_NoHandler verifies that dispatching deleteStream
// without a registered handler returns a protocol error (so the caller can
// log it) rather than silently swallowing it.
func TestDispatcher_DeleteStream_NoHandler(t *testing.T) {
	d := NewDispatcher(nil)
	// No OnDeleteStream handler set — should return a protocol error.
	err := d.Dispatch(buildCmd(t, "deleteStream", 0.0, nil, 1.0))
	if err == nil {
		t.Fatal("expected error when no deleteStream handler is registered")
	}
}

// TestDispatcher_CloseStream verifies that the closeStream command is correctly
// routed to the registered CloseStream handler. Some RTMP clients (e.g. OBS)
// send closeStream instead of deleteStream when ending a session.
func TestDispatcher_CloseStream(t *testing.T) {
	var called bool
	d := NewDispatcher(nil)
	d.OnCloseStream = func(vals []interface{}, msg *chunk.Message) error {
		called = true
		if len(vals) < 1 {
			t.Fatal("expected at least 1 value")
		}
		name, ok := vals[0].(string)
		if !ok || name != "closeStream" {
			t.Fatalf("expected command name 'closeStream', got %v", vals[0])
		}
		return nil
	}
	// closeStream payload: name="closeStream", txnID=6, null
	if err := d.Dispatch(buildCmd(t, "closeStream", 6.0, nil)); err != nil {
		t.Fatalf("dispatch closeStream: %v", err)
	}
	if !called {
		t.Fatal("closeStream handler was not invoked")
	}
}

// TestDispatcher_CloseStream_NoHandler verifies that dispatching closeStream
// without a handler does NOT return an error — it is gracefully ignored. This
// differs from deleteStream because closeStream is a non-standard extension
// and some servers intentionally choose not to handle it.
func TestDispatcher_CloseStream_NoHandler(t *testing.T) {
	d := NewDispatcher(nil)
	// No OnCloseStream handler set — should be silently ignored (no error).
	err := d.Dispatch(buildCmd(t, "closeStream", 6.0, nil))
	if err != nil {
		t.Fatalf("closeStream without handler should not error, got: %v", err)
	}
}

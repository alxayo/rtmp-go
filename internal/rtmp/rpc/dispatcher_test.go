package rpc

import (
	"bytes"
	"strings"
	"testing"

	"github.com/alxayo/go-rtmp/internal/logger"
	"github.com/alxayo/go-rtmp/internal/rtmp/amf"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// helper to build a command message with AMF0 payload
func buildCmd(t *testing.T, values ...interface{}) *chunk.Message {
	t.Helper()
	p, err := amf.EncodeAll(values...)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return &chunk.Message{TypeID: commandMessageAMF0TypeID, Payload: p, MessageLength: uint32(len(p)), MessageStreamID: 0}
}

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

func TestDispatcher_NoHandlerRegistered(t *testing.T) {
	d := NewDispatcher(nil)
	// Only register connect handler to show missing publish handler errors.
	d.OnConnect = func(c *ConnectCommand, _ *chunk.Message) error { return nil }
	if err := d.Dispatch(buildCmd(t, "publish", 0.0, nil, "foo", "live")); err == nil {
		t.Fatalf("expected error for missing publish handler")
	}
}

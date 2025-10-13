package rpc

import (
	"testing"

	"github.com/alxayo/go-rtmp/internal/rtmp/amf"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

func buildPublishMessage(payload []byte) *chunk.Message {
	return &chunk.Message{TypeID: 20, Payload: payload}
}

func TestParsePublishCommand_Valid(t *testing.T) {
	payload, err := amf.EncodeAll(
		"publish", // command name
		0.0,       // transaction ID (ignored, spec uses 0)
		nil,       // null per spec
		"stream1", // publishingName
		"live",    // publishingType
	)
	if err != nil {
		fatalf(t, "encode: %v", err)
	}

	cmd, err := ParsePublishCommand("app", buildPublishMessage(payload))
	if err != nil {
		fatalf(t, "ParsePublishCommand error: %v", err)
	}
	if cmd.StreamKey != "app/stream1" || cmd.PublishingType != "live" {
		fatalf(t, "unexpected parsed command: %+v", cmd)
	}
}

func TestParsePublishCommand_MissingPublishingName(t *testing.T) {
	payload, err := amf.EncodeAll(
		"publish",
		0.0,
		nil,
		// omit publishingName and rest
	)
	if err != nil {
		fatalf(t, "encode: %v", err)
	}

	if _, err := ParsePublishCommand("app", buildPublishMessage(payload)); err == nil {
		fatalf(t, "expected error for missing publishingName")
	}
}

// fatalf is a tiny helper to reduce noise and still mark the test failed.
func fatalf(t *testing.T, format string, args ...interface{}) { t.Helper(); t.Fatalf(format, args...) }

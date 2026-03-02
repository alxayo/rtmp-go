// publish_test.go – tests for parsing the RTMP "publish" command.
//
// The "publish" command starts a media stream. AMF0 encoding:
//
//	["publish", 0.0, null, publishingName, publishingType]
//
// ParsePublishCommand builds the stream key as "app/publishingName".
package rpc

import (
	"testing"

	"github.com/alxayo/go-rtmp/internal/rtmp/amf"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// buildPublishMessage wraps payload as a TypeID 20 command message.
func buildPublishMessage(payload []byte) *chunk.Message {
	return &chunk.Message{TypeID: 20, Payload: payload}
}

// TestParsePublishCommand_Valid encodes a valid publish command and
// verifies streamKey = "app/stream1" and publishingType = "live".
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

// TestParsePublishCommand_MissingPublishingName omits the stream name
// field and expects an error.
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

// fatalf is a tiny helper to reduce noise and mark the caller as the
// failure site via t.Helper().
func fatalf(t *testing.T, format string, args ...interface{}) { t.Helper(); t.Fatalf(format, args...) }

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
	if len(cmd.QueryParams) != 0 {
		fatalf(t, "expected empty QueryParams, got %v", cmd.QueryParams)
	}
}

// TestParsePublishCommand_WithToken verifies that query parameters
// (like ?token=abc) are parsed from the stream name and stripped from
// PublishingName and StreamKey.
func TestParsePublishCommand_WithToken(t *testing.T) {
	payload, err := amf.EncodeAll(
		"publish",
		0.0,
		nil,
		"stream1?token=abc123&expires=999", // name with query params
		"live",
	)
	if err != nil {
		fatalf(t, "encode: %v", err)
	}

	cmd, err := ParsePublishCommand("app", buildPublishMessage(payload))
	if err != nil {
		fatalf(t, "error: %v", err)
	}
	if cmd.PublishingName != "stream1" {
		fatalf(t, "expected clean PublishingName 'stream1', got %q", cmd.PublishingName)
	}
	if cmd.StreamKey != "app/stream1" {
		fatalf(t, "expected clean StreamKey 'app/stream1', got %q", cmd.StreamKey)
	}
	if cmd.QueryParams["token"] != "abc123" {
		fatalf(t, "expected token=abc123, got %q", cmd.QueryParams["token"])
	}
	if cmd.QueryParams["expires"] != "999" {
		fatalf(t, "expected expires=999, got %q", cmd.QueryParams["expires"])
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

// play_test.go – tests for parsing the RTMP "play" command.
//
// The "play" command requests a stream from the server. AMF0 encoding:
//
//	["play", 0.0, null, streamName, start, duration, reset]
//
// ParsePlayCommand builds the stream key as "app/streamName" and extracts
// optional fields (start, duration, reset) with defaults.
package rpc

import (
	"testing"

	"github.com/alxayo/go-rtmp/internal/rtmp/amf"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// buildPlayMessage wraps payload as a TypeID 20 command message.
func buildPlayMessage(payload []byte) *chunk.Message {
	return &chunk.Message{TypeID: 20, Payload: payload}
}

// TestParsePlayCommand_Valid encodes a full play command (all optional
// fields) and verifies streamKey, start, duration, and reset.
func TestParsePlayCommand_Valid(t *testing.T) {
	payload, err := amf.EncodeAll(
		"play",       // command
		0.0,          // transaction id (ignored)
		nil,          // null
		"testStream", // stream name
		-2.0,         // start (live)
		-1.0,         // duration (all)
		true,         // reset
	)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	cmd, err := ParsePlayCommand(buildPlayMessage(payload), "live")
	if err != nil {
		t.Fatalf("ParsePlayCommand error: %v", err)
	}

	if cmd.StreamName != "testStream" || cmd.StreamKey != "live/testStream" {
		t.Fatalf("unexpected stream fields: %+v", cmd)
	}
	if cmd.Start != -2 || cmd.Duration != -1 || !cmd.Reset {
		t.Fatalf("unexpected optional fields: %+v", cmd)
	}
	if len(cmd.QueryParams) != 0 {
		t.Fatalf("expected empty QueryParams, got %v", cmd.QueryParams)
	}
}

// TestParsePlayCommand_WithToken verifies that query parameters are
// parsed from the stream name and stripped from StreamName and StreamKey.
func TestParsePlayCommand_WithToken(t *testing.T) {
	payload, err := amf.EncodeAll(
		"play",
		0.0,
		nil,
		"testStream?token=secret123", // stream name with query params
	)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	cmd, err := ParsePlayCommand(buildPlayMessage(payload), "live")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if cmd.StreamName != "testStream" {
		t.Fatalf("expected clean StreamName 'testStream', got %q", cmd.StreamName)
	}
	if cmd.StreamKey != "live/testStream" {
		t.Fatalf("expected clean StreamKey 'live/testStream', got %q", cmd.StreamKey)
	}
	if cmd.QueryParams["token"] != "secret123" {
		t.Fatalf("expected token=secret123, got %q", cmd.QueryParams["token"])
	}
}

// TestParsePlayCommand_MissingStreamName omits the stream name and
// expects an error.
func TestParsePlayCommand_MissingStreamName(t *testing.T) {
	// Omit index 3 (stream name)
	payload, err := amf.EncodeAll(
		"play",
		0.0,
		nil,
	)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	if _, err := ParsePlayCommand(buildPlayMessage(payload), "live"); err == nil {
		// Must error because streamName required
		t.Fatalf("expected error for missing streamName")
	}
}

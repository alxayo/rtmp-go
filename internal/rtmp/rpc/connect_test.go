// connect_test.go – tests for parsing the RTMP "connect" command.
//
// The "connect" command is the first application-level message a client
// sends. It carries AMF0-encoded fields:
//
//	[0] "connect"  (string)      – command name
//	[1] 1.0        (number)      – transaction ID
//	[2] { app, flashVer, tcUrl, objectEncoding } (object) – connection properties
//
// ParseConnectCommand decodes this and validates:
//   - "app" field must be present.
//   - objectEncoding must be 0 (AMF0); AMF3 (3) is rejected.
package rpc

import (
	"testing"

	"github.com/alxayo/go-rtmp/internal/rtmp/amf"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// buildMessage wraps a raw payload in a *chunk.Message with TypeID 20
// (AMF0 command). Reused by several tests in this file.
func buildMessage(payload []byte) *chunk.Message {
	return &chunk.Message{TypeID: 20, Payload: payload}
}

// TestParseConnectCommand_Valid encodes a well-formed connect command
// with all required fields and verifies the parsed struct.
func TestParseConnectCommand_Valid(t *testing.T) {
	payload, err := amf.EncodeAll(
		"connect", // command name
		1.0,       // transaction ID
		map[string]interface{}{
			"app":            "live",
			"flashVer":       "LNX 9,0,124,2",
			"tcUrl":          "rtmp://localhost:1935/live",
			"objectEncoding": 0.0,
		},
	)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	cmd, err := ParseConnectCommand(buildMessage(payload))
	if err != nil {
		t.Fatalf("ParseConnectCommand returned error: %v", err)
	}

	if cmd.App != "live" || cmd.FlashVer == "" || cmd.TcURL == "" || cmd.ObjectEncoding != 0 {
		t.Fatalf("unexpected parsed fields: %+v", cmd)
	}
}

// TestParseConnectCommand_MissingApp omits the mandatory "app" field
// from the connect properties object and expects an error.
func TestParseConnectCommand_MissingApp(t *testing.T) {
	payload, err := amf.EncodeAll(
		"connect",
		1.0,
		map[string]interface{}{
			"flashVer":       "LNX 9,0,124,2",
			"tcUrl":          "rtmp://localhost:1935/live",
			"objectEncoding": 0.0,
		},
	)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	if _, err := ParseConnectCommand(buildMessage(payload)); err == nil {
		// Must error because app is mandatory
		t.Fatalf("expected error for missing app field")
	}
}

// TestParseConnectCommand_AMF3Rejected sets objectEncoding=3 (AMF3)
// which this server does not support – must return error.
func TestParseConnectCommand_AMF3Rejected(t *testing.T) {
	payload, err := amf.EncodeAll(
		"connect",
		1.0,
		map[string]interface{}{
			"app":            "live",
			"flashVer":       "LNX 9,0,124,2",
			"tcUrl":          "rtmp://localhost:1935/live",
			"objectEncoding": 3.0, // AMF3 (unsupported)
		},
	)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	if _, err := ParseConnectCommand(buildMessage(payload)); err == nil {
		// Must error because only objectEncoding 0 (AMF0) supported
		t.Fatalf("expected error for AMF3 objectEncoding")
	}
}

// TestParseConnectCommand_ExtraFields verifies that additional fields
// in the connect object (beyond the known ones) are captured in the
// Extra map. This is used to pass custom auth tokens or parameters.
func TestParseConnectCommand_ExtraFields(t *testing.T) {
	payload, err := amf.EncodeAll(
		"connect",
		1.0,
		map[string]interface{}{
			"app":            "live",
			"flashVer":       "LNX 9,0,124,2",
			"tcUrl":          "rtmp://localhost:1935/live",
			"objectEncoding": 0.0,
			"authToken":      "my_secret",
			"customField":    "hello",
		},
	)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	cmd, err := ParseConnectCommand(buildMessage(payload))
	if err != nil {
		t.Fatalf("ParseConnectCommand error: %v", err)
	}
	if cmd.Extra == nil {
		t.Fatal("expected Extra map to be populated")
	}
	if cmd.Extra["authToken"] != "my_secret" {
		t.Fatalf("expected authToken='my_secret', got %v", cmd.Extra["authToken"])
	}
	if cmd.Extra["customField"] != "hello" {
		t.Fatalf("expected customField='hello', got %v", cmd.Extra["customField"])
	}
	// Known fields should NOT appear in Extra
	if _, exists := cmd.Extra["app"]; exists {
		t.Fatal("known field 'app' should not be in Extra")
	}
}

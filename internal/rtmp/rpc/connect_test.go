package rpc

import (
	"testing"

	"github.com/alxayo/go-rtmp/internal/rtmp/amf"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

func buildMessage(payload []byte) *chunk.Message {
	return &chunk.Message{TypeID: 20, Payload: payload}
}

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

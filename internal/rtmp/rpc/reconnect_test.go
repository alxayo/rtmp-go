package rpc

// reconnect_test.go – tests for the E-RTMP v2 Reconnect Request builder.
//
// BuildReconnectRequest encodes an AMF0 onStatus command with 4 values:
//
//	[0] "onStatus"     (string)  – command name
//	[1] 0.0            (number)  – transaction ID (no reply expected)
//	[2] null           (null)    – no command object
//	[3] info           (object)  – {level, code, description, [tcUrl]}

import (
	"testing"

	"github.com/alxayo/go-rtmp/internal/rtmp/amf"
)

// TestBuildReconnectRequest_WithTcUrl verifies that a reconnect request with a
// redirect URL includes the tcUrl field in the info object.
func TestBuildReconnectRequest_WithTcUrl(t *testing.T) {
	msg, err := BuildReconnectRequest("rtmp://new-server:1935/live", "Server maintenance")
	if err != nil {
		t.Fatalf("BuildReconnectRequest error: %v", err)
	}

	// Verify the message envelope fields
	if msg.TypeID != commandMessageAMF0TypeID {
		t.Fatalf("expected TypeID %d, got %d", commandMessageAMF0TypeID, msg.TypeID)
	}
	if msg.CSID != 3 {
		t.Fatalf("expected CSID 3, got %d", msg.CSID)
	}
	if msg.MessageStreamID != 0 {
		t.Fatalf("expected MessageStreamID 0, got %d", msg.MessageStreamID)
	}
	if msg.MessageLength != uint32(len(msg.Payload)) {
		t.Fatalf("MessageLength %d != len(Payload) %d", msg.MessageLength, len(msg.Payload))
	}

	// Decode the AMF payload and verify the info object includes tcUrl
	vals, err := amf.DecodeAll(msg.Payload)
	if err != nil {
		t.Fatalf("amf decode error: %v", err)
	}
	if len(vals) != 4 {
		t.Fatalf("expected 4 AMF values, got %d", len(vals))
	}

	info, ok := vals[3].(map[string]interface{})
	if !ok {
		t.Fatalf("info value is not an object: %#v", vals[3])
	}
	if info["tcUrl"] != "rtmp://new-server:1935/live" {
		t.Fatalf("expected tcUrl 'rtmp://new-server:1935/live', got %#v", info["tcUrl"])
	}
}

// TestBuildReconnectRequest_WithoutTcUrl verifies that a reconnect request
// without a redirect URL omits the tcUrl field from the info object.
func TestBuildReconnectRequest_WithoutTcUrl(t *testing.T) {
	msg, err := BuildReconnectRequest("", "Routine maintenance")
	if err != nil {
		t.Fatalf("BuildReconnectRequest error: %v", err)
	}

	// Verify message envelope
	if msg.TypeID != commandMessageAMF0TypeID {
		t.Fatalf("expected TypeID %d, got %d", commandMessageAMF0TypeID, msg.TypeID)
	}

	// Decode and verify tcUrl is absent
	vals, err := amf.DecodeAll(msg.Payload)
	if err != nil {
		t.Fatalf("amf decode error: %v", err)
	}
	if len(vals) != 4 {
		t.Fatalf("expected 4 AMF values, got %d", len(vals))
	}

	info, ok := vals[3].(map[string]interface{})
	if !ok {
		t.Fatalf("info value is not an object: %#v", vals[3])
	}

	// tcUrl should NOT be present when no redirect is specified
	if _, exists := info["tcUrl"]; exists {
		t.Fatalf("tcUrl should be absent when empty, but found: %#v", info["tcUrl"])
	}

	// description should still be present
	if info["description"] != "Routine maintenance" {
		t.Fatalf("expected description 'Routine maintenance', got %#v", info["description"])
	}
}

// TestBuildReconnectRequest_DecodeVerify builds a reconnect request and then
// decodes the full AMF payload, verifying every field matches the expected
// onStatus structure: ["onStatus", 0.0, null, {level, code, description, tcUrl}].
func TestBuildReconnectRequest_DecodeVerify(t *testing.T) {
	tcUrl := "rtmp://backup-server:1935/live"
	desc := "Load balancing — redirecting to backup server"

	msg, err := BuildReconnectRequest(tcUrl, desc)
	if err != nil {
		t.Fatalf("BuildReconnectRequest error: %v", err)
	}

	// Decode the complete AMF payload
	vals, err := amf.DecodeAll(msg.Payload)
	if err != nil {
		t.Fatalf("amf decode error: %v", err)
	}

	// Verify exactly 4 AMF values: command name, txn ID, null, info object
	if len(vals) != 4 {
		t.Fatalf("expected 4 AMF values, got %d", len(vals))
	}

	// [0] Command name must be "onStatus"
	cmdName, ok := vals[0].(string)
	if !ok || cmdName != "onStatus" {
		t.Fatalf("expected command name 'onStatus', got %#v", vals[0])
	}

	// [1] Transaction ID must be 0.0 (no response expected)
	txnID, ok := vals[1].(float64)
	if !ok || txnID != 0.0 {
		t.Fatalf("expected transaction ID 0.0, got %#v", vals[1])
	}

	// [2] Command object must be null
	if vals[2] != nil {
		t.Fatalf("expected null command object, got %#v", vals[2])
	}

	// [3] Info object — verify all fields
	info, ok := vals[3].(map[string]interface{})
	if !ok {
		t.Fatalf("info value is not an object: %#v", vals[3])
	}

	// Check required fields
	if info["level"] != "status" {
		t.Fatalf("expected level 'status', got %#v", info["level"])
	}
	if info["code"] != "NetConnection.Connect.ReconnectRequest" {
		t.Fatalf("expected code 'NetConnection.Connect.ReconnectRequest', got %#v", info["code"])
	}
	if info["description"] != desc {
		t.Fatalf("expected description %q, got %#v", desc, info["description"])
	}
	if info["tcUrl"] != tcUrl {
		t.Fatalf("expected tcUrl %q, got %#v", tcUrl, info["tcUrl"])
	}
}

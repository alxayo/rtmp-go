// connect_response_test.go – tests for building the RTMP "_result" response
// to a "connect" command.
//
// BuildConnectResponse encodes an AMF0 response with 4 values:
//
//	[0] "_result"       (string)  – response name
//	[1] transactionID   (number)  – matches the request
//	[2] properties      (object)  – server capabilities (fmsVer, capabilities, mode)
//	[3] information     (object)  – status info (level, code, description)
package rpc

import (
	"testing"

	"github.com/alxayo/go-rtmp/internal/rtmp/amf"
)

// TestBuildConnectResponse_EncodesStructure builds a connect response and
// decodes it back, verifying all 4 AMF values and key fields.
func TestBuildConnectResponse_EncodesStructure(t *testing.T) {
	msg, err := BuildConnectResponse(1.0, "Connection succeeded.")
	if err != nil {
		ttFatal(t, "BuildConnectResponse error: %v", err)
	}
	if msg.TypeID != commandMessageAMF0TypeID {
		ttFatal(t, "unexpected TypeID %d", msg.TypeID)
	}

	vals, err := amf.DecodeAll(msg.Payload)
	if err != nil {
		ttFatal(t, "decode: %v", err)
	}
	if len(vals) != 4 {
		ttFatal(t, "expected 4 AMF values, got %d", len(vals))
	}
	if name, ok := vals[0].(string); !ok || name != "_result" {
		ttFatal(t, "first value not _result: %#v", vals[0])
	}
	if trx, ok := vals[1].(float64); !ok || trx != 1.0 {
		ttFatal(t, "transaction id mismatch: %#v", vals[1])
	}
	props, ok := vals[2].(map[string]interface{})
	if !ok {
		ttFatal(t, "properties not object: %#v", vals[2])
	}
	info, ok := vals[3].(map[string]interface{})
	if !ok {
		ttFatal(t, "info not object: %#v", vals[3])
	}

	// Validate properties subset
	if props["fmsVer"] == "" || props["capabilities"] == nil || props["mode"] == nil {
		ttFatal(t, "missing properties fields: %#v", props)
	}
	// Validate info subset
	if info["level"] != "status" || info["code"] != "NetConnection.Connect.Success" {
		ttFatal(t, "info core fields unexpected: %#v", info)
	}
	if _, ok := info["description"]; !ok {
		ttFatal(t, "missing description field")
	}
}

// ttFatal is a local test helper for concise failure messages with
// accurate line numbers via t.Helper().
func ttFatal(t *testing.T, format string, args ...interface{}) {
	// Mirror style used in other tests: mark helper for accurate line numbers.
	// (Using t.Helper inside this wrapper is fine.)
	// We avoid pulling in shared helpers to keep test self-contained per task instructions.
	// This also assists with achieving high coverage for this file specifically.
	t.Helper()
	t.Fatalf(format, args...)
}

package srt

import (
	"testing"
)

// TestParseStreamIDStructured verifies parsing of the structured "#!::" format
// with basic resource and mode fields.
func TestParseStreamIDStructured(t *testing.T) {
	info := ParseStreamID("#!::r=live/test,m=publish")

	if info.Resource != "live/test" {
		t.Errorf("Resource: got %q, want %q", info.Resource, "live/test")
	}
	if info.Mode != "publish" {
		t.Errorf("Mode: got %q, want %q", info.Mode, "publish")
	}
}

// TestParseStreamIDStructuredAllFields verifies parsing of a structured
// stream ID with all possible fields populated.
func TestParseStreamIDStructuredAllFields(t *testing.T) {
	info := ParseStreamID("#!::r=live/test,m=publish,u=user1,s=sess1,h=host1,t=stream")

	if info.Resource != "live/test" {
		t.Errorf("Resource: got %q, want %q", info.Resource, "live/test")
	}
	if info.Mode != "publish" {
		t.Errorf("Mode: got %q, want %q", info.Mode, "publish")
	}
	if info.User != "user1" {
		t.Errorf("User: got %q, want %q", info.User, "user1")
	}
	if info.Session != "sess1" {
		t.Errorf("Session: got %q, want %q", info.Session, "sess1")
	}
	if info.Host != "host1" {
		t.Errorf("Host: got %q, want %q", info.Host, "host1")
	}
	if info.Type != "stream" {
		t.Errorf("Type: got %q, want %q", info.Type, "stream")
	}
	if info.Raw != "#!::r=live/test,m=publish,u=user1,s=sess1,h=host1,t=stream" {
		t.Errorf("Raw: got %q, want original string", info.Raw)
	}
}

// TestParseStreamIDSimple verifies parsing of a simple stream ID where
// the entire string is treated as the resource with default mode "request".
func TestParseStreamIDSimple(t *testing.T) {
	info := ParseStreamID("live/test")

	if info.Resource != "live/test" {
		t.Errorf("Resource: got %q, want %q", info.Resource, "live/test")
	}
	if info.Mode != "request" {
		t.Errorf("Mode: got %q, want %q (default)", info.Mode, "request")
	}
}

// TestParseStreamIDPrefixed verifies parsing of the "mode:resource" prefix
// convention commonly used by SRT clients.
func TestParseStreamIDPrefixed(t *testing.T) {
	info := ParseStreamID("publish:live/test")

	if info.Resource != "live/test" {
		t.Errorf("Resource: got %q, want %q", info.Resource, "live/test")
	}
	if info.Mode != "publish" {
		t.Errorf("Mode: got %q, want %q", info.Mode, "publish")
	}
}

// TestParseStreamIDPrefixedRequest verifies the "request:resource" prefix.
func TestParseStreamIDPrefixedRequest(t *testing.T) {
	info := ParseStreamID("request:live/test")

	if info.Resource != "live/test" {
		t.Errorf("Resource: got %q, want %q", info.Resource, "live/test")
	}
	if info.Mode != "request" {
		t.Errorf("Mode: got %q, want %q", info.Mode, "request")
	}
}

// TestParseStreamIDEmpty verifies that an empty stream ID returns sensible
// defaults: empty resource and mode "request".
func TestParseStreamIDEmpty(t *testing.T) {
	info := ParseStreamID("")

	if info.Resource != "" {
		t.Errorf("Resource: got %q, want %q", info.Resource, "")
	}
	if info.Mode != "request" {
		t.Errorf("Mode: got %q, want %q (default)", info.Mode, "request")
	}
}

// TestStreamKeyStripsLeadingSlash verifies that StreamKey() removes any
// leading "/" from the resource for RTMP compatibility.
func TestStreamKeyStripsLeadingSlash(t *testing.T) {
	info := ParseStreamID("/live/test")

	if info.StreamKey() != "live/test" {
		t.Errorf("StreamKey: got %q, want %q", info.StreamKey(), "live/test")
	}
}

// TestStreamKeyDefault verifies that StreamKey() returns "live/default"
// when the resource is empty, providing a sensible fallback.
func TestStreamKeyDefault(t *testing.T) {
	info := ParseStreamID("")

	if info.StreamKey() != "live/default" {
		t.Errorf("StreamKey: got %q, want %q", info.StreamKey(), "live/default")
	}
}

// TestStreamKeyFromResource verifies that StreamKey() returns the resource
// as-is when it doesn't start with "/".
func TestStreamKeyFromResource(t *testing.T) {
	info := ParseStreamID("live/mystream")

	if info.StreamKey() != "live/mystream" {
		t.Errorf("StreamKey: got %q, want %q", info.StreamKey(), "live/mystream")
	}
}

// TestIsPublish verifies that IsPublish() returns true for "publish" mode
// and false for all other modes.
func TestIsPublish(t *testing.T) {
	// publish mode should return true.
	publishInfo := ParseStreamID("#!::r=live/test,m=publish")
	if !publishInfo.IsPublish() {
		t.Error("IsPublish: got false for mode=publish, want true")
	}

	// request mode should return false.
	requestInfo := ParseStreamID("#!::r=live/test,m=request")
	if requestInfo.IsPublish() {
		t.Error("IsPublish: got true for mode=request, want false")
	}

	// default mode (no explicit mode) should return false.
	defaultInfo := ParseStreamID("live/test")
	if defaultInfo.IsPublish() {
		t.Error("IsPublish: got true for default mode, want false")
	}
}

// TestParseStreamIDUnknownPrefix verifies that a colon-separated string
// with an unknown prefix (not "publish" or "request") is treated as a
// simple resource name, not a mode:resource pair.
func TestParseStreamIDUnknownPrefix(t *testing.T) {
	// "rtmp:live/test" should treat the entire string as the resource,
	// since "rtmp" is not a recognized mode.
	info := ParseStreamID("rtmp:live/test")

	if info.Resource != "rtmp:live/test" {
		t.Errorf("Resource: got %q, want %q", info.Resource, "rtmp:live/test")
	}
	if info.Mode != "request" {
		t.Errorf("Mode: got %q, want %q (default)", info.Mode, "request")
	}
}

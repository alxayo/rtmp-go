package srt

// This file implements the SRT Access Control Stream ID parser.
//
// SRT uses a "Stream ID" to identify what resource a caller wants to
// access and what it wants to do (publish or subscribe). This is similar
// to an RTMP stream key, but SRT supports a structured format with
// key-value pairs for richer access control.
//
// Two formats are supported:
//
//   1. Structured format: "#!::r=live/mystream,m=publish,u=user1"
//      - Starts with "#!::" prefix
//      - Key-value pairs separated by commas
//      - Standard keys: r (resource), m (mode), u (user), s (session),
//        h (host), t (type)
//
//   2. Simple format: "live/mystream" or "publish:live/mystream"
//      - The entire string is treated as the resource, OR
//      - A "mode:resource" prefix convention is used
//
// Reference: SRT Access Control specification (SRT Alliance)

import "strings"

// StreamIDInfo holds parsed SRT Access Control fields extracted from
// a Stream ID string. The Stream ID is sent by the caller during the
// SRT handshake and tells the server what the caller wants to do.
type StreamIDInfo struct {
	// Resource is the stream path, similar to an RTMP stream key.
	// For example: "live/mystream".
	Resource string

	// Mode indicates the caller's intent: "publish" to send media,
	// "request" to receive media (subscribe). Defaults to "request".
	Mode string

	// Session is an optional session identifier for grouping related
	// connections (e.g., for reconnection support).
	Session string

	// User is an optional username for authentication.
	User string

	// Host is an optional hostname field.
	Host string

	// Type describes the content type: "stream" (live media),
	// "file" (file transfer), or "auth" (authentication-only).
	Type string

	// Raw is the original unparsed stream ID string, preserved for
	// logging and debugging.
	Raw string
}

// structuredPrefix is the prefix that identifies a structured Stream ID.
// If a stream ID starts with this, it uses the key-value pair format.
const structuredPrefix = "#!::"

// ParseStreamID parses an SRT stream ID string into its component fields.
//
// It handles three formats:
//   1. Structured: "#!::r=live/mystream,m=publish" → key-value pairs
//   2. Prefixed:   "publish:live/mystream" → mode and resource extracted
//   3. Simple:     "live/mystream" → entire string is the resource
//
// When no mode is specified, it defaults to "request" (subscribe).
func ParseStreamID(raw string) StreamIDInfo {
	info := StreamIDInfo{
		Raw:  raw,
		Mode: "request", // Default mode is subscribe ("request")
	}

	// Handle empty string — return defaults.
	if raw == "" {
		return info
	}

	// Check if this uses the structured "#!::" format.
	if strings.HasPrefix(raw, structuredPrefix) {
		// Strip the prefix and parse the key-value pairs.
		kvString := raw[len(structuredPrefix):]
		parseStructuredStreamID(kvString, &info)
		return info
	}

	// Check for the "mode:resource" prefix convention.
	// Common in practice: "publish:live/test" or "request:live/test".
	if idx := strings.Index(raw, ":"); idx >= 0 {
		prefix := raw[:idx]
		// Only treat it as a mode prefix if it's a known mode value.
		if prefix == "publish" || prefix == "request" {
			info.Mode = prefix
			info.Resource = raw[idx+1:]
			return info
		}
	}

	// Simple format: the entire string is the resource.
	info.Resource = raw
	return info
}

// parseStructuredStreamID parses the key-value portion of a structured
// stream ID (after stripping the "#!::" prefix).
//
// Format: "r=live/test,m=publish,u=user1"
// Each key is a single letter, and pairs are separated by commas.
func parseStructuredStreamID(kvString string, info *StreamIDInfo) {
	// Split by comma to get individual key=value pairs.
	pairs := strings.Split(kvString, ",")

	for _, pair := range pairs {
		// Split each pair by "=" to get the key and value.
		eqIdx := strings.Index(pair, "=")
		if eqIdx < 0 {
			// No "=" found — skip this malformed pair.
			continue
		}

		key := pair[:eqIdx]
		value := pair[eqIdx+1:]

		// Map each single-letter key to the corresponding field.
		switch key {
		case "r":
			info.Resource = value
		case "m":
			info.Mode = value
		case "s":
			info.Session = value
		case "u":
			info.User = value
		case "h":
			info.Host = value
		case "t":
			info.Type = value
		}
		// Unknown keys are silently ignored for forward compatibility.
	}
}

// StreamKey returns the RTMP-style stream key derived from the resource.
// It strips any leading "/" for consistency (RTMP keys don't start with "/").
// If the resource is empty, it returns "live/default" as a sensible fallback.
func (s StreamIDInfo) StreamKey() string {
	r := s.Resource

	// Strip leading slash for consistency with RTMP stream keys.
	r = strings.TrimPrefix(r, "/")

	// If the resource is empty after trimming, return a sensible default.
	if r == "" {
		return "live/default"
	}

	return r
}

// IsPublish returns true if the caller wants to publish (send) media.
// This is determined by the mode field being set to "publish".
func (s StreamIDInfo) IsPublish() bool {
	return s.Mode == "publish"
}

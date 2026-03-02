// Package logger – tests for the structured JSON logger.
//
// The logger wraps Go's log/slog package to produce machine-readable JSON
// lines. Each test redirects output to a bytes.Buffer, writes log messages,
// and then parses the JSON to verify fields and filtering.
//
// Key Go concepts demonstrated:
//   - bytes.Buffer as an in-memory io.Writer for capturing log output.
//   - encoding/json.Unmarshal for parsing JSON log lines into maps.
//   - Helper functions with t.Helper() so failures report the caller’s line.
package logger

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// decodeLines is a test helper that parses newline-delimited JSON from a
// buffer into a slice of maps. Each log message is one JSON object per line.
//
// t.Helper() tells Go's test framework to report failures at the caller's
// line number rather than inside this helper, making failures easier to find.
func decodeLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	s := bufio.NewScanner(buf)
	var out []map[string]any
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			// Provide context for debugging
			t.Fatalf("invalid JSON line: %s err=%v", line, err)
		}
		out = append(out, m)
	}
	if err := s.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}
	return out
}

// TestLogLevelFiltering verifies that log messages below the configured level
// are suppressed. At INFO level, Debug() calls should produce zero output.
// After switching to DEBUG level, Debug() calls should appear.
//
// This is important for production RTMP servers where debug logging would be
// too noisy – operators set the level to "info" or "warn".
func TestLogLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	UseWriter(&buf) // redirect log output to our buffer
	if err := SetLevel("info"); err != nil {
		t.Fatalf("SetLevel: %v", err)
	}

	Debug("debug message should be filtered") // below INFO → dropped
	Info("info message", "k", 1)              // at INFO → kept

	records := decodeLines(t, &buf)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0]["msg"].(string) != "info message" {
		t.Fatalf("unexpected message: %+v", records[0])
	}

	// Switch to DEBUG level and verify debug messages now appear.
	buf.Reset()
	if err := SetLevel("debug"); err != nil {
		t.Fatalf("SetLevel: %v", err)
	}
	Debug("visible debug", "a", 2)
	records = decodeLines(t, &buf)
	if len(records) != 1 {
		t.Fatalf("expected 1 record after debug, got %d", len(records))
	}
	if lvl, ok := records[0]["level"].(string); !ok || lvl != "DEBUG" {
		t.Fatalf("expected DEBUG level, got %v", records[0]["level"])
	}
}

// TestFieldExtraction validates the structured logging enrichment helpers:
// WithConn, WithStream, and WithMessageMeta. These add RTMP-specific context
// fields (conn_id, peer_addr, stream_key, msg_type, csid, msid, timestamp)
// to every log line, which is critical for debugging multi-connection servers.
//
// The test builds a fully-enriched logger and checks that all required fields
// are present in the JSON output and have the correct values.
func TestFieldExtraction(t *testing.T) {
	var buf bytes.Buffer
	UseWriter(&buf)
	if err := SetLevel("debug"); err != nil {
		t.Fatalf("SetLevel: %v", err)
	}

	// Chain all enrichment helpers to build a logger with full RTMP context.
	l := WithMessageMeta(WithStream(WithConn(Logger(), "c1", "127.0.0.1:1234"), "live/test"), "command", 4, 0, 12345)
	l.Info("hello world", "extra", 42)

	records := decodeLines(t, &buf)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	rec := records[0]
	// Verify every structured field is present in the JSON output.
	required := []string{"conn_id", "peer_addr", "stream_key", "msg_type", "csid", "msid", "timestamp"}
	for _, k := range required {
		if _, ok := rec[k]; !ok {
			t.Fatalf("missing field %s in record: %+v", k, rec)
		}
	}
	if rec["conn_id"].(string) != "c1" {
		t.Fatalf("conn_id mismatch: %v", rec["conn_id"])
	}
	if rec["stream_key"].(string) != "live/test" {
		t.Fatalf("stream_key mismatch: %v", rec["stream_key"])
	}
	if rec["msg_type"].(string) != "command" {
		t.Fatalf("msg_type mismatch: %v", rec["msg_type"])
	}
}

// TestParseLevel is a table-driven test that verifies all supported log
// level strings ("debug", "info", "warn", "error") are accepted by
// SetLevel, and that an invalid string returns an error.
//
// The map iteration order in Go is random, which is fine here – each entry
// is independent.
func TestParseLevel(t *testing.T) {
	cases := map[string]string{
		"debug": "DEBUG",
		"info":  "INFO",
		"warn":  "WARN",
		"error": "ERROR",
	}
	for in, expect := range cases {
		if err := SetLevel(in); err != nil {
			t.Fatalf("SetLevel(%s): %v", in, err)
		}
		if got := strings.ToUpper(Level()); !strings.Contains(got, expect) { // slog returns e.g. "INFO"
			t.Fatalf("expected %s got %s", expect, got)
		}
	}
	// Invalid level must return an error, not silently succeed.
	if err := SetLevel("bogus"); err == nil {
		t.Fatalf("expected error for invalid level")
	}
}

package logger

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// helper to read all JSON objects from buffer
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

func TestLogLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	UseWriter(&buf)
	if err := SetLevel("info"); err != nil {
		t.Fatalf("SetLevel: %v", err)
	}

	Debug("debug message should be filtered")
	Info("info message", "k", 1)

	records := decodeLines(t, &buf)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0]["msg"].(string) != "info message" {
		t.Fatalf("unexpected message: %+v", records[0])
	}

	// Enable debug and ensure it appears
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

func TestFieldExtraction(t *testing.T) {
	var buf bytes.Buffer
	UseWriter(&buf)
	if err := SetLevel("debug"); err != nil {
		t.Fatalf("SetLevel: %v", err)
	}

	l := WithMessageMeta(WithStream(WithConn(Logger(), "c1", "127.0.0.1:1234"), "live/test"), "command", 4, 0, 12345)
	l.Info("hello world", "extra", 42)

	records := decodeLines(t, &buf)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	rec := records[0]
	// Validate required structured fields
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
	if err := SetLevel("bogus"); err == nil {
		t.Fatalf("expected error for invalid level")
	}
}

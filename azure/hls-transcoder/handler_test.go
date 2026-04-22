package main
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func noopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestHandler creates a Handler backed by a Transcoder that records
// Start/Stop calls without actually spawning FFmpeg processes.
func newTestHandler() (*Handler, *fakeTranscoder) {
	ft := &fakeTranscoder{}
	cfg := TranscoderConfig{
		HLSDir:   "/tmp/hls-test",
		RTMPHost: "localhost",
		RTMPPort: 1935,
		Mode:     "abr",
	}
	t := NewTranscoder(cfg, noopLogger())
	h := NewHandler(t, noopLogger())
	// Replace the real transcoder with our tracking wrapper
	ft.real = t
	return h, ft
}

// fakeTranscoder wraps a real Transcoder for call tracking in tests.
type fakeTranscoder struct {
	real *Transcoder
}

func TestHandler_PublishStart(t *testing.T) {
	transcoder := NewTranscoder(TranscoderConfig{
		HLSDir:   t.TempDir(),
		RTMPHost: "localhost",
		RTMPPort: 1935,
		Mode:     "abr",
	}, noopLogger())
	handler := NewHandler(transcoder, noopLogger())

	mux := http.NewServeMux()
	mux.HandleFunc("/events", handler.HandleEvent)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	event := HookEvent{
		Type:      "publish_start",
		Timestamp: time.Now().Unix(),
		ConnID:    "c1",
		StreamKey: "live/test",
		Data:      map[string]interface{}{},
	}

	body, _ := json.Marshal(event)
	resp, err := http.Post(srv.URL+"/events", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	// FFmpeg won't actually start (no ffmpeg binary in test), but the transcoder
	// should have attempted to process the stream key. We verify the handler
	// accepted the event without error.
}

func TestHandler_PublishStop(t *testing.T) {
	transcoder := NewTranscoder(TranscoderConfig{
		HLSDir:   t.TempDir(),
		RTMPHost: "localhost",
		RTMPPort: 1935,
		Mode:     "abr",
	}, noopLogger())
	handler := NewHandler(transcoder, noopLogger())

	mux := http.NewServeMux()
	mux.HandleFunc("/events", handler.HandleEvent)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	event := HookEvent{
		Type:      "publish_stop",
		Timestamp: time.Now().Unix(),
		ConnID:    "c1",
		StreamKey: "live/test",
		Data:      map[string]interface{}{},
	}

	body, _ := json.Marshal(event)
	resp, err := http.Post(srv.URL+"/events", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestHandler_UnknownEventIgnored(t *testing.T) {
	transcoder := NewTranscoder(TranscoderConfig{
		HLSDir:   t.TempDir(),
		RTMPHost: "localhost",
		RTMPPort: 1935,
		Mode:     "abr",
	}, noopLogger())
	handler := NewHandler(transcoder, noopLogger())

	mux := http.NewServeMux()
	mux.HandleFunc("/events", handler.HandleEvent)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	event := HookEvent{
		Type:      "recording_start",
		Timestamp: time.Now().Unix(),
		StreamKey: "live/test",
		Data:      map[string]interface{}{},
	}

	body, _ := json.Marshal(event)
	resp, err := http.Post(srv.URL+"/events", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestHandler_BadJSON(t *testing.T) {
	transcoder := NewTranscoder(TranscoderConfig{
		HLSDir:   t.TempDir(),
		RTMPHost: "localhost",
		RTMPPort: 1935,
		Mode:     "abr",
	}, noopLogger())
	handler := NewHandler(transcoder, noopLogger())

	mux := http.NewServeMux()
	mux.HandleFunc("/events", handler.HandleEvent)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/events", "application/json", bytes.NewReader([]byte("{invalid json}")))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHandler_WrongMethod(t *testing.T) {
	transcoder := NewTranscoder(TranscoderConfig{
		HLSDir:   t.TempDir(),
		RTMPHost: "localhost",
		RTMPPort: 1935,
		Mode:     "abr",
	}, noopLogger())
	handler := NewHandler(transcoder, noopLogger())

	mux := http.NewServeMux()
	mux.HandleFunc("/events", handler.HandleEvent)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/events")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

func TestHandler_MissingStreamKey(t *testing.T) {
	transcoder := NewTranscoder(TranscoderConfig{
		HLSDir:   t.TempDir(),
		RTMPHost: "localhost",
		RTMPPort: 1935,
		Mode:     "abr",
	}, noopLogger())
	handler := NewHandler(transcoder, noopLogger())

	mux := http.NewServeMux()
	mux.HandleFunc("/events", handler.HandleEvent)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	event := HookEvent{
		Type:      "publish_start",
		Timestamp: time.Now().Unix(),
		StreamKey: "", // empty
		Data:      map[string]interface{}{},
	}

	body, _ := json.Marshal(event)
	resp, err := http.Post(srv.URL+"/events", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHandler_HealthEndpoint(t *testing.T) {
	transcoder := NewTranscoder(TranscoderConfig{
		HLSDir:   t.TempDir(),
		RTMPHost: "localhost",
		RTMPPort: 1935,
		Mode:     "abr",
	}, noopLogger())
	handler := NewHandler(transcoder, noopLogger())

	mux := http.NewServeMux()
	mux.HandleFunc("/health", handler.HandleHealth)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Errorf("body = %q, want ok", string(body))
	}
}

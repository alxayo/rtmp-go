package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWebhookListener_SegmentComplete(t *testing.T) {
	var received []HookEvent
	listener := NewWebhookListener(":0", noopLogger(), func(event HookEvent) {
		received = append(received, event)
	})

	// Use httptest to test the handler directly
	mux := http.NewServeMux()
	mux.HandleFunc("/events", listener.handleEvent)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	event := HookEvent{
		Type:      "segment_complete",
		Timestamp: time.Now().Unix(),
		ConnID:    "c1",
		StreamKey: "live/stream1",
		Data: map[string]interface{}{
			"path":          "/recordings/live_stream1_seg001.flv",
			"size":          float64(1024),
			"segment_index": float64(1),
			"duration_ms":   float64(180000),
		},
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

	if len(received) != 1 {
		t.Fatalf("expected 1 event, got %d", len(received))
	}
	if received[0].StreamKey != "live/stream1" {
		t.Errorf("stream_key = %q, want live/stream1", received[0].StreamKey)
	}
	if path, _ := received[0].Data["path"].(string); path != "/recordings/live_stream1_seg001.flv" {
		t.Errorf("path = %q, want /recordings/live_stream1_seg001.flv", path)
	}
}

func TestWebhookListener_RecordingStartNoUpload(t *testing.T) {
	var received []HookEvent
	listener := NewWebhookListener(":0", noopLogger(), func(event HookEvent) {
		received = append(received, event)
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/events", listener.handleEvent)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	event := HookEvent{
		Type:      "recording_start",
		Timestamp: time.Now().Unix(),
		StreamKey: "live/stream1",
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

	// recording_start should NOT trigger onSegment callback
	if len(received) != 0 {
		t.Errorf("expected 0 segment events, got %d", len(received))
	}
}

func TestWebhookListener_BadJSON(t *testing.T) {
	listener := NewWebhookListener(":0", noopLogger(), func(event HookEvent) {
		t.Error("callback should not be called for bad JSON")
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/events", listener.handleEvent)
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

func TestWebhookListener_WrongMethod(t *testing.T) {
	listener := NewWebhookListener(":0", noopLogger(), func(event HookEvent) {
		t.Error("callback should not be called for GET")
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/events", listener.handleEvent)
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

func TestWebhookListener_HealthEndpoint(t *testing.T) {
	listener := NewWebhookListener(":0", noopLogger(), func(event HookEvent) {})

	mux := http.NewServeMux()
	mux.HandleFunc("/health", listener.handleHealth)
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

func TestWebhookListener_RunAndShutdown(t *testing.T) {
	listener := NewWebhookListener("127.0.0.1:0", slog.New(slog.NewTextHandler(io.Discard, nil)), func(event HookEvent) {})

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- listener.Run(ctx)
	}()

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context should trigger shutdown
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}

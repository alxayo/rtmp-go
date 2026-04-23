package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestBuildEventStreamKey(t *testing.T) {
	tests := []struct {
		name      string
		safeKey   string
		outputDir string
		filePath  string
		want      string
	}{
		{
			name:      "root file (master playlist)",
			safeKey:   "live_stream1",
			outputDir: "/hls-output/live_stream1",
			filePath:  "/hls-output/live_stream1/master.m3u8",
			want:      "hls/live_stream1",
		},
		{
			name:      "rendition segment",
			safeKey:   "live_stream1",
			outputDir: "/hls-output/live_stream1",
			filePath:  "/hls-output/live_stream1/stream_0/seg_00001.ts",
			want:      "hls/live_stream1/stream_0",
		},
		{
			name:      "rendition playlist",
			safeKey:   "live_stream1",
			outputDir: "/hls-output/live_stream1",
			filePath:  "/hls-output/live_stream1/stream_2/index.m3u8",
			want:      "hls/live_stream1/stream_2",
		},
		{
			name:      "copy mode segment (no subdirectory)",
			safeKey:   "live_test",
			outputDir: "/hls-output/live_test",
			filePath:  "/hls-output/live_test/seg_00005.ts",
			want:      "hls/live_test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildEventStreamKey(tt.safeKey, tt.outputDir, tt.filePath)
			if got != tt.want {
				t.Errorf("buildEventStreamKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSegmentNotifier_Enabled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("enabled with URL", func(t *testing.T) {
		n := NewSegmentNotifier("http://localhost:8080/events", logger)
		if !n.Enabled() {
			t.Error("expected notifier to be enabled with URL")
		}
	})

	t.Run("disabled without URL", func(t *testing.T) {
		n := NewSegmentNotifier("", logger)
		if n.Enabled() {
			t.Error("expected notifier to be disabled without URL")
		}
	})
}

func TestSegmentNotifier_ScanDir(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Set up a mock webhook server that collects events
	var mu sync.Mutex
	var events []map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var event map[string]interface{}
		json.Unmarshal(body, &event)
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	n := NewSegmentNotifier(server.URL, logger)

	// Create a temp directory with HLS files
	dir := t.TempDir()
	streamDir := filepath.Join(dir, "stream_0")
	os.MkdirAll(streamDir, 0o755)

	// Write some test files (segments must be >= 1024 bytes to pass size gate)
	segData := make([]byte, 2048)
	os.WriteFile(filepath.Join(dir, "master.m3u8"), []byte("#EXTM3U\n"), 0o644)
	os.WriteFile(filepath.Join(streamDir, "index.m3u8"), []byte("#EXTINF:2\n"), 0o644)
	os.WriteFile(filepath.Join(streamDir, "seg_00001.ts"), segData, 0o644)
	os.WriteFile(filepath.Join(streamDir, "seg_00002.ts"), segData, 0o644)

	// First scan — should detect 2 playlists immediately but only record .ts sizes (not notify)
	seen := make(map[string]int64)
	playlistMods := make(map[string]time.Time)
	n.scanDir(t.Context(), "live_test", dir, seen, playlistMods)

	mu.Lock()
	count := len(events)
	mu.Unlock()

	if count != 2 {
		t.Fatalf("expected 2 events after first scan (playlists only), got %d", count)
	}

	// Second scan — .ts sizes stable, should now notify for 2 segments
	n.scanDir(t.Context(), "live_test", dir, seen, playlistMods)

	mu.Lock()
	count = len(events)
	mu.Unlock()

	if count != 4 {
		t.Fatalf("expected 4 events after second scan (2 playlists + 2 segments), got %d", count)
	}

	// Third scan with no changes — should fire 0 new events
	n.scanDir(t.Context(), "live_test", dir, seen, playlistMods)

	mu.Lock()
	count = len(events)
	mu.Unlock()

	if count != 4 {
		t.Fatalf("expected still 4 events after third scan (no changes), got %d", count)
	}

	// Update a playlist — should fire 1 event for the modified playlist
	time.Sleep(10 * time.Millisecond) // ensure different mod time
	os.WriteFile(filepath.Join(streamDir, "index.m3u8"), []byte("#EXTINF:2\nseg_00003.ts\n"), 0o644)

	n.scanDir(t.Context(), "live_test", dir, seen, playlistMods)

	mu.Lock()
	count = len(events)
	mu.Unlock()

	if count != 5 {
		t.Fatalf("expected 5 events after playlist update, got %d", count)
	}

	// Add a new segment — first scan records size, second scan notifies
	os.WriteFile(filepath.Join(streamDir, "seg_00003.ts"), segData, 0o644)

	n.scanDir(t.Context(), "live_test", dir, seen, playlistMods)

	mu.Lock()
	count = len(events)
	mu.Unlock()

	if count != 5 {
		t.Fatalf("expected 5 events after first scan of new segment (size recorded, no notify), got %d", count)
	}

	n.scanDir(t.Context(), "live_test", dir, seen, playlistMods)

	mu.Lock()
	count = len(events)
	mu.Unlock()

	if count != 6 {
		t.Fatalf("expected 6 events after second scan of new segment (stable), got %d", count)
	}
}

func TestSegmentNotifier_EventFormat(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	var receivedEvent map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("expected Content-Type: application/json")
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedEvent)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	n := NewSegmentNotifier(server.URL, logger)

	dir := t.TempDir()
	streamDir := filepath.Join(dir, "stream_0")
	os.MkdirAll(streamDir, 0o755)
	segFile := filepath.Join(streamDir, "seg_00001.ts")
	segData := make([]byte, 2048)
	os.WriteFile(segFile, segData, 0o644)

	seen := make(map[string]int64)
	playlistMods := make(map[string]time.Time)

	// First scan records size, second scan triggers notification
	n.scanDir(t.Context(), "live_test", dir, seen, playlistMods)
	n.scanDir(t.Context(), "live_test", dir, seen, playlistMods)

	// Verify event structure matches blob-sidecar expectations
	if receivedEvent["type"] != "segment_complete" {
		t.Errorf("expected type=segment_complete, got %v", receivedEvent["type"])
	}
	if receivedEvent["conn_id"] != "hls-transcoder" {
		t.Errorf("expected conn_id=hls-transcoder, got %v", receivedEvent["conn_id"])
	}
	if receivedEvent["stream_key"] != "hls/live_test/stream_0" {
		t.Errorf("expected stream_key=hls/live_test/stream_0, got %v", receivedEvent["stream_key"])
	}

	data, ok := receivedEvent["data"].(map[string]interface{})
	if !ok {
		t.Fatal("expected data to be a map")
	}
	if data["path"] != segFile {
		t.Errorf("expected data.path=%s, got %v", segFile, data["path"])
	}
}

func TestSegmentNotifier_WebhookError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Server that returns errors
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	n := NewSegmentNotifier(server.URL, logger)

	dir := t.TempDir()
	segData := make([]byte, 2048)
	os.WriteFile(filepath.Join(dir, "seg_00001.ts"), segData, 0o644)

	// Should not panic — errors are logged and ignored
	seen := make(map[string]int64)
	playlistMods := make(map[string]time.Time)
	// Two scans: first records size, second triggers (failing) webhook
	n.scanDir(t.Context(), "test", dir, seen, playlistMods)
	n.scanDir(t.Context(), "test", dir, seen, playlistMods)

	// Segment should still be marked as seen (no retry)
	if seen[filepath.Join(dir, "seg_00001.ts")] != -1 {
		t.Error("expected segment to be marked as notified (-1) even after webhook error")
	}
}

func TestSegmentNotifier_IgnoresNonHLSFiles(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	var eventCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		eventCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	n := NewSegmentNotifier(server.URL, logger)

	dir := t.TempDir()
	// HLS files (segments must be >= 1024 bytes)
	segData := make([]byte, 2048)
	os.WriteFile(filepath.Join(dir, "seg_00001.ts"), segData, 0o644)
	os.WriteFile(filepath.Join(dir, "index.m3u8"), []byte("m3u8"), 0o644)
	// Non-HLS files — should be ignored
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("text"), 0o644)
	os.WriteFile(filepath.Join(dir, "thumb.jpg"), []byte("jpg"), 0o644)
	os.WriteFile(filepath.Join(dir, "data.json"), []byte("json"), 0o644)

	seen := make(map[string]int64)
	playlistMods := make(map[string]time.Time)
	// First scan: playlist fires, segment recorded but not notified
	n.scanDir(t.Context(), "test", dir, seen, playlistMods)

	if eventCount != 1 {
		t.Errorf("expected 1 event after first scan (playlist only), got %d", eventCount)
	}

	// Second scan: segment stable, fires
	n.scanDir(t.Context(), "test", dir, seen, playlistMods)

	if eventCount != 2 {
		t.Errorf("expected 2 events after second scan (playlist + segment), got %d", eventCount)
	}
}

func TestSegmentNotifier_RejectsUndersizedSegments(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	var eventCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		eventCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	n := NewSegmentNotifier(server.URL, logger)

	dir := t.TempDir()

	// Undersized segment (< 1024 bytes) — should be ignored entirely
	os.WriteFile(filepath.Join(dir, "seg_00001.ts"), []byte("tiny"), 0o644)
	// Valid-sized segment
	validData := make([]byte, 2048)
	os.WriteFile(filepath.Join(dir, "seg_00002.ts"), validData, 0o644)

	seen := make(map[string]int64)
	playlistMods := make(map[string]time.Time)

	// First scan: undersized rejected, valid recorded
	n.scanDir(t.Context(), "test", dir, seen, playlistMods)
	if eventCount != 0 {
		t.Errorf("expected 0 events after first scan, got %d", eventCount)
	}

	// Second scan: valid segment is now stable → notified
	n.scanDir(t.Context(), "test", dir, seen, playlistMods)
	if eventCount != 1 {
		t.Errorf("expected 1 event after second scan (only valid segment), got %d", eventCount)
	}

	// Undersized file should not be in seen map at all
	if _, exists := seen[filepath.Join(dir, "seg_00001.ts")]; exists {
		t.Error("undersized segment should not be tracked in seen map")
	}
}

func TestSegmentNotifier_StabilityGrowingFile(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	var eventCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		eventCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	n := NewSegmentNotifier(server.URL, logger)

	dir := t.TempDir()
	segPath := filepath.Join(dir, "seg_00001.ts")

	// Write initial content (>= 1024 bytes)
	data1 := make([]byte, 2048)
	os.WriteFile(segPath, data1, 0o644)

	seen := make(map[string]int64)
	playlistMods := make(map[string]time.Time)

	// First scan: records size
	n.scanDir(t.Context(), "test", dir, seen, playlistMods)
	if eventCount != 0 {
		t.Fatalf("expected 0 events after first scan, got %d", eventCount)
	}

	// File grows between polls (simulates still-writing on SMB)
	data2 := make([]byte, 4096)
	os.WriteFile(segPath, data2, 0o644)

	// Second scan: size changed → update, don't notify
	n.scanDir(t.Context(), "test", dir, seen, playlistMods)
	if eventCount != 0 {
		t.Fatalf("expected 0 events after second scan (file grew), got %d", eventCount)
	}

	// Third scan: size now stable → notify
	n.scanDir(t.Context(), "test", dir, seen, playlistMods)
	if eventCount != 1 {
		t.Fatalf("expected 1 event after third scan (stable), got %d", eventCount)
	}
}

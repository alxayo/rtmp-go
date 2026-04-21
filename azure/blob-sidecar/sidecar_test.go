package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractStreamKey_FlatLayout(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "standard pattern with live prefix",
			path: "recordings/live_mystream_20260419_103406_seg001.flv",
			want: "live/mystream",
		},
		{
			name: "multi-word stream key",
			path: "recordings/live_camera1_20260420_143000_seg002.mp4",
			want: "live/camera1",
		},
		{
			name: "tenant prefix",
			path: "recordings/app_conference_20260420_143000_seg001.flv",
			want: "app/conference",
		},
		{
			name: "unknown prefix stays underscore",
			path: "recordings/tenantA_stream1_20260420_143000_seg001.flv",
			want: "tenantA_stream1",
		},
	}

	router := &Router{logger: noopLogger()}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := router.ExtractStreamKey(tt.path)
			if got != tt.want {
				t.Errorf("ExtractStreamKey(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestExtractStreamKey_NestedLayout(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "subdirectory is stream key",
			path: "recordings/live_mystream/20260419_103406_seg001.flv",
			want: "live/mystream",
		},
		{
			name: "deep subdirectory",
			path: "recordings/app_conference/20260420_143000_seg002.mp4",
			want: "app/conference",
		},
	}

	router := &Router{logger: noopLogger()}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := router.ExtractStreamKey(tt.path)
			if got != tt.want {
				t.Errorf("ExtractStreamKey(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestFileResolver(t *testing.T) {
	cfg := &Config{}
	cfg.current = &TenantConfig{
		Tenants: map[string]*StorageTarget{
			"live": {
				StorageAccount: "https://live.blob.core.windows.net",
				Container:      "recordings",
			},
			"tenant-a": {
				StorageAccount: "https://tenanta.blob.core.windows.net",
				Container:      "streams",
			},
		},
		Default: &StorageTarget{
			StorageAccount: "https://default.blob.core.windows.net",
			Container:      "unrouted",
		},
	}

	resolver := NewFileResolver(cfg)

	tests := []struct {
		name       string
		streamKey  string
		wantAcct   string
		wantNil    bool
	}{
		{"exact match", "live", "https://live.blob.core.windows.net", false},
		{"app prefix match", "live/mystream", "https://live.blob.core.windows.net", false},
		{"tenant prefix", "tenant-a", "https://tenanta.blob.core.windows.net", false},
		{"tenant prefix with stream", "tenant-a/cam1", "https://tenanta.blob.core.windows.net", false},
		{"no match", "unknown/stream", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := resolver.Resolve(tt.streamKey)
			if tt.wantNil {
				if target != nil {
					t.Errorf("Resolve(%q) = %v, want nil", tt.streamKey, target)
				}
				return
			}
			if target == nil {
				t.Fatalf("Resolve(%q) = nil, want non-nil", tt.streamKey)
			}
			if target.StorageAccount != tt.wantAcct {
				t.Errorf("Resolve(%q).StorageAccount = %q, want %q", tt.streamKey, target.StorageAccount, tt.wantAcct)
			}
		})
	}
}

func TestConfigLoad(t *testing.T) {
	// Write a temp config file
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "tenants.json")

	content := `{
		"tenants": {
			"live": {
				"storage_account": "https://live.blob.core.windows.net",
				"container": "recs"
			}
		},
		"default": {
			"storage_account": "https://default.blob.core.windows.net",
			"container": "all"
		}
	}`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	tc := cfg.Get()
	if len(tc.Tenants) != 1 {
		t.Errorf("expected 1 tenant, got %d", len(tc.Tenants))
	}
	if tc.Default == nil {
		t.Error("expected default to be set")
	}
}

func TestConfigReload(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "tenants.json")

	// Initial config
	initial := `{"tenants": {"live": {"storage_account": "https://v1.blob.core.windows.net", "container": "a"}}}`
	os.WriteFile(cfgPath, []byte(initial), 0o644)

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Get().Tenants["live"].StorageAccount != "https://v1.blob.core.windows.net" {
		t.Fatal("unexpected initial value")
	}

	// Update config
	updated := `{"tenants": {"live": {"storage_account": "https://v2.blob.core.windows.net", "container": "b"}}}`
	os.WriteFile(cfgPath, []byte(updated), 0o644)

	if err := cfg.Reload(); err != nil {
		t.Fatalf("Reload failed: %v", err)
	}

	if cfg.Get().Tenants["live"].StorageAccount != "https://v2.blob.core.windows.net" {
		t.Error("config did not reload")
	}
}

func TestIsSegmentFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"segment_001.flv", true},
		{"segment_001.mp4", true},
		{"segment_001.FLV", true},
		{"readme.md", false},
		{"data.json", false},
		{".flv", true},
	}

	for _, tt := range tests {
		if got := isSegmentFile(tt.path); got != tt.want {
			t.Errorf("isSegmentFile(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func noopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestStdinListener_SegmentComplete(t *testing.T) {
	input := `some random log line
RTMP_EVENT: {"type":"segment_complete","timestamp":1714168200,"conn_id":"c1","stream_key":"live/stream1","data":{"path":"/recordings/live_stream1_seg001.flv","size":1024,"segment_index":1,"duration_ms":180000}}
RTMP_EVENT: {"type":"recording_start","timestamp":1714168000,"conn_id":"c1","stream_key":"live/stream1","data":{}}
RTMP_EVENT: {"type":"segment_complete","timestamp":1714168400,"conn_id":"c1","stream_key":"live/stream2","data":{"path":"/recordings/live_stream2_seg001.mp4","size":2048,"segment_index":1,"duration_ms":60000}}
`
	reader := strings.NewReader(input)
	var events []HookEvent
	listener := NewStdinListener(reader, noopLogger(), func(event HookEvent) {
		events = append(events, event)
	})

	err := listener.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 segment_complete events, got %d", len(events))
	}

	if events[0].StreamKey != "live/stream1" {
		t.Errorf("event[0] stream_key = %q, want live/stream1", events[0].StreamKey)
	}
	if events[1].StreamKey != "live/stream2" {
		t.Errorf("event[1] stream_key = %q, want live/stream2", events[1].StreamKey)
	}
	if path, _ := events[0].Data["path"].(string); path != "/recordings/live_stream1_seg001.flv" {
		t.Errorf("event[0] path = %q, want /recordings/live_stream1_seg001.flv", path)
	}
}

func TestStdinListener_MalformedJSON(t *testing.T) {
	input := `RTMP_EVENT: {invalid json}
RTMP_EVENT: {"type":"segment_complete","timestamp":1,"conn_id":"c1","stream_key":"live/ok","data":{"path":"/x.flv"}}
`
	reader := strings.NewReader(input)
	var events []HookEvent
	listener := NewStdinListener(reader, noopLogger(), func(event HookEvent) {
		events = append(events, event)
	})

	err := listener.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should parse the valid line despite the malformed one
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].StreamKey != "live/ok" {
		t.Errorf("stream_key = %q, want live/ok", events[0].StreamKey)
	}
}

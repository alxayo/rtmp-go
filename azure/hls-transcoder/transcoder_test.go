package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeStreamKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"live/test", "live_test"},
		{"live/stream1", "live_stream1"},
		{"app/user/cam1", "app_user_cam1"},
		{"noSlash", "noSlash"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeStreamKey(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeStreamKey(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSanitizeURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"with token", "rtmp://host:1935/live/test?token=secret123", "rtmp://host:1935/live/test?token=***"},
		{"no token", "rtmp://host:1935/live/test", "rtmp://host:1935/live/test"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeURL(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTranscoder_BuildRTMPURL(t *testing.T) {
	tests := []struct {
		name      string
		config    TranscoderConfig
		streamKey string
		want      string
	}{
		{
			name:      "without token",
			config:    TranscoderConfig{RTMPHost: "rtmp-server", RTMPPort: 1935},
			streamKey: "live/test",
			want:      "rtmp://rtmp-server:1935/live/test",
		},
		{
			name:      "with token",
			config:    TranscoderConfig{RTMPHost: "rtmp-server", RTMPPort: 1935, RTMPToken: "secret"},
			streamKey: "live/test",
			want:      "rtmp://rtmp-server:1935/live/test?token=secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := NewTranscoder(tt.config, noopLogger())
			got := tr.buildRTMPURL(tt.streamKey)
			if got != tt.want {
				t.Errorf("buildRTMPURL(%q) = %q, want %q", tt.streamKey, got, tt.want)
			}
		})
	}
}

func TestTranscoder_BuildABRArgs(t *testing.T) {
	tr := NewTranscoder(TranscoderConfig{
		HLSDir:   "/hls-output",
		RTMPHost: "rtmp-server",
		RTMPPort: 1935,
		Mode:     "abr",
	}, noopLogger())

	args := tr.BuildABRArgs("rtmp://rtmp-server:1935/live/test", "/hls-output/live_test")

	// Verify key arguments are present
	argStr := strings.Join(args, " ")

	checks := []string{
		"-fflags +genpts+discardcorrupt",
		"-err_detect ignore_err",
		"-ec deblock+guess_mvs",
		"-i rtmp://rtmp-server:1935/live/test",
		"-var_stream_map",
		"v:0,a:0 v:1,a:1 v:2,a:2",
		"-c:v:0 copy",
		"-c:a:0 copy",
		"-c:v:1 libx264",
		"-s:v:1 1280x720",
		"-b:v:1 2500k",
		"-preset:v:1 ultrafast",
		"-c:v:2 libx264",
		"-s:v:2 854x480",
		"-b:v:2 1000k",
		"-preset:v:2 ultrafast",
		"-force_key_frames:v:1",
		"-force_key_frames:v:2",
		"-async 1",
		"-fps_mode:v:1 cfr",
		"-fps_mode:v:2 cfr",
		"-hls_time 3",
		"-hls_list_size 6",
		"-hls_flags independent_segments",
	}

	for _, check := range checks {
		if !strings.Contains(argStr, check) {
			t.Errorf("ABR args missing %q\nGot: %s", check, argStr)
		}
	}

	// Verify segment filename pattern uses %v for stream variant
	segPattern := filepath.Join("/hls-output/live_test", "stream_%v", "seg_%05d.ts")
	if !strings.Contains(argStr, segPattern) {
		t.Errorf("ABR args missing segment pattern %q\nGot: %s", segPattern, argStr)
	}
}

func TestTranscoder_BuildCopyArgs(t *testing.T) {
	tr := NewTranscoder(TranscoderConfig{
		HLSDir:   "/hls-output",
		RTMPHost: "rtmp-server",
		RTMPPort: 1935,
		Mode:     "copy",
	}, noopLogger())

	args := tr.BuildCopyArgs("rtmp://rtmp-server:1935/live/test", "/hls-output/live_test")

	argStr := strings.Join(args, " ")

	checks := []string{
		"-fflags +genpts+discardcorrupt",
		"-err_detect ignore_err",
		"-i rtmp://rtmp-server:1935/live/test",
		"-c copy",
		"-f hls",
		"-hls_time 3",
		"-hls_list_size 6",
		"-hls_flags independent_segments",
	}

	for _, check := range checks {
		if !strings.Contains(argStr, check) {
			t.Errorf("Copy args missing %q\nGot: %s", check, argStr)
		}
	}

	// Copy mode should NOT have var_stream_map, master playlist, or error concealment
	if strings.Contains(argStr, "-var_stream_map") {
		t.Error("Copy args should not contain -var_stream_map")
	}
	if strings.Contains(argStr, "-master_pl_name") {
		t.Error("Copy args should not contain -master_pl_name")
	}
	if strings.Contains(argStr, "-ec ") {
		t.Error("Copy args should not contain -ec (error concealment requires decoding)")
	}
}

func TestTranscoder_StartIdempotent(t *testing.T) {
	// Use a non-existent ffmpeg path so Start fails gracefully but tests the
	// idempotency logic (second call should be a no-op).
	tr := NewTranscoder(TranscoderConfig{
		HLSDir:   t.TempDir(),
		RTMPHost: "localhost",
		RTMPPort: 1935,
		Mode:     "copy",
	}, noopLogger())

	// First Start — will fail because ffmpeg isn't available, but that's OK.
	// We're testing that the map logic works.
	tr.Start("live/test")

	// Whether it succeeded or failed, calling Start again should not panic.
	tr.Start("live/test")

	// Cleanup
	tr.StopAll()
}

func TestTranscoder_StopNonExistent(t *testing.T) {
	tr := NewTranscoder(TranscoderConfig{
		HLSDir:   t.TempDir(),
		RTMPHost: "localhost",
		RTMPPort: 1935,
		Mode:     "abr",
	}, noopLogger())

	// Stop on a stream that was never started — should be a no-op
	tr.Stop("live/nonexistent")
}

func TestTranscoder_StopAll(t *testing.T) {
	tr := NewTranscoder(TranscoderConfig{
		HLSDir:   t.TempDir(),
		RTMPHost: "localhost",
		RTMPPort: 1935,
		Mode:     "abr",
	}, noopLogger())

	// StopAll on empty transcoder — should be a no-op
	tr.StopAll()

	if tr.ActiveStreams() != 0 {
		t.Errorf("ActiveStreams() = %d, want 0", tr.ActiveStreams())
	}
}

func TestTranscoder_ActiveStreams(t *testing.T) {
	tr := NewTranscoder(TranscoderConfig{
		HLSDir:   t.TempDir(),
		RTMPHost: "localhost",
		RTMPPort: 1935,
		Mode:     "abr",
	}, noopLogger())

	if tr.ActiveStreams() != 0 {
		t.Errorf("ActiveStreams() = %d, want 0", tr.ActiveStreams())
	}
}

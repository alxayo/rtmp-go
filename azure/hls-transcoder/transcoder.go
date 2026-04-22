package main

// Transcoder manages FFmpeg processes that convert live RTMP streams into HLS
// output. It supports two modes:
//
//   - ABR (Adaptive Bitrate): single FFmpeg process producing 3 renditions
//     (1080p/720p/480p) with aligned keyframes via -var_stream_map. Generates
//     a master.m3u8 playlist for adaptive bitrate switching.
//
//   - Copy (Remux): single FFmpeg process that remuxes the RTMP stream to HLS
//     without transcoding (-c copy). Lower CPU usage but single-bitrate only.
//
// Each active stream is tracked by stream key. Start is idempotent (duplicate
// calls for the same key are ignored). Stop sends SIGTERM to the FFmpeg process
// and waits for it to exit cleanly.

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

// TranscoderConfig holds the configuration for FFmpeg process construction.
type TranscoderConfig struct {
	HLSDir    string // Root directory for HLS output
	RTMPHost  string // RTMP server hostname
	RTMPPort  int    // RTMP server port
	RTMPToken string // Auth token for subscribing (optional)
	Mode      string // "abr" or "copy"
}

// streamProcess tracks a running FFmpeg process for a single stream.
type streamProcess struct {
	cmd       *exec.Cmd
	streamKey string
	outputDir string
}

// Transcoder manages FFmpeg processes for active streams.
type Transcoder struct {
	config  TranscoderConfig
	logger  *slog.Logger
	mu      sync.Mutex
	streams map[string]*streamProcess // keyed by stream key
}

// NewTranscoder creates a transcoder with the given configuration.
func NewTranscoder(cfg TranscoderConfig, logger *slog.Logger) *Transcoder {
	return &Transcoder{
		config:  cfg,
		logger:  logger,
		streams: make(map[string]*streamProcess),
	}
}

// Start begins HLS transcoding for the given stream key. If transcoding is
// already active for this key, the call is a no-op (idempotent).
// The FFmpeg process subscribes to the RTMP stream and writes HLS output
// to {hlsDir}/{safeStreamKey}/.
func (t *Transcoder) Start(streamKey string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, exists := t.streams[streamKey]; exists {
		t.logger.Info("transcoding already active, ignoring duplicate start", "stream_key", streamKey)
		return
	}

	safeKey := sanitizeStreamKey(streamKey)
	outputDir := filepath.Join(t.config.HLSDir, safeKey)

	// Create output directory structure
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.logger.Error("failed to create output directory", "dir", outputDir, "error", err)
		return
	}

	// Build the RTMP source URL
	rtmpURL := t.buildRTMPURL(streamKey)

	// Build FFmpeg command based on mode
	var args []string
	switch t.config.Mode {
	case "copy":
		args = t.buildCopyArgs(rtmpURL, outputDir)
	default:
		args = t.buildABRArgs(rtmpURL, outputDir)
	}

	cmd := exec.Command("ffmpeg", args...)
	cmd.Stdout = os.Stderr // FFmpeg logs go to stderr for container log aggregation
	cmd.Stderr = os.Stderr
	// Set process group so we can kill the entire group on shutdown
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	t.logger.Info("starting FFmpeg transcoder",
		"stream_key", streamKey,
		"mode", t.config.Mode,
		"output_dir", outputDir,
		"rtmp_url", sanitizeURL(rtmpURL),
	)

	if err := cmd.Start(); err != nil {
		t.logger.Error("failed to start FFmpeg", "stream_key", streamKey, "error", err)
		return
	}

	sp := &streamProcess{
		cmd:       cmd,
		streamKey: streamKey,
		outputDir: outputDir,
	}
	t.streams[streamKey] = sp

	t.logger.Info("FFmpeg transcoder started", "stream_key", streamKey, "pid", cmd.Process.Pid)

	// Monitor FFmpeg process in background — log exit status and clean up map entry
	go t.monitor(sp)
}

// Stop terminates the FFmpeg process for the given stream key.
// Sends SIGTERM for graceful shutdown (FFmpeg finalizes HLS playlists).
func (t *Transcoder) Stop(streamKey string) {
	t.mu.Lock()
	sp, exists := t.streams[streamKey]
	if !exists {
		t.mu.Unlock()
		t.logger.Debug("no active transcoder for stream, ignoring stop", "stream_key", streamKey)
		return
	}
	delete(t.streams, streamKey)
	t.mu.Unlock()

	t.logger.Info("stopping FFmpeg transcoder", "stream_key", streamKey, "pid", sp.cmd.Process.Pid)

	// Send SIGTERM to the process group for clean shutdown
	if err := syscall.Kill(-sp.cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.logger.Warn("failed to send SIGTERM to FFmpeg", "stream_key", streamKey, "error", err)
		// Fallback: kill the process directly
		sp.cmd.Process.Kill()
	}
}

// StopAll terminates all active FFmpeg processes. Called during graceful shutdown.
func (t *Transcoder) StopAll() {
	t.mu.Lock()
	keys := make([]string, 0, len(t.streams))
	for k := range t.streams {
		keys = append(keys, k)
	}
	t.mu.Unlock()

	for _, key := range keys {
		t.Stop(key)
	}

	t.logger.Info("all transcoders stopped", "count", len(keys))
}

// ActiveStreams returns the number of currently active transcoding sessions.
func (t *Transcoder) ActiveStreams() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.streams)
}

// monitor waits for an FFmpeg process to exit and logs the result.
// On unexpected exit (not triggered by Stop), it cleans up the map entry.
func (t *Transcoder) monitor(sp *streamProcess) {
	err := sp.cmd.Wait()

	t.mu.Lock()
	// Only clean up if this process is still the registered one for the key
	// (it might have been removed by Stop already)
	if current, exists := t.streams[sp.streamKey]; exists && current == sp {
		delete(t.streams, sp.streamKey)
	}
	t.mu.Unlock()

	if err != nil {
		t.logger.Warn("FFmpeg process exited with error",
			"stream_key", sp.streamKey,
			"error", err,
		)
	} else {
		t.logger.Info("FFmpeg process exited cleanly", "stream_key", sp.streamKey)
	}
}

// buildRTMPURL constructs the RTMP URL for subscribing to a stream.
func (t *Transcoder) buildRTMPURL(streamKey string) string {
	url := fmt.Sprintf("rtmp://%s:%d/%s", t.config.RTMPHost, t.config.RTMPPort, streamKey)
	if t.config.RTMPToken != "" {
		url += "?token=" + t.config.RTMPToken
	}
	return url
}

// buildABRArgs constructs FFmpeg arguments for multi-bitrate adaptive HLS.
// Uses a single FFmpeg process with -var_stream_map to produce 3 renditions
// (1080p, 720p, 480p) with aligned keyframes for seamless quality switching.
//
// Rendition presets (aligned with scripts/on-publish-abr.sh):
//
//	stream 0: 1920x1080 @ 5000k video, 192k audio
//	stream 1: 1280x720  @ 2500k video, 128k audio
//	stream 2: 854x480   @ 1000k video, 96k audio
func (t *Transcoder) buildABRArgs(rtmpURL, outputDir string) []string {
	return []string{
		"-hide_banner",
		"-loglevel", "warning",

		// Input
		"-i", rtmpURL,

		// Map: 3 video + 3 audio streams for 3 renditions
		"-map", "0:v", "-map", "0:a",
		"-map", "0:v", "-map", "0:a",
		"-map", "0:v", "-map", "0:a",

		// Rendition 0: 1080p
		"-c:v:0", "libx264", "-s:v:0", "1920x1080",
		"-b:v:0", "5000k", "-maxrate:v:0", "5500k", "-bufsize:v:0", "10000k",

		// Rendition 1: 720p
		"-c:v:1", "libx264", "-s:v:1", "1280x720",
		"-b:v:1", "2500k", "-maxrate:v:1", "2750k", "-bufsize:v:1", "5000k",

		// Rendition 2: 480p
		"-c:v:2", "libx264", "-s:v:2", "854x480",
		"-b:v:2", "1000k", "-maxrate:v:2", "1100k", "-bufsize:v:2", "2000k",

		// Shared video settings — aligned keyframes across all renditions
		"-preset", "veryfast",
		"-r", "30",
		"-force_key_frames", "expr:gte(t,n_forced*2)",
		"-sc_threshold", "0",

		// Audio encoding per rendition
		"-c:a:0", "aac", "-b:a:0", "192k", "-ar:a:0", "48000",
		"-c:a:1", "aac", "-b:a:1", "128k", "-ar:a:1", "48000",
		"-c:a:2", "aac", "-b:a:2", "96k", "-ar:a:2", "48000",

		// HLS output settings
		"-f", "hls",
		"-hls_time", "2",
		"-hls_list_size", "10",
		"-hls_flags", "delete_segments+temp_file",

		// Multi-variant stream map — produces separate directories per rendition
		"-var_stream_map", "v:0,a:0 v:1,a:1 v:2,a:2",

		// Master playlist
		"-master_pl_name", "master.m3u8",

		// Segment and playlist output pattern
		"-hls_segment_filename", filepath.Join(outputDir, "stream_%v", "seg_%05d.ts"),
		filepath.Join(outputDir, "stream_%v", "index.m3u8"),
	}
}

// buildCopyArgs constructs FFmpeg arguments for remux-only HLS output.
// No transcoding — copies video and audio codecs directly (-c copy).
// Produces single-bitrate HLS with minimal CPU usage.
func (t *Transcoder) buildCopyArgs(rtmpURL, outputDir string) []string {
	return []string{
		"-hide_banner",
		"-loglevel", "warning",

		// Input
		"-i", rtmpURL,

		// Copy codecs (no transcoding)
		"-c", "copy",

		// HLS output settings
		"-f", "hls",
		"-hls_time", "2",
		"-hls_list_size", "10",
		"-hls_flags", "delete_segments+temp_file",

		// Segment and playlist output
		"-hls_segment_filename", filepath.Join(outputDir, "seg_%05d.ts"),
		filepath.Join(outputDir, "index.m3u8"),
	}
}

// sanitizeStreamKey replaces "/" with "_" to create a filesystem-safe directory
// name. This matches rtmp-go's segment naming convention.
func sanitizeStreamKey(streamKey string) string {
	return strings.ReplaceAll(streamKey, "/", "_")
}

// sanitizeURL removes the token query parameter from a URL for safe logging.
func sanitizeURL(url string) string {
	if idx := strings.Index(url, "?token="); idx >= 0 {
		return url[:idx] + "?token=***"
	}
	return url
}

// BuildABRArgs exposes ABR argument construction for testing.
func (t *Transcoder) BuildABRArgs(rtmpURL, outputDir string) []string {
	return t.buildABRArgs(rtmpURL, outputDir)
}

// BuildCopyArgs exposes copy argument construction for testing.
func (t *Transcoder) BuildCopyArgs(rtmpURL, outputDir string) []string {
	return t.buildCopyArgs(rtmpURL, outputDir)
}

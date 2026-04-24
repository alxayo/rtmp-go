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
	"context"
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
	HLSDir         string // Root directory for HLS output
	RTMPHost       string // RTMP server hostname
	RTMPPort       int    // RTMP server port
	RTMPToken      string // Auth token for subscribing (optional)
	Mode           string // "abr" or "copy"
	BlobWebhookURL string // Webhook URL for blob-sidecar (empty = no blob upload)

	// HTTP output mode (Phase 2)
	// OutputMode: "file" (default, writes to local disk) or "http" (streams to blob-sidecar)
	OutputMode string // Output destination: "file" or "http"
	// IngestURL: Base URL for blob-sidecar HTTP ingest endpoint (required when OutputMode="http")
	// Format: "http://blob-sidecar:8081/ingest/" (with trailing slash)
	IngestURL string // HTTP ingest base URL (required if OutputMode="http")
	// IngestToken: Optional bearer token for authentication to blob-sidecar (for secure deployments)
	IngestToken string // Bearer token for HTTP ingest (optional)
}

// streamProcess tracks a running FFmpeg process for a single stream.
type streamProcess struct {
	cmd          *exec.Cmd
	streamKey    string
	outputDir    string
	cancelNotify context.CancelFunc // cancels the segment notifier goroutine
}

// Transcoder manages FFmpeg processes for active streams.
type Transcoder struct {
	config   TranscoderConfig
	logger   *slog.Logger
	notifier *SegmentNotifier
	mu       sync.Mutex
	streams  map[string]*streamProcess // keyed by stream key
}

// NewTranscoder creates a transcoder with the given configuration.
func NewTranscoder(cfg TranscoderConfig, logger *slog.Logger) *Transcoder {
	return &Transcoder{
		config:   cfg,
		logger:   logger,
		notifier: NewSegmentNotifier(cfg.BlobWebhookURL, logger),
		streams:  make(map[string]*streamProcess),
	}
}

// ValidateHTTPConfig validates that HTTP output mode has all required configuration.
// Returns an error if OutputMode is "http" but IngestURL is not set.
// Called before Start() to catch configuration errors early.
func (cfg *TranscoderConfig) ValidateHTTPConfig() error {
	if cfg.OutputMode != "http" {
		return nil // Validation only applies to HTTP mode
	}
	if cfg.IngestURL == "" {
		return fmt.Errorf("HTTP output mode requires IngestURL to be set")
	}
	return nil
}

// Start begins HLS transcoding for the given stream key. If transcoding is
// already active for this key, the call is a no-op (idempotent).
//
// Behavior depends on OutputMode:
//   - file mode: FFmpeg writes to {hlsDir}/{safeStreamKey}/ on local disk
//   - http mode: FFmpeg streams directly to blob-sidecar HTTP ingest endpoint
//
// The FFmpeg process subscribes to the RTMP stream and outputs HLS segments
// according to the configured mode and transcoding settings.
func (t *Transcoder) Start(streamKey string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, exists := t.streams[streamKey]; exists {
		t.logger.Info("transcoding already active, ignoring duplicate start", "stream_key", streamKey)
		return
	}

	safeKey := sanitizeStreamKey(streamKey)

	// Build the RTMP source URL
	rtmpURL := t.buildRTMPURL(streamKey)

	// Validate HTTP configuration before proceeding
	if err := t.config.ValidateHTTPConfig(); err != nil {
		t.logger.Error("invalid transcoder configuration", "error", err)
		return
	}

	// Local directory handling — only for file mode
	// HTTP mode streams directly to blob-sidecar and doesn't need local disk I/O
	var outputDir string
	if t.config.OutputMode == "file" {
		outputDir = filepath.Join(t.config.HLSDir, safeKey)
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			t.logger.Error("failed to create output directory", "dir", outputDir, "error", err)
			return
		}

		// Write master.m3u8 explicitly for ABR mode (file mode only)
		// In HTTP mode, FFmpeg handles this via -master_pl_name
		if t.config.Mode != "copy" {
			if err := writeMasterPlaylist(outputDir); err != nil {
				t.logger.Error("failed to write master playlist", "dir", outputDir, "error", err)
				return
			}
			// Verify the file persists on the filesystem
			masterPath := filepath.Join(outputDir, "master.m3u8")
			if info, err := os.Stat(masterPath); err != nil {
				t.logger.Error("master.m3u8 written but stat failed", "path", masterPath, "error", err)
			} else {
				t.logger.Info("master.m3u8 written successfully", "path", masterPath, "size", info.Size())
			}
		}
	} else if t.config.OutputMode != "http" {
		// Validate output mode — should be caught by ValidateHTTPConfig but double-check
		t.logger.Error("unknown output mode", "mode", t.config.OutputMode)
		return
	}

	// Build FFmpeg command based on output mode and transcoding mode
	var args []string
	switch t.config.OutputMode {
	case "http":
		// HTTP mode: stream directly to blob-sidecar
		switch t.config.Mode {
		case "copy":
			args = t.buildCopyArgsHTTP(rtmpURL, streamKey)
		default:
			args = t.buildABRArgsHTTP(rtmpURL, streamKey)
		}
	default:
		// File mode: write to local filesystem (default behavior)
		switch t.config.Mode {
		case "copy":
			args = t.buildCopyArgs(rtmpURL, outputDir)
		default:
			args = t.buildABRArgs(rtmpURL, outputDir)
		}
	}

	cmd := exec.Command("ffmpeg", args...)
	cmd.Stdout = os.Stderr // FFmpeg logs go to stderr for container log aggregation
	cmd.Stderr = os.Stderr
	// Set process group so we can kill the entire group on shutdown
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	t.logger.Info("starting FFmpeg transcoder",
		"stream_key", streamKey,
		"mode", t.config.Mode,
		"output_mode", t.config.OutputMode,
		"output_dir", outputDir,
		"rtmp_url", sanitizeURL(rtmpURL),
	)

	if err := cmd.Start(); err != nil {
		t.logger.Error("failed to start FFmpeg", "stream_key", streamKey, "error", err)
		return
	}

	// Start segment notifier goroutine for blob upload (file mode only)
	// HTTP mode uses FFmpeg's HTTP PUT directly; no local segment polling needed
	var cancelNotify context.CancelFunc
	if t.config.OutputMode == "file" && t.notifier.Enabled() {
		notifyCtx, cancel := context.WithCancel(context.Background())
		cancelNotify = cancel
		go t.notifier.WatchStream(notifyCtx, streamKey, outputDir)
	}

	sp := &streamProcess{
		cmd:          cmd,
		streamKey:    streamKey,
		outputDir:    outputDir,
		cancelNotify: cancelNotify,
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

	// Stop segment notifier first (stops sending events for this stream)
	if sp.cancelNotify != nil {
		sp.cancelNotify()
	}

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

		// Input error handling — must come BEFORE -i.
		// Source encoders using Main/High profile send B-frames that cause
		// H.264 decoder errors (reference count overflow, illegal reordering,
		// corrupted NAL units). These flags prevent corrupted frames from
		// propagating into output segments.
		"-fflags", "+genpts+discardcorrupt", // regenerate PTS + discard corrupt packets
		"-err_detect", "ignore_err", // continue decoding on reference errors
		"-ec", "deblock+guess_mvs", // error concealment: reconstruct damaged frames

		// Input
		"-i", rtmpURL,

		// Map: 3 video + 3 audio streams for 3 renditions
		"-map", "0:v", "-map", "0:a",
		"-map", "0:v", "-map", "0:a",
		"-map", "0:v", "-map", "0:a",

		// Rendition 0: 1080p — passthrough (copy) to save CPU.
		// The heaviest encode is eliminated; relies on the ingest encoder
		// for bitrate/resolution. Audio is also copied.
		"-c:v:0", "copy",
		"-c:a:0", "copy",

		// Rendition 1: 720p — transcode
		"-c:v:1", "libx264", "-s:v:1", "1280x720",
		"-b:v:1", "2500k", "-maxrate:v:1", "2750k", "-bufsize:v:1", "5000k",
		"-preset:v:1", "ultrafast",

		// Rendition 2: 480p — transcode
		"-c:v:2", "libx264", "-s:v:2", "854x480",
		"-b:v:2", "1000k", "-maxrate:v:2", "1100k", "-bufsize:v:2", "2000k",
		"-preset:v:2", "ultrafast",

		// Shared video settings for encoded renditions (not applied to copy)
		"-r:v:1", "30", "-r:v:2", "30",
		"-force_key_frames:v:1", "expr:gte(t,n_forced*2)",
		"-force_key_frames:v:2", "expr:gte(t,n_forced*2)",
		"-sc_threshold", "0",

		// Timestamp correction — fixes non-monotonic DTS from source encoders
		// that send B-frames or irregular audio timestamps. Without these,
		// FFmpeg outputs "Non-monotonic DTS" warnings and produces segments
		// with micro-gaps that cause choppy playback.
		"-async", "1",
		"-fps_mode:v:1", "cfr", "-fps_mode:v:2", "cfr",

		// Audio encoding for transcoded renditions (rendition 0 audio is copied above)
		"-c:a:1", "aac", "-b:a:1", "128k", "-ar:a:1", "48000",
		"-c:a:2", "aac", "-b:a:2", "96k", "-ar:a:2", "48000",

		// HLS output settings
		// - hls_time 3: gives buffer margin for SMB write → sidecar poll → blob upload pipeline
		// - hls_list_size 6: 6 × 3s = 18s playlist window (adequate for live)
		// - independent_segments: signals each segment is independently decodable;
		//   we do NOT use delete_segments because the blob-sidecar manages segment
		//   lifecycle via upload-once semantics, and FFmpeg's delete races with the
		//   sidecar's polling on Azure Files SMB mounts.
		"-f", "hls",
		"-hls_time", "3",
		"-hls_list_size", "6",
		"-hls_flags", "independent_segments",

		// Multi-variant stream map — produces separate directories per rendition
		"-var_stream_map", "v:0,a:0 v:1,a:1 v:2,a:2",

		// Master playlist is written by writeMasterPlaylist() before FFmpeg starts.
		// Do NOT use -master_pl_name here: combined with -hls_flags temp_file,
		// FFmpeg's rename(master.m3u8.tmp → master.m3u8) fails on Azure Files SMB,
		// deleting the target file first then failing the rename — leaving no file.

		// Segment and playlist output pattern
		"-hls_segment_filename", filepath.Join(outputDir, "stream_%v", "seg_%05d.ts"),
		filepath.Join(outputDir, "stream_%v", "index.m3u8"),
	}
}

// buildHTTPOutputPath constructs the HTTP output URL for FFmpeg HLS output to blob-sidecar.
// FFmpeg uses this URL with -method PUT to upload .m3u8 and .ts files directly.
//
// For an event ID "mystream", the resulting URL is:
//   http://blob-sidecar:8081/ingest/hls/mystream/stream_%v/index.m3u8
//
// The %v is replaced by FFmpeg with the variant number (0, 1, 2 for ABR; omitted for copy).
// If IngestToken is set, it's passed via X-Token header during PUT operations.
func (t *Transcoder) buildHTTPOutputPath(eventID string) string {
	// Extract event ID from stream key (e.g., "live/mystream" → "mystream")
	parts := strings.Split(eventID, "/")
	safeName := parts[len(parts)-1]

	// Base path: http://blob-sidecar:8081/ingest/hls/{eventId}/stream_%v/index.m3u8
	// For copy mode, omit /stream_%v: http://blob-sidecar:8081/ingest/hls/{eventId}/index.m3u8
	if t.config.Mode == "copy" {
		return strings.TrimSuffix(t.config.IngestURL, "/") + "/hls/" + safeName + "/index.m3u8"
	}
	return strings.TrimSuffix(t.config.IngestURL, "/") + "/hls/" + safeName + "/stream_%v/index.m3u8"
}

// buildABRArgsHTTP constructs FFmpeg arguments for multi-bitrate HLS output via HTTP PUT.
// Streams HLS segments and playlists directly to blob-sidecar's HTTP ingest endpoint,
// bypassing local filesystem I/O entirely.
//
// Key design decisions:
//   - -method PUT: FFmpeg uploads each segment and playlist with HTTP PUT requests
//   - -master_pl_name master.m3u8: Generates master playlist for ABR variant switching
//   - No local output directory needed: all I/O goes over HTTP
//   - Same video/audio encoding as buildABRArgs: 3 renditions (1080p/720p/480p)
//   - SegmentNotifier is NOT used in HTTP mode (no local files to poll)
func (t *Transcoder) buildABRArgsHTTP(rtmpURL, eventID string) []string {
	httpPath := t.buildHTTPOutputPath(eventID)
	httpHeaders := ""
	if t.config.IngestToken != "" {
		// If bearer token is configured, FFmpeg will send X-Token header with each PUT
		httpHeaders = "X-Token: Bearer " + t.config.IngestToken
	}

	args := []string{
		"-hide_banner",
		"-loglevel", "warning",

		// Input error handling — must come BEFORE -i.
		// Same rationale as buildABRArgs: prevents B-frame and corrupted packets
		// from propagating into output segments.
		"-fflags", "+genpts+discardcorrupt",
		"-err_detect", "ignore_err",
		"-ec", "deblock+guess_mvs",

		// Input
		"-i", rtmpURL,

		// Map: 3 video + 3 audio streams for 3 renditions
		"-map", "0:v", "-map", "0:a",
		"-map", "0:v", "-map", "0:a",
		"-map", "0:v", "-map", "0:a",

		// Rendition 0: 1080p — copy video and audio (no transcode)
		"-c:v:0", "copy",
		"-c:a:0", "copy",

		// Rendition 1: 720p — transcode to lower bitrate
		"-c:v:1", "libx264", "-s:v:1", "1280x720",
		"-b:v:1", "2500k", "-maxrate:v:1", "2750k", "-bufsize:v:1", "5000k",
		"-preset:v:1", "ultrafast",

		// Rendition 2: 480p — transcode to lowest bitrate
		"-c:v:2", "libx264", "-s:v:2", "854x480",
		"-b:v:2", "1000k", "-maxrate:v:2", "1100k", "-bufsize:v:2", "2000k",
		"-preset:v:2", "ultrafast",

		// Shared video settings for encoded renditions
		"-r:v:1", "30", "-r:v:2", "30",
		"-force_key_frames:v:1", "expr:gte(t,n_forced*2)",
		"-force_key_frames:v:2", "expr:gte(t,n_forced*2)",
		"-sc_threshold", "0",

		// Timestamp correction — fixes non-monotonic DTS from source encoders
		"-async", "1",
		"-fps_mode:v:1", "cfr", "-fps_mode:v:2", "cfr",

		// Audio encoding for transcoded renditions
		"-c:a:1", "aac", "-b:a:1", "128k", "-ar:a:1", "48000",
		"-c:a:2", "aac", "-b:a:2", "96k", "-ar:a:2", "48000",

		// HLS output settings (same as buildABRArgs)
		"-f", "hls",
		"-hls_time", "3",
		"-hls_list_size", "6",
		"-hls_flags", "independent_segments",

		// Multi-variant stream map — produces separate URL paths per rendition
		"-var_stream_map", "v:0,a:0 v:1,a:1 v:2,a:2",

		// Master playlist — includes variant streams for adaptive bitrate switching
		"-master_pl_name", "master.m3u8",

		// HTTP output configuration
		// - method PUT: Upload segments via HTTP PUT instead of writing to disk
		// - custom_http_headers: Send auth token if configured (if httpHeaders is set, add these args)
		"-method", "PUT",
	}

	// Add auth header if token is configured
	if httpHeaders != "" {
		args = append(args, "-custom_http_headers", httpHeaders)
	}

	// HTTP output path — FFmpeg will upload segments to this URL
	args = append(args, httpPath)

	return args
}

// buildCopyArgsHTTP constructs FFmpeg arguments for copy-only (remux) HLS output via HTTP PUT.
// Streams HLS segments directly to blob-sidecar without transcoding, using minimal CPU.
//
// Key design decisions:
//   - Inherits all HTTP settings from buildABRArgsHTTP (PUT method, HTTP headers, etc.)
//   - Single-bitrate output: no multi-variant map, single index.m3u8 at root
//   - No -master_pl_name: copy mode only generates a single playlist
//   - Same codec copying strategy as buildCopyArgs: -c copy for both video and audio
func (t *Transcoder) buildCopyArgsHTTP(rtmpURL, eventID string) []string {
	httpPath := t.buildHTTPOutputPath(eventID)
	httpHeaders := ""
	if t.config.IngestToken != "" {
		httpHeaders = "X-Token: Bearer " + t.config.IngestToken
	}

	args := []string{
		"-hide_banner",
		"-loglevel", "warning",

		// Input error handling — same as buildABRArgsHTTP
		// Note: no -ec (error concealment) since we're not transcoding
		"-fflags", "+genpts+discardcorrupt",
		"-err_detect", "ignore_err",

		// Input
		"-i", rtmpURL,

		// Copy codecs — no transcoding, minimal CPU usage
		"-c", "copy",

		// HLS output settings
		"-f", "hls",
		"-hls_time", "3",
		"-hls_list_size", "6",
		"-hls_flags", "independent_segments",

		// HTTP output configuration
		"-method", "PUT",
	}

	// Add auth header if token is configured
	if httpHeaders != "" {
		args = append(args, "-custom_http_headers", httpHeaders)
	}

	// HTTP output path — single playlist at root (no stream_%v subdirectories in copy mode)
	args = append(args, httpPath)

	return args
}


// buildCopyArgs constructs FFmpeg arguments for remux-only HLS output.
// No transcoding — copies video and audio codecs directly (-c copy).
// Produces single-bitrate HLS with minimal CPU usage.
func (t *Transcoder) buildCopyArgs(rtmpURL, outputDir string) []string {
	return []string{
		"-hide_banner",
		"-loglevel", "warning",

		// Input error handling — must come BEFORE -i (see buildABRArgs for rationale).
		// No -ec flag in copy mode: error concealment requires decoding.
		"-fflags", "+genpts+discardcorrupt",
		"-err_detect", "ignore_err",

		// Input
		"-i", rtmpURL,

		// Copy codecs (no transcoding)
		"-c", "copy",

		// HLS output settings (see buildABRArgs for rationale)
		"-f", "hls",
		"-hls_time", "3",
		"-hls_list_size", "6",
		"-hls_flags", "independent_segments",

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

// writeMasterPlaylist writes a static HLS master playlist that references the
// three ABR renditions. Uses explicit f.Sync() to force the SMB client to
// flush data to the Azure Files server — os.WriteFile alone only writes to
// the local SMB cache which may never commit to the server.
//
// The content matches what FFmpeg would generate with the ABR settings in
// buildABRArgs: 1080p (stream_0), 720p (stream_1), 480p (stream_2).
func writeMasterPlaylist(outputDir string) error {
	const masterContent = `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-STREAM-INF:BANDWIDTH=5192000,RESOLUTION=1920x1080
stream_0/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=2628000,RESOLUTION=1280x720
stream_1/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=1096000,RESOLUTION=854x480
stream_2/index.m3u8
`
	masterPath := filepath.Join(outputDir, "master.m3u8")

	// Use explicit Open → Write → Sync → Close to force SMB FLUSH.
	f, err := os.OpenFile(masterPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open master.m3u8: %w", err)
	}
	if _, err := f.WriteString(masterContent); err != nil {
		f.Close()
		return fmt.Errorf("write master.m3u8: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("sync master.m3u8: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close master.m3u8: %w", err)
	}

	// Also sync the parent directory to flush the new directory entry.
	dir, err := os.Open(outputDir)
	if err == nil {
		_ = dir.Sync()
		dir.Close()
	}

	return nil
}

// BuildABRArgs exposes ABR argument construction for testing.
func (t *Transcoder) BuildABRArgs(rtmpURL, outputDir string) []string {
	return t.buildABRArgs(rtmpURL, outputDir)
}

// BuildCopyArgs exposes copy argument construction for testing.
func (t *Transcoder) BuildCopyArgs(rtmpURL, outputDir string) []string {
	return t.buildCopyArgs(rtmpURL, outputDir)
}

// BuildABRArgsHTTP exposes HTTP ABR argument construction for testing.
func (t *Transcoder) BuildABRArgsHTTP(rtmpURL, eventID string) []string {
	return t.buildABRArgsHTTP(rtmpURL, eventID)
}

// BuildCopyArgsHTTP exposes HTTP copy argument construction for testing.
func (t *Transcoder) BuildCopyArgsHTTP(rtmpURL, eventID string) []string {
	return t.buildCopyArgsHTTP(rtmpURL, eventID)
}

// BuildHTTPOutputPath exposes HTTP output path construction for testing.
func (t *Transcoder) BuildHTTPOutputPath(eventID string) string {
	return t.buildHTTPOutputPath(eventID)
}

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
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
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
	connID       string // RTMP connection ID for start/stop correlation
	eventID      string // Platform event ID (for session cleanup on stop)
	outputDir    string
	cancelNotify context.CancelFunc // cancels the segment notifier goroutine
}

// Transcoder manages FFmpeg processes for active streams.
type Transcoder struct {
	config        TranscoderConfig
	codec         string         // This transcoder's codec identity (e.g., "h264")
	configFetcher *ConfigFetcher // Fetches per-event config from Platform API
	logger        *slog.Logger
	notifier      *SegmentNotifier
	mu            sync.Mutex
	streams       map[string]*streamProcess // keyed by stream key
}

// NewTranscoder creates a transcoder with the given configuration.
// The configFetcher is created in main.go and is process-lifetime scoped.
func NewTranscoder(cfg TranscoderConfig, codec string, configFetcher *ConfigFetcher, logger *slog.Logger) *Transcoder {
	return &Transcoder{
		config:        cfg,
		codec:         codec,
		configFetcher: configFetcher,
		logger:        logger,
		notifier:      NewSegmentNotifier(cfg.BlobWebhookURL, logger),
		streams:       make(map[string]*streamProcess),
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
func (t *Transcoder) Start(streamKey, connID string) {
	// --- Step 1: Check idempotency inside the lock (fast path) ---
	t.mu.Lock()
	if _, exists := t.streams[streamKey]; exists {
		t.mu.Unlock()
		t.logger.Info("transcoding already active, ignoring duplicate start", "stream_key", streamKey, "conn_id", connID)
		return
	}
	t.mu.Unlock()

	// --- Step 2: Fetch per-event config OUTSIDE the lock (avoids serializing on network I/O) ---
	// Extract event ID from stream key (e.g., "live/uuid" → "uuid")
	var eventConfig *EventTranscoderConfig
	var configSource string
	var perEventToken string
	var platformEventID string // UUID from Platform API (for session cleanup)

	if t.configFetcher != nil {
		eventID, err := extractEventID(streamKey)
		if err != nil {
			t.logger.Error("invalid stream key, cannot extract event ID", "stream_key", streamKey, "error", err)
			return
		}

		// Fetch config from Platform API (with four-tier fallback chain)
		streamCfg, source, err := t.configFetcher.FetchEventConfig(eventID)
		if err != nil {
			// Non-retriable error (404, 403) — do not start FFmpeg
			t.logger.Warn("config fetch rejected, not starting transcoder", "stream_key", streamKey, "error", err)
			return
		}

		// Codec self-filter: only start if this transcoder's codec is enabled for the event
		codecEnabled := false
		for _, c := range streamCfg.Transcoder.Codecs {
			if c == t.codec {
				codecEnabled = true
				break
			}
		}
		if !codecEnabled {
			t.logger.Info("codec not enabled for event, skipping",
				"stream_key", streamKey, "codec", t.codec, "event_codecs", streamCfg.Transcoder.Codecs)
			return
		}

		eventConfig = &streamCfg.Transcoder
		configSource = source
		perEventToken = streamCfg.RTMPToken
		platformEventID = streamCfg.EventID
	} else {
		// No config fetcher (e.g., in tests) — use hardcoded defaults
		defaults := DefaultEventTranscoderConfig
		eventConfig = &defaults
		configSource = "hardcoded"
	}

	// --- Step 3: Re-acquire lock and re-check idempotency (another publish_start may have won) ---
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, exists := t.streams[streamKey]; exists {
		t.logger.Info("transcoding started by concurrent request, ignoring", "stream_key", streamKey, "conn_id", connID)
		return
	}

	safeKey := sanitizeStreamKey(streamKey)

	// Build the RTMP source URL
	rtmpURL := t.buildRTMPURL(streamKey, perEventToken)

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

	// Build FFmpeg command based on output mode, transcoding mode, and event config
	var args []string
	switch t.config.OutputMode {
	case "http":
		// HTTP mode: stream directly to blob-sidecar
		// Use platformEventID (UUID) for blob paths so HLS server can find them
		blobEventID := platformEventID
		if blobEventID == "" {
			// Fallback: extract from stream key (e.g., "live/mystream" → "mystream")
			parts := strings.Split(streamKey, "/")
			blobEventID = parts[len(parts)-1]
		}
		switch t.config.Mode {
		case "copy":
			args = t.buildCopyArgsHTTP(rtmpURL, blobEventID, eventConfig)
		default:
			args = t.buildABRArgsHTTP(rtmpURL, blobEventID, eventConfig)
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
		"conn_id", connID,
		"mode", t.config.Mode,
		"output_mode", t.config.OutputMode,
		"output_dir", outputDir,
		"rtmp_url", sanitizeURL(rtmpURL),
		"config_source", configSource,
		"hls_time", eventConfig.HLSTime,
		"hls_list_size", eventConfig.HLSListSize,
		"profile", eventConfig.Profile,
		"h264_tune", eventConfig.H264.Tune,
		"h264_preset", eventConfig.H264.Preset,
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

	// In HTTP+ABR mode, upload master.m3u8 explicitly.
	// FFmpeg's -master_pl_name only writes to local filesystem, not HTTP output.
	// The master playlist must match the profile's rendition count.
	if t.config.OutputMode == "http" && t.config.Mode != "copy" {
		masterContent := generateMasterPlaylist(eventConfig)
		blobID := platformEventID
		if blobID == "" {
			parts := strings.Split(streamKey, "/")
			blobID = parts[len(parts)-1]
		}
		go func() {
			// Small delay to let FFmpeg initialize and sidecar be ready
			time.Sleep(2 * time.Second)
			if err := t.uploadMasterPlaylistContent(blobID, masterContent); err != nil {
				t.logger.Error("failed to upload master.m3u8", "stream_key", streamKey, "error", err)
			}
		}()
	}

	sp := &streamProcess{
		cmd:          cmd,
		streamKey:    streamKey,
		connID:       connID,
		eventID:      platformEventID,
		outputDir:    outputDir,
		cancelNotify: cancelNotify,
	}
	t.streams[streamKey] = sp

	t.logger.Info("FFmpeg transcoder started", "stream_key", streamKey, "conn_id", connID, "pid", cmd.Process.Pid)

	// Monitor FFmpeg process in background — log exit status and clean up map entry
	go t.monitor(sp)
}

// Stop terminates the FFmpeg process for the given stream key.
// Sends SIGTERM for graceful shutdown (FFmpeg finalizes HLS playlists).
// Also notifies the Platform API to close the RTMP session.
func (t *Transcoder) Stop(streamKey, connID string) {
	t.mu.Lock()
	sp, exists := t.streams[streamKey]
	if !exists {
		t.mu.Unlock()
		t.logger.Debug("no active transcoder for stream, ignoring stop", "stream_key", streamKey, "conn_id", connID)
		return
	}

	// ConnID guard: if the stop is from a different connection than the one
	// that started this process, ignore it. This prevents a stale publish_stop
	// from killing a newly started transcoder for the same stream key.
	if sp.connID != connID {
		t.mu.Unlock()
		t.logger.Warn("ignoring stop from mismatched conn_id",
			"stream_key", streamKey,
			"stop_conn_id", connID,
			"active_conn_id", sp.connID,
		)
		return
	}

	eventID := sp.eventID
	delete(t.streams, streamKey)
	t.mu.Unlock()

	// Stop segment notifier first (stops sending events for this stream)
	if sp.cancelNotify != nil {
		sp.cancelNotify()
	}

	t.logger.Info("stopping FFmpeg transcoder", "stream_key", streamKey, "conn_id", connID, "pid", sp.cmd.Process.Pid)

	// Send SIGTERM to the process group for clean shutdown
	if err := syscall.Kill(-sp.cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.logger.Warn("failed to send SIGTERM to FFmpeg", "stream_key", streamKey, "error", err)
		// Fallback: kill the process directly
		sp.cmd.Process.Kill()
	}

	// Notify Platform API to close the RTMP session (fire-and-forget)
	if eventID != "" && t.configFetcher != nil {
		go t.notifyDisconnect(eventID, streamKey)
	}
}

// StopAll terminates all active FFmpeg processes. Called during graceful shutdown.
func (t *Transcoder) StopAll() {
	t.mu.Lock()
	type streamInfo struct {
		key    string
		connID string
	}
	streams := make([]streamInfo, 0, len(t.streams))
	for k, sp := range t.streams {
		streams = append(streams, streamInfo{key: k, connID: sp.connID})
	}
	t.mu.Unlock()

	for _, s := range streams {
		t.Stop(s.key, s.connID)
	}

	t.logger.Info("all transcoders stopped", "count", len(streams))
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
// If perEventToken is non-empty, it takes precedence over the static config token.
func (t *Transcoder) buildRTMPURL(streamKey string, perEventToken string) string {
	url := fmt.Sprintf("rtmp://%s:%d/%s", t.config.RTMPHost, t.config.RTMPPort, streamKey)
	token := perEventToken
	if token == "" {
		token = t.config.RTMPToken
	}
	if token != "" {
		url += "?token=" + token
	}
	return url
}

// notifyDisconnect calls the Platform API to close the RTMP session for an event.
// This prevents stale sessions from blocking future publish attempts.
// Runs as a fire-and-forget goroutine — errors are logged but not retried.
func (t *Transcoder) notifyDisconnect(eventID, streamKey string) {
	cfg := t.configFetcher.Config()
	disconnectURL := strings.TrimRight(cfg.PlatformURL, "/") + "/api/rtmp/disconnect"

	payload := fmt.Sprintf(`{"eventId":"%s"}`, eventID)
	req, err := http.NewRequest("POST", disconnectURL, strings.NewReader(payload))
	if err != nil {
		t.logger.Error("failed to create disconnect request", "event_id", eventID, "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Api-Key", cfg.APIKey)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.logger.Warn("failed to notify platform disconnect", "event_id", eventID, "stream_key", streamKey, "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.logger.Info("platform RTMP session closed", "event_id", eventID, "stream_key", streamKey)
	} else {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		t.logger.Warn("platform disconnect returned non-200", "event_id", eventID, "status", resp.StatusCode, "body", string(body))
	}
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
//
//	http://blob-sidecar:8081/ingest/hls/mystream/stream_%v/index.m3u8
//
// The %v is replaced by FFmpeg with the variant number (0, 1, 2 for ABR; omitted for copy).
// If IngestToken is set, it's passed via X-Token header during PUT operations.
func (t *Transcoder) buildHTTPOutputPath(eventID string) string {
	// eventID is already the clean identifier (UUID or slug) — no extraction needed.
	// Base path: http://blob-sidecar:8081/ingest/hls/{eventId}/stream_%v/index.m3u8
	// For copy mode, omit /stream_%v: http://blob-sidecar:8081/ingest/hls/{eventId}/index.m3u8
	if t.config.Mode == "copy" {
		return strings.TrimSuffix(t.config.IngestURL, "/") + "/hls/" + eventID + "/index.m3u8"
	}
	return strings.TrimSuffix(t.config.IngestURL, "/") + "/hls/" + eventID + "/stream_%v/index.m3u8"
}

// buildABRArgsHTTP constructs FFmpeg arguments for multi-bitrate HLS output via HTTP PUT.
// Now accepts EventTranscoderConfig to use dynamic values from admin settings
// instead of hardcoded defaults. This enables per-event latency tuning.
func (t *Transcoder) buildABRArgsHTTP(rtmpURL, eventID string, cfg *EventTranscoderConfig) []string {
	httpPath := t.buildHTTPOutputPath(eventID)
	httpHeaders := ""
	if t.config.IngestToken != "" {
		httpHeaders = "Authorization: Bearer " + t.config.IngestToken + "\r\n"
	}

	// Look up the rendition list for the configured profile
	renditions, ok := RenderProfiles[cfg.Profile]
	if !ok {
		// Fall back to full-abr if profile name is unrecognized
		renditions = RenderProfiles["full-abr-1080p-720p-480p"]
	}

	// Dynamic values from admin config (no more hardcoded 3s segments!)
	hlsTime := fmt.Sprintf("%d", cfg.HLSTime)
	hlsListSize := fmt.Sprintf("%d", cfg.HLSListSize)
	keyFrameExpr := fmt.Sprintf("expr:gte(t,n_forced*%d)", cfg.ForceKeyFrameInterval)

	args := []string{
		"-hide_banner",
		"-loglevel", "warning",

		// Input error handling — must come BEFORE -i.
		"-fflags", "+genpts+discardcorrupt",
		"-err_detect", "ignore_err",
		"-ec", "deblock+guess_mvs",

		// Input
		"-i", rtmpURL,
	}

	// Build map entries and codec settings dynamically from the profile's rendition list
	for range renditions {
		args = append(args, "-map", "0:v", "-map", "0:a")
	}

	// Configure each rendition based on the profile
	for i, r := range renditions {
		vi := fmt.Sprintf(":v:%d", i)
		ai := fmt.Sprintf(":a:%d", i)

		if r.Mode == "copy" {
			// Copy/passthrough rendition — no transcoding, lowest CPU
			args = append(args, "-c"+vi, "copy", "-c"+ai, "copy")
		} else {
			// Transcoded rendition — apply encoding settings from admin config
			args = append(args,
				"-c"+vi, "libx264",
				"-s"+vi, fmt.Sprintf("%dx%d", r.Width, r.Height),
				"-b"+vi, r.VideoBitrate,
				"-maxrate"+vi, adjustMaxrate(r.VideoBitrate),
				"-bufsize"+vi, adjustBufsize(r.VideoBitrate),
				"-preset"+vi, cfg.H264.Preset,
			)

			// Apply -tune zerolatency only to transcoded renditions (not copy)
			// This disables B-frames and reduces encoder buffering (~0.5s latency saving)
			if cfg.H264.Tune == "zerolatency" {
				args = append(args, "-tune"+vi, "zerolatency")
			}

			// Audio encoding for transcoded renditions
			args = append(args, "-c"+ai, "aac", "-b"+ai, r.AudioBitrate, "-ar"+ai, "48000")

			// Frame rate and keyframe settings for transcoded renditions
			args = append(args,
				"-r"+vi, "30",
				"-force_key_frames"+vi, keyFrameExpr,
			)
		}
	}

	// Scene change threshold off (ensures consistent keyframe placement)
	args = append(args, "-sc_threshold", "0")

	// Timestamp correction
	for i, r := range renditions {
		if r.Mode == "transcode" {
			args = append(args, fmt.Sprintf("-fps_mode:v:%d", i), "cfr")
		}
	}
	args = append(args, "-async", "1")

	// HLS output settings — now using admin-configured values
	args = append(args,
		"-f", "hls",
		"-hls_time", hlsTime,
		"-hls_list_size", hlsListSize,
		"-hls_flags", "independent_segments",
	)

	// Build -var_stream_map for the number of renditions in this profile
	varMap := ""
	for i := range renditions {
		if i > 0 {
			varMap += " "
		}
		varMap += fmt.Sprintf("v:%d,a:%d", i, i)
	}
	args = append(args, "-var_stream_map", varMap)

	// Master playlist
	args = append(args, "-master_pl_name", "master.m3u8")

	// HTTP output
	args = append(args, "-method", "PUT")

	if httpHeaders != "" {
		args = append(args, "-headers", httpHeaders)
	}

	args = append(args, httpPath)

	return args
}

// buildCopyArgsHTTP constructs FFmpeg arguments for copy-only (remux) HLS output via HTTP PUT.
// Now accepts EventTranscoderConfig for dynamic hlsTime/hlsListSize values.
func (t *Transcoder) buildCopyArgsHTTP(rtmpURL, eventID string, cfg *EventTranscoderConfig) []string {
	httpPath := t.buildHTTPOutputPath(eventID)
	httpHeaders := ""
	if t.config.IngestToken != "" {
		httpHeaders = "Authorization: Bearer " + t.config.IngestToken + "\r\n"
	}

	hlsTime := fmt.Sprintf("%d", cfg.HLSTime)
	hlsListSize := fmt.Sprintf("%d", cfg.HLSListSize)

	args := []string{
		"-hide_banner",
		"-loglevel", "warning",

		// Input error handling
		"-fflags", "+genpts+discardcorrupt",
		"-err_detect", "ignore_err",

		// Input
		"-i", rtmpURL,

		// Copy codecs — no transcoding, minimal CPU usage
		"-c", "copy",

		// HLS output settings — using admin-configured values
		"-f", "hls",
		"-hls_time", hlsTime,
		"-hls_list_size", hlsListSize,
		"-hls_flags", "independent_segments",

		// HTTP output configuration
		"-method", "PUT",
	}

	// Add auth header if token is configured
	if httpHeaders != "" {
		args = append(args, "-headers", httpHeaders)
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
// masterPlaylistContent is the static ABR master playlist shared between file and HTTP modes.
// Only used for file-mode output; HTTP mode uses generateMasterPlaylist() for dynamic content.
const masterPlaylistContent = `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-STREAM-INF:BANDWIDTH=5192000,RESOLUTION=1920x1080
stream_0/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=2628000,RESOLUTION=1280x720
stream_1/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=1096000,RESOLUTION=854x480
stream_2/index.m3u8
`

// generateMasterPlaylist creates a master playlist from the profile's rendition list.
// This replaces the hardcoded 3-rendition constant for HTTP mode, so profiles with
// different rendition counts (1, 2, or 3) generate the correct variant list.
func generateMasterPlaylist(cfg *EventTranscoderConfig) string {
	renditions, ok := RenderProfiles[cfg.Profile]
	if !ok {
		renditions = RenderProfiles["full-abr-1080p-720p-480p"]
	}

	content := "#EXTM3U\n#EXT-X-VERSION:3\n"
	for i, r := range renditions {
		// Calculate bandwidth from bitrate string (e.g., "2500k" → 2500000)
		bandwidth := estimateBandwidth(r.VideoBitrate, r.AudioBitrate)
		content += fmt.Sprintf("#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d\n", bandwidth, r.Width, r.Height)
		content += fmt.Sprintf("stream_%d/index.m3u8\n", i)
	}
	return content
}

// estimateBandwidth calculates total bandwidth from video+audio bitrate strings.
// For "copy" renditions, uses a reasonable estimate (5000k video + 192k audio).
func estimateBandwidth(videoBitrate, audioBitrate string) int {
	vbr := parseBitrate(videoBitrate)
	abr := parseBitrate(audioBitrate)
	return vbr + abr
}

// parseBitrate converts a bitrate string like "2500k" to bits per second.
// Returns a default for "copy" mode renditions.
func parseBitrate(s string) int {
	if s == "copy" {
		return 5000000 // Default estimate for copy/passthrough
	}
	s = strings.TrimSuffix(s, "k")
	val := 0
	fmt.Sscanf(s, "%d", &val)
	return val * 1000
}

// adjustMaxrate adds ~10% headroom to the video bitrate for VBV maxrate.
// e.g., "2500k" → "2750k"
func adjustMaxrate(bitrate string) string {
	if bitrate == "copy" {
		return "copy"
	}
	s := strings.TrimSuffix(bitrate, "k")
	val := 0
	fmt.Sscanf(s, "%d", &val)
	return fmt.Sprintf("%dk", val+val/10)
}

// adjustBufsize doubles the video bitrate for VBV buffer size.
// e.g., "2500k" → "5000k"
func adjustBufsize(bitrate string) string {
	if bitrate == "copy" {
		return "copy"
	}
	s := strings.TrimSuffix(bitrate, "k")
	val := 0
	fmt.Sscanf(s, "%d", &val)
	return fmt.Sprintf("%dk", val*2)
}

// uploadMasterPlaylistContent uploads dynamic master.m3u8 content to the blob-sidecar.
// This replaces the old uploadMasterPlaylist that used a hardcoded constant.
func (t *Transcoder) uploadMasterPlaylistContent(eventID, content string) error {
	url := strings.TrimSuffix(t.config.IngestURL, "/") + "/hls/" + eventID + "/master.m3u8"

	body := bytes.NewReader([]byte(content))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/vnd.apple.mpegurl")
	if t.config.IngestToken != "" {
		req.Header.Set("Authorization", "Bearer "+t.config.IngestToken)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("PUT master.m3u8: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("PUT master.m3u8: status %d", resp.StatusCode)
	}

	t.logger.Info("master.m3u8 uploaded via HTTP",
		"event_id", eventID,
		"url", url,
		"size", len(content))
	return nil
}

// uploadMasterPlaylist uploads the static master.m3u8 (legacy, for backward compatibility).
func (t *Transcoder) uploadMasterPlaylist(eventID string) error {
	return t.uploadMasterPlaylistContent(eventID, masterPlaylistContent)
}

func writeMasterPlaylist(outputDir string) error {
	masterPath := filepath.Join(outputDir, "master.m3u8")

	// Use explicit Open → Write → Sync → Close to force SMB FLUSH.
	f, err := os.OpenFile(masterPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open master.m3u8: %w", err)
	}
	if _, err := f.WriteString(masterPlaylistContent); err != nil {
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
// Uses hardcoded defaults for backward compatibility with existing tests.
func (t *Transcoder) BuildABRArgsHTTP(rtmpURL, eventID string) []string {
	defaults := DefaultEventTranscoderConfig
	return t.buildABRArgsHTTP(rtmpURL, eventID, &defaults)
}

// BuildCopyArgsHTTP exposes HTTP copy argument construction for testing.
func (t *Transcoder) BuildCopyArgsHTTP(rtmpURL, eventID string) []string {
	defaults := DefaultEventTranscoderConfig
	return t.buildCopyArgsHTTP(rtmpURL, eventID, &defaults)
}

// BuildHTTPOutputPath exposes HTTP output path construction for testing.
func (t *Transcoder) BuildHTTPOutputPath(eventID string) string {
	return t.buildHTTPOutputPath(eventID)
}

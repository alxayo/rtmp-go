package main

// File Transcoder — VOD Upload Pipeline for StreamGate
// ======================================================
// Standalone CLI program that transcodes uploaded video files into
// multi-rendition HLS output in fMP4/CMAF format. Designed to run as
// a one-shot container job (e.g., Azure Container Apps Job).
//
// Pipeline:
//  1. Parse job configuration from environment variables
//  2. Determine source: HTTP URL → download; local path → use directly
//  3. Probe source video for duration (needed for progress reporting)
//  4. Build FFmpeg command for the specified codec
//  5. Run FFmpeg, piping stdout through the progress parser
//  6. On success: POST completion callback, exit 0
//  7. On failure: POST failure callback, exit 1
//
// Storage modes:
//   - Production: AZURE_STORAGE_CONNECTION_STRING is set → download source
//     from blob storage, upload output to blob storage
//   - Development: No connection string → read/write local files only,
//     log what would be uploaded
//
// Usage:
//
//	JOB_ID=abc123 CODEC=h264 SOURCE_BLOB_URL=/path/to/video.mp4 \
//	RENDITIONS='[{"label":"1080p","width":1920,"height":1080,"videoBitrate":"5000k","audioBitrate":"192k"}]' \
//	CALLBACK_URL=http://localhost:3000/api/internal/transcode/callback \
//	file-transcoder

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	// --- Logger setup (matches hls-transcoder: JSON to stderr) ---
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// --- Step 1: Parse and validate configuration ---
	cfg, err := loadConfig()
	if err != nil {
		logger.Error("configuration error", "error", err)
		os.Exit(1)
	}

	logger.Info("file-transcoder starting",
		"job_id", cfg.JobID,
		"event_id", cfg.EventID,
		"codec", cfg.Codec,
		"renditions", len(cfg.Renditions),
		"source", sanitizeSourceURL(cfg.SourceBlobURL),
		"hls_time", cfg.HLSTime,
		"local_mode", cfg.AzureStorageConnectionString == "",
	)

	// --- Step 2: Resolve source file ---
	// If the source is an HTTP URL, download it to a local temp file.
	// If it's a local path, use it directly.
	sourcePath, cleanup, err := resolveSource(cfg, logger)
	if err != nil {
		logger.Error("failed to resolve source", "error", err)
		sendFailureCallback(cfg, err, logger)
		os.Exit(1)
	}
	defer cleanup()

	// --- Step 3: Probe source duration ---
	// We need the total duration to compute progress percentages.
	duration := probeVideoDuration(sourcePath, logger)

	// Update the config with the resolved local source path
	// (buildFFmpegArgs uses cfg.SourceBlobURL as the -i input)
	cfg.SourceBlobURL = sourcePath

	// --- Step 4: Create output directory ---
	// Structure: {OUTPUT_DIR}/{EVENT_ID}/{CODEC}/
	outputDir := filepath.Join(cfg.OutputDir, cfg.EventID, cfg.Codec)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		logger.Error("failed to create output directory", "dir", outputDir, "error", err)
		sendFailureCallback(cfg, err, logger)
		os.Exit(1)
	}

	// Create subdirectories for each rendition (FFmpeg won't create them)
	for i := range cfg.Renditions {
		streamDir := filepath.Join(outputDir, fmt.Sprintf("stream_%d", i))
		if err := os.MkdirAll(streamDir, 0o755); err != nil {
			logger.Error("failed to create stream directory", "dir", streamDir, "error", err)
			sendFailureCallback(cfg, err, logger)
			os.Exit(1)
		}
	}

	// --- Step 5: Build and run FFmpeg ---
	args := buildFFmpegArgs(cfg, outputDir)
	logger.Info("running FFmpeg", "args_count", len(args))
	logger.Debug("FFmpeg command", "args", args)

	cmd := exec.Command("ffmpeg", args...)

	// FFmpeg's -progress pipe:1 writes progress to stdout.
	// FFmpeg's warnings/errors go to stderr (we capture for error reporting).
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logger.Error("failed to create stdout pipe", "error", err)
		sendFailureCallback(cfg, err, logger)
		os.Exit(1)
	}

	// Capture stderr for error messages if FFmpeg fails
	var stderrBuf strings.Builder
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)

	if err := cmd.Start(); err != nil {
		logger.Error("failed to start FFmpeg", "error", err)
		sendFailureCallback(cfg, err, logger)
		os.Exit(1)
	}

	logger.Info("FFmpeg started", "pid", cmd.Process.Pid)

	// Start progress monitoring in background
	reporter := newProgressReporter(cfg, duration, logger)
	go reporter.monitorProgress(stdout)

	// Wait for FFmpeg to complete
	if err := cmd.Wait(); err != nil {
		stderrOutput := stderrBuf.String()
		logger.Error("FFmpeg failed",
			"error", err,
			"stderr", truncate(stderrOutput, 500),
		)
		sendFailureCallback(cfg, fmt.Errorf("FFmpeg failed: %w — %s", err, truncate(stderrOutput, 200)), logger)
		os.Exit(1)
	}

	logger.Info("FFmpeg completed successfully")

	// --- Step 6: Report results ---
	variants := listVariantPlaylists(outputDir, len(cfg.Renditions))

	// In local dev mode (no Azure connection string), just log the output
	if cfg.AzureStorageConnectionString == "" {
		logger.Info("local mode: output files written",
			"output_dir", outputDir,
			"variants", variants,
		)
		logger.Info("local mode: would upload to blob storage",
			"prefix", cfg.OutputBlobPrefix,
			"file_count", countOutputFiles(outputDir),
		)
	}

	// Send success callback to Platform App
	sendSuccessCallback(cfg, duration, variants, logger)

	logger.Info("file-transcoder completed",
		"job_id", cfg.JobID,
		"codec", cfg.Codec,
		"duration", duration,
		"variants", len(variants),
	)
}

// resolveSource determines the source file path. If the source is an HTTP(S)
// URL, it downloads the file to the output directory. Returns the local path,
// a cleanup function, and any error.
//
// In development mode (local file path), cleanup is a no-op.
func resolveSource(cfg *JobConfig, logger *slog.Logger) (string, func(), error) {
	noop := func() {}

	// Check if source is a URL (HTTP/HTTPS)
	if strings.HasPrefix(cfg.SourceBlobURL, "http://") || strings.HasPrefix(cfg.SourceBlobURL, "https://") {
		logger.Info("downloading source video", "url", sanitizeSourceURL(cfg.SourceBlobURL))

		// Download to output directory (not /tmp — container may not have /tmp)
		downloadDir := filepath.Join(cfg.OutputDir, ".downloads")
		if err := os.MkdirAll(downloadDir, 0o755); err != nil {
			return "", noop, fmt.Errorf("create download directory: %w", err)
		}

		destPath := filepath.Join(downloadDir, "source_video")

		if err := downloadFile(cfg.SourceBlobURL, destPath, logger); err != nil {
			return "", noop, fmt.Errorf("download source: %w", err)
		}

		// Cleanup function removes the downloaded file when done
		cleanup := func() {
			if err := os.RemoveAll(downloadDir); err != nil {
				logger.Warn("failed to clean up download", "error", err)
			}
		}

		return destPath, cleanup, nil
	}

	// Local file path — verify it exists
	if _, err := os.Stat(cfg.SourceBlobURL); err != nil {
		return "", noop, fmt.Errorf("source file not found: %w", err)
	}

	return cfg.SourceBlobURL, noop, nil
}

// downloadFile downloads a file from a URL to a local path.
// Supports large files by streaming directly to disk.
func downloadFile(url, destPath string, logger *slog.Logger) error {
	client := &http.Client{Timeout: 30 * time.Minute} // Large files may take a while

	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("HTTP GET failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from source URL", resp.StatusCode)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer out.Close()

	written, err := io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("download write: %w", err)
	}

	logger.Info("source video downloaded", "bytes", written, "path", destPath)
	return nil
}

// sanitizeSourceURL masks query parameters in source URLs for safe logging.
// Blob SAS URLs contain auth tokens that should not appear in logs.
func sanitizeSourceURL(url string) string {
	if idx := strings.Index(url, "?"); idx >= 0 {
		return url[:idx] + "?***"
	}
	return url
}

// countOutputFiles recursively counts all files in a directory.
// Used for logging in local dev mode.
func countOutputFiles(dir string) int {
	count := 0
	filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			count++
		}
		return nil
	})
	return count
}

// truncate limits a string to maxLen characters, appending "..." if truncated.
// Used for including stderr snippets in error messages without excessive length.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

package main

// Progress Parser — FFmpeg Progress Monitoring and Reporting
// ===========================================================
// Parses FFmpeg's machine-readable progress output (from -progress pipe:1)
// and sends periodic updates to the Platform App.
//
// FFmpeg's -progress flag outputs key=value pairs on stdout:
//
//	frame=120
//	fps=30.0
//	stream_0_0_q=28.0
//	total_size=2048000
//	out_time_us=4000000
//	out_time_ms=4000000
//	out_time=00:00:04.000000
//	dup_frames=0
//	drop_frames=0
//	speed=1.5x
//	progress=continue
//
// We parse "out_time" to compute progress percentage against the total
// duration (extracted from FFmpeg's stderr during input analysis).
//
// Progress updates are POSTed every 5 seconds to avoid overwhelming
// the Platform App.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// progressUpdate is the JSON payload sent to the Platform App's progress endpoint.
type progressUpdate struct {
	JobID    string `json:"jobId"`
	Codec    string `json:"codec"`
	Progress int    `json:"progress"` // 0–100 percentage
}

// progressReporter reads FFmpeg's -progress output from stdout and sends
// periodic updates to the Platform App.
type progressReporter struct {
	cfg           *JobConfig
	totalDuration float64 // Total video duration in seconds (from probe)
	logger        *slog.Logger
	client        *http.Client
	lastReport    time.Time // Throttle: last time we sent an update
}

// newProgressReporter creates a reporter that sends updates to the configured
// PROGRESS_URL. If PROGRESS_URL is empty, updates are logged but not sent.
func newProgressReporter(cfg *JobConfig, totalDuration float64, logger *slog.Logger) *progressReporter {
	return &progressReporter{
		cfg:           cfg,
		totalDuration: totalDuration,
		logger:        logger,
		client:        &http.Client{Timeout: 5 * time.Second},
	}
}

// monitorProgress reads from FFmpeg's stdout (the -progress pipe:1 output)
// and sends progress updates. It also copies FFmpeg's stderr through for
// logging.
//
// This function blocks until the reader is closed (FFmpeg exits).
func (p *progressReporter) monitorProgress(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()

		// Parse "out_time=HH:MM:SS.ffffff" lines from -progress output
		if strings.HasPrefix(line, "out_time=") {
			timeStr := strings.TrimPrefix(line, "out_time=")
			seconds := parseFFmpegTime(timeStr)
			if seconds > 0 && p.totalDuration > 0 {
				pct := int((seconds / p.totalDuration) * 100)
				if pct > 100 {
					pct = 100
				}
				p.reportProgress(pct)
			}
		}
	}
}

// reportProgress sends a progress update if enough time has elapsed since
// the last report. This throttling prevents flooding the Platform App with
// updates for fast encodes.
func (p *progressReporter) reportProgress(pct int) {
	// Throttle: send at most one update every 5 seconds
	now := time.Now()
	if now.Sub(p.lastReport) < 5*time.Second {
		return
	}
	p.lastReport = now

	p.logger.Info("transcode progress", "progress", pct, "job_id", p.cfg.JobID)

	// Skip HTTP POST if no progress URL is configured (dev mode)
	if p.cfg.ProgressURL == "" {
		return
	}

	update := progressUpdate{
		JobID:    p.cfg.JobID,
		Codec:    p.cfg.Codec,
		Progress: pct,
	}

	body, err := json.Marshal(update)
	if err != nil {
		p.logger.Warn("failed to marshal progress update", "error", err)
		return
	}

	req, err := http.NewRequest(http.MethodPost, p.cfg.ProgressURL, bytes.NewReader(body))
	if err != nil {
		p.logger.Warn("failed to create progress request", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if p.cfg.InternalAPIKey != "" {
		req.Header.Set("X-Internal-Api-Key", p.cfg.InternalAPIKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		p.logger.Warn("failed to send progress update", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		p.logger.Warn("progress update rejected", "status", resp.StatusCode)
	}
}

// parseFFmpegTime converts an FFmpeg time string to seconds.
//
// FFmpeg outputs time in the format "HH:MM:SS.ffffff" (with microsecond
// precision). This function handles both "00:05:30.500000" and negative
// values like "-00:00:00.000000" (which FFmpeg outputs at start).
//
// Examples:
//
//	"00:05:30.500000" → 330.5
//	"00:00:04.000000" → 4.0
//	"-00:00:00.000000" → 0.0 (negative = not started yet)
func parseFFmpegTime(s string) float64 {
	// Handle negative time values (FFmpeg outputs these during init)
	if strings.HasPrefix(s, "-") {
		return 0
	}

	// Trim whitespace that FFmpeg sometimes includes
	s = strings.TrimSpace(s)

	var hours, minutes, seconds float64
	_, err := fmt.Sscanf(s, "%f:%f:%f", &hours, &minutes, &seconds)
	if err != nil {
		return 0
	}

	return hours*3600 + minutes*60 + seconds
}

// probeVideoDuration uses FFmpeg to determine the total duration of the
// source video file. This is needed to compute progress percentage.
//
// Runs: ffmpeg -i <source> -hide_banner
// FFmpeg prints duration in stderr as: "Duration: 00:05:30.00, ..."
//
// Returns 0 if duration cannot be determined (progress will show 0%).
func probeVideoDuration(sourcePath string, logger *slog.Logger) float64 {
	// Use ffprobe-style invocation: give FFmpeg the input but no output,
	// which causes it to print media info (including Duration) and exit.
	out, _ := exec.Command("ffmpeg", "-i", sourcePath, "-hide_banner", "-f", "null", "-").CombinedOutput()

	// Parse "Duration: HH:MM:SS.ss" from the output
	output := string(out)
	idx := strings.Index(output, "Duration: ")
	if idx < 0 {
		logger.Warn("could not determine video duration from FFmpeg output")
		return 0
	}

	// Extract the time string after "Duration: " up to the next comma
	timeStart := idx + len("Duration: ")
	timeEnd := strings.Index(output[timeStart:], ",")
	if timeEnd < 0 {
		return 0
	}

	timeStr := output[timeStart : timeStart+timeEnd]
	duration := parseFFmpegTime(timeStr)

	logger.Info("source video duration", "duration_seconds", duration, "raw", timeStr)
	return duration
}

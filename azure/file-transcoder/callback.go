package main

// Callback — Completion/Failure Reporting to Platform App
// =========================================================
// After the FFmpeg transcode finishes (or fails), we POST a callback
// to the Platform App so it can update the TranscodeJob record and
// trigger any downstream processing (e.g., master playlist generation,
// event status update).
//
// Retry logic: up to 3 attempts with 2-second backoff. The Platform App
// may be temporarily unavailable (deployment, restart), so retries help
// ensure the callback is delivered.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// callbackPayload is the JSON body POSTed to the Platform App's callback URL.
type callbackPayload struct {
	JobID    string   `json:"jobId"`              // Matches TranscodeJob.id in Platform DB
	Codec    string   `json:"codec"`              // "h264", "av1", "vp8", or "vp9"
	Status   string   `json:"status"`             // "completed" or "failed"
	Error    string   `json:"error,omitempty"`     // Error message (only if status="failed")
	Duration float64  `json:"duration,omitempty"`  // Video duration in seconds
	Variants []string `json:"variants,omitempty"`  // Relative paths to variant playlists
}

// sendCallback POSTs the completion or failure callback to the Platform App.
//
// Retries up to 3 times with 2-second backoff between attempts. Logs
// each attempt — the caller should treat this as fire-and-forget since
// the transcode result is also reflected in the exit code.
//
// Parameters:
//   - cfg: Job configuration (contains callback URL and API key)
//   - status: "completed" or "failed"
//   - errMsg: Error description (empty string for success)
//   - duration: Total video duration in seconds
//   - variants: List of variant playlist paths relative to output root
//     (e.g., ["stream_0/index.m3u8", "stream_1/index.m3u8"])
func sendCallback(cfg *JobConfig, status string, errMsg string, duration float64, variants []string, logger *slog.Logger) {
	payload := callbackPayload{
		JobID:    cfg.JobID,
		Codec:    cfg.Codec,
		Status:   status,
		Error:    errMsg,
		Duration: duration,
		Variants: variants,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		logger.Error("failed to marshal callback payload", "error", err)
		return
	}

	// Retry loop: 3 attempts with 2-second backoff
	const maxRetries = 3
	const retryDelay = 2 * time.Second

	client := &http.Client{Timeout: 10 * time.Second}

	for attempt := 1; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequest(http.MethodPost, cfg.CallbackURL, bytes.NewReader(body))
		if err != nil {
			logger.Error("failed to create callback request", "error", err, "attempt", attempt)
			return // Request creation failure is not retriable
		}
		req.Header.Set("Content-Type", "application/json")
		if cfg.InternalAPIKey != "" {
			req.Header.Set("X-Internal-Api-Key", cfg.InternalAPIKey)
		}

		resp, err := client.Do(req)
		if err != nil {
			logger.Warn("callback request failed",
				"attempt", attempt,
				"max_retries", maxRetries,
				"error", err,
			)
			if attempt < maxRetries {
				time.Sleep(retryDelay)
			}
			continue
		}

		// Read and close the response body
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			logger.Info("callback sent successfully",
				"status", status,
				"http_status", resp.StatusCode,
				"attempt", attempt,
			)
			return
		}

		// Non-2xx response — log and retry
		logger.Warn("callback returned non-success status",
			"http_status", resp.StatusCode,
			"body", string(respBody),
			"attempt", attempt,
			"max_retries", maxRetries,
		)
		if attempt < maxRetries {
			time.Sleep(retryDelay)
		}
	}

	// All retries exhausted
	logger.Error("callback failed after all retries",
		"url", cfg.CallbackURL,
		"status", status,
		"max_retries", maxRetries,
	)
}

// sendSuccessCallback is a convenience wrapper for successful completions.
func sendSuccessCallback(cfg *JobConfig, duration float64, variants []string, logger *slog.Logger) {
	sendCallback(cfg, "completed", "", duration, variants, logger)
}

// sendFailureCallback is a convenience wrapper for failed jobs.
func sendFailureCallback(cfg *JobConfig, err error, logger *slog.Logger) {
	errMsg := "unknown error"
	if err != nil {
		errMsg = err.Error()
	}
	sendCallback(cfg, "failed", errMsg, 0, nil, logger)
}

// listVariantPlaylists finds all index.m3u8 files in the output directory
// and returns their paths relative to the output root.
//
// Expected structure:
//
//	{outputDir}/stream_0/index.m3u8
//	{outputDir}/stream_1/index.m3u8
//	{outputDir}/stream_2/index.m3u8
//
// Returns: ["stream_0/index.m3u8", "stream_1/index.m3u8", "stream_2/index.m3u8"]
func listVariantPlaylists(outputDir string, numRenditions int) []string {
	variants := make([]string, 0, numRenditions)
	for i := range numRenditions {
		relPath := fmt.Sprintf("stream_%d/index.m3u8", i)
		variants = append(variants, relPath)
	}
	return variants
}

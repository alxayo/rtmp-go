package main

// Config — Environment Variable Parsing for VOD File Transcoder
// ===============================================================
// Reads all job configuration from environment variables and validates
// required fields. This is the first step in the transcode pipeline:
// the container orchestrator (e.g., Azure Container Apps Job) sets
// env vars before launching the binary.
//
// Environment variables:
//   - JOB_ID:               Unique job identifier (matches TranscodeJob in Platform DB)
//   - EVENT_ID:             Event UUID for output path construction
//   - CODEC:                Video codec: "h264", "av1", "vp8", or "vp9"
//   - SOURCE_BLOB_URL:      URL or local file path to the source video
//   - OUTPUT_BLOB_PREFIX:   Blob path prefix for uploaded output files
//   - RENDITIONS:           JSON array of rendition specs (resolution, bitrate)
//   - CODEC_CONFIG:         JSON object with codec-specific overrides (optional)
//   - HLS_TIME:             Segment duration in seconds (default: 4)
//   - FORCE_KEYFRAME_INTERVAL: Keyframe interval in seconds (default: 4)
//   - CALLBACK_URL:         URL to POST completion/failure results
//   - PROGRESS_URL:         URL to POST progress updates
//   - INTERNAL_API_KEY:     Auth key for callback/progress requests
//   - AZURE_STORAGE_CONNECTION_STRING: Blob storage connection (optional; local mode if missing)
//   - OUTPUT_DIR:           Local output directory (default: /out/transcode-output)

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

// Rendition defines a single video quality level to produce.
// The transcoder creates one HLS variant stream per rendition.
//
// Example JSON:
//
//	{"label":"1080p","width":1920,"height":1080,"videoBitrate":"5000k","audioBitrate":"192k"}
type Rendition struct {
	Label        string `json:"label"`        // Human-readable name (e.g., "1080p", "720p")
	Width        int    `json:"width"`         // Output width in pixels
	Height       int    `json:"height"`        // Output height in pixels
	VideoBitrate string `json:"videoBitrate"`  // FFmpeg bitrate string (e.g., "5000k")
	AudioBitrate string `json:"audioBitrate"`  // FFmpeg audio bitrate (e.g., "192k")
}

// CodecConfig holds optional codec-specific overrides.
// These allow per-job tuning of encoder parameters beyond the defaults.
type CodecConfig struct {
	// H.264 options
	Preset string `json:"preset,omitempty"` // e.g., "medium", "slow"
	Tune   string `json:"tune,omitempty"`   // e.g., "film", "animation"

	// AV1 options
	CRF int `json:"crf,omitempty"` // Constant rate factor (lower = better quality)

	// VP8/VP9 options
	CPUUsed  int    `json:"cpuUsed,omitempty"`  // Speed vs quality tradeoff
	Deadline string `json:"deadline,omitempty"` // "good", "best", "realtime"
}

// JobConfig holds all configuration for a single transcode job.
// Populated from environment variables by loadConfig().
type JobConfig struct {
	// Job identification
	JobID   string // Unique job ID (from Platform DB)
	EventID string // Event UUID for output path construction

	// Codec selection — determines encoder, audio codec, and FFmpeg flags
	Codec string // "h264", "av1", "vp8", or "vp9"

	// Source and output paths
	SourceBlobURL    string // URL or local path to source video
	OutputBlobPrefix string // Blob storage path prefix for uploads

	// Rendition specs — one HLS variant per entry
	Renditions []Rendition

	// Codec-specific overrides (optional)
	CodecConfig CodecConfig

	// HLS segmentation settings
	HLSTime               int // Segment duration in seconds (default: 4)
	ForceKeyframeInterval int // Keyframe interval in seconds (default: 4)

	// Callback URLs for Platform App communication
	CallbackURL    string // POST completion/failure here
	ProgressURL    string // POST progress updates here
	InternalAPIKey string // Auth header value for callbacks

	// Storage configuration
	AzureStorageConnectionString string // Blob storage access (empty = local dev mode)
	OutputDir                    string // Local output directory
}

// loadConfig reads environment variables and returns a validated JobConfig.
// Returns an error if any required field is missing or invalid.
//
// This function is the single source of truth for configuration — the rest
// of the pipeline trusts that JobConfig is valid if loadConfig() succeeds.
func loadConfig() (*JobConfig, error) {
	cfg := &JobConfig{
		JobID:            os.Getenv("JOB_ID"),
		EventID:          os.Getenv("EVENT_ID"),
		Codec:            os.Getenv("CODEC"),
		SourceBlobURL:    os.Getenv("SOURCE_BLOB_URL"),
		OutputBlobPrefix: os.Getenv("OUTPUT_BLOB_PREFIX"),
		CallbackURL:      os.Getenv("CALLBACK_URL"),
		ProgressURL:      os.Getenv("PROGRESS_URL"),
		InternalAPIKey:   os.Getenv("INTERNAL_API_KEY"),

		AzureStorageConnectionString: os.Getenv("AZURE_STORAGE_CONNECTION_STRING"),
		OutputDir:                    os.Getenv("OUTPUT_DIR"),
	}

	// --- Parse RENDITIONS JSON ---
	// This is a JSON array of objects, e.g.:
	//   [{"label":"1080p","width":1920,"height":1080,"videoBitrate":"5000k","audioBitrate":"192k"}]
	renditionsJSON := os.Getenv("RENDITIONS")
	if renditionsJSON != "" {
		if err := json.Unmarshal([]byte(renditionsJSON), &cfg.Renditions); err != nil {
			return nil, fmt.Errorf("invalid RENDITIONS JSON: %w", err)
		}
	}

	// --- Parse CODEC_CONFIG JSON (optional) ---
	// Allows overriding codec-specific defaults, e.g.:
	//   {"preset":"slow","tune":"animation"}
	codecConfigJSON := os.Getenv("CODEC_CONFIG")
	if codecConfigJSON != "" && codecConfigJSON != "{}" {
		if err := json.Unmarshal([]byte(codecConfigJSON), &cfg.CodecConfig); err != nil {
			return nil, fmt.Errorf("invalid CODEC_CONFIG JSON: %w", err)
		}
	}

	// --- Parse numeric settings with defaults ---
	cfg.HLSTime = parseIntEnv("HLS_TIME", 4)
	cfg.ForceKeyframeInterval = parseIntEnv("FORCE_KEYFRAME_INTERVAL", 4)

	// --- Default output directory ---
	if cfg.OutputDir == "" {
		cfg.OutputDir = "/out/transcode-output"
	}

	// --- Validate required fields ---
	if cfg.JobID == "" {
		return nil, fmt.Errorf("JOB_ID is required")
	}
	if cfg.Codec == "" {
		return nil, fmt.Errorf("CODEC is required")
	}
	if cfg.Codec != "h264" && cfg.Codec != "av1" && cfg.Codec != "vp8" && cfg.Codec != "vp9" {
		return nil, fmt.Errorf("CODEC must be one of: h264, av1, vp8, vp9 (got %q)", cfg.Codec)
	}
	if cfg.SourceBlobURL == "" {
		return nil, fmt.Errorf("SOURCE_BLOB_URL is required")
	}
	if cfg.CallbackURL == "" {
		return nil, fmt.Errorf("CALLBACK_URL is required")
	}
	if len(cfg.Renditions) == 0 {
		return nil, fmt.Errorf("RENDITIONS must contain at least one entry")
	}

	// Validate each rendition has required fields
	for i, r := range cfg.Renditions {
		if r.Width <= 0 || r.Height <= 0 {
			return nil, fmt.Errorf("RENDITIONS[%d]: width and height must be positive", i)
		}
		if r.VideoBitrate == "" {
			return nil, fmt.Errorf("RENDITIONS[%d]: videoBitrate is required", i)
		}
		if r.AudioBitrate == "" {
			return nil, fmt.Errorf("RENDITIONS[%d]: audioBitrate is required", i)
		}
	}

	return cfg, nil
}

// parseIntEnv reads an integer from an environment variable, returning
// the default value if the variable is empty or unparseable.
func parseIntEnv(key string, defaultVal int) int {
	s := os.Getenv(key)
	if s == "" {
		return defaultVal
	}
	val, err := strconv.Atoi(s)
	if err != nil {
		return defaultVal
	}
	return val
}

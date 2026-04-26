package main

// Config Types for Per-Event Stream Configuration
// =================================================
// These types mirror the TypeScript models in shared/src/stream-config.ts.
// They represent the stream configuration fetched from the Platform API.
//
// IMPORTANT: The existing TranscoderConfig struct in transcoder.go holds
// infrastructure config (HLSDir, RTMPHost, etc.). These types are for
// stream-quality config (hlsTime, profile, h264 settings, etc.) and use
// distinct names to avoid collision.
//
// Both Go and TypeScript definitions must stay in sync.
// The JSON schema contract (§0.1 in the plan) is the source of truth.

// EventTranscoderConfig holds per-event transcoder settings fetched from the Platform API.
// These control how FFmpeg encodes a specific live stream.
//
// Named "EventTranscoderConfig" (not "TranscoderConfig") because the existing
// TranscoderConfig struct in transcoder.go holds infrastructure settings.
type EventTranscoderConfig struct {
	// Codecs lists which video codecs to encode. Currently only ["h264"] is active.
	Codecs []string `json:"codecs"`

	// Profile is the rendition preset name (e.g., "full-abr-1080p-720p-480p").
	// The transcoder looks up this name in the RenderProfiles map to get
	// the list of renditions (resolution, bitrate, copy vs transcode).
	Profile string `json:"profile"`

	// HLSTime is the duration of each HLS segment in seconds.
	// Lower values = less latency but more HTTP overhead. Default: 2.
	HLSTime int `json:"hlsTime"`

	// HLSListSize is how many segments to keep in the live playlist.
	// 6 segments × 2s = 12s rewind window. Default: 6.
	HLSListSize int `json:"hlsListSize"`

	// ForceKeyFrameInterval is seconds between forced keyframes.
	// Must be ≤ HLSTime for clean segment boundaries. Default: 2.
	ForceKeyFrameInterval int `json:"forceKeyFrameInterval"`

	// H264 contains H.264-specific encoding options.
	H264 H264Config `json:"h264"`
}

// H264Config holds H.264 codec-specific encoding options.
type H264Config struct {
	// Tune controls encoder latency behavior:
	//   "zerolatency" — disables B-frames, reduces buffering (adds ~5% bitrate)
	//   "none"        — default encoder behavior
	// Only affects transcoded renditions; copy/passthrough renditions ignore this.
	Tune string `json:"tune"`

	// Preset controls encoding speed vs compression tradeoff:
	//   "ultrafast"  — fastest encode, worst compression, lowest CPU
	//   "superfast"  — middle ground
	//   "veryfast"   — better quality, more CPU
	// Only affects transcoded renditions.
	Preset string `json:"preset"`
}

// EventPlayerConfig holds per-event player settings (hls.js configuration).
// These are fetched alongside transcoder config but used by the Platform's
// VideoPlayer component, not by the transcoder. The transcoder receives them
// in the API response but doesn't use them directly.
type EventPlayerConfig struct {
	LiveSyncDurationCount       int  `json:"liveSyncDurationCount"`
	LiveMaxLatencyDurationCount int  `json:"liveMaxLatencyDurationCount"`
	BackBufferLength            int  `json:"backBufferLength"`
	LowLatencyMode              bool `json:"lowLatencyMode"`
}

// StreamConfigResponse is the full response from GET /api/internal/events/:id/stream-config.
// The transcoder uses this to configure FFmpeg for a specific event.
type StreamConfigResponse struct {
	EventID      string                `json:"eventId"`
	EventActive  bool                  `json:"eventActive"`
	ConfigSource string                `json:"configSource"` // "event" or "system-default"
	Transcoder   EventTranscoderConfig `json:"transcoder"`
	Player       EventPlayerConfig     `json:"player"`
}

// SystemDefaultsResponse is the response from GET /api/internal/stream-config/defaults.
// The transcoder caches this at startup for fallback when per-event fetches fail.
type SystemDefaultsResponse struct {
	Transcoder EventTranscoderConfig `json:"transcoder"`
	Player     EventPlayerConfig     `json:"player"`
}

// RenderProfile defines a single video rendition (quality level) in a profile.
type RenderProfile struct {
	Label        string `json:"label"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	VideoBitrate string `json:"videoBitrate"` // e.g., "2500k" or "copy"
	AudioBitrate string `json:"audioBitrate"` // e.g., "128k" or "copy"
	Mode         string `json:"mode"`         // "copy" or "transcode"
}

// RenderProfiles maps profile names to their rendition lists.
// This must stay in sync with the TypeScript RENDER_PROFILES constant
// in shared/src/stream-config.ts.
var RenderProfiles = map[string][]RenderProfile{
	"passthrough-only": {
		{Label: "1080p (source)", Width: 1920, Height: 1080, VideoBitrate: "copy", AudioBitrate: "copy", Mode: "copy"},
	},
	"low-latency-720p-480p": {
		{Label: "720p", Width: 1280, Height: 720, VideoBitrate: "2500k", AudioBitrate: "128k", Mode: "transcode"},
		{Label: "480p", Width: 854, Height: 480, VideoBitrate: "1000k", AudioBitrate: "96k", Mode: "transcode"},
	},
	"low-latency-1080p-720p-480p": {
		{Label: "1080p", Width: 1920, Height: 1080, VideoBitrate: "5000k", AudioBitrate: "192k", Mode: "transcode"},
		{Label: "720p", Width: 1280, Height: 720, VideoBitrate: "2500k", AudioBitrate: "128k", Mode: "transcode"},
		{Label: "480p", Width: 854, Height: 480, VideoBitrate: "1000k", AudioBitrate: "96k", Mode: "transcode"},
	},
	"full-abr-1080p-720p-480p": {
		{Label: "1080p (source)", Width: 1920, Height: 1080, VideoBitrate: "copy", AudioBitrate: "copy", Mode: "copy"},
		{Label: "720p", Width: 1280, Height: 720, VideoBitrate: "2500k", AudioBitrate: "128k", Mode: "transcode"},
		{Label: "480p", Width: 854, Height: 480, VideoBitrate: "1000k", AudioBitrate: "96k", Mode: "transcode"},
	},
}

// DefaultEventTranscoderConfig is the hardcoded fallback when both the Platform API
// and the cached system defaults are unavailable. These values match the TypeScript
// DEFAULT_TRANSCODER_CONFIG constant in shared/src/stream-config.ts.
var DefaultEventTranscoderConfig = EventTranscoderConfig{
	Codecs:                []string{"h264"},
	Profile:               "full-abr-1080p-720p-480p",
	HLSTime:               2,
	HLSListSize:           6,
	ForceKeyFrameInterval: 2,
	H264:                  H264Config{Tune: "zerolatency", Preset: "ultrafast"},
}

// DefaultEventPlayerConfig is the hardcoded fallback for player settings.
// Matches DEFAULT_PLAYER_CONFIG in shared/src/stream-config.ts.
var DefaultEventPlayerConfig = EventPlayerConfig{
	LiveSyncDurationCount:       2,
	LiveMaxLatencyDurationCount: 4,
	BackBufferLength:            0,
	LowLatencyMode:              true,
}

package main

// FFmpeg Command Builder for VOD File Transcoding
// =================================================
// Constructs FFmpeg arguments for multi-rendition HLS output in fMP4/CMAF
// format. Supports four codecs (H.264, AV1, VP8, VP9) with appropriate
// encoder and audio codec selection.
//
// Key differences from the live hls-transcoder:
//   - Uses fMP4/CMAF container (-hls_segment_type fmp4) instead of MPEG-TS
//   - Produces VOD playlists (-hls_playlist_type vod) with #EXT-X-ENDLIST
//   - No live-specific flags (no hls_list_size limit, no hls_flags delete_segments)
//   - Input is a file (not RTMP stream), so no error concealment flags needed
//   - Uses -progress pipe:1 for machine-readable progress output

import (
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
)

// buildFFmpegArgs constructs the full FFmpeg argument list for a VOD transcode job.
//
// The output structure uses -var_stream_map to produce one directory per rendition:
//
//	{outputDir}/stream_0/   ← highest quality (e.g., 1080p)
//	{outputDir}/stream_0/init.mp4
//	{outputDir}/stream_0/seg_00000.m4s
//	{outputDir}/stream_0/index.m3u8
//	{outputDir}/stream_1/   ← next quality (e.g., 720p)
//	...
//
// Each index.m3u8 contains #EXT-X-ENDLIST (VOD) and #EXT-X-MAP:URI="init.mp4" (fMP4).
func buildFFmpegArgs(cfg *JobConfig, outputDir string) []string {
	args := []string{
		"-hide_banner",

		// Use -progress to get machine-readable progress on stdout.
		// This outputs key=value lines (including out_time) that the
		// progress parser reads to compute percentage.
		"-progress", "pipe:1",

		// Log warnings and errors to stderr (progress parser ignores these)
		"-loglevel", "warning",

		// Read input at native frame rate to avoid overloading the encoder.
		// Not strictly necessary for local files but helpful for URL sources
		// where download speed >> encode speed could cause buffering issues.
		"-nostdin",

		// Input file (local path or downloaded temp file)
		"-i", cfg.SourceBlobURL,
	}

	// --- Map inputs: one video + one audio per rendition ---
	// FFmpeg needs explicit -map entries for each output stream.
	// For 3 renditions, we map the single input video/audio 3 times.
	for range cfg.Renditions {
		args = append(args, "-map", "0:v:0", "-map", "0:a:0?")
	}

	// --- Per-rendition encoding settings ---
	// Each rendition gets codec, resolution, bitrate, and buffer settings.
	videoEncoder, videoArgs := buildVideoEncoderArgs(cfg)
	audioCodec, audioArgs := buildAudioCodecArgs(cfg)

	for i, r := range cfg.Renditions {
		vi := fmt.Sprintf(":v:%d", i)
		ai := fmt.Sprintf(":a:%d", i)

		// Video codec and encoder-specific flags (applied once, shared across renditions)
		args = append(args, "-c"+vi, videoEncoder)

		// Per-rendition resolution and bitrate
		args = append(args,
			"-s"+vi, fmt.Sprintf("%dx%d", r.Width, r.Height),
			"-b"+vi, r.VideoBitrate,
			"-maxrate"+vi, r.VideoBitrate,
			"-bufsize"+vi, doubleBitrate(r.VideoBitrate),
		)

		// Audio codec and bitrate
		args = append(args, "-c"+ai, audioCodec)
		args = append(args, "-b"+ai, r.AudioBitrate)
		args = append(args, "-ar"+ai, "48000")
	}

	// --- Encoder-specific global flags ---
	// These apply to all renditions of the given codec
	args = append(args, videoArgs...)
	args = append(args, audioArgs...)

	// --- Keyframe alignment ---
	// Forces keyframes at regular intervals so every HLS segment starts with
	// a keyframe. This is critical for adaptive bitrate switching — the player
	// can only switch renditions at keyframe boundaries.
	args = append(args,
		"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", cfg.ForceKeyframeInterval),
		"-sc_threshold", "0", // Disable scene-change detection (would insert extra keyframes)
	)

	// --- HLS output settings (fMP4/CMAF) ---
	// fMP4 is the modern HLS container format (replacing MPEG-TS):
	//   - Better compression (no 188-byte TS packet overhead)
	//   - Required for AV1/VP9 in HLS (MPEG-TS doesn't support these codecs)
	//   - Compatible with DASH (dual-format from single encode)
	args = append(args,
		"-f", "hls",
		"-hls_time", fmt.Sprintf("%d", cfg.HLSTime),
		"-hls_segment_type", "fmp4",                   // Use fragmented MP4 segments (.m4s)
		"-hls_fmp4_init_filename", "init.mp4",          // Name of the initialization segment
		"-hls_flags", "independent_segments",            // Each segment is independently decodable
		"-hls_playlist_type", "vod",                     // VOD playlist (includes #EXT-X-ENDLIST)
		"-hls_segment_filename", filepath.Join(outputDir, "stream_%v", "seg_%05d.m4s"),
	)

	// --- Multi-rendition stream map ---
	// Tells FFmpeg which streams belong to which variant. Produces
	// stream_0/, stream_1/, etc. directories automatically.
	varMap := buildVarStreamMap(len(cfg.Renditions))
	args = append(args, "-var_stream_map", varMap)

	// --- Output playlist pattern ---
	// The %v is replaced by FFmpeg with the variant number (0, 1, 2, etc.)
	args = append(args, filepath.Join(outputDir, "stream_%v", "index.m3u8"))

	return args
}

// buildVideoEncoderArgs returns the FFmpeg video encoder name and any
// codec-specific flags based on the job's CODEC setting.
//
// Codec selection guide:
//   - h264:  Widest compatibility (all browsers/devices). Uses libx264.
//   - av1:   Best compression (~30% smaller than H.264 at same quality).
//            Uses libsvtav1 (fast) or libaom-av1 (fallback).
//   - vp8:   Legacy WebM codec. Uses libvpx.
//   - vp9:   Good compression, wide browser support. Uses libvpx-vp9.
func buildVideoEncoderArgs(cfg *JobConfig) (encoder string, args []string) {
	switch cfg.Codec {
	case "h264":
		// libx264: the gold standard for H.264 encoding
		encoder = "libx264"
		preset := cfg.CodecConfig.Preset
		if preset == "" {
			preset = "medium" // Good balance of speed and quality for VOD
		}
		tune := cfg.CodecConfig.Tune
		if tune == "" {
			tune = "film" // Optimized for live-action video content
		}
		args = []string{"-preset", preset, "-tune", tune}

	case "av1":
		// SVT-AV1 is significantly faster than libaom for comparable quality.
		// We detect availability at startup (see detectAV1Encoder) and fall
		// back to libaom-av1 if SVT-AV1 is not installed.
		encoder = detectAV1Encoder()
		crf := cfg.CodecConfig.CRF
		if crf == 0 {
			crf = 30 // Default CRF for AV1 (lower = better quality, bigger files)
		}
		if encoder == "libsvtav1" {
			// SVT-AV1: preset 6 is a good speed/quality tradeoff for VOD
			args = []string{"-preset", "6", "-crf", fmt.Sprintf("%d", crf)}
		} else {
			// libaom-av1: cpu-used 4 is roughly equivalent to SVT preset 6
			args = []string{"-cpu-used", "4", "-crf", fmt.Sprintf("%d", crf), "-strict", "experimental"}
		}

	case "vp8":
		// libvpx: VP8 encoder (legacy, but still useful for older devices)
		encoder = "libvpx"
		deadline := cfg.CodecConfig.Deadline
		if deadline == "" {
			deadline = "good"
		}
		cpuUsed := cfg.CodecConfig.CPUUsed
		if cpuUsed == 0 {
			cpuUsed = 2
		}
		args = []string{"-deadline", deadline, "-cpu-used", fmt.Sprintf("%d", cpuUsed)}

	case "vp9":
		// libvpx-vp9: VP9 encoder (better compression than VP8)
		encoder = "libvpx-vp9"
		deadline := cfg.CodecConfig.Deadline
		if deadline == "" {
			deadline = "good"
		}
		cpuUsed := cfg.CodecConfig.CPUUsed
		if cpuUsed == 0 {
			cpuUsed = 2
		}
		args = []string{"-deadline", deadline, "-cpu-used", fmt.Sprintf("%d", cpuUsed)}
	}

	return encoder, args
}

// buildAudioCodecArgs returns the audio codec name and any extra flags.
//
// Audio codec pairing:
//   - H.264 → AAC:  Most compatible audio codec for HLS
//   - AV1/VP8/VP9 → Opus: Modern, efficient codec (better quality at lower bitrates)
func buildAudioCodecArgs(cfg *JobConfig) (codec string, args []string) {
	switch cfg.Codec {
	case "h264":
		// AAC is the standard audio codec for H.264/HLS content
		codec = "aac"
	default:
		// Opus is the preferred audio codec for AV1/VP8/VP9
		// Better quality than AAC at equivalent bitrates
		codec = "libopus"
	}
	return codec, nil
}

// buildVarStreamMap constructs the -var_stream_map value for FFmpeg.
// For n renditions, it produces: "v:0,a:0 v:1,a:1 v:2,a:2 ..."
//
// This tells FFmpeg how to group video and audio streams into HLS variants.
// Each "v:N,a:N" pair becomes a separate stream_N/ directory.
func buildVarStreamMap(numRenditions int) string {
	parts := make([]string, numRenditions)
	for i := range numRenditions {
		parts[i] = fmt.Sprintf("v:%d,a:%d", i, i)
	}
	return strings.Join(parts, " ")
}

// detectAV1Encoder checks which AV1 encoder is available in the FFmpeg
// installation. Prefers SVT-AV1 (libsvtav1) for its speed advantage,
// falling back to libaom-av1 which is more commonly available in
// package managers (including Alpine's ffmpeg package).
//
// This is called once at startup — the result doesn't change during
// the process lifetime.
func detectAV1Encoder() string {
	// Check if SVT-AV1 is available by querying FFmpeg's encoder list
	out, err := exec.Command("ffmpeg", "-hide_banner", "-encoders").Output()
	if err != nil {
		slog.Warn("failed to query FFmpeg encoders, falling back to libaom-av1", "error", err)
		return "libaom-av1"
	}

	if strings.Contains(string(out), "libsvtav1") {
		slog.Info("AV1 encoder: using libsvtav1 (SVT-AV1)")
		return "libsvtav1"
	}

	slog.Info("AV1 encoder: using libaom-av1 (SVT-AV1 not available)")
	return "libaom-av1"
}

// doubleBitrate takes a bitrate string like "5000k" and returns "10000k".
// Used for -bufsize which is conventionally 2× the target bitrate.
//
// The VBV buffer size controls how much the bitrate can spike above the
// target. A 2× buffer allows short bursts (e.g., scene changes) while
// keeping the average close to the target.
func doubleBitrate(bitrate string) string {
	s := strings.TrimSuffix(bitrate, "k")
	val := 0
	fmt.Sscanf(s, "%d", &val)
	return fmt.Sprintf("%dk", val*2)
}

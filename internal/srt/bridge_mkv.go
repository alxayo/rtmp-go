package srt

// This file implements Matroska/WebM frame handling for the SRT-to-RTMP bridge.
//
// When the SRT bridge detects a Matroska container (instead of MPEG-TS), it
// routes data through the MKV demuxer and dispatches frames here. Each codec
// gets its own handler that converts MKV frame data into Enhanced RTMP tags.
//
// Key differences from the MPEG-TS path (bridge.go):
//
//   - Timestamps are in milliseconds (not 90kHz clock units)
//   - H.264/H.265 NALUs are length-prefixed (AVCC format), NOT Annex B
//   - AAC frames are raw (no ADTS headers)
//   - VP8/VP9/AV1/Opus/FLAC are new codecs not available via MPEG-TS
//
// The bridge struct holds MKV-specific state (separate from TS state) to
// track sequence headers and timestamps for each codec.

import (
	"github.com/alxayo/go-rtmp/internal/codec"
	"github.com/alxayo/go-rtmp/internal/mkv"
)

// onMKVFrame is the main dispatcher for frames from the MKV demuxer.
// It routes each frame to the appropriate codec-specific handler based
// on the Codec field (e.g., "VP9", "H264", "Opus").
//
// This is the MKV equivalent of onFrame() which dispatches TS frames.
func (b *Bridge) onMKVFrame(frame *mkv.Frame) {
	if frame.IsVideo {
		b.handleMKVVideo(frame)
	} else {
		b.handleMKVAudio(frame)
	}
}

// handleMKVVideo dispatches video frames to codec-specific handlers.
// Each handler is responsible for:
//   1. Sending the sequence header (on first frame, using CodecPrivate)
//   2. Converting the frame data to Enhanced RTMP format
//   3. Pushing the RTMP message to subscribers
func (b *Bridge) handleMKVVideo(frame *mkv.Frame) {
	switch frame.Codec {
	case "VP8":
		b.handleMKVVP8(frame)
	case "VP9":
		b.handleMKVVP9(frame)
	case "AV1":
		b.handleMKVAV1(frame)
	case "H264":
		b.handleMKVH264(frame)
	case "H265":
		b.handleMKVH265(frame)
	default:
		b.log.Debug("unsupported MKV video codec", "codec", frame.Codec)
	}
}

// handleMKVAudio dispatches audio frames to codec-specific handlers.
func (b *Bridge) handleMKVAudio(frame *mkv.Frame) {
	switch frame.Codec {
	case "Opus":
		b.handleMKVOpus(frame)
	case "FLAC":
		b.handleMKVFLAC(frame)
	case "AAC":
		b.handleMKVAAC(frame)
	case "AC3":
		b.handleMKVAC3(frame)
	case "EAC3":
		b.handleMKVEAC3(frame)
	default:
		b.log.Debug("unsupported MKV audio codec", "codec", frame.Codec)
	}
}

// ─── Video codec handlers ───────────────────────────────────────────────────

// handleMKVVP8 processes VP8 video frames from MKV.
// VP8 is self-describing (no decoder configuration record needed), so the
// sequence header is just a FourCC announcement. Frame data passes through
// directly to the Enhanced RTMP tag builder.
func (b *Bridge) handleMKVVP8(frame *mkv.Frame) {
	// Send VP8 sequence header on first frame
	if !b.mkvVideoSeqSent {
		seqHeader := codec.BuildVP8SequenceHeader()
		b.pushVideo(seqHeader, 0)
		b.mkvVideoSeqSent = true

		b.log.Info("sent VP8 sequence header (MKV)")
	}

	// Calculate RTMP timestamp relative to first video frame
	rtmpTS := b.mkvVideoTimestamp(frame.Timestamp)

	// Build Enhanced RTMP video tag — use demuxer's keyframe detection
	payload := codec.BuildVP8VideoFrame(frame.Data, frame.IsKey)
	b.pushVideo(payload, rtmpTS)
}

// handleMKVVP9 processes VP9 video frames from MKV.
// VP9 may include a VPCodecConfigurationRecord in CodecPrivate.
// If present, it's sent as part of the sequence header.
func (b *Bridge) handleMKVVP9(frame *mkv.Frame) {
	// Send VP9 sequence header on first frame (CodecPrivate is optional for VP9)
	if !b.mkvVideoSeqSent {
		seqHeader := codec.BuildVP9SequenceHeader(frame.CodecPrivate)
		b.pushVideo(seqHeader, 0)
		b.mkvVideoSeqSent = true

		b.log.Info("sent VP9 sequence header (MKV)",
			"config_len", len(frame.CodecPrivate),
		)
	}

	rtmpTS := b.mkvVideoTimestamp(frame.Timestamp)
	payload := codec.BuildVP9VideoFrame(frame.Data, frame.IsKey)
	b.pushVideo(payload, rtmpTS)
}

// handleMKVAV1 processes AV1 video frames from MKV.
// AV1 CodecPrivate contains the AV1CodecConfigurationRecord (with the
// sequence header OBU that decoders need for initialization).
func (b *Bridge) handleMKVAV1(frame *mkv.Frame) {
	// Send AV1 sequence header on first frame — CodecPrivate is the config record
	if !b.mkvVideoSeqSent {
		if frame.CodecPrivate == nil {
			b.log.Warn("AV1 frame without CodecPrivate, waiting for config")
			return
		}

		seqHeader := codec.BuildAV1SequenceHeader(frame.CodecPrivate)
		b.pushVideo(seqHeader, 0)
		b.mkvVideoSeqSent = true

		b.log.Info("sent AV1 sequence header (MKV)",
			"config_len", len(frame.CodecPrivate),
		)
	}

	rtmpTS := b.mkvVideoTimestamp(frame.Timestamp)
	payload := codec.BuildAV1VideoFrame(frame.Data, frame.IsKey)
	b.pushVideo(payload, rtmpTS)
}

// handleMKVH264 processes H.264/AVC video frames from MKV.
//
// IMPORTANT: H.264 in MKV is fundamentally different from MPEG-TS:
//   - MKV: NALUs are length-prefixed (AVCC format), CodecPrivate is the
//     AVCDecoderConfigurationRecord containing SPS, PPS, and NALU length size.
//   - MPEG-TS: NALUs use Annex B start codes, SPS/PPS are inline in the stream.
//
// We parse the AVCDecoderConfigurationRecord from CodecPrivate to get SPS/PPS,
// then reconstruct the RTMP sequence header using the existing builder (which
// always uses 4-byte NALU lengths). Frame NALUs are split using the MKV length
// size and re-encoded to 4-byte lengths via ToAVCC().
func (b *Bridge) handleMKVH264(frame *mkv.Frame) {
	// On first frame, parse CodecPrivate (AVCDecoderConfigurationRecord)
	// and send the RTMP sequence header with SPS + PPS.
	if !b.mkvVideoSeqSent {
		if frame.CodecPrivate == nil {
			b.log.Warn("H.264 frame without CodecPrivate, waiting for config")
			return
		}

		// Parse the AVCDecoderConfigurationRecord to extract SPS, PPS,
		// and the NALU length field size used in frame data.
		config, err := codec.ParseAVCDecoderConfig(frame.CodecPrivate)
		if err != nil {
			b.log.Warn("failed to parse AVC decoder config", "error", err)
			return
		}

		// Store the NALU length size — needed to split frame data later.
		// MKV may use 1, 2, 3, or 4 byte length fields; RTMP always uses 4.
		b.mkvNALULenSize = config.NALULengthSize

		// Build RTMP sequence header from extracted SPS + PPS.
		// This reconstructs the AVCDecoderConfigurationRecord with 4-byte
		// NALU lengths, which is what RTMP subscribers expect.
		seqHeader := codec.BuildAVCSequenceHeader(config.SPS, config.PPS)
		b.pushVideo(seqHeader, 0)
		b.mkvVideoSeqSent = true
		b.videoCodec = "H264"

		b.log.Info("sent H.264 sequence header (MKV)",
			"sps_len", len(config.SPS),
			"pps_len", len(config.PPS),
			"nalu_len_size", config.NALULengthSize,
		)
	}

	if b.mkvNALULenSize == 0 {
		return // No config parsed yet
	}

	// Split the length-prefixed frame data into individual NALUs.
	// MKV stores H.264 as [length][NALU][length][NALU]... where each
	// length field is mkvNALULenSize bytes (from the config record).
	nalus := codec.SplitLengthPrefixed(frame.Data, b.mkvNALULenSize)
	if len(nalus) == 0 {
		return
	}

	// Filter out parameter set NALUs (SPS, PPS, AUD) — these are already
	// sent in the sequence header and shouldn't appear in coded frames.
	var frameNalus [][]byte
	for _, nalu := range nalus {
		switch codec.NALUType(nalu) {
		case codec.NALUTypeSPS, codec.NALUTypePPS, codec.NALUTypeAUD:
			continue // Skip parameter sets and delimiters
		default:
			frameNalus = append(frameNalus, nalu)
		}
	}

	if len(frameNalus) == 0 {
		return
	}

	rtmpTS := b.mkvVideoTimestamp(frame.Timestamp)

	// CTS = 0 for live streaming (no B-frame reordering support from MKV).
	// MKV frames only have a single timestamp (PTS), not separate DTS/PTS.
	payload := codec.BuildAVCVideoFrame(frameNalus, frame.IsKey, 0)
	b.pushVideo(payload, rtmpTS)
}

// handleMKVH265 processes H.265/HEVC video frames from MKV.
//
// Similar to H.264 but uses HEVCDecoderConfigurationRecord with three
// parameter sets (VPS + SPS + PPS) instead of two (SPS + PPS).
// NALUs are still length-prefixed in MKV, same as H.264.
func (b *Bridge) handleMKVH265(frame *mkv.Frame) {
	// On first frame, parse CodecPrivate (HEVCDecoderConfigurationRecord)
	// and send the RTMP sequence header with VPS + SPS + PPS.
	if !b.mkvVideoSeqSent {
		if frame.CodecPrivate == nil {
			b.log.Warn("H.265 frame without CodecPrivate, waiting for config")
			return
		}

		// Parse the HEVCDecoderConfigurationRecord to extract VPS, SPS, PPS,
		// and the NALU length field size used in frame data.
		config, err := codec.ParseHEVCDecoderConfig(frame.CodecPrivate)
		if err != nil {
			b.log.Warn("failed to parse HEVC decoder config", "error", err)
			return
		}

		// Store the NALU length size for frame data splitting
		b.mkvNALULenSize = config.NALULengthSize

		// Build RTMP sequence header from extracted VPS + SPS + PPS.
		// This reconstructs the HEVCDecoderConfigurationRecord with 4-byte
		// NALU lengths for RTMP compatibility.
		seqHeader := codec.BuildHEVCSequenceHeader(config.VPS, config.SPS, config.PPS)
		b.pushVideo(seqHeader, 0)
		b.mkvVideoSeqSent = true
		b.videoCodec = "H265"

		b.log.Info("sent H.265 sequence header (MKV)",
			"vps_len", len(config.VPS),
			"sps_len", len(config.SPS),
			"pps_len", len(config.PPS),
			"nalu_len_size", config.NALULengthSize,
		)
	}

	if b.mkvNALULenSize == 0 {
		return // No config parsed yet
	}

	// Split the length-prefixed frame data into individual NALUs
	nalus := codec.SplitLengthPrefixed(frame.Data, b.mkvNALULenSize)
	if len(nalus) == 0 {
		return
	}

	// Filter out parameter set NALUs (VPS=32, SPS=33, PPS=34, AUD=35)
	var frameNalus [][]byte
	for _, nalu := range nalus {
		naluType := codec.H265NALUType(nalu)
		if naluType == 32 || naluType == 33 || naluType == 34 || naluType == 35 {
			continue // Skip VPS, SPS, PPS, AUD
		}
		frameNalus = append(frameNalus, nalu)
	}

	if len(frameNalus) == 0 {
		return
	}

	rtmpTS := b.mkvVideoTimestamp(frame.Timestamp)

	// CTS = 0 for live streaming (same rationale as H.264)
	payload := codec.BuildHEVCVideoFrame(frameNalus, frame.IsKey, 0)
	b.pushVideo(payload, rtmpTS)
}

// ─── Audio codec handlers ───────────────────────────────────────────────────

// handleMKVOpus processes Opus audio frames from MKV.
// Opus CodecPrivate contains the OpusHead structure (typically 19 bytes)
// with channel count, sample rate, and pre-skip information.
func (b *Bridge) handleMKVOpus(frame *mkv.Frame) {
	// Send Opus sequence header on first frame
	if !b.mkvAudioSeqSent {
		if frame.CodecPrivate == nil {
			b.log.Warn("Opus frame without CodecPrivate, waiting for config")
			return
		}

		seqHeader := codec.BuildOpusSequenceHeader(frame.CodecPrivate)
		b.pushAudio(seqHeader, 0)
		b.mkvAudioSeqSent = true

		b.log.Info("sent Opus sequence header (MKV)",
			"config_len", len(frame.CodecPrivate),
		)
	}

	rtmpTS := b.mkvAudioTimestamp(frame.Timestamp)
	payload := codec.BuildOpusAudioFrame(frame.Data)
	b.pushAudio(payload, rtmpTS)
}

// handleMKVFLAC processes FLAC audio frames from MKV.
// FLAC CodecPrivate contains the "fLaC" marker + METADATA_BLOCK_HEADER +
// STREAMINFO block (typically ~42 bytes total).
func (b *Bridge) handleMKVFLAC(frame *mkv.Frame) {
	// Send FLAC sequence header on first frame
	if !b.mkvAudioSeqSent {
		if frame.CodecPrivate == nil {
			b.log.Warn("FLAC frame without CodecPrivate, waiting for config")
			return
		}

		seqHeader := codec.BuildFLACSequenceHeader(frame.CodecPrivate)
		b.pushAudio(seqHeader, 0)
		b.mkvAudioSeqSent = true

		b.log.Info("sent FLAC sequence header (MKV)",
			"config_len", len(frame.CodecPrivate),
		)
	}

	rtmpTS := b.mkvAudioTimestamp(frame.Timestamp)
	payload := codec.BuildFLACAudioFrame(frame.Data)
	b.pushAudio(payload, rtmpTS)
}

// handleMKVAAC processes AAC audio frames from MKV.
//
// IMPORTANT: AAC in MKV is different from MPEG-TS:
//   - MKV: CodecPrivate = AudioSpecificConfig (typically 2 bytes), frames are raw AAC
//   - MPEG-TS: Each frame has an ADTS header that we strip to get raw AAC
//
// Since MKV gives us the AudioSpecificConfig directly, we use
// BuildAACSequenceHeaderFromConfig() instead of BuildAACSequenceHeader().
func (b *Bridge) handleMKVAAC(frame *mkv.Frame) {
	// Send AAC sequence header on first frame using raw AudioSpecificConfig
	if !b.mkvAudioSeqSent {
		if frame.CodecPrivate == nil {
			b.log.Warn("AAC frame without CodecPrivate, waiting for config")
			return
		}

		// Wrap the raw AudioSpecificConfig in RTMP audio tag format
		seqHeader := codec.BuildAACSequenceHeaderFromConfig(frame.CodecPrivate)
		b.pushAudio(seqHeader, 0)
		b.mkvAudioSeqSent = true

		b.log.Info("sent AAC sequence header (MKV)",
			"config_len", len(frame.CodecPrivate),
		)
	}

	rtmpTS := b.mkvAudioTimestamp(frame.Timestamp)

	// BuildAACFrame works for both MKV and TS — it just wraps raw AAC data
	// in the RTMP audio tag format (0xAF 0x01 + data).
	payload := codec.BuildAACFrame(frame.Data)
	b.pushAudio(payload, rtmpTS)
}

// handleMKVAC3 processes AC-3 (Dolby Digital) audio frames from MKV.
// AC-3 syncframes have the same format in both MKV and MPEG-TS, so we
// can reuse the existing syncframe parser and RTMP tag builders.
func (b *Bridge) handleMKVAC3(frame *mkv.Frame) {
	// Validate minimum frame size for AC-3 syncframe header
	if len(frame.Data) < 8 {
		b.log.Debug("AC-3 frame too short (MKV)", "len", len(frame.Data))
		return
	}

	// Send AC-3 sequence header on first frame by parsing the syncframe
	if !b.mkvAudioSeqSent {
		info, err := codec.ParseAC3SyncFrame(frame.Data)
		if err != nil {
			b.log.Debug("failed to parse AC-3 syncframe (MKV)", "error", err)
			return
		}

		seqHeader := codec.BuildAC3SequenceHeader(info)
		b.pushAudio(seqHeader, 0)
		b.mkvAudioSeqSent = true

		b.log.Info("sent AC-3 sequence header (MKV)",
			"sample_rate", info.SampleRate,
			"channels", info.Channels,
		)
	}

	rtmpTS := b.mkvAudioTimestamp(frame.Timestamp)
	payload := codec.BuildAC3AudioFrame(frame.Data)
	b.pushAudio(payload, rtmpTS)
}

// handleMKVEAC3 processes E-AC-3 (Dolby Digital Plus) audio frames from MKV.
// Like AC-3, the syncframe format is identical in MKV and MPEG-TS.
func (b *Bridge) handleMKVEAC3(frame *mkv.Frame) {
	// Validate minimum frame size for E-AC-3 syncframe header
	if len(frame.Data) < 6 {
		b.log.Debug("E-AC-3 frame too short (MKV)", "len", len(frame.Data))
		return
	}

	// Send E-AC-3 sequence header on first frame
	if !b.mkvAudioSeqSent {
		info, err := codec.ParseEAC3SyncFrame(frame.Data)
		if err != nil {
			b.log.Debug("failed to parse E-AC-3 syncframe (MKV)", "error", err)
			return
		}

		seqHeader := codec.BuildEAC3SequenceHeader(info)
		b.pushAudio(seqHeader, 0)
		b.mkvAudioSeqSent = true

		b.log.Info("sent E-AC-3 sequence header (MKV)",
			"sample_rate", info.SampleRate,
			"channels", info.Channels,
		)
	}

	rtmpTS := b.mkvAudioTimestamp(frame.Timestamp)
	payload := codec.BuildEAC3AudioFrame(frame.Data)
	b.pushAudio(payload, rtmpTS)
}

// ─── Timestamp helpers ──────────────────────────────────────────────────────

// mkvVideoTimestamp converts an MKV timestamp (milliseconds) to an RTMP
// timestamp (also milliseconds, but relative to the first video frame).
func (b *Bridge) mkvVideoTimestamp(tsMS int64) uint32 {
	if !b.mkvVideoTSSet {
		b.mkvVideoTSBase = tsMS
		b.mkvVideoTSSet = true
	}
	return uint32(tsMS - b.mkvVideoTSBase)
}

// mkvAudioTimestamp converts an MKV timestamp (milliseconds) to an RTMP
// timestamp (also milliseconds, but relative to the first audio frame).
func (b *Bridge) mkvAudioTimestamp(tsMS int64) uint32 {
	if !b.mkvAudioTSSet {
		b.mkvAudioTSBase = tsMS
		b.mkvAudioTSSet = true
	}
	return uint32(tsMS - b.mkvAudioTSBase)
}

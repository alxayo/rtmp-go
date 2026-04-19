package srt

// Bridge connects an SRT connection to the RTMP publishing pipeline.
//
// The data flow is:
//
//	SRT Connection → Container Detection → Demuxer → Codec Converter → chunk.Message → Ingress Manager
//
// SRT can carry two container formats:
//   - MPEG-TS (traditional): Detected by 0x47 sync byte at 188-byte boundaries.
//     Supports H.264, H.265, AAC, AC-3, E-AC-3.
//   - Matroska/WebM (new): Detected by EBML header magic bytes (0x1A45DFA3).
//     Supports VP8, VP9, AV1, Opus, FLAC plus H.264, H.265, AAC, AC-3, E-AC-3.
//
// The bridge auto-detects the container from the first bytes of data, then:
//   1. Creates the appropriate demuxer (TS or MKV)
//   2. Feeds raw bytes to the demuxer, which extracts elementary streams
//   3. Converts codec data to RTMP format (Annex B→AVCC, ADTS→raw, etc.)
//   4. Wraps everything in chunk.Message and pushes to the ingress manager
//
// From the RTMP server's perspective, SRT streams look identical to native
// RTMP publishes — same internal data format, same routing, same subscribers.

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"

	"github.com/alxayo/go-rtmp/internal/codec"
	"github.com/alxayo/go-rtmp/internal/ingress"
	"github.com/alxayo/go-rtmp/internal/mkv"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
	"github.com/alxayo/go-rtmp/internal/rtmp/metrics"
	"github.com/alxayo/go-rtmp/internal/srt/conn"
	"github.com/alxayo/go-rtmp/internal/ts"
)

// containerType indicates which container format the SRT stream uses.
type containerType int

const (
	containerUnknown containerType = iota // Not yet detected
	containerTS                           // MPEG-TS (0x47 sync bytes)
	containerMKV                          // Matroska/WebM (EBML header)
)

// mkvMagic is the first 4 bytes of any Matroska/WebM file — the EBML
// element ID. Used for container auto-detection.
var mkvMagic = []byte{0x1A, 0x45, 0xDF, 0xA3}

// minDetectionBytes is the minimum number of bytes needed to reliably
// detect the container format. We need at least 4 bytes for MKV magic
// and at least 188+1 bytes for TS sync validation.
const minDetectionBytes = 189

// Bridge reads media from an SRT connection, demuxes audio/video,
// converts codecs, and pushes RTMP-format messages to the publish session.
type Bridge struct {
	// conn is the SRT connection we're reading from.
	conn *conn.Conn

	// container tracks which format was auto-detected (TS or MKV).
	container containerType

	// demuxer breaks the MPEG-TS byte stream into audio/video frames.
	// Only used when container == containerTS.
	demuxer *ts.Demuxer

	// mkvDemuxer breaks the Matroska byte stream into audio/video frames.
	// Only used when container == containerMKV.
	mkvDemuxer *mkv.Demuxer

	// session is the publish session in the ingress manager.
	// Media messages are pushed here for distribution to RTMP subscribers.
	session *ingress.PublishSession

	// --- Video state (H.264, TS path) ---

	// sps and pps cache the most recent H.264 Sequence Parameter Set
	// and Picture Parameter Set. These are needed to build the RTMP
	// video sequence header that tells decoders how to decode the video.
	sps []byte
	pps []byte

	// h264SeqHeaderSent is true once we've sent the H.264 video sequence header.
	// We delay sending it until we see an SPS+PPS pair in the stream.
	h264SeqHeaderSent bool

	// --- Video state (H.265, TS path) ---

	// vps, sps265, and pps265 cache the most recent H.265 Video Parameter Set,
	// Sequence Parameter Set, and Picture Parameter Set. H.265 requires all three.
	// These are needed to build the RTMP sequence header for H.265 streams.
	vps []byte
	sps265 []byte
	pps265 []byte

	// h265SeqHeaderSent is true once we've sent the H.265 video sequence header.
	// We delay sending it until we see all three parameter sets (VPS, SPS, PPS).
	h265SeqHeaderSent bool

	// videoCodec tracks which codec is in use: "H264" or "H265"
	// This tells us which parameter sets and sequence header builder to use.
	videoCodec string

	// --- Common video state (TS path, 90kHz timestamps) ---

	// videoTSBase is the DTS of the first video frame (in 90kHz units).
	// All subsequent video timestamps are relative to this base.
	videoTSBase int64

	// videoTSSet is true once we've recorded the first video timestamp.
	videoTSSet bool

	// --- Audio state (AAC, TS path) ---

	// aacConfigSent is true once we've sent the AAC sequence header.
	// We delay sending it until we parse the first ADTS header.
	aacConfigSent bool

	// --- Audio state (AC-3, TS path) ---

	// ac3ConfigSent is true once we've sent the AC-3 Enhanced RTMP sequence header.
	// We delay sending it until we parse the first AC-3 syncframe header.
	ac3ConfigSent bool

	// --- Audio state (E-AC-3, TS path) ---

	// eac3ConfigSent is true once we've sent the E-AC-3 Enhanced RTMP sequence header.
	// We delay sending it until we parse the first E-AC-3 syncframe header.
	eac3ConfigSent bool

	// audioTSBase is the PTS of the first audio frame (in 90kHz units).
	// All subsequent audio timestamps are relative to this base.
	audioTSBase int64

	// audioTSSet is true once we've recorded the first audio timestamp.
	audioTSSet bool

	// --- MKV-specific state ---
	// These are separate from TS state because MKV timestamps are in
	// milliseconds (not 90kHz), and the bridge is either TS or MKV, never both.

	// mkvVideoSeqSent tracks whether we've sent the video sequence header
	// for the MKV path (works for all codecs: VP8, VP9, AV1, H264, H265).
	mkvVideoSeqSent bool

	// mkvAudioSeqSent tracks whether we've sent the audio sequence header
	// for the MKV path (works for all codecs: Opus, FLAC, AAC, AC-3, E-AC-3).
	mkvAudioSeqSent bool

	// mkvVideoTSBase is the timestamp (ms) of the first MKV video frame.
	mkvVideoTSBase int64

	// mkvVideoTSSet is true once we've recorded the first MKV video timestamp.
	mkvVideoTSSet bool

	// mkvAudioTSBase is the timestamp (ms) of the first MKV audio frame.
	mkvAudioTSBase int64

	// mkvAudioTSSet is true once we've recorded the first MKV audio timestamp.
	mkvAudioTSSet bool

	// mkvNALULenSize is the NALU length field size from the H.264/H.265
	// decoder configuration record (1, 2, 3, or 4 bytes). Only used for
	// MKV H.264/H.265 where frame data is length-prefixed (AVCC format).
	mkvNALULenSize int

	// log is the logger for this bridge, tagged with connection context.
	log *slog.Logger
}

// NewBridge creates a bridge for the given SRT connection and publish session.
// The bridge auto-detects whether the SRT stream carries MPEG-TS or Matroska
// when the first data arrives. Call Run() to start processing.
func NewBridge(c *conn.Conn, session *ingress.PublishSession, log *slog.Logger) *Bridge {
	return &Bridge{
		conn:    c,
		session: session,
		log:     log,
	}
}

// Run reads data from the SRT connection, auto-detects the container format,
// and feeds data to the appropriate demuxer. It blocks until the connection
// is closed or an error occurs.
// This is typically called in a goroutine for each SRT publisher.
func (b *Bridge) Run() error {
	// Read buffer — 1500 bytes is a typical MTU size for UDP packets.
	// SRT sends data in chunks roughly this size.
	buf := make([]byte, 1500)

	// Detection buffer — accumulates initial bytes until we have enough
	// to reliably identify the container format.
	var detectBuf []byte

	for {
		// Read the next chunk of data from the SRT connection.
		// This blocks until data is available or the connection closes.
		n, err := b.conn.Read(buf)
		if err != nil {
			if err == io.EOF {
				// Clean shutdown — the sender closed the connection.
				b.log.Info("SRT connection closed by sender")
				return nil
			}
			return err
		}

		metrics.SRTBytesReceived.Add(int64(n))
		metrics.SRTPacketsReceived.Add(1)

		// If container not yet detected, accumulate bytes and try detection
		if b.container == containerUnknown {
			detectBuf = append(detectBuf, buf[:n]...)
			detected := b.detectContainer(detectBuf)
			if !detected {
				// Need more bytes for reliable detection
				continue
			}
			// Container detected — replay all buffered data through the demuxer
			if err := b.feedDemuxer(detectBuf); err != nil {
				b.log.Warn("demux error on initial data", "error", err)
			}
			detectBuf = nil // Free the detection buffer
			continue
		}

		// Normal path: feed data to the detected demuxer
		if err := b.feedDemuxer(buf[:n]); err != nil {
			b.log.Warn("demux error", "error", err)
			// For TS: continue processing (a single bad packet shouldn't kill the stream).
			// For MKV: errors are more likely fatal, but we log and try to continue.
		}
	}
}

// detectContainer examines buffered data to identify the container format.
// Returns true if detection succeeded (and sets b.container), false if more data needed.
//
// Detection strategy:
//   - Matroska: First 4 bytes are the EBML element ID (0x1A 0x45 0xDF 0xA3).
//     This is unambiguous — no other container starts with these bytes.
//   - MPEG-TS: Look for 0x47 sync byte at 188-byte boundaries. Checking at
//     two positions (0 and 188) gives high confidence it's really TS.
//   - Fallback: If we have enough data and neither pattern matches, default
//     to TS (backward compatible with existing behavior).
func (b *Bridge) detectContainer(data []byte) bool {
	// Check for Matroska/WebM EBML header magic (4 bytes)
	if len(data) >= 4 && bytes.Equal(data[:4], mkvMagic) {
		b.container = containerMKV
		b.mkvDemuxer = mkv.NewDemuxer(b.onMKVFrame, b.log)
		b.log.Info("detected Matroska/WebM container")
		return true
	}

	// Check for MPEG-TS sync bytes at packet boundaries.
	// A single 0x47 could be coincidental; verifying at offset 188
	// confirms it's really TS packet alignment.
	if len(data) >= minDetectionBytes {
		if data[0] == 0x47 && data[188] == 0x47 {
			b.container = containerTS
			b.demuxer = ts.NewDemuxer(b.onFrame)
			b.log.Info("detected MPEG-TS container")
			return true
		}

		// Neither pattern matched — try single 0x47 as fallback (some
		// TS streams may not be perfectly aligned in the first read).
		if data[0] == 0x47 {
			b.container = containerTS
			b.demuxer = ts.NewDemuxer(b.onFrame)
			b.log.Info("detected MPEG-TS container (single sync)")
			return true
		}

		// Unknown format — default to TS for backward compatibility.
		// The TS demuxer will handle misalignment gracefully.
		b.container = containerTS
		b.demuxer = ts.NewDemuxer(b.onFrame)
		b.log.Warn("unknown SRT container format, defaulting to MPEG-TS",
			"first_bytes", fmt.Sprintf("%02x %02x %02x %02x", data[0], data[1], data[2], data[3]),
		)
		return true
	}

	// Not enough data yet — but check for early MKV detection (only 4 bytes needed)
	// We already checked MKV above, so if we're here with < minDetectionBytes,
	// we just need more data for TS validation.
	return false
}

// feedDemuxer routes data to the active demuxer based on detected container.
func (b *Bridge) feedDemuxer(data []byte) error {
	switch b.container {
	case containerTS:
		return b.demuxer.Feed(data)
	case containerMKV:
		return b.mkvDemuxer.Feed(data)
	default:
		return nil // Should not happen after detection
	}
}

// onFrame is called by the TS demuxer each time it extracts a complete
// media frame (either audio or video). We dispatch to the appropriate
// codec converter based on the stream type.
func (b *Bridge) onFrame(frame *ts.MediaFrame) {
	switch frame.Stream.StreamType {
	case ts.StreamTypeH264:
		b.videoCodec = "H264"
		b.handleH264Frame(frame)
	case ts.StreamTypeH265:
		b.videoCodec = "H265"
		b.handleH265Frame(frame)
	case ts.StreamTypeAAC_ADTS:
		b.handleAACFrame(frame)
	case ts.StreamTypeAC3:
		b.handleAC3Frame(frame)
	case ts.StreamTypeEAC3:
		b.handleEAC3Frame(frame)
	default:
		// We support H.264, H.265, AAC, AC-3, and E-AC-3 for now.
		// Other codecs (MPEG-2, MP3, etc.) are silently ignored.
	}
}

// handleH264Frame converts an H.264 access unit from Annex B format
// (as carried in MPEG-TS) to RTMP's AVCC format and pushes it.
func (b *Bridge) handleH264Frame(frame *ts.MediaFrame) {
	// Split the raw data into individual NAL units.
	// Annex B format uses start codes (0x00000001) to separate NALUs.
	nalus := codec.SplitAnnexB(frame.Data)
	if len(nalus) == 0 {
		return
	}

	// Look for SPS and PPS NALUs — these are the "decoder configuration"
	// and must be sent as a sequence header before any video frames.
	sps, pps, found := codec.ExtractSPSPPS(nalus)
	if found {
		// Check if the SPS/PPS changed (e.g., mid-stream resolution change)
		if !b.h264SeqHeaderSent || !bytes.Equal(b.sps, sps) || !bytes.Equal(b.pps, pps) {
			b.sps = bytes.Clone(sps)
			b.pps = bytes.Clone(pps)

			// Build and send the RTMP video sequence header
			seqHeader := codec.BuildAVCSequenceHeader(b.sps, b.pps)
			b.pushVideo(seqHeader, 0)
			b.h264SeqHeaderSent = true

			b.log.Info("sent H.264 sequence header",
				"sps_len", len(sps),
				"pps_len", len(pps),
			)
		}
	}

	// If we haven't seen SPS/PPS yet, we can't send video frames
	// because the decoder wouldn't know how to decode them.
	if !b.h264SeqHeaderSent {
		return
	}

	// Convert timestamps from MPEG-TS (90kHz) to RTMP (1kHz/milliseconds).
	// We use DTS (Decode Timestamp) as the base RTMP timestamp because
	// RTMP timestamps represent decode order, not display order.
	dts := frame.DTS
	if dts < 0 {
		dts = frame.PTS // Fallback if DTS not present
	}
	if dts < 0 {
		return // No valid timestamp — skip this frame
	}

	// Record the first timestamp as our base (so timestamps start at 0)
	if !b.videoTSSet {
		b.videoTSBase = dts
		b.videoTSSet = true
	}

	// Convert 90kHz → milliseconds
	rtmpTS := uint32((dts - b.videoTSBase) / 90)

	// Calculate Composition Time Offset (CTS) for B-frame support.
	// CTS = PTS - DTS, telling the player how to reorder frames for display.
	// For live streams without B-frames, this is always 0.
	cts := int32(0)
	if frame.PTS >= 0 && frame.DTS >= 0 {
		cts = int32((frame.PTS - frame.DTS) / 90)
	}

	// Filter out non-VCL NALUs from the frame data.
	// SPS, PPS, and AUD are sent separately or not needed in RTMP.
	var frameNalus [][]byte
	for _, nalu := range nalus {
		switch codec.NALUType(nalu) {
		case codec.NALUTypeSPS, codec.NALUTypePPS, codec.NALUTypeAUD:
			// Skip parameter sets and access unit delimiters
			continue
		default:
			frameNalus = append(frameNalus, nalu)
		}
	}

	if len(frameNalus) == 0 {
		return
	}

	// Determine if this is a keyframe by checking the first VCL NALU type
	isKey := codec.NALUType(frameNalus[0]) == codec.NALUTypeIDR

	// Build the RTMP video frame payload (AVCC format)
	payload := codec.BuildAVCVideoFrame(frameNalus, isKey, cts)
	b.pushVideo(payload, rtmpTS)
}

// handleH265Frame converts an H.265 access unit from Annex B format
// (as carried in MPEG-TS) to RTMP's AVCC format and pushes it.
// H.265 requires three parameter sets (VPS, SPS, PPS) vs. H.264's two (SPS, PPS).
func (b *Bridge) handleH265Frame(frame *ts.MediaFrame) {
	// Split the raw data into individual H.265 NAL units.
	// Annex B format uses start codes (0x00000001) to separate NALUs.
	// H.265 uses the same Annex B format as H.264.
	nalus := codec.SplitH265AnnexB(frame.Data)
	if len(nalus) == 0 {
		return
	}

	// Look for VPS, SPS, and PPS NALUs — these are the "decoder configuration"
	// and must be sent as a sequence header before any H.265 video frames.
	// H.265 uniquely requires the VPS (Video Parameter Set) in addition to SPS/PPS.
	vps, sps, pps, found := codec.ExtractH265VPSSPSPPS(nalus)
	if found {
		// Check if the parameter sets changed (e.g., mid-stream profile change)
		// This is important because H.265 allows switching profiles on-the-fly.
		if !b.h265SeqHeaderSent ||
			!bytes.Equal(b.vps, vps) ||
			!bytes.Equal(b.sps265, sps) ||
			!bytes.Equal(b.pps265, pps) {

			b.vps = bytes.Clone(vps)
			b.sps265 = bytes.Clone(sps)
			b.pps265 = bytes.Clone(pps)

			// Build and send the RTMP video sequence header for H.265
			// The HEVCDecoderConfigurationRecord includes all three parameter sets.
			seqHeader := codec.BuildHEVCSequenceHeader(b.vps, b.sps265, b.pps265)
			b.pushVideo(seqHeader, 0)
			b.h265SeqHeaderSent = true

			b.log.Info("sent H.265 sequence header",
				"vps_len", len(vps),
				"sps_len", len(sps),
				"pps_len", len(pps),
			)
		}
	}

	// If we haven't seen all three parameter sets yet, we can't send video frames
	// because the decoder wouldn't know how to decode them.
	if !b.h265SeqHeaderSent {
		return
	}

	// Convert timestamps from MPEG-TS (90kHz) to RTMP (1kHz/milliseconds).
	// We use DTS (Decode Timestamp) as the base RTMP timestamp because
	// RTMP timestamps represent decode order, not display order.
	dts := frame.DTS
	if dts < 0 {
		dts = frame.PTS // Fallback if DTS not present
	}
	if dts < 0 {
		return // No valid timestamp — skip this frame
	}

	// Record the first timestamp as our base (so timestamps start at 0)
	if !b.videoTSSet {
		b.videoTSBase = dts
		b.videoTSSet = true
	}

	// Convert 90kHz → milliseconds
	rtmpTS := uint32((dts - b.videoTSBase) / 90)

	// Calculate Composition Time Offset (CTS) for B-frame support.
	// CTS = PTS - DTS, telling the player how to reorder frames for display.
	// For live streams without B-frames, this is always 0.
	cts := int32(0)
	if frame.PTS >= 0 && frame.DTS >= 0 {
		cts = int32((frame.PTS - frame.DTS) / 90)
	}

	// Filter out non-VCL (Video Coding Layer) NALUs from the frame data.
	// VPS, SPS, PPS, and AUD are sent separately or not needed in RTMP.
	var frameNalus [][]byte
	for _, nalu := range nalus {
		naluType := codec.H265NALUType(nalu)
		// In H.265, parameter set types are: VPS=32, SPS=33, PPS=34, AUD=35
		if naluType == 32 || naluType == 33 || naluType == 34 || naluType == 35 {
			// Skip parameter sets and access unit delimiters
			continue
		}
		frameNalus = append(frameNalus, nalu)
	}

	if len(frameNalus) == 0 {
		return
	}

	// Determine if this is a keyframe by checking the first VCL NALU type.
	// In H.265, keyframes (IDR) have NAL types 16-21.
	isKey := codec.IsH265KeyframeNALU(frameNalus[0])

	// Build the RTMP video frame payload (AVCC format).
	// H.265 uses the same AVCC format as H.264 for frame data in RTMP.
	payload := codec.BuildHEVCVideoFrame(frameNalus, isKey, cts)
	b.pushVideo(payload, rtmpTS)
}

// handleAACFrame converts an AAC ADTS frame to RTMP's raw AAC format
// and pushes it.
func (b *Bridge) handleAACFrame(frame *ts.MediaFrame) {
	// Strip the ADTS header to get raw AAC data.
	// The ADTS header also tells us the audio format parameters.
	rawFrame, adts, err := codec.StripADTS(frame.Data)
	if err != nil {
		b.log.Debug("failed to strip ADTS", "error", err)
		return
	}

	// Send the AudioSpecificConfig sequence header on the first frame.
	// This tells the decoder the sample rate, channel count, and AAC profile.
	if !b.aacConfigSent {
		seqHeader := codec.BuildAACSequenceHeader(adts)
		b.pushAudio(seqHeader, 0)
		b.aacConfigSent = true

		b.log.Info("sent AAC sequence header",
			"profile", adts.Profile,
			"freq_idx", adts.SamplingFreqIdx,
			"channels", adts.ChannelConfig,
		)
	}

	// Convert PTS from 90kHz to milliseconds
	pts := frame.PTS
	if pts < 0 {
		return // No valid timestamp
	}

	if !b.audioTSSet {
		b.audioTSBase = pts
		b.audioTSSet = true
	}

	rtmpTS := uint32((pts - b.audioTSBase) / 90)

	// Build the RTMP audio frame payload (raw AAC without ADTS)
	payload := codec.BuildAACFrame(rawFrame)
	b.pushAudio(payload, rtmpTS)
}

// handleAC3Frame converts an AC-3 syncframe to an Enhanced RTMP audio message
// and pushes it. AC-3 (Dolby Digital) uses Enhanced RTMP tags with FourCC 'ac-3'.
func (b *Bridge) handleAC3Frame(frame *ts.MediaFrame) {
	// Validate the syncframe — we need at least 8 bytes for the AC-3 header.
	if len(frame.Data) < 8 {
		b.log.Debug("AC-3 frame too short", "len", len(frame.Data))
		return
	}

	// Send the sequence header on the first frame.
	// Parse the syncframe header to extract sample rate, channels, etc.
	if !b.ac3ConfigSent {
		info, err := codec.ParseAC3SyncFrame(frame.Data)
		if err != nil {
			b.log.Debug("failed to parse AC-3 syncframe", "error", err)
			return
		}

		seqHeader := codec.BuildAC3SequenceHeader(info)
		b.pushAudio(seqHeader, 0)
		b.ac3ConfigSent = true

		b.log.Info("sent AC-3 sequence header",
			"sample_rate", info.SampleRate,
			"channels", info.Channels,
			"bsid", info.Bsid,
			"acmod", info.Acmod,
		)
	}

	// Convert PTS from 90kHz to milliseconds (same pattern as AAC)
	pts := frame.PTS
	if pts < 0 {
		return // No valid timestamp
	}

	if !b.audioTSSet {
		b.audioTSBase = pts
		b.audioTSSet = true
	}

	rtmpTS := uint32((pts - b.audioTSBase) / 90)

	// Build the Enhanced RTMP audio frame with the complete AC-3 syncframe
	payload := codec.BuildAC3AudioFrame(frame.Data)
	b.pushAudio(payload, rtmpTS)
}

// handleEAC3Frame converts an E-AC-3 syncframe to an Enhanced RTMP audio message
// and pushes it. E-AC-3 (Dolby Digital Plus) uses Enhanced RTMP tags with FourCC 'ec-3'.
func (b *Bridge) handleEAC3Frame(frame *ts.MediaFrame) {
	// Validate the syncframe — we need at least 6 bytes for the E-AC-3 header.
	if len(frame.Data) < 6 {
		b.log.Debug("E-AC-3 frame too short", "len", len(frame.Data))
		return
	}

	// Send the sequence header on the first frame.
	// Parse the syncframe header to extract sample rate, channels, etc.
	if !b.eac3ConfigSent {
		info, err := codec.ParseEAC3SyncFrame(frame.Data)
		if err != nil {
			b.log.Debug("failed to parse E-AC-3 syncframe", "error", err)
			return
		}

		seqHeader := codec.BuildEAC3SequenceHeader(info)
		b.pushAudio(seqHeader, 0)
		b.eac3ConfigSent = true

		b.log.Info("sent E-AC-3 sequence header",
			"sample_rate", info.SampleRate,
			"channels", info.Channels,
			"bsid", info.Bsid,
			"acmod", info.Acmod,
			"lfeon", info.Lfeon,
		)
	}

	// Convert PTS from 90kHz to milliseconds (same pattern as AAC)
	pts := frame.PTS
	if pts < 0 {
		return // No valid timestamp
	}

	if !b.audioTSSet {
		b.audioTSBase = pts
		b.audioTSSet = true
	}

	rtmpTS := uint32((pts - b.audioTSBase) / 90)

	// Build the Enhanced RTMP audio frame with the complete E-AC-3 syncframe
	payload := codec.BuildEAC3AudioFrame(frame.Data)
	b.pushAudio(payload, rtmpTS)
}

// pushVideo creates an RTMP video message and pushes it to the publish session.
// TypeID 9 = video in RTMP.
func (b *Bridge) pushVideo(payload []byte, timestamp uint32) {
	msg := &chunk.Message{
		CSID:            6,                    // Video chunk stream (RTMP convention)
		Timestamp:       timestamp,            // Milliseconds
		MessageLength:   uint32(len(payload)), // Payload size
		TypeID:          9,                    // Video message type
		MessageStreamID: 1,                    // First media stream
		Payload:         payload,
	}
	b.session.PushMedia(msg)
}

// pushAudio creates an RTMP audio message and pushes it to the publish session.
// TypeID 8 = audio in RTMP.
func (b *Bridge) pushAudio(payload []byte, timestamp uint32) {
	msg := &chunk.Message{
		CSID:            4,                    // Audio chunk stream (RTMP convention)
		Timestamp:       timestamp,            // Milliseconds
		MessageLength:   uint32(len(payload)), // Payload size
		TypeID:          8,                    // Audio message type
		MessageStreamID: 1,                    // First media stream
		Payload:         payload,
	}
	b.session.PushMedia(msg)
}


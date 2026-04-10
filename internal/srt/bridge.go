package srt

// Bridge connects an SRT connection to the RTMP publishing pipeline.
//
// The data flow is:
//
//	SRT Connection → MPEG-TS Demuxer → Codec Converter → chunk.Message → Ingress Manager
//
// Each SRT stream carries MPEG-TS (the standard transport for SRT). The bridge:
//   1. Reads raw bytes from the SRT connection
//   2. Feeds them to the TS demuxer, which extracts elementary streams
//   3. Converts H.264 video from Annex B to AVCC format (what RTMP expects)
//   4. Converts AAC audio from ADTS to raw format (what RTMP expects)
//   5. Wraps everything in chunk.Message and pushes it to the ingress manager
//
// From the RTMP server's perspective, SRT streams look identical to native
// RTMP publishes — same internal data format, same routing, same subscribers.

import (
	"io"
	"log/slog"

	"github.com/alxayo/go-rtmp/internal/codec"
	"github.com/alxayo/go-rtmp/internal/ingress"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
	"github.com/alxayo/go-rtmp/internal/srt/conn"
	"github.com/alxayo/go-rtmp/internal/ts"
)

// Bridge reads MPEG-TS from an SRT connection, demuxes audio/video,
// converts codecs, and pushes RTMP-format messages to the publish session.
type Bridge struct {
	// conn is the SRT connection we're reading from.
	conn *conn.Conn

	// demuxer breaks the MPEG-TS byte stream into audio/video frames.
	demuxer *ts.Demuxer

	// session is the publish session in the ingress manager.
	// Media messages are pushed here for distribution to RTMP subscribers.
	session *ingress.PublishSession

	// --- Video state ---

	// sps and pps cache the most recent H.264 Sequence Parameter Set
	// and Picture Parameter Set. These are needed to build the RTMP
	// video sequence header that tells decoders how to decode the video.
	sps []byte
	pps []byte

	// seqHeaderSent is true once we've sent the video sequence header.
	// We delay sending it until we see an SPS+PPS pair in the stream.
	seqHeaderSent bool

	// videoTSBase is the DTS of the first video frame (in 90kHz units).
	// All subsequent video timestamps are relative to this base.
	videoTSBase int64

	// videoTSSet is true once we've recorded the first video timestamp.
	videoTSSet bool

	// --- Audio state ---

	// aacConfigSent is true once we've sent the AAC sequence header.
	// We delay sending it until we parse the first ADTS header.
	aacConfigSent bool

	// audioTSBase is the PTS of the first audio frame (in 90kHz units).
	// All subsequent audio timestamps are relative to this base.
	audioTSBase int64

	// audioTSSet is true once we've recorded the first audio timestamp.
	audioTSSet bool

	// log is the logger for this bridge, tagged with connection context.
	log *slog.Logger
}

// NewBridge creates a bridge for the given SRT connection and publish session.
// The bridge is ready to run after creation — call Run() to start processing.
func NewBridge(c *conn.Conn, session *ingress.PublishSession, log *slog.Logger) *Bridge {
	b := &Bridge{
		conn:    c,
		session: session,
		log:     log,
	}

	// Create the MPEG-TS demuxer with our frame callback.
	// Each time the demuxer extracts a complete audio or video frame,
	// it calls b.onFrame to handle the codec conversion.
	b.demuxer = ts.NewDemuxer(b.onFrame)

	return b
}

// Run reads data from the SRT connection and feeds it to the TS demuxer.
// It blocks until the connection is closed or an error occurs.
// This is typically called in a goroutine for each SRT publisher.
func (b *Bridge) Run() error {
	// Read buffer — 1500 bytes is a typical MTU size for UDP packets.
	// SRT sends data in chunks roughly this size.
	buf := make([]byte, 1500)

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

		// Feed the raw bytes to the TS demuxer. It handles:
		// - Packet boundary alignment (188-byte TS packets)
		// - PAT/PMT parsing to discover stream PIDs
		// - PES reassembly to reconstruct complete audio/video frames
		if err := b.demuxer.Feed(buf[:n]); err != nil {
			b.log.Warn("TS demux error", "error", err)
			// Continue processing — a single bad packet shouldn't kill the stream
		}
	}
}

// onFrame is called by the TS demuxer each time it extracts a complete
// media frame (either audio or video). We dispatch to the appropriate
// codec converter based on the stream type.
func (b *Bridge) onFrame(frame *ts.MediaFrame) {
	switch frame.Stream.StreamType {
	case ts.StreamTypeH264:
		b.handleH264Frame(frame)
	case ts.StreamTypeAAC_ADTS:
		b.handleAACFrame(frame)
	default:
		// We only support H.264 and AAC for now.
		// Other codecs (H.265, MPEG-2, etc.) are silently ignored.
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
		if !b.seqHeaderSent || !bytesEqual(b.sps, sps) || !bytesEqual(b.pps, pps) {
			b.sps = copyBytes(sps)
			b.pps = copyBytes(pps)

			// Build and send the RTMP video sequence header
			seqHeader := codec.BuildAVCSequenceHeader(b.sps, b.pps)
			b.pushVideo(seqHeader, 0)
			b.seqHeaderSent = true

			b.log.Info("sent H.264 sequence header",
				"sps_len", len(sps),
				"pps_len", len(pps),
			)
		}
	}

	// If we haven't seen SPS/PPS yet, we can't send video frames
	// because the decoder wouldn't know how to decode them.
	if !b.seqHeaderSent {
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

// bytesEqual compares two byte slices for equality.
// Handles nil slices correctly.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// copyBytes makes a copy of a byte slice.
// Returns nil if the input is nil.
func copyBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

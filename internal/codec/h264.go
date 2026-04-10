package codec

// This file implements H.264/AVC video conversion for RTMP.
//
// When H.264 video travels over RTMP, it needs two things:
//
//   1. A "sequence header" — sent once at the start (and on keyframes).
//      This contains the AVCDecoderConfigurationRecord which tells the
//      decoder the video's profile, level, resolution (via SPS), and
//      picture parameters (via PPS).
//
//   2. Video frames — each keyframe or inter-frame, wrapped in a
//      specific RTMP tag format with AVCC-formatted NALUs.
//
// The RTMP video tag format (FLV-style) is:
//
//	Byte 0: [FrameType:4][CodecID:4]
//	  - FrameType: 1 = keyframe, 2 = inter-frame
//	  - CodecID:   7 = AVC (H.264)
//
//	Byte 1: AVCPacketType
//	  - 0 = Sequence header (AVCDecoderConfigurationRecord)
//	  - 1 = NALU (video frame data)
//
//	Bytes 2-4: CompositionTimeOffset (CTS) in milliseconds
//	  - For sequence headers: always 0
//	  - For frames: PTS - DTS (for B-frame reordering)
//
//	Remaining: Either the config record or AVCC NALUs

import "encoding/binary"

// BuildAVCSequenceHeader builds the RTMP video tag payload for an H.264
// sequence header. This must be sent before any video frames so the
// decoder knows how to interpret the video data.
//
// The payload contains an AVCDecoderConfigurationRecord:
//
//	ConfigVersion:    1 (always)
//	AVCProfileInd:    from SPS[1]
//	ProfileCompat:    from SPS[2]
//	AVCLevelInd:      from SPS[3]
//	LengthSizeM1:     3 (meaning 4-byte NALU lengths, 0xFF)
//	NumSPS:           1 (0xE1 = reserved bits + count)
//	SPSLength:        2 bytes, big-endian
//	SPS data:         the raw SPS NALU
//	NumPPS:           1
//	PPSLength:        2 bytes, big-endian
//	PPS data:         the raw PPS NALU
func BuildAVCSequenceHeader(sps, pps []byte) []byte {
	// The RTMP tag header is 5 bytes + the config record
	// Config record = 6 bytes fixed + 2 + len(sps) + 1 + 2 + len(pps)
	recordLen := 6 + 2 + len(sps) + 1 + 2 + len(pps)
	buf := make([]byte, 5+recordLen)

	// -- RTMP video tag header (5 bytes) --
	// Byte 0: FrameType=1 (keyframe) << 4 | CodecID=7 (AVC) = 0x17
	buf[0] = 0x17
	// Byte 1: AVCPacketType=0 (sequence header)
	buf[1] = 0x00
	// Bytes 2-4: CompositionTimeOffset = 0 for sequence headers
	buf[2] = 0x00
	buf[3] = 0x00
	buf[4] = 0x00

	// -- AVCDecoderConfigurationRecord --
	off := 5

	// ConfigurationVersion = 1 (always 1)
	buf[off] = 1
	off++

	// AVCProfileIndication — the H.264 profile from the SPS
	// (Baseline=66, Main=77, High=100, etc.)
	if len(sps) > 1 {
		buf[off] = sps[1] // Profile
	}
	off++

	// ProfileCompatibility — constraint flags from the SPS
	if len(sps) > 2 {
		buf[off] = sps[2]
	}
	off++

	// AVCLevelIndication — the encoding level (e.g., 3.1, 4.0)
	if len(sps) > 3 {
		buf[off] = sps[3]
	}
	off++

	// LengthSizeMinusOne = 3 (meaning NALU length fields are 4 bytes)
	// The 0xFC mask sets the reserved upper 6 bits to 1
	buf[off] = 0xFF // 0xFC | 0x03
	off++

	// Number of SPS = 1 (the 0xE0 mask sets the reserved upper 3 bits to 1)
	buf[off] = 0xE1 // 0xE0 | 0x01
	off++

	// SPS length (2 bytes, big-endian) + SPS data
	binary.BigEndian.PutUint16(buf[off:], uint16(len(sps)))
	off += 2
	copy(buf[off:], sps)
	off += len(sps)

	// Number of PPS = 1
	buf[off] = 0x01
	off++

	// PPS length (2 bytes, big-endian) + PPS data
	binary.BigEndian.PutUint16(buf[off:], uint16(len(pps)))
	off += 2
	copy(buf[off:], pps)

	return buf
}

// BuildAVCVideoFrame builds the RTMP video tag payload for an H.264 frame.
//
// Parameters:
//   - nalus: The NAL units that make up this frame (already split from Annex B).
//     These should be VCL NALUs only (no SPS/PPS/AUD).
//   - isKeyframe: true if this is an IDR frame (I-frame / keyframe)
//   - cts: Composition Time Offset in milliseconds (PTS - DTS).
//     This is 0 for streams without B-frames, which is typical for live.
func BuildAVCVideoFrame(nalus [][]byte, isKeyframe bool, cts int32) []byte {
	// Convert NALUs to AVCC format (4-byte length prefix each)
	avccData := ToAVCC(nalus)

	// Allocate: 5 bytes RTMP header + AVCC data
	buf := make([]byte, 5+len(avccData))

	// -- RTMP video tag header (5 bytes) --
	// Byte 0: [FrameType:4][CodecID:4]
	if isKeyframe {
		buf[0] = 0x17 // Keyframe (1) + AVC (7)
	} else {
		buf[0] = 0x27 // Inter-frame (2) + AVC (7)
	}

	// Byte 1: AVCPacketType = 1 (NALU data)
	buf[1] = 0x01

	// Bytes 2-4: CompositionTimeOffset (signed 24-bit, big-endian)
	// CTS = PTS - DTS, in milliseconds
	buf[2] = byte(cts >> 16)
	buf[3] = byte(cts >> 8)
	buf[4] = byte(cts)

	// Copy the AVCC data after the header
	copy(buf[5:], avccData)

	return buf
}

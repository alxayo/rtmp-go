// video_test.go – tests for RTMP video message parsing.
//
// Tests cover both legacy FLV format and Enhanced RTMP (E-RTMP) format:
//
// Legacy: High nibble (bits 7-4) = FrameType, Low nibble (bits 3-0) = CodecID
// Enhanced: bit[7] = IsExHeader, bits[6:4] = FrameType(3bit), bits[3:0] = PacketType
//           + 4-byte FourCC after header byte
package media

import "testing"

// --- Legacy Format Tests (backward compatibility) ---

// TestParseVideoMessage_AVCSequenceHeader verifies parsing of an H.264
// sequence header (frameType=1 keyframe, codecID=7 AVC, avcPacketType=0).
// The sequence header contains SPS/PPS data used to initialize the decoder.
func TestParseVideoMessage_AVCSequenceHeader(t *testing.T) {
	// frameType=1 (keyframe), codecID=7 (AVC)
	data := []byte{(1 << 4) | 7, 0x00, 0x17, 0x34, 0x56} // 0x00 = sequence header; rest pretend SPS/PPS bytes
	m, err := ParseVideoMessage(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if m.Codec != VideoCodecAVC {
		_tFatalf(t, "codec mismatch want AVC got %s", m.Codec)
	}
	if m.FrameType != VideoFrameTypeKey {
		_tFatalf(t, "frameType mismatch want keyframe got %s", m.FrameType)
	}
	if m.PacketType != AVCPacketTypeSequenceHeader {
		_tFatalf(t, "packetType mismatch want sequence_header got %s", m.PacketType)
	}
	if len(m.Payload) != 3 || m.Payload[0] != 0x17 {
		_tFatalf(t, "payload mismatch: %+v", m.Payload)
	}
	if m.Enhanced {
		_tFatalf(t, "should not be enhanced")
	}
}

// TestParseVideoMessage_AVCKeyframeNALU verifies an H.264 keyframe
// with actual NALU data (avcPacketType=1). These are I-frames.
func TestParseVideoMessage_AVCKeyframeNALU(t *testing.T) {
	// frameType=1 keyframe, codecID=7 AVC, avcPacketType=1 (NALU)
	data := []byte{(1 << 4) | 7, 0x01, 0xAA, 0xBB, 0xCC}
	m, err := ParseVideoMessage(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if m.Codec != VideoCodecAVC || m.PacketType != AVCPacketTypeNALU || m.FrameType != VideoFrameTypeKey {
		_tFatalf(t, "unexpected metadata: %+v", m)
	}
	if len(m.Payload) != 3 || m.Payload[2] != 0xCC {
		_tFatalf(t, "payload mismatch: %+v", m.Payload)
	}
}

// TestParseVideoMessage_AVCInterNALU verifies an H.264 inter-frame
// (P or B frame, frameType=2) with NALU payload.
func TestParseVideoMessage_AVCInterNALU(t *testing.T) {
	// frameType=2 inter, codecID=7 AVC, avcPacketType=1 (NALU)
	data := []byte{(2 << 4) | 7, 0x01, 0x01, 0x02}
	m, err := ParseVideoMessage(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if m.FrameType != VideoFrameTypeInter || m.PacketType != AVCPacketTypeNALU {
		_tFatalf(t, "unexpected metadata: %+v", m)
	}
	if len(m.Payload) != 2 || m.Payload[0] != 0x01 {
		_tFatalf(t, "payload mismatch: %+v", m.Payload)
	}
}

// TestParseVideoMessage_LegacyHEVC verifies the non-standard CodecID=12 HEVC
// path still works (backward compat with Chinese CDN implementations).
func TestParseVideoMessage_LegacyHEVC(t *testing.T) {
	// frameType=1 keyframe, codecID=12 (non-standard HEVC), packetType=0 (seq header)
	data := []byte{(1 << 4) | 12, 0x00, 0xDE, 0xAD}
	m, err := ParseVideoMessage(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if m.Codec != VideoCodecHEVC {
		_tFatalf(t, "codec mismatch want H265 got %s", m.Codec)
	}
	if m.FrameType != VideoFrameTypeKey {
		_tFatalf(t, "frameType mismatch want keyframe got %s", m.FrameType)
	}
	if m.PacketType != AVCPacketTypeSequenceHeader {
		_tFatalf(t, "packetType mismatch want sequence_header got %s", m.PacketType)
	}
	if m.Enhanced {
		_tFatalf(t, "legacy HEVC should not be enhanced")
	}
}

// --- Enhanced RTMP Tests ---

// buildEnhancedVideoTag constructs a minimal Enhanced RTMP video tag.
// header byte: IsExHeader=1 (bit7) | frameType(3bit) shifted to bits[6:4] | packetType(4bit)
func buildEnhancedVideoTag(frameType, packetType uint8, fourCC string, payload []byte) []byte {
	b0 := byte(0x80) | (frameType << 4) | packetType
	data := []byte{b0}
	data = append(data, []byte(fourCC)...)
	data = append(data, payload...)
	return data
}

func TestParseVideoMessage_EnhancedHEVCSequenceStart(t *testing.T) {
	payload := []byte{0x01, 0x02, 0x03} // pretend VPS/SPS/PPS
	data := buildEnhancedVideoTag(1, 0, "hvc1", payload) // keyframe, SequenceStart
	m, err := ParseVideoMessage(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if !m.Enhanced {
		_tFatalf(t, "should be enhanced")
	}
	if m.Codec != VideoCodecHEVC {
		_tFatalf(t, "codec mismatch want H265 got %s", m.Codec)
	}
	if m.FourCC != "hvc1" {
		_tFatalf(t, "fourcc mismatch want hvc1 got %s", m.FourCC)
	}
	if m.FrameType != VideoFrameTypeKey {
		_tFatalf(t, "frameType mismatch want keyframe got %s", m.FrameType)
	}
	if m.PacketType != PacketTypeSequenceStart {
		_tFatalf(t, "packetType mismatch want sequence_start got %s", m.PacketType)
	}
	if len(m.Payload) != 3 || m.Payload[0] != 0x01 {
		_tFatalf(t, "payload mismatch: %+v", m.Payload)
	}
}

func TestParseVideoMessage_EnhancedAV1SequenceStart(t *testing.T) {
	payload := []byte{0xAA, 0xBB}
	data := buildEnhancedVideoTag(1, 0, "av01", payload)
	m, err := ParseVideoMessage(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if m.Codec != VideoCodecAV1 || m.FourCC != "av01" {
		_tFatalf(t, "codec/fourcc mismatch: %s / %s", m.Codec, m.FourCC)
	}
	if m.PacketType != PacketTypeSequenceStart {
		_tFatalf(t, "packetType mismatch: %s", m.PacketType)
	}
}

func TestParseVideoMessage_EnhancedVP9CodedFrames(t *testing.T) {
	// CodedFrames (pktType=1) includes 3-byte composition time after FourCC.
	compTime := []byte{0x00, 0x00, 0x00} // composition time = 0
	payload := []byte{0x11, 0x22}
	tag := buildEnhancedVideoTag(2, 1, "vp09", append(compTime, payload...)) // inter, CodedFrames
	m, err := ParseVideoMessage(tag)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if m.Codec != VideoCodecVP9 || m.FourCC != "vp09" {
		_tFatalf(t, "codec/fourcc mismatch: %s / %s", m.Codec, m.FourCC)
	}
	if m.FrameType != VideoFrameTypeInter {
		_tFatalf(t, "frameType mismatch want inter got %s", m.FrameType)
	}
	if m.PacketType != PacketTypeCodedFrames {
		_tFatalf(t, "packetType mismatch want coded_frames got %s", m.PacketType)
	}
	// Payload should exclude the 3-byte composition time
	if len(m.Payload) != 2 || m.Payload[0] != 0x11 {
		_tFatalf(t, "payload mismatch (should skip comp time): %+v", m.Payload)
	}
}

// TestParseVideoMessage_EnhancedVP8SequenceStart verifies that the VP8 codec
// (FourCC "vp08") is correctly identified in an Enhanced RTMP sequence start tag.
// VP8 follows the same Enhanced RTMP pattern as VP9, AV1, etc.
func TestParseVideoMessage_EnhancedVP8SequenceStart(t *testing.T) {
	payload := []byte{0xAA, 0xBB} // pretend VP8 codec config
	data := buildEnhancedVideoTag(1, 0, "vp08", payload) // keyframe, SequenceStart
	m, err := ParseVideoMessage(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if m.Codec != VideoCodecVP8 || m.FourCC != "vp08" {
		_tFatalf(t, "codec/fourcc mismatch: %s / %s", m.Codec, m.FourCC)
	}
	if m.PacketType != PacketTypeSequenceStart {
		_tFatalf(t, "packetType mismatch: %s", m.PacketType)
	}
	if !m.Enhanced {
		_tFatalf(t, "should be enhanced")
	}
	if m.FrameType != VideoFrameTypeKey {
		_tFatalf(t, "frameType mismatch want keyframe got %s", m.FrameType)
	}
}

// TestParseVideoMessage_EnhancedVP8CodedFrames verifies that VP8 coded frames
// (with 3-byte composition time offset) are parsed correctly.
func TestParseVideoMessage_EnhancedVP8CodedFrames(t *testing.T) {
	compTime := []byte{0x00, 0x00, 0x00} // composition time = 0
	payload := []byte{0x33, 0x44}
	tag := buildEnhancedVideoTag(2, 1, "vp08", append(compTime, payload...)) // inter, CodedFrames
	m, err := ParseVideoMessage(tag)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if m.Codec != VideoCodecVP8 || m.FourCC != "vp08" {
		_tFatalf(t, "codec/fourcc mismatch: %s / %s", m.Codec, m.FourCC)
	}
	if m.FrameType != VideoFrameTypeInter {
		_tFatalf(t, "frameType mismatch want inter got %s", m.FrameType)
	}
	if m.PacketType != PacketTypeCodedFrames {
		_tFatalf(t, "packetType mismatch want coded_frames got %s", m.PacketType)
	}
	// Payload should exclude the 3-byte composition time
	if len(m.Payload) != 2 || m.Payload[0] != 0x33 {
		_tFatalf(t, "payload mismatch (should skip comp time): %+v", m.Payload)
	}
}

func TestParseVideoMessage_EnhancedCodedFramesX(t *testing.T) {
	// CodedFramesX (pktType=3) has NO composition time (DTS==PTS optimization).
	payload := []byte{0xAA, 0xBB, 0xCC}
	data := buildEnhancedVideoTag(1, 3, "hvc1", payload) // keyframe, CodedFramesX
	m, err := ParseVideoMessage(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if m.PacketType != PacketTypeCodedFramesX {
		_tFatalf(t, "packetType mismatch want coded_frames_x got %s", m.PacketType)
	}
	// Payload comes right after FourCC, no composition time skip
	if len(m.Payload) != 3 || m.Payload[0] != 0xAA {
		_tFatalf(t, "payload mismatch: %+v", m.Payload)
	}
}

func TestParseVideoMessage_EnhancedAVCViaFourCC(t *testing.T) {
	// AVC can also be signaled via enhanced FourCC "avc1"
	data := buildEnhancedVideoTag(1, 0, "avc1", []byte{0xFF})
	m, err := ParseVideoMessage(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if m.Codec != VideoCodecAVC || m.FourCC != "avc1" {
		_tFatalf(t, "codec/fourcc mismatch: %s / %s", m.Codec, m.FourCC)
	}
	if !m.Enhanced {
		_tFatalf(t, "should be enhanced")
	}
}

func TestParseVideoMessage_EnhancedVVC(t *testing.T) {
	data := buildEnhancedVideoTag(1, 0, "vvc1", []byte{0x01})
	m, err := ParseVideoMessage(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if m.Codec != VideoCodecVVC || m.FourCC != "vvc1" {
		_tFatalf(t, "codec/fourcc mismatch: %s / %s", m.Codec, m.FourCC)
	}
}

func TestParseVideoMessage_EnhancedSequenceEnd(t *testing.T) {
	data := buildEnhancedVideoTag(1, 2, "hvc1", nil) // SequenceEnd, no payload
	m, err := ParseVideoMessage(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if m.PacketType != PacketTypeSequenceEnd {
		_tFatalf(t, "packetType mismatch want sequence_end got %s", m.PacketType)
	}
}

func TestParseVideoMessage_EnhancedMetadata(t *testing.T) {
	// PacketType=4 (Metadata) with FrameType=5 (command/info)
	payload := []byte{0x02, 0x00, 0x09} // pretend AMF data
	data := buildEnhancedVideoTag(5, 4, "hvc1", payload)
	m, err := ParseVideoMessage(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if m.PacketType != PacketTypeMetadata {
		_tFatalf(t, "packetType mismatch want metadata got %s", m.PacketType)
	}
	if m.FrameType != "command" {
		_tFatalf(t, "frameType mismatch want command got %s", m.FrameType)
	}
}

// --- Enhanced Error Cases ---

func TestParseVideoMessage_EnhancedErrorCases(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
	}{
		{"enhancedTruncated", []byte{0x80, 'h', 'v'}},                     // only 3 bytes, need 5
		{"enhancedUnknownFourCC", []byte{0x80 | (1 << 4), 'Z', 'Z', 'Z', 'Z'}}, // unknown fourcc
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseVideoMessage(tc.in); err == nil {
				_tFatalf(t, "expected error for case %s", tc.name)
			}
		})
	}
}

// --- Legacy Error Cases ---

func TestParseVideoMessage_ErrorCases(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
	}{
		{"empty", []byte{}},
		{"truncatedAVC", []byte{(1 << 4) | 7}},
		{"unsupportedCodec", []byte{(1 << 4) | 5, 0x00}}, // codec 5 (On2 VP6) -> unsupported
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseVideoMessage(tc.in); err == nil {
				_tFatalf(t, "expected error for case %s", tc.name)
			}
		})
	}
}

// --- ModEx Nanosecond Timestamp Tests ---

// TestParseVideoMessage_ModExNanosecondTimestamp verifies that a ModEx video packet
// correctly extracts the nanosecond offset and provides the unwrapped payload.
// ModEx (PacketType 7) wraps another packet with modifier extensions; the most
// important modifier is the nanosecond timestamp offset for sub-ms A/V sync.
func TestParseVideoMessage_ModExNanosecondTimestamp(t *testing.T) {
	// Build the ModEx data that goes after the FourCC:
	//   byte 0: [ModExType=0 (TimestampOffsetNano):4bits][DataSizeCode=1 (2 bytes):4bits] → 0x01
	//   bytes 1-2: nanosecond offset value = 500 (0x01F4)
	//   bytes 3-4: wrapped payload (0xAA, 0xBB) — the inner video data
	modexAndPayload := []byte{0x01, 0x01, 0xF4, 0xAA, 0xBB}

	// Construct a full enhanced video tag: keyframe (1), ModEx packet type (7), HEVC codec
	tag := buildEnhancedVideoTag(1, 7, "hvc1", modexAndPayload)
	m, err := ParseVideoMessage(tag)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if m.PacketType != PacketTypeModEx {
		_tFatalf(t, "packetType mismatch: got %s, want modex", m.PacketType)
	}
	// The nanosecond offset should be extracted from the ModEx header.
	if m.NanosecondOffset != 500 {
		_tFatalf(t, "nano offset mismatch: got %d, want 500", m.NanosecondOffset)
	}
	// The payload should be the unwrapped inner data (after the ModEx header).
	if len(m.Payload) != 2 || m.Payload[0] != 0xAA || m.Payload[1] != 0xBB {
		_tFatalf(t, "payload mismatch: got %v, want [0xAA 0xBB]", m.Payload)
	}
}

// TestParseVideoMessage_ModExLargeNanosecondOffset verifies extraction of a larger
// nanosecond offset (999999) stored in 4 bytes (dataSizeCode=3).
func TestParseVideoMessage_ModExLargeNanosecondOffset(t *testing.T) {
	// byte 0: type=0, dataSizeCode=3 (4 bytes) → 0x03
	// bytes 1-4: 999999 = 0x000F423F
	// byte 5: wrapped payload
	modexAndPayload := []byte{0x03, 0x00, 0x0F, 0x42, 0x3F, 0xCC}
	tag := buildEnhancedVideoTag(1, 7, "hvc1", modexAndPayload)
	m, err := ParseVideoMessage(tag)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if m.NanosecondOffset != 999999 {
		_tFatalf(t, "nano offset mismatch: got %d, want 999999", m.NanosecondOffset)
	}
}

// TestParseVideoMessage_ModExInvalidFallback verifies that when the ModEx data is
// too short to parse, the parser falls back to passing through raw data after FourCC
// instead of returning an error.
func TestParseVideoMessage_ModExInvalidFallback(t *testing.T) {
	// Only 1 byte of ModEx data (too short — ParseModEx needs at least 2 bytes)
	modexAndPayload := []byte{0x00}
	tag := buildEnhancedVideoTag(1, 7, "hvc1", modexAndPayload)
	m, err := ParseVideoMessage(tag)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if m.PacketType != PacketTypeModEx {
		_tFatalf(t, "packetType mismatch: got %s, want modex", m.PacketType)
	}
	// NanosecondOffset should be zero since ModEx parsing failed.
	if m.NanosecondOffset != 0 {
		_tFatalf(t, "nano offset should be 0 on parse failure, got %d", m.NanosecondOffset)
	}
	// Raw fallback payload should be whatever was after FourCC.
	if len(m.Payload) != 1 || m.Payload[0] != 0x00 {
		_tFatalf(t, "payload mismatch on fallback: got %v", m.Payload)
	}
}

// --- IsVideoSequenceHeader Tests ---

func TestIsVideoSequenceHeader(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want bool
	}{
		{"legacyAVC_seqHeader", []byte{(1 << 4) | 7, 0x00}, true},
		{"legacyAVC_nalu", []byte{(1 << 4) | 7, 0x01}, false},
		{"legacyHEVC_seqHeader", []byte{(1 << 4) | 12, 0x00}, true},
		{"enhancedSeqStart", buildEnhancedVideoTag(1, 0, "hvc1", []byte{0x01}), true},
		{"enhancedVP8SeqStart", buildEnhancedVideoTag(1, 0, "vp08", []byte{0x01}), true},
		{"enhancedCodedFrames", buildEnhancedVideoTag(2, 1, "hvc1", []byte{0x00, 0x00, 0x00, 0x01}), false},
		{"enhancedCodedFramesX", buildEnhancedVideoTag(1, 3, "hvc1", []byte{0x01}), false},
		{"tooShort", []byte{0x17}, false},
		{"empty", []byte{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsVideoSequenceHeader(tc.data)
			if got != tc.want {
				t.Errorf("IsVideoSequenceHeader(%v) = %v, want %v", tc.data[:min(len(tc.data), 5)], got, tc.want)
			}
		})
	}
}

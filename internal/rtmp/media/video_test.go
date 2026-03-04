// video_test.go – tests for RTMP video message parsing.
//
// RTMP video messages (TypeID 9) encode metadata in the first byte:
//   - High nibble (bits 7-4) = FrameType (1=keyframe, 2=inter-frame)
//   - Low nibble (bits 3-0) = CodecID (7=AVC/H.264)
//   - For AVC: second byte = AVCPacketType (0=sequence header, 1=NALU)
//
// Tests verify parsing of:
//   - AVC keyframe sequence header (SPS/PPS configuration).
//   - AVC keyframe NALU (actual video data).
//   - AVC inter-frame NALU (P/B frames).
//   - Error cases: empty, truncated, unsupported codec.
package media

import "testing"

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

// TestParseVideoMessage_ErrorCases is a table-driven negative test:
// empty data, truncated AVC header, and unsupported codec (VP6=5).
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

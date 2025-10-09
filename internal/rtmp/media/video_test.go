package media

import "testing"

// Helper reused from audio tests style.
func _tVidFatalf(t *testing.T, format string, args ...interface{}) {
	t.Helper()
	t.Fatalf(format, args...)
}

func TestParseVideoMessage_AVCSequenceHeader(t *testing.T) {
	// frameType=1 (keyframe), codecID=7 (AVC)
	data := []byte{(1 << 4) | 7, 0x00, 0x17, 0x34, 0x56} // 0x00 = sequence header; rest pretend SPS/PPS bytes
	m, err := ParseVideoMessage(data)
	if err != nil {
		_tVidFatalf(t, "unexpected error: %v", err)
	}
	if m.Codec != VideoCodecAVC {
		_tVidFatalf(t, "codec mismatch want AVC got %s", m.Codec)
	}
	if m.FrameType != VideoFrameTypeKey {
		_tVidFatalf(t, "frameType mismatch want keyframe got %s", m.FrameType)
	}
	if m.PacketType != AVCPacketTypeSequenceHeader {
		_tVidFatalf(t, "packetType mismatch want sequence_header got %s", m.PacketType)
	}
	if len(m.Payload) != 3 || m.Payload[0] != 0x17 {
		_tVidFatalf(t, "payload mismatch: %+v", m.Payload)
	}
}

func TestParseVideoMessage_AVCKeyframeNALU(t *testing.T) {
	// frameType=1 keyframe, codecID=7 AVC, avcPacketType=1 (NALU)
	data := []byte{(1 << 4) | 7, 0x01, 0xAA, 0xBB, 0xCC}
	m, err := ParseVideoMessage(data)
	if err != nil {
		_tVidFatalf(t, "unexpected error: %v", err)
	}
	if m.Codec != VideoCodecAVC || m.PacketType != AVCPacketTypeNALU || m.FrameType != VideoFrameTypeKey {
		_tVidFatalf(t, "unexpected metadata: %+v", m)
	}
	if len(m.Payload) != 3 || m.Payload[2] != 0xCC {
		_tVidFatalf(t, "payload mismatch: %+v", m.Payload)
	}
}

func TestParseVideoMessage_AVCInterNALU(t *testing.T) {
	// frameType=2 inter, codecID=7 AVC, avcPacketType=1 (NALU)
	data := []byte{(2 << 4) | 7, 0x01, 0x01, 0x02}
	m, err := ParseVideoMessage(data)
	if err != nil {
		_tVidFatalf(t, "unexpected error: %v", err)
	}
	if m.FrameType != VideoFrameTypeInter || m.PacketType != AVCPacketTypeNALU {
		_tVidFatalf(t, "unexpected metadata: %+v", m)
	}
	if len(m.Payload) != 2 || m.Payload[0] != 0x01 {
		_tVidFatalf(t, "payload mismatch: %+v", m.Payload)
	}
}

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
		if _, err := ParseVideoMessage(tc.in); err == nil {
			_tVidFatalf(t, "expected error for case %s", tc.name)
		}
	}
}

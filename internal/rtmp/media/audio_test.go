// audio_test.go – tests for RTMP audio message parsing.
//
// RTMP audio messages (TypeID 8) encode codec info in the first byte:
//   - High nibble (bits 7-4) = SoundFormat (10=AAC, 2=MP3, etc.)
//   - For AAC: second byte = packet type (0=sequence header, 1=raw)
//
// Tests verify:
//   - AAC sequence header: codec=AAC, packetType=sequence_header, payload.
//   - AAC raw frame: packetType=raw.
//   - MP3: no packet type sub-field.
//   - Error cases: empty, truncated AAC, unsupported format.
package media

import "testing"

// TestParseAudioMessage_AACSequenceHeader verifies parsing of an AAC
// sequence header (soundFormat=10, aacPacketType=0). The payload after
// the 2-byte header should be extracted as AudioDecoderSpecificConfig.
func TestParseAudioMessage_AACSequenceHeader(t *testing.T) {
	// soundFormat=10 (AAC) in high nibble, rest bits zero.
	data := []byte{10 << 4, 0x00, 0x12, 0x34, 0x56}
	m, err := ParseAudioMessage(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if m.Codec != AudioCodecAAC {
		_tFatalf(t, "codec mismatch: want AAC got %s", m.Codec)
	}
	if m.PacketType != AACPacketTypeSequenceHeader {
		_tFatalf(t, "packetType mismatch: want sequence_header got %s", m.PacketType)
	}
	if len(m.Payload) != 3 || m.Payload[0] != 0x12 {
		_tFatalf(t, "payload mismatch: %+v", m.Payload)
	}
}

// TestParseAudioMessage_AACRaw verifies parsing of an AAC raw audio
// frame (aacPacketType=1). Payload should be the raw AAC data.
func TestParseAudioMessage_AACRaw(t *testing.T) {
	data := []byte{10 << 4, 0x01, 0xDE, 0xAD, 0xBE, 0xEF}
	m, err := ParseAudioMessage(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if m.Codec != AudioCodecAAC || m.PacketType != AACPacketTypeRaw {
		_tFatalf(t, "unexpected codec/packet: %+v", m)
	}
	if len(m.Payload) != 4 || m.Payload[0] != 0xDE {
		_tFatalf(t, "payload mismatch: %+v", m.Payload)
	}
}

// TestParseAudioMessage_MP3 verifies that MP3 (soundFormat=2) is
// recognized. MP3 has no sub-packet-type field, so PacketType is empty.
func TestParseAudioMessage_MP3(t *testing.T) {
	data := []byte{2<<4 | 0x02, 0x11, 0x22, 0x33}
	m, err := ParseAudioMessage(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if m.Codec != AudioCodecMP3 {
		_tFatalf(t, "codec mismatch: want MP3 got %s", m.Codec)
	}
	if m.PacketType != "" {
		_tFatalf(t, "mp3 packetType should be empty got %s", m.PacketType)
	}
	if len(m.Payload) != 3 || m.Payload[0] != 0x11 {
		_tFatalf(t, "payload mismatch: %+v", m.Payload)
	}
}

// TestParseAudioMessage_Errors is a table-driven negative test covering:
// empty data, AAC with truncated header, and unsupported codec (format 15).
func TestParseAudioMessage_Errors(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
	}{
		{"empty", []byte{}},
		{"aacTruncated", []byte{10 << 4}},
		{"unsupported", []byte{15 << 4, 0x01}}, // 15 not supported
	}
	for _, tc := range cases {
		if _, err := ParseAudioMessage(tc.in); err == nil {
			_tFatalf(t, "expected error for case %s", tc.name)
		}
	}
}

// _tFatalf is a test helper that marks itself with t.Helper() so failure
// line numbers point to the caller, not this function.
func _tFatalf(t *testing.T, format string, args ...interface{}) {
	// Mark as helper for cleaner failure line numbers.
	// The project uses table-driven style; this is consistent.
	t.Helper()
	t.Fatalf(format, args...)
}

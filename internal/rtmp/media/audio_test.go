// audio_test.go – tests for RTMP audio message parsing.
//
// Tests cover both legacy FLV format and Enhanced RTMP (E-RTMP) format:
//
// Legacy: High nibble (bits 7-4) = SoundFormat (10=AAC, 2=MP3, etc.)
// Enhanced: SoundFormat=9 (ExHeader) + AudioPacketType(4bit) + 4-byte FourCC
package media

import "testing"

// --- Legacy Format Tests ---

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
	if m.Enhanced {
		_tFatalf(t, "should not be enhanced")
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

// --- Enhanced RTMP Audio Tests ---

// buildEnhancedAudioTag constructs a minimal Enhanced RTMP audio tag.
// header byte: SoundFormat=9 (bits[7:4]) | AudioPacketType (bits[3:0])
func buildEnhancedAudioTag(pktType uint8, fourCC string, payload []byte) []byte {
	b0 := byte(9<<4) | pktType
	data := []byte{b0}
	data = append(data, []byte(fourCC)...)
	data = append(data, payload...)
	return data
}

func TestParseAudioMessage_EnhancedAACSequenceStart(t *testing.T) {
	payload := []byte{0x12, 0x10} // pretend AudioSpecificConfig
	data := buildEnhancedAudioTag(0, "mp4a", payload) // SequenceStart
	m, err := ParseAudioMessage(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if !m.Enhanced {
		_tFatalf(t, "should be enhanced")
	}
	if m.Codec != AudioCodecAAC {
		_tFatalf(t, "codec mismatch want AAC got %s", m.Codec)
	}
	if m.FourCC != "mp4a" {
		_tFatalf(t, "fourcc mismatch want mp4a got %s", m.FourCC)
	}
	if m.PacketType != AudioPacketTypeSequenceStart {
		_tFatalf(t, "packetType mismatch want sequence_start got %s", m.PacketType)
	}
	if len(m.Payload) != 2 || m.Payload[0] != 0x12 {
		_tFatalf(t, "payload mismatch: %+v", m.Payload)
	}
}

func TestParseAudioMessage_EnhancedOpusCodedFrames(t *testing.T) {
	payload := []byte{0xAA, 0xBB, 0xCC}
	data := buildEnhancedAudioTag(1, "Opus", payload) // CodedFrames
	m, err := ParseAudioMessage(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if m.Codec != AudioCodecOpus || m.FourCC != "Opus" {
		_tFatalf(t, "codec/fourcc mismatch: %s / %s", m.Codec, m.FourCC)
	}
	if m.PacketType != AudioPacketTypeCodedFrames {
		_tFatalf(t, "packetType mismatch want coded_frames got %s", m.PacketType)
	}
}

func TestParseAudioMessage_EnhancedFLAC(t *testing.T) {
	data := buildEnhancedAudioTag(0, "fLaC", []byte{0x01})
	m, err := ParseAudioMessage(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if m.Codec != AudioCodecFLAC || m.FourCC != "fLaC" {
		_tFatalf(t, "codec/fourcc mismatch: %s / %s", m.Codec, m.FourCC)
	}
}

func TestParseAudioMessage_EnhancedAC3(t *testing.T) {
	data := buildEnhancedAudioTag(1, "ac-3", []byte{0x01, 0x02})
	m, err := ParseAudioMessage(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if m.Codec != AudioCodecAC3 || m.FourCC != "ac-3" {
		_tFatalf(t, "codec/fourcc mismatch: %s / %s", m.Codec, m.FourCC)
	}
}

func TestParseAudioMessage_EnhancedEAC3(t *testing.T) {
	data := buildEnhancedAudioTag(1, "ec-3", []byte{0x01})
	m, err := ParseAudioMessage(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if m.Codec != AudioCodecEAC3 || m.FourCC != "ec-3" {
		_tFatalf(t, "codec/fourcc mismatch: %s / %s", m.Codec, m.FourCC)
	}
}

func TestParseAudioMessage_EnhancedMP3ViaFourCC(t *testing.T) {
	data := buildEnhancedAudioTag(1, ".mp3", []byte{0xFF, 0xFB})
	m, err := ParseAudioMessage(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if m.Codec != AudioCodecMP3 || m.FourCC != ".mp3" {
		_tFatalf(t, "codec/fourcc mismatch: %s / %s", m.Codec, m.FourCC)
	}
	if !m.Enhanced {
		_tFatalf(t, "should be enhanced")
	}
}

// --- Enhanced Error Cases ---

func TestParseAudioMessage_EnhancedErrorCases(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
	}{
		{"enhancedTruncated", []byte{9 << 4, 'm', 'p'}},                             // only 3 bytes, need 5
		{"enhancedUnknownFourCC", []byte{9 << 4, 'Z', 'Z', 'Z', 'Z'}},              // unknown fourcc
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseAudioMessage(tc.in); err == nil {
				_tFatalf(t, "expected error for case %s", tc.name)
			}
		})
	}
}

// --- Legacy Error Cases ---

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
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseAudioMessage(tc.in); err == nil {
				_tFatalf(t, "expected error for case %s", tc.name)
			}
		})
	}
}

// --- IsAudioSequenceHeader Tests ---

func TestIsAudioSequenceHeader(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want bool
	}{
		{"legacyAAC_seqHeader", []byte{10 << 4, 0x00}, true},
		{"legacyAAC_raw", []byte{10 << 4, 0x01}, false},
		{"legacyMP3", []byte{2 << 4, 0x00}, false},
		{"enhancedSeqStart", buildEnhancedAudioTag(0, "mp4a", []byte{0x12}), true},
		{"enhancedCodedFrames", buildEnhancedAudioTag(1, "Opus", []byte{0x01}), false},
		{"tooShort", []byte{10 << 4}, false},
		{"empty", []byte{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsAudioSequenceHeader(tc.data)
			if got != tc.want {
				t.Errorf("IsAudioSequenceHeader() = %v, want %v", got, tc.want)
			}
		})
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

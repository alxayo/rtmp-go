package codec

import (
	"testing"
)

// buildTestADTS creates a minimal valid ADTS frame for testing.
// Parameters reflect common live-stream settings.
func buildTestADTS(profile, freqIdx, chanConfig uint8, dataLen int) []byte {
	frameLen := 7 + dataLen // 7-byte header + raw data

	buf := make([]byte, frameLen)
	// Sync word (12 bits: 0xFFF)
	buf[0] = 0xFF
	buf[1] = 0xF1 // ID=0 (MPEG-4), Layer=0, protection_absent=1

	// Profile (2 bits) | FreqIdx (4 bits) | Private (1 bit) | ChannelConfig high (1 bit)
	buf[2] = (profile << 6) | (freqIdx << 2) | (chanConfig >> 2)

	// ChannelConfig low (2 bits) | original_copy | home | copyright_id | copyright_start | frame_length high (2 bits)
	buf[3] = (chanConfig << 6) | byte((frameLen>>11)&0x03)

	// Frame length middle (8 bits)
	buf[4] = byte((frameLen >> 3) & 0xFF)

	// Frame length low (3 bits) | buffer fullness high (5 bits)
	buf[5] = byte((frameLen&0x07)<<5) | 0x1F

	// Buffer fullness low (6 bits) | number of frames - 1 (2 bits)
	buf[6] = 0xFC

	// Fill with dummy audio data
	for i := 7; i < frameLen; i++ {
		buf[i] = 0xAA
	}

	return buf
}

// TestParseADTSHeader tests ADTS header parsing.
func TestParseADTSHeader(t *testing.T) {
	// AAC-LC, 44.1kHz, stereo, 128 bytes of audio data
	data := buildTestADTS(1, 4, 2, 128)

	h, err := ParseADTSHeader(data)
	if err != nil {
		t.Fatalf("ParseADTSHeader: %v", err)
	}

	if h.Profile != 1 {
		t.Errorf("Profile: got %d, want 1 (AAC-LC)", h.Profile)
	}
	if h.SamplingFreqIdx != 4 {
		t.Errorf("SamplingFreqIdx: got %d, want 4 (44100Hz)", h.SamplingFreqIdx)
	}
	if h.ChannelConfig != 2 {
		t.Errorf("ChannelConfig: got %d, want 2 (stereo)", h.ChannelConfig)
	}
	if h.HeaderSize != 7 {
		t.Errorf("HeaderSize: got %d, want 7", h.HeaderSize)
	}
	if h.FrameLength != 135 { // 7 + 128
		t.Errorf("FrameLength: got %d, want 135", h.FrameLength)
	}
}

// TestParseADTSHeader_InvalidSync tests error on invalid sync word.
func TestParseADTSHeader_InvalidSync(t *testing.T) {
	data := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	_, err := ParseADTSHeader(data)
	if err == nil {
		t.Error("expected error for invalid sync word")
	}
}

// TestParseADTSHeader_TooShort tests error on truncated data.
func TestParseADTSHeader_TooShort(t *testing.T) {
	_, err := ParseADTSHeader([]byte{0xFF, 0xF1})
	if err == nil {
		t.Error("expected error for short data")
	}
}

// TestBuildAudioSpecificConfig tests AudioSpecificConfig construction.
func TestBuildAudioSpecificConfig(t *testing.T) {
	h := &ADTSHeader{
		Profile:         1, // AAC-LC → objectType = 2
		SamplingFreqIdx: 4, // 44100Hz
		ChannelConfig:   2, // Stereo
	}

	config := BuildAudioSpecificConfig(h)
	if len(config) != 2 {
		t.Fatalf("AudioSpecificConfig length: got %d, want 2", len(config))
	}

	// audioObjectType = 2 (5 bits): 00010
	// sampFreqIdx = 4 (4 bits):     0100
	// channelConfig = 2 (4 bits):   0010
	// padding = 0 (3 bits):         000
	// = 00010 0100 0010 000
	// = 0001 0010 0001 0000
	// = 0x12 0x10
	if config[0] != 0x12 || config[1] != 0x10 {
		t.Errorf("AudioSpecificConfig: got [0x%02X 0x%02X], want [0x12 0x10]", config[0], config[1])
	}
}

// TestBuildAACSequenceHeader tests the RTMP AAC sequence header construction.
func TestBuildAACSequenceHeader(t *testing.T) {
	h := &ADTSHeader{
		Profile:         1,
		SamplingFreqIdx: 4,
		ChannelConfig:   2,
	}

	header := BuildAACSequenceHeader(h)

	// Byte 0: 0xAF (AAC, 44kHz, 16-bit, stereo)
	if header[0] != 0xAF {
		t.Errorf("byte 0: got 0x%02X, want 0xAF", header[0])
	}
	// Byte 1: 0x00 (sequence header)
	if header[1] != 0x00 {
		t.Errorf("byte 1: got 0x%02X, want 0x00 (sequence header)", header[1])
	}
	// Bytes 2-3: AudioSpecificConfig
	if len(header) != 4 {
		t.Errorf("length: got %d, want 4", len(header))
	}
}

// TestBuildAACFrame tests the RTMP AAC raw frame construction.
func TestBuildAACFrame(t *testing.T) {
	rawData := []byte{0xAA, 0xBB, 0xCC, 0xDD}
	frame := BuildAACFrame(rawData)

	if frame[0] != 0xAF {
		t.Errorf("byte 0: got 0x%02X, want 0xAF", frame[0])
	}
	if frame[1] != 0x01 {
		t.Errorf("byte 1: got 0x%02X, want 0x01 (raw frame)", frame[1])
	}
	if len(frame) != 6 { // 2 header + 4 data
		t.Errorf("length: got %d, want 6", len(frame))
	}
}

// TestStripADTS tests ADTS header stripping.
func TestStripADTS(t *testing.T) {
	data := buildTestADTS(1, 4, 2, 64) // 7-byte header + 64 bytes data

	raw, h, err := StripADTS(data)
	if err != nil {
		t.Fatalf("StripADTS: %v", err)
	}

	if len(raw) != 64 {
		t.Errorf("raw data length: got %d, want 64", len(raw))
	}
	if h.Profile != 1 {
		t.Errorf("profile: got %d, want 1", h.Profile)
	}
}

// TestStripADTS_Truncated tests error on truncated ADTS frame.
func TestStripADTS_Truncated(t *testing.T) {
	// Header says 135 bytes total, but we only provide 10
	data := buildTestADTS(1, 4, 2, 128)
	_, _, err := StripADTS(data[:10])
	if err == nil {
		t.Error("expected error for truncated frame")
	}
}

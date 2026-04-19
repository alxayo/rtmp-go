package codec

import (
	"testing"
)

// buildTestAC3SyncFrame creates a minimal valid AC-3 syncframe for testing.
// Parameters:
//   - fscod:      sample rate code (0=48kHz, 1=44.1kHz, 2=32kHz)
//   - frmsizecod: frame size code (0-37, determines bit rate and frame size)
//   - bsid:       bitstream ID (must be ≤ 10 for AC-3)
//   - bsmod:      bitstream mode (0=main audio)
//   - acmod:      audio coding mode (channel layout, 0-7)
func buildTestAC3SyncFrame(fscod, frmsizecod, bsid, bsmod, acmod uint8) []byte {
	// Build an 8-byte header (minimum we need) plus some padding
	buf := make([]byte, 16)

	// Bytes 0-1: Syncword 0x0B77
	buf[0] = 0x0B
	buf[1] = 0x77

	// Bytes 2-3: CRC1 (dummy value, not validated by our parser)
	buf[2] = 0x00
	buf[3] = 0x00

	// Byte 4: [fscod:2][frmsizecod:6]
	buf[4] = (fscod << 6) | (frmsizecod & 0x3F)

	// Byte 5: [bsid:5][bsmod:3]
	buf[5] = (bsid << 3) | (bsmod & 0x07)

	// Byte 6: [acmod:3][cmixlev/surmixlev/other:5]
	buf[6] = (acmod << 5)

	// Remaining bytes: dummy data
	for i := 7; i < len(buf); i++ {
		buf[i] = 0xAA
	}

	return buf
}

// TestParseAC3SyncFrame tests AC-3 syncframe header parsing with typical values.
func TestParseAC3SyncFrame(t *testing.T) {
	// 48kHz, frmsizecod=26 (320 kbps), bsid=8, bsmod=0, acmod=7 (5 channels / 5.1 layout)
	data := buildTestAC3SyncFrame(0, 26, 8, 0, 7)

	info, err := ParseAC3SyncFrame(data)
	if err != nil {
		t.Fatalf("ParseAC3SyncFrame: %v", err)
	}

	if info.SampleRate != 48000 {
		t.Errorf("SampleRate: got %d, want 48000", info.SampleRate)
	}
	if info.Channels != 5 {
		t.Errorf("Channels: got %d, want 5 (acmod=7 → L,C,R,SL,SR)", info.Channels)
	}
	if info.Fscod != 0 {
		t.Errorf("Fscod: got %d, want 0", info.Fscod)
	}
	if info.Frmsizecod != 26 {
		t.Errorf("Frmsizecod: got %d, want 26", info.Frmsizecod)
	}
	if info.Bsid != 8 {
		t.Errorf("Bsid: got %d, want 8", info.Bsid)
	}
	if info.Bsmod != 0 {
		t.Errorf("Bsmod: got %d, want 0", info.Bsmod)
	}
	if info.Acmod != 7 {
		t.Errorf("Acmod: got %d, want 7", info.Acmod)
	}
}

// TestParseAC3SyncFrame_Stereo tests parsing a stereo AC-3 stream at 44.1kHz.
func TestParseAC3SyncFrame_Stereo(t *testing.T) {
	// 44.1kHz, frmsizecod=16 (128 kbps), bsid=6, bsmod=0, acmod=2 (stereo)
	data := buildTestAC3SyncFrame(1, 16, 6, 0, 2)

	info, err := ParseAC3SyncFrame(data)
	if err != nil {
		t.Fatalf("ParseAC3SyncFrame: %v", err)
	}

	if info.SampleRate != 44100 {
		t.Errorf("SampleRate: got %d, want 44100", info.SampleRate)
	}
	if info.Channels != 2 {
		t.Errorf("Channels: got %d, want 2 (stereo)", info.Channels)
	}
	if info.Acmod != 2 {
		t.Errorf("Acmod: got %d, want 2", info.Acmod)
	}
}

// TestParseAC3SyncFrame_InvalidSyncword tests error handling for bad syncword.
func TestParseAC3SyncFrame_InvalidSyncword(t *testing.T) {
	data := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	_, err := ParseAC3SyncFrame(data)
	if err == nil {
		t.Error("expected error for invalid syncword")
	}
}

// TestParseAC3SyncFrame_TooShort tests error handling for truncated data.
func TestParseAC3SyncFrame_TooShort(t *testing.T) {
	// Only 4 bytes — not enough for the full header
	data := []byte{0x0B, 0x77, 0x00, 0x00}
	_, err := ParseAC3SyncFrame(data)
	if err == nil {
		t.Error("expected error for too-short data")
	}
}

// TestParseAC3SyncFrame_ReservedFscod tests error handling for reserved fscod=3.
func TestParseAC3SyncFrame_ReservedFscod(t *testing.T) {
	data := buildTestAC3SyncFrame(3, 0, 8, 0, 2) // fscod=3 is reserved
	_, err := ParseAC3SyncFrame(data)
	if err == nil {
		t.Error("expected error for reserved fscod value 3")
	}
}

// TestParseAC3SyncFrame_InvalidFrmsizecod tests error for out-of-range frmsizecod.
func TestParseAC3SyncFrame_InvalidFrmsizecod(t *testing.T) {
	data := buildTestAC3SyncFrame(0, 38, 8, 0, 2) // frmsizecod=38 is invalid (max 37)
	_, err := ParseAC3SyncFrame(data)
	if err == nil {
		t.Error("expected error for invalid frmsizecod 38")
	}
}

// TestParseAC3SyncFrame_EAC3Bsid tests that bsid > 10 is rejected as not AC-3.
func TestParseAC3SyncFrame_EAC3Bsid(t *testing.T) {
	data := buildTestAC3SyncFrame(0, 26, 16, 0, 7) // bsid=16 is E-AC-3
	_, err := ParseAC3SyncFrame(data)
	if err == nil {
		t.Error("expected error for E-AC-3 bsid (16)")
	}
}

// TestBuildAC3SequenceHeader tests the Enhanced RTMP sequence header construction.
func TestBuildAC3SequenceHeader(t *testing.T) {
	info := &AC3SyncInfo{
		SampleRate: 48000,
		Channels:   5,
		Fscod:      0,
		Frmsizecod: 26,
		Bsid:       8,
		Bsmod:      0,
		Acmod:      7,
	}

	header := BuildAC3SequenceHeader(info)

	// Should be 8 bytes: 1 header + 4 FourCC + 3 config
	if len(header) != 8 {
		t.Fatalf("header length: got %d, want 8", len(header))
	}

	// Byte 0: 0x90 (SoundFormat=9, AudioPacketType=0)
	if header[0] != 0x90 {
		t.Errorf("byte 0: got 0x%02X, want 0x90", header[0])
	}

	// Bytes 1-4: FourCC 'ac-3'
	fourCC := string(header[1:5])
	if fourCC != "ac-3" {
		t.Errorf("FourCC: got %q, want %q", fourCC, "ac-3")
	}

	// Verify the config bytes encode the correct values.
	// Byte 5: [fscod=0:2][bsid=8:5][bsmod_high=0:1] = 0b00_10000_0 = 0x10
	if header[5] != 0x10 {
		t.Errorf("config byte 0: got 0x%02X, want 0x10", header[5])
	}

	// Byte 6: [bsmod_low=00:2][acmod=111:3][lfeon=0:1][bit_rate_code_high:2]
	// bit_rate_code = frmsizecod/2 = 13 = 0b01101
	// bit_rate_code_high = 01
	// = 0b00_111_0_01 = 0x39
	if header[6] != 0x39 {
		t.Errorf("config byte 1: got 0x%02X, want 0x39", header[6])
	}

	// Byte 7: [bit_rate_code_low=101:3][reserved=00000:5]
	// = 0b101_00000 = 0xA0
	if header[7] != 0xA0 {
		t.Errorf("config byte 2: got 0x%02X, want 0xA0", header[7])
	}
}

// TestBuildAC3AudioFrame tests the Enhanced RTMP audio frame construction.
func TestBuildAC3AudioFrame(t *testing.T) {
	// Simulated raw AC-3 syncframe data
	rawData := []byte{0x0B, 0x77, 0xAA, 0xBB, 0xCC, 0xDD}
	frame := BuildAC3AudioFrame(rawData)

	// Should be 5 + len(rawData) = 11 bytes
	if len(frame) != 11 {
		t.Fatalf("frame length: got %d, want 11", len(frame))
	}

	// Byte 0: 0x91 (SoundFormat=9, AudioPacketType=1)
	if frame[0] != 0x91 {
		t.Errorf("byte 0: got 0x%02X, want 0x91", frame[0])
	}

	// Bytes 1-4: FourCC 'ac-3'
	fourCC := string(frame[1:5])
	if fourCC != "ac-3" {
		t.Errorf("FourCC: got %q, want %q", fourCC, "ac-3")
	}

	// Bytes 5+: should match original data
	for i, b := range rawData {
		if frame[5+i] != b {
			t.Errorf("payload byte %d: got 0x%02X, want 0x%02X", i, frame[5+i], b)
		}
	}
}

// TestBuildAC3AudioFrame_EmptyData tests frame building with empty payload.
func TestBuildAC3AudioFrame_EmptyData(t *testing.T) {
	frame := BuildAC3AudioFrame([]byte{})

	// Should be exactly 5 bytes (header + FourCC only)
	if len(frame) != 5 {
		t.Fatalf("frame length: got %d, want 5", len(frame))
	}

	if frame[0] != 0x91 {
		t.Errorf("byte 0: got 0x%02X, want 0x91", frame[0])
	}
}

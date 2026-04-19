package codec

import (
	"testing"
)

// buildTestEAC3SyncFrame creates a minimal valid E-AC-3 syncframe for testing.
// Parameters:
//   - fscod:  sample rate code (0=48kHz, 1=44.1kHz, 2=32kHz)
//   - frmsiz: 11-bit frame size field. Frame size in bytes = (frmsiz+1)*2
//   - bsid:   bitstream ID (must be > 10 for E-AC-3, typically 16)
//   - acmod:  audio coding mode (channel layout, 0-7)
//   - lfeon:  LFE channel present (true/false)
func buildTestEAC3SyncFrame(fscod uint8, frmsiz uint16, bsid, acmod uint8, lfeon bool) []byte {
	// Build a frame with the exact number of bytes indicated by frmsiz
	frameSize := int((frmsiz + 1) * 2)
	if frameSize < 8 {
		frameSize = 8 // Minimum size for our test frames
	}
	buf := make([]byte, frameSize)

	// Bytes 0-1: Syncword 0x0B77
	buf[0] = 0x0B
	buf[1] = 0x77

	// Byte 2: [strmtyp:2=00][substreamid:3=000][frmsiz_high:3]
	// strmtyp=0 (independent), substreamid=0
	buf[2] = byte((frmsiz >> 8) & 0x07) // only low 3 bits of high byte

	// Byte 3: [frmsiz_low:8]
	buf[3] = byte(frmsiz & 0xFF)

	// Byte 4: [fscod:2][numblkscod:2=11][acmod:3][lfeon:1]
	var lfeonBit uint8
	if lfeon {
		lfeonBit = 1
	}
	buf[4] = (fscod << 6) | (0x03 << 4) | (acmod << 1) | lfeonBit

	// Byte 5: [bsid:5][dialnorm_high:3]
	buf[5] = (bsid << 3)

	// Remaining bytes: dummy data
	for i := 6; i < len(buf); i++ {
		buf[i] = 0xBB
	}

	return buf
}

// TestParseEAC3SyncFrame tests E-AC-3 syncframe header parsing with 5.1 surround.
func TestParseEAC3SyncFrame(t *testing.T) {
	// 48kHz, frmsiz=127 (frame=256 bytes), bsid=16, acmod=7 (5ch), lfeon=true
	data := buildTestEAC3SyncFrame(0, 127, 16, 7, true)

	info, err := ParseEAC3SyncFrame(data)
	if err != nil {
		t.Fatalf("ParseEAC3SyncFrame: %v", err)
	}

	if info.SampleRate != 48000 {
		t.Errorf("SampleRate: got %d, want 48000", info.SampleRate)
	}
	if info.Channels != 5 {
		t.Errorf("Channels: got %d, want 5 (acmod=7 → L,C,R,SL,SR)", info.Channels)
	}
	if info.FrameSize != 256 {
		t.Errorf("FrameSize: got %d, want 256", info.FrameSize)
	}
	if info.Fscod != 0 {
		t.Errorf("Fscod: got %d, want 0", info.Fscod)
	}
	if info.Bsid != 16 {
		t.Errorf("Bsid: got %d, want 16", info.Bsid)
	}
	if info.Acmod != 7 {
		t.Errorf("Acmod: got %d, want 7", info.Acmod)
	}
	if !info.Lfeon {
		t.Error("Lfeon: got false, want true")
	}
}

// TestParseEAC3SyncFrame_Stereo tests parsing a stereo E-AC-3 stream.
func TestParseEAC3SyncFrame_Stereo(t *testing.T) {
	// 44.1kHz, frmsiz=63 (frame=128 bytes), bsid=16, acmod=2 (stereo), no LFE
	data := buildTestEAC3SyncFrame(1, 63, 16, 2, false)

	info, err := ParseEAC3SyncFrame(data)
	if err != nil {
		t.Fatalf("ParseEAC3SyncFrame: %v", err)
	}

	if info.SampleRate != 44100 {
		t.Errorf("SampleRate: got %d, want 44100", info.SampleRate)
	}
	if info.Channels != 2 {
		t.Errorf("Channels: got %d, want 2 (stereo)", info.Channels)
	}
	if info.Lfeon {
		t.Error("Lfeon: got true, want false")
	}
}

// TestParseEAC3SyncFrame_InvalidSyncword tests error handling for bad syncword.
func TestParseEAC3SyncFrame_InvalidSyncword(t *testing.T) {
	data := []byte{0xFF, 0xFF, 0x00, 0x00, 0x00, 0x00}
	_, err := ParseEAC3SyncFrame(data)
	if err == nil {
		t.Error("expected error for invalid syncword")
	}
}

// TestParseEAC3SyncFrame_TooShort tests error handling for truncated data.
func TestParseEAC3SyncFrame_TooShort(t *testing.T) {
	// Only 3 bytes — not enough
	data := []byte{0x0B, 0x77, 0x00}
	_, err := ParseEAC3SyncFrame(data)
	if err == nil {
		t.Error("expected error for too-short data")
	}
}

// TestParseEAC3SyncFrame_AC3Bsid tests that bsid ≤ 10 is rejected as not E-AC-3.
func TestParseEAC3SyncFrame_AC3Bsid(t *testing.T) {
	data := buildTestEAC3SyncFrame(0, 127, 8, 7, true) // bsid=8 is AC-3
	_, err := ParseEAC3SyncFrame(data)
	if err == nil {
		t.Error("expected error for AC-3 bsid (8)")
	}
}

// TestBuildEAC3SequenceHeader tests the Enhanced RTMP sequence header construction.
func TestBuildEAC3SequenceHeader(t *testing.T) {
	info := &EAC3SyncInfo{
		SampleRate: 48000,
		Channels:   5,
		Fscod:      0,
		FrameSize:  256,
		Bsid:       16,
		Bsmod:      0,
		Acmod:      7,
		Lfeon:      true,
	}

	header := BuildEAC3SequenceHeader(info)

	// Should be 8 bytes: 1 header + 4 FourCC + 3 config
	if len(header) != 8 {
		t.Fatalf("header length: got %d, want 8", len(header))
	}

	// Byte 0: 0x90 (SoundFormat=9, AudioPacketType=0)
	if header[0] != 0x90 {
		t.Errorf("byte 0: got 0x%02X, want 0x90", header[0])
	}

	// Bytes 1-4: FourCC 'ec-3'
	fourCC := string(header[1:5])
	if fourCC != "ec-3" {
		t.Errorf("FourCC: got %q, want %q", fourCC, "ec-3")
	}

	// Verify config encodes correct values:
	// 16-bit word: [num_ind_sub=0:3][fscod=0:2][bsid=16:5][bsmod=0:3][acmod=7:3]
	// = 0b000_00_10000_000_111
	// = 0b0000_0100_0000_0111
	// = 0x04 0x07
	if header[5] != 0x04 {
		t.Errorf("config byte 0: got 0x%02X, want 0x04", header[5])
	}
	if header[6] != 0x07 {
		t.Errorf("config byte 1: got 0x%02X, want 0x07", header[6])
	}

	// Byte 7: [lfeon=1:1][reserved=000:3][num_dep_sub=0000:4]
	// = 0b1_000_0000 = 0x80
	if header[7] != 0x80 {
		t.Errorf("config byte 2: got 0x%02X, want 0x80 (lfeon=1)", header[7])
	}
}

// TestBuildEAC3SequenceHeader_NoLFE tests sequence header without LFE channel.
func TestBuildEAC3SequenceHeader_NoLFE(t *testing.T) {
	info := &EAC3SyncInfo{
		SampleRate: 48000,
		Channels:   2,
		Fscod:      0,
		Bsid:       16,
		Bsmod:      0,
		Acmod:      2,
		Lfeon:      false,
	}

	header := BuildEAC3SequenceHeader(info)

	// Byte 7: [lfeon=0:1][reserved:3][num_dep_sub:4] = 0x00
	if header[7] != 0x00 {
		t.Errorf("config byte 2: got 0x%02X, want 0x00 (lfeon=0)", header[7])
	}
}

// TestBuildEAC3AudioFrame tests the Enhanced RTMP audio frame construction.
func TestBuildEAC3AudioFrame(t *testing.T) {
	// Simulated raw E-AC-3 syncframe data
	rawData := []byte{0x0B, 0x77, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}
	frame := BuildEAC3AudioFrame(rawData)

	// Should be 5 + len(rawData) = 13 bytes
	if len(frame) != 13 {
		t.Fatalf("frame length: got %d, want 13", len(frame))
	}

	// Byte 0: 0x91 (SoundFormat=9, AudioPacketType=1)
	if frame[0] != 0x91 {
		t.Errorf("byte 0: got 0x%02X, want 0x91", frame[0])
	}

	// Bytes 1-4: FourCC 'ec-3'
	fourCC := string(frame[1:5])
	if fourCC != "ec-3" {
		t.Errorf("FourCC: got %q, want %q", fourCC, "ec-3")
	}

	// Bytes 5+: should match original data
	for i, b := range rawData {
		if frame[5+i] != b {
			t.Errorf("payload byte %d: got 0x%02X, want 0x%02X", i, frame[5+i], b)
		}
	}
}

// TestBuildEAC3AudioFrame_EmptyData tests frame building with empty payload.
func TestBuildEAC3AudioFrame_EmptyData(t *testing.T) {
	frame := BuildEAC3AudioFrame([]byte{})

	// Should be exactly 5 bytes (header + FourCC only)
	if len(frame) != 5 {
		t.Fatalf("frame length: got %d, want 5", len(frame))
	}

	if frame[0] != 0x91 {
		t.Errorf("byte 0: got 0x%02X, want 0x91", frame[0])
	}
}

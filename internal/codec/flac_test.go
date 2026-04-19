package codec

import (
	"testing"
)

// buildTestFLACCodecPrivate creates a minimal FLAC CodecPrivate for testing.
// This simulates the Matroska CodecPrivate for FLAC, which contains:
//   - "fLaC" marker (4 bytes)
//   - METADATA_BLOCK_HEADER (4 bytes): [last_block=1:1][type=0(STREAMINFO):7][length=34:24]
//   - STREAMINFO (34 bytes): stream parameters
//
// Total: 42 bytes
func buildTestFLACCodecPrivate(sampleRate uint32, channels uint8, bitsPerSample uint8) []byte {
	buf := make([]byte, 42)

	// Bytes 0-3: "fLaC" stream marker
	copy(buf[0:4], []byte("fLaC"))

	// Bytes 4-7: METADATA_BLOCK_HEADER
	// [last_metadata_block=1:1][block_type=0(STREAMINFO):7][length=34:24]
	// = 0x80 (last block, type 0) + 0x000022 (length 34)
	buf[4] = 0x80 // last_metadata_block=1, block_type=0
	buf[5] = 0x00 // length high byte
	buf[6] = 0x00 // length mid byte
	buf[7] = 0x22 // length low byte (34 = 0x22)

	// Bytes 8-41: STREAMINFO (34 bytes)
	// Bytes 8-9: minimum block size = 4096
	buf[8] = 0x10 // 4096 >> 8
	buf[9] = 0x00 // 4096 & 0xFF

	// Bytes 10-11: maximum block size = 4096
	buf[10] = 0x10
	buf[11] = 0x00

	// Bytes 12-14: minimum frame size = 0 (unknown)
	buf[12] = 0x00
	buf[13] = 0x00
	buf[14] = 0x00

	// Bytes 15-17: maximum frame size = 0 (unknown)
	buf[15] = 0x00
	buf[16] = 0x00
	buf[17] = 0x00

	// Bytes 18-21: [sample_rate:20][channels_minus1:3][bits_per_sample_minus1:5][total_samples_high:4]
	// Sample rate (20 bits), channels-1 (3 bits), bits_per_sample-1 (5 bits), total_samples (4 bits)
	// Pack sample_rate (20 bits) into bytes 18-19 and top 4 bits of byte 20
	buf[18] = byte(sampleRate >> 12)
	buf[19] = byte(sampleRate >> 4)
	// byte 20: [sample_rate_low:4][channels_minus1:3][bps_minus1_high:1]
	channelsMinus1 := channels - 1
	bpsMinus1 := bitsPerSample - 1
	buf[20] = byte(sampleRate&0x0F)<<4 | (channelsMinus1&0x07)<<1 | (bpsMinus1>>4)&0x01
	// byte 21: [bps_minus1_low:4][total_samples_high:4]
	buf[21] = (bpsMinus1 & 0x0F) << 4

	// Bytes 22-25: total samples low 32 bits = 0 (unknown)
	// Bytes 26-41: MD5 signature (16 bytes, all zeros for testing)

	return buf
}

// TestBuildFLACSequenceHeader verifies the Enhanced RTMP SequenceStart for FLAC.
func TestBuildFLACSequenceHeader(t *testing.T) {
	codecPrivate := buildTestFLACCodecPrivate(44100, 2, 16)
	header := BuildFLACSequenceHeader(codecPrivate)

	// Should be 5 + 42 = 47 bytes
	if len(header) != 47 {
		t.Fatalf("header length: got %d, want 47", len(header))
	}

	// Byte 0: 0x90 (SoundFormat=9, AudioPacketType=0)
	if header[0] != 0x90 {
		t.Errorf("byte 0: got 0x%02X, want 0x90", header[0])
	}

	// Bytes 1-4: FourCC "fLaC" (case-sensitive!)
	fourCC := string(header[1:5])
	if fourCC != "fLaC" {
		t.Errorf("FourCC: got %q, want %q", fourCC, "fLaC")
	}

	// Bytes 5-8: "fLaC" marker from codec private
	marker := string(header[5:9])
	if marker != "fLaC" {
		t.Errorf("FLAC marker: got %q, want %q", marker, "fLaC")
	}

	// Byte 9: METADATA_BLOCK_HEADER first byte = 0x80 (last block, type STREAMINFO)
	if header[9] != 0x80 {
		t.Errorf("metadata block header: got 0x%02X, want 0x80", header[9])
	}

	// Bytes 10-12: STREAMINFO length = 34 (0x000022)
	if header[10] != 0x00 || header[11] != 0x00 || header[12] != 0x22 {
		t.Errorf("STREAMINFO length: got [0x%02X,0x%02X,0x%02X], want [0x00,0x00,0x22]",
			header[10], header[11], header[12])
	}
}

// TestBuildFLACSequenceHeader_FourCCCaseSensitivity verifies the FourCC is exactly "fLaC".
func TestBuildFLACSequenceHeader_FourCCCaseSensitivity(t *testing.T) {
	header := BuildFLACSequenceHeader([]byte{})

	// Verify each byte of the FourCC individually for case correctness
	if header[1] != 'f' {
		t.Errorf("FourCC byte 1: got '%c' (0x%02X), want 'f' (0x66)", header[1], header[1])
	}
	if header[2] != 'L' {
		t.Errorf("FourCC byte 2: got '%c' (0x%02X), want 'L' (0x4C)", header[2], header[2])
	}
	if header[3] != 'a' {
		t.Errorf("FourCC byte 3: got '%c' (0x%02X), want 'a' (0x61)", header[3], header[3])
	}
	if header[4] != 'C' {
		t.Errorf("FourCC byte 4: got '%c' (0x%02X), want 'C' (0x43)", header[4], header[4])
	}
}

// TestBuildFLACSequenceHeader_EmptyConfig verifies SequenceStart with empty config.
func TestBuildFLACSequenceHeader_EmptyConfig(t *testing.T) {
	header := BuildFLACSequenceHeader([]byte{})

	// Empty config: 1 header + 4 FourCC = 5 bytes
	if len(header) != 5 {
		t.Fatalf("header length: got %d, want 5", len(header))
	}

	if header[0] != 0x90 {
		t.Errorf("byte 0: got 0x%02X, want 0x90", header[0])
	}
}

// TestBuildFLACSequenceHeader_NilConfig verifies SequenceStart with nil config.
func TestBuildFLACSequenceHeader_NilConfig(t *testing.T) {
	header := BuildFLACSequenceHeader(nil)

	if len(header) != 5 {
		t.Fatalf("header length: got %d, want 5", len(header))
	}

	if header[0] != 0x90 {
		t.Errorf("byte 0: got 0x%02X, want 0x90", header[0])
	}
}

// TestBuildFLACAudioFrame verifies the Enhanced RTMP audio frame for FLAC.
func TestBuildFLACAudioFrame(t *testing.T) {
	// Simulated raw FLAC frame data (starts with sync code 0xFFF8)
	rawData := []byte{0xFF, 0xF8, 0x69, 0x18, 0x00, 0xAA, 0xBB, 0xCC}
	frame := BuildFLACAudioFrame(rawData)

	// Should be 5 + len(rawData) = 13 bytes
	if len(frame) != 13 {
		t.Fatalf("frame length: got %d, want 13", len(frame))
	}

	// Byte 0: 0x91 (SoundFormat=9, AudioPacketType=1)
	if frame[0] != 0x91 {
		t.Errorf("byte 0: got 0x%02X, want 0x91", frame[0])
	}

	// Bytes 1-4: FourCC "fLaC"
	fourCC := string(frame[1:5])
	if fourCC != "fLaC" {
		t.Errorf("FourCC: got %q, want %q", fourCC, "fLaC")
	}

	// Bytes 5+: should match original data
	for i, b := range rawData {
		if frame[5+i] != b {
			t.Errorf("payload byte %d: got 0x%02X, want 0x%02X", i, frame[5+i], b)
		}
	}
}

// TestBuildFLACAudioFrame_EmptyData tests frame building with empty payload.
func TestBuildFLACAudioFrame_EmptyData(t *testing.T) {
	frame := BuildFLACAudioFrame([]byte{})

	// Should be exactly 5 bytes (header + FourCC only)
	if len(frame) != 5 {
		t.Fatalf("frame length: got %d, want 5", len(frame))
	}

	if frame[0] != 0x91 {
		t.Errorf("byte 0: got 0x%02X, want 0x91", frame[0])
	}
}

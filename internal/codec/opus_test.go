package codec

import (
	"testing"
)

// buildTestOpusHead creates a minimal valid OpusHead structure for testing.
// OpusHead is 19 bytes:
//
//	Bytes 0-7:   "OpusHead" magic
//	Byte 8:      Version = 1
//	Byte 9:      Channel count
//	Bytes 10-11: Pre-skip (little-endian uint16)
//	Bytes 12-15: Input sample rate (little-endian uint32)
//	Bytes 16-17: Output gain (little-endian int16)
//	Byte 18:     Channel mapping family
func buildTestOpusHead(channels uint8, sampleRate uint32) []byte {
	head := make([]byte, 19)

	// Magic string "OpusHead"
	copy(head[0:8], []byte("OpusHead"))

	// Version = 1
	head[8] = 0x01

	// Channel count
	head[9] = channels

	// Pre-skip = 3840 samples (little-endian) — typical for 80ms at 48kHz
	head[10] = 0x00 // low byte
	head[11] = 0x0F // high byte (3840 = 0x0F00)

	// Input sample rate (little-endian) — 48000 Hz = 0x0000BB80
	head[12] = byte(sampleRate)
	head[13] = byte(sampleRate >> 8)
	head[14] = byte(sampleRate >> 16)
	head[15] = byte(sampleRate >> 24)

	// Output gain = 0 (no gain adjustment)
	head[16] = 0x00
	head[17] = 0x00

	// Channel mapping family = 0 (mono or stereo, no mapping table)
	head[18] = 0x00

	return head
}

// TestBuildOpusSequenceHeader verifies the Enhanced RTMP SequenceStart for Opus.
func TestBuildOpusSequenceHeader(t *testing.T) {
	opusHead := buildTestOpusHead(2, 48000)
	header := BuildOpusSequenceHeader(opusHead)

	// Should be 5 + 19 = 24 bytes
	if len(header) != 24 {
		t.Fatalf("header length: got %d, want 24", len(header))
	}

	// Byte 0: 0x90 (SoundFormat=9, AudioPacketType=0)
	if header[0] != 0x90 {
		t.Errorf("byte 0: got 0x%02X, want 0x90", header[0])
	}

	// Bytes 1-4: FourCC "Opus" (case-sensitive — capital 'O')
	fourCC := string(header[1:5])
	if fourCC != "Opus" {
		t.Errorf("FourCC: got %q, want %q", fourCC, "Opus")
	}

	// Bytes 5-12: "OpusHead" magic
	magic := string(header[5:13])
	if magic != "OpusHead" {
		t.Errorf("OpusHead magic: got %q, want %q", magic, "OpusHead")
	}

	// Byte 13: version = 1
	if header[13] != 0x01 {
		t.Errorf("OpusHead version: got %d, want 1", header[13])
	}

	// Byte 14: channel count = 2
	if header[14] != 0x02 {
		t.Errorf("OpusHead channels: got %d, want 2", header[14])
	}
}

// TestBuildOpusSequenceHeader_EmptyHead verifies SequenceStart with empty OpusHead.
func TestBuildOpusSequenceHeader_EmptyHead(t *testing.T) {
	header := BuildOpusSequenceHeader([]byte{})

	// Empty OpusHead: 1 header + 4 FourCC = 5 bytes
	if len(header) != 5 {
		t.Fatalf("header length: got %d, want 5", len(header))
	}

	if header[0] != 0x90 {
		t.Errorf("byte 0: got 0x%02X, want 0x90", header[0])
	}

	fourCC := string(header[1:5])
	if fourCC != "Opus" {
		t.Errorf("FourCC: got %q, want %q", fourCC, "Opus")
	}
}

// TestBuildOpusSequenceHeader_NilHead verifies SequenceStart with nil OpusHead.
func TestBuildOpusSequenceHeader_NilHead(t *testing.T) {
	header := BuildOpusSequenceHeader(nil)

	// Nil OpusHead: 1 header + 4 FourCC = 5 bytes
	if len(header) != 5 {
		t.Fatalf("header length: got %d, want 5", len(header))
	}

	if header[0] != 0x90 {
		t.Errorf("byte 0: got 0x%02X, want 0x90", header[0])
	}
}

// TestBuildOpusAudioFrame verifies the Enhanced RTMP audio frame for Opus.
func TestBuildOpusAudioFrame(t *testing.T) {
	// Simulated raw Opus packet data
	rawData := []byte{0xFC, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	frame := BuildOpusAudioFrame(rawData)

	// Should be 5 + len(rawData) = 12 bytes
	if len(frame) != 12 {
		t.Fatalf("frame length: got %d, want 12", len(frame))
	}

	// Byte 0: 0x91 (SoundFormat=9, AudioPacketType=1)
	if frame[0] != 0x91 {
		t.Errorf("byte 0: got 0x%02X, want 0x91", frame[0])
	}

	// Bytes 1-4: FourCC "Opus"
	fourCC := string(frame[1:5])
	if fourCC != "Opus" {
		t.Errorf("FourCC: got %q, want %q", fourCC, "Opus")
	}

	// Bytes 5+: should match original data
	for i, b := range rawData {
		if frame[5+i] != b {
			t.Errorf("payload byte %d: got 0x%02X, want 0x%02X", i, frame[5+i], b)
		}
	}
}

// TestBuildOpusAudioFrame_EmptyData tests frame building with empty payload.
func TestBuildOpusAudioFrame_EmptyData(t *testing.T) {
	frame := BuildOpusAudioFrame([]byte{})

	// Should be exactly 5 bytes (header + FourCC only)
	if len(frame) != 5 {
		t.Fatalf("frame length: got %d, want 5", len(frame))
	}

	if frame[0] != 0x91 {
		t.Errorf("byte 0: got 0x%02X, want 0x91", frame[0])
	}
}

// TestBuildOpusSequenceHeader_Mono verifies with mono OpusHead.
func TestBuildOpusSequenceHeader_Mono(t *testing.T) {
	opusHead := buildTestOpusHead(1, 48000)
	header := BuildOpusSequenceHeader(opusHead)

	// Byte 14 (offset 9 in OpusHead): channel count = 1
	if header[14] != 0x01 {
		t.Errorf("OpusHead channels: got %d, want 1", header[14])
	}
}

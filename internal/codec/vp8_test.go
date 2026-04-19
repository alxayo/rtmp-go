package codec

import (
	"testing"
)

// TestBuildVP8SequenceHeader verifies the Enhanced RTMP SequenceStart tag for VP8.
func TestBuildVP8SequenceHeader(t *testing.T) {
	header := BuildVP8SequenceHeader()

	// VP8 SequenceStart is exactly 5 bytes: 1 header + 4 FourCC (no config data)
	if len(header) != 5 {
		t.Fatalf("header length: got %d, want 5", len(header))
	}

	// Byte 0: 0x90 = [IsExHeader=1][FrameType=001(keyframe)][PacketType=0000(SequenceStart)]
	if header[0] != 0x90 {
		t.Errorf("byte 0: got 0x%02X, want 0x90", header[0])
	}

	// Bytes 1-4: FourCC "vp08"
	fourCC := string(header[1:5])
	if fourCC != "vp08" {
		t.Errorf("FourCC: got %q, want %q", fourCC, "vp08")
	}
}

// TestBuildVP8VideoFrame_Keyframe verifies CodedFramesX tag for a VP8 keyframe.
func TestBuildVP8VideoFrame_Keyframe(t *testing.T) {
	// Simulated VP8 keyframe data (starts with frame_type=0 bit)
	rawData := []byte{0x9D, 0x01, 0x2A, 0x80, 0x02, 0xE0, 0x01}
	frame := BuildVP8VideoFrame(rawData, true)

	// Should be 5 + len(rawData) = 12 bytes
	if len(frame) != 12 {
		t.Fatalf("frame length: got %d, want 12", len(frame))
	}

	// Byte 0: 0x93 = [IsExHeader=1][FrameType=001(keyframe)][PacketType=0011(CodedFramesX)]
	if frame[0] != 0x93 {
		t.Errorf("byte 0: got 0x%02X, want 0x93", frame[0])
	}

	// Bytes 1-4: FourCC "vp08"
	fourCC := string(frame[1:5])
	if fourCC != "vp08" {
		t.Errorf("FourCC: got %q, want %q", fourCC, "vp08")
	}

	// Bytes 5+: should match original data
	for i, b := range rawData {
		if frame[5+i] != b {
			t.Errorf("payload byte %d: got 0x%02X, want 0x%02X", i, frame[5+i], b)
		}
	}
}

// TestBuildVP8VideoFrame_Inter verifies CodedFramesX tag for a VP8 inter-frame.
func TestBuildVP8VideoFrame_Inter(t *testing.T) {
	// Simulated VP8 inter-frame data (frame_type=1 in bit 0)
	rawData := []byte{0x01, 0xAA, 0xBB, 0xCC}
	frame := BuildVP8VideoFrame(rawData, false)

	// Should be 5 + len(rawData) = 9 bytes
	if len(frame) != 9 {
		t.Fatalf("frame length: got %d, want 9", len(frame))
	}

	// Byte 0: 0xA3 = [IsExHeader=1][FrameType=010(inter)][PacketType=0011(CodedFramesX)]
	if frame[0] != 0xA3 {
		t.Errorf("byte 0: got 0x%02X, want 0xA3", frame[0])
	}

	// Bytes 1-4: FourCC "vp08"
	fourCC := string(frame[1:5])
	if fourCC != "vp08" {
		t.Errorf("FourCC: got %q, want %q", fourCC, "vp08")
	}
}

// TestBuildVP8VideoFrame_EmptyData tests frame building with empty payload.
func TestBuildVP8VideoFrame_EmptyData(t *testing.T) {
	frame := BuildVP8VideoFrame([]byte{}, true)

	// Should be exactly 5 bytes (header + FourCC only)
	if len(frame) != 5 {
		t.Fatalf("frame length: got %d, want 5", len(frame))
	}

	if frame[0] != 0x93 {
		t.Errorf("byte 0: got 0x%02X, want 0x93", frame[0])
	}
}

// TestIsVP8Keyframe tests VP8 keyframe detection.
func TestIsVP8Keyframe(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{
			// Keyframe: bit 0 = 0 (0x9D has bit 0 = 1... let's use 0x00)
			// VP8 keyframe starts with frame_type=0 in bit 0
			"keyframe_0x00",
			[]byte{0x00, 0x9D, 0x01, 0x2A},
			true,
		},
		{
			// Another keyframe: byte 0x9C has bit 0 = 0
			"keyframe_0x9C",
			[]byte{0x9C, 0x01, 0x2A},
			true,
		},
		{
			// Inter-frame: bit 0 = 1
			"inter_0x01",
			[]byte{0x01, 0x9D, 0x01, 0x2A},
			false,
		},
		{
			// Inter-frame: byte 0x9D has bit 0 = 1
			"inter_0x9D",
			[]byte{0x9D, 0x01, 0x2A},
			false,
		},
		{
			// Even byte → keyframe (bit 0 = 0)
			"keyframe_0xFE",
			[]byte{0xFE},
			true,
		},
		{
			// Odd byte → inter-frame (bit 0 = 1)
			"inter_0xFF",
			[]byte{0xFF},
			false,
		},
		{
			// Empty data → not a keyframe (safety check)
			"empty",
			[]byte{},
			false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsVP8Keyframe(tc.data)
			if got != tc.want {
				t.Errorf("IsVP8Keyframe(%v) = %v, want %v", tc.data, got, tc.want)
			}
		})
	}
}

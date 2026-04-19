package codec

import (
	"testing"
)

// TestBuildVP9SequenceHeader_NilConfig verifies SequenceStart with no config data.
func TestBuildVP9SequenceHeader_NilConfig(t *testing.T) {
	header := BuildVP9SequenceHeader(nil)

	// With nil config: 1 header + 4 FourCC = 5 bytes
	if len(header) != 5 {
		t.Fatalf("header length: got %d, want 5", len(header))
	}

	// Byte 0: 0x90 = [IsExHeader=1][FrameType=001(keyframe)][PacketType=0000(SequenceStart)]
	if header[0] != 0x90 {
		t.Errorf("byte 0: got 0x%02X, want 0x90", header[0])
	}

	// Bytes 1-4: FourCC "vp09"
	fourCC := string(header[1:5])
	if fourCC != "vp09" {
		t.Errorf("FourCC: got %q, want %q", fourCC, "vp09")
	}
}

// TestBuildVP9SequenceHeader_WithConfig verifies SequenceStart with VPCodecConfigurationRecord.
func TestBuildVP9SequenceHeader_WithConfig(t *testing.T) {
	// Simulated VPCodecConfigurationRecord (8 bytes for testing)
	config := []byte{0x01, 0x00, 0x10, 0x01, 0x01, 0x01, 0x01, 0x00}
	header := BuildVP9SequenceHeader(config)

	// With config: 1 header + 4 FourCC + 8 config = 13 bytes
	if len(header) != 13 {
		t.Fatalf("header length: got %d, want 13", len(header))
	}

	// Byte 0: 0x90
	if header[0] != 0x90 {
		t.Errorf("byte 0: got 0x%02X, want 0x90", header[0])
	}

	// Bytes 1-4: FourCC "vp09"
	fourCC := string(header[1:5])
	if fourCC != "vp09" {
		t.Errorf("FourCC: got %q, want %q", fourCC, "vp09")
	}

	// Bytes 5+: should match the config data
	for i, b := range config {
		if header[5+i] != b {
			t.Errorf("config byte %d: got 0x%02X, want 0x%02X", i, header[5+i], b)
		}
	}
}

// TestBuildVP9SequenceHeader_EmptyConfig verifies SequenceStart with empty (not nil) config.
func TestBuildVP9SequenceHeader_EmptyConfig(t *testing.T) {
	header := BuildVP9SequenceHeader([]byte{})

	// Empty config behaves like nil: 5 bytes total
	if len(header) != 5 {
		t.Fatalf("header length: got %d, want 5", len(header))
	}

	if header[0] != 0x90 {
		t.Errorf("byte 0: got 0x%02X, want 0x90", header[0])
	}
}

// TestBuildVP9VideoFrame_Keyframe verifies CodedFramesX tag for a VP9 keyframe.
func TestBuildVP9VideoFrame_Keyframe(t *testing.T) {
	rawData := []byte{0x82, 0x49, 0x83, 0x42, 0x00}
	frame := BuildVP9VideoFrame(rawData, true)

	// Should be 5 + len(rawData) = 10 bytes
	if len(frame) != 10 {
		t.Fatalf("frame length: got %d, want 10", len(frame))
	}

	// Byte 0: 0x93 = [IsExHeader=1][FrameType=001(keyframe)][PacketType=0011(CodedFramesX)]
	if frame[0] != 0x93 {
		t.Errorf("byte 0: got 0x%02X, want 0x93", frame[0])
	}

	// Bytes 1-4: FourCC "vp09"
	fourCC := string(frame[1:5])
	if fourCC != "vp09" {
		t.Errorf("FourCC: got %q, want %q", fourCC, "vp09")
	}

	// Bytes 5+: should match original data
	for i, b := range rawData {
		if frame[5+i] != b {
			t.Errorf("payload byte %d: got 0x%02X, want 0x%02X", i, frame[5+i], b)
		}
	}
}

// TestBuildVP9VideoFrame_Inter verifies CodedFramesX tag for a VP9 inter-frame.
func TestBuildVP9VideoFrame_Inter(t *testing.T) {
	rawData := []byte{0x86, 0xAA, 0xBB}
	frame := BuildVP9VideoFrame(rawData, false)

	// Should be 5 + len(rawData) = 8 bytes
	if len(frame) != 8 {
		t.Fatalf("frame length: got %d, want 8", len(frame))
	}

	// Byte 0: 0xA3 = [IsExHeader=1][FrameType=010(inter)][PacketType=0011(CodedFramesX)]
	if frame[0] != 0xA3 {
		t.Errorf("byte 0: got 0x%02X, want 0xA3", frame[0])
	}

	// Bytes 1-4: FourCC "vp09"
	fourCC := string(frame[1:5])
	if fourCC != "vp09" {
		t.Errorf("FourCC: got %q, want %q", fourCC, "vp09")
	}
}

// TestBuildVP9VideoFrame_EmptyData tests frame building with empty payload.
func TestBuildVP9VideoFrame_EmptyData(t *testing.T) {
	frame := BuildVP9VideoFrame([]byte{}, true)

	// Should be exactly 5 bytes (header + FourCC only)
	if len(frame) != 5 {
		t.Fatalf("frame length: got %d, want 5", len(frame))
	}

	if frame[0] != 0x93 {
		t.Errorf("byte 0: got 0x%02X, want 0x93", frame[0])
	}
}

// TestIsVP9Keyframe tests VP9 keyframe detection with various frame headers.
func TestIsVP9Keyframe(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{
			// VP9 keyframe, Profile 0:
			// frame_marker=10, profile_low=0, reserved=0, show_existing=0, frame_type=0
			// Byte: 0b10_0_0_0_0_xx = 0x80
			"keyframe_profile0",
			[]byte{0x80, 0x00, 0x00},
			true,
		},
		{
			// VP9 inter-frame, Profile 0:
			// frame_marker=10, profile_low=0, reserved=0, show_existing=0, frame_type=1
			// Byte: 0b10_0_0_0_1_xx = 0x84
			"inter_profile0",
			[]byte{0x84, 0x00, 0x00},
			false,
		},
		{
			// VP9 keyframe, Profile 1:
			// frame_marker=10, profile_low=1, reserved=0, show_existing=0, frame_type=0
			// Byte: 0b10_1_0_0_0_xx = 0xA0
			"keyframe_profile1",
			[]byte{0xA0, 0x00, 0x00},
			true,
		},
		{
			// VP9 show_existing_frame=1 (not a real coded frame):
			// frame_marker=10, profile_low=0, reserved=0, show_existing=1, frame_type=0
			// Byte: 0b10_0_0_1_0_xx = 0x88
			"show_existing_frame",
			[]byte{0x88, 0x00},
			false,
		},
		{
			// Invalid frame_marker (not 0b10):
			// frame_marker=00, rest doesn't matter
			// Byte: 0b00_0_0_0_0_xx = 0x00
			"invalid_marker_00",
			[]byte{0x00, 0x00},
			false,
		},
		{
			// Invalid frame_marker (0b11):
			// Byte: 0b11_0_0_0_0_xx = 0xC0
			"invalid_marker_11",
			[]byte{0xC0, 0x00},
			false,
		},
		{
			// Empty data → not a keyframe
			"empty",
			[]byte{},
			false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsVP9Keyframe(tc.data)
			if got != tc.want {
				t.Errorf("IsVP9Keyframe(0x%02X...) = %v, want %v", tc.data[0], got, tc.want)
			}
		})
	}
}

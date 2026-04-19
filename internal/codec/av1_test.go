package codec

import (
	"testing"
)

// TestBuildAV1SequenceHeader verifies the Enhanced RTMP SequenceStart tag for AV1.
func TestBuildAV1SequenceHeader(t *testing.T) {
	// Simulated AV1CodecConfigurationRecord (4 bytes for testing):
	// Byte 0: [marker=1:1][version=1:7] = 0x81
	// Byte 1: [seq_profile=0:3][seq_level_idx_0=1:5] = 0x01
	// Byte 2: [seq_tier_0=0:1][high_bitdepth=0:1][twelve_bit=0:1][monochrome=0:1]
	//         [chroma_sub_x=1:1][chroma_sub_y=1:1][chroma_sample_pos=0:2] = 0x0C
	// Byte 3: [reserved=0:3][initial_delay=0:1][reserved:4] = 0x00
	configRecord := []byte{0x81, 0x01, 0x0C, 0x00}
	header := BuildAV1SequenceHeader(configRecord)

	// Should be 5 + 4 = 9 bytes
	if len(header) != 9 {
		t.Fatalf("header length: got %d, want 9", len(header))
	}

	// Byte 0: 0x90 = [IsExHeader=1][FrameType=001(keyframe)][PacketType=0000(SequenceStart)]
	if header[0] != 0x90 {
		t.Errorf("byte 0: got 0x%02X, want 0x90", header[0])
	}

	// Bytes 1-4: FourCC "av01"
	fourCC := string(header[1:5])
	if fourCC != "av01" {
		t.Errorf("FourCC: got %q, want %q", fourCC, "av01")
	}

	// Bytes 5-8: should match config record
	for i, b := range configRecord {
		if header[5+i] != b {
			t.Errorf("config byte %d: got 0x%02X, want 0x%02X", i, header[5+i], b)
		}
	}
}

// TestBuildAV1SequenceHeader_EmptyConfig verifies SequenceStart with empty config.
func TestBuildAV1SequenceHeader_EmptyConfig(t *testing.T) {
	header := BuildAV1SequenceHeader([]byte{})

	// Empty config: 1 header + 4 FourCC = 5 bytes
	if len(header) != 5 {
		t.Fatalf("header length: got %d, want 5", len(header))
	}

	if header[0] != 0x90 {
		t.Errorf("byte 0: got 0x%02X, want 0x90", header[0])
	}

	fourCC := string(header[1:5])
	if fourCC != "av01" {
		t.Errorf("FourCC: got %q, want %q", fourCC, "av01")
	}
}

// TestBuildAV1VideoFrame_Keyframe verifies CodedFramesX tag for an AV1 keyframe.
func TestBuildAV1VideoFrame_Keyframe(t *testing.T) {
	// Simulated AV1 frame data (sequence header OBU + frame OBU)
	rawData := []byte{0x0A, 0x0B, 0x00, 0x00, 0x00, 0x32, 0x00}
	frame := BuildAV1VideoFrame(rawData, true)

	// Should be 5 + len(rawData) = 12 bytes
	if len(frame) != 12 {
		t.Fatalf("frame length: got %d, want 12", len(frame))
	}

	// Byte 0: 0x93 = [IsExHeader=1][FrameType=001(keyframe)][PacketType=0011(CodedFramesX)]
	if frame[0] != 0x93 {
		t.Errorf("byte 0: got 0x%02X, want 0x93", frame[0])
	}

	// Bytes 1-4: FourCC "av01"
	fourCC := string(frame[1:5])
	if fourCC != "av01" {
		t.Errorf("FourCC: got %q, want %q", fourCC, "av01")
	}

	// Bytes 5+: should match original data
	for i, b := range rawData {
		if frame[5+i] != b {
			t.Errorf("payload byte %d: got 0x%02X, want 0x%02X", i, frame[5+i], b)
		}
	}
}

// TestBuildAV1VideoFrame_Inter verifies CodedFramesX tag for an AV1 inter-frame.
func TestBuildAV1VideoFrame_Inter(t *testing.T) {
	rawData := []byte{0x32, 0xAA, 0xBB, 0xCC}
	frame := BuildAV1VideoFrame(rawData, false)

	// Should be 5 + len(rawData) = 9 bytes
	if len(frame) != 9 {
		t.Fatalf("frame length: got %d, want 9", len(frame))
	}

	// Byte 0: 0xA3 = [IsExHeader=1][FrameType=010(inter)][PacketType=0011(CodedFramesX)]
	if frame[0] != 0xA3 {
		t.Errorf("byte 0: got 0x%02X, want 0xA3", frame[0])
	}

	fourCC := string(frame[1:5])
	if fourCC != "av01" {
		t.Errorf("FourCC: got %q, want %q", fourCC, "av01")
	}
}

// TestBuildAV1VideoFrame_EmptyData tests frame building with empty payload.
func TestBuildAV1VideoFrame_EmptyData(t *testing.T) {
	frame := BuildAV1VideoFrame([]byte{}, true)

	if len(frame) != 5 {
		t.Fatalf("frame length: got %d, want 5", len(frame))
	}

	if frame[0] != 0x93 {
		t.Errorf("byte 0: got 0x%02X, want 0x93", frame[0])
	}
}

// TestIsAV1Keyframe tests AV1 keyframe detection with various OBU types.
func TestIsAV1Keyframe(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{
			// OBU_SEQUENCE_HEADER (type=1) → always precedes a keyframe
			// OBU header byte: [forbidden=0:1][type=0001:4][ext=0:1][has_size=1:1][reserved=0:1]
			// = 0b0_0001_0_1_0 = 0x0A
			"sequence_header_obu",
			[]byte{0x0A, 0x05, 0x00, 0x00, 0x00, 0x00},
			true,
		},
		{
			// OBU_FRAME (type=6) with frame_type=0 (KEY_FRAME)
			// OBU header byte: [forbidden=0:1][type=0110:4][ext=0:1][has_size=1:1][reserved=0:1]
			// = 0b0_0110_0_1_0 = 0x32
			// Payload byte: [show_existing=0:1][frame_type=00:2][...] = 0b0_00_xxxxx = 0x00
			"frame_obu_keyframe",
			[]byte{0x32, 0x05, 0x00, 0xAA, 0xBB, 0xCC},
			true,
		},
		{
			// OBU_FRAME (type=6) with frame_type=1 (INTER_FRAME)
			// Payload byte: [show_existing=0:1][frame_type=01:2][...] = 0b0_01_xxxxx = 0x20
			"frame_obu_inter",
			[]byte{0x32, 0x05, 0x20, 0xAA, 0xBB, 0xCC},
			false,
		},
		{
			// OBU_FRAME_HEADER (type=3) with frame_type=0 (KEY_FRAME)
			// OBU header byte: [forbidden=0:1][type=0011:4][ext=0:1][has_size=1:1][reserved=0:1]
			// = 0b0_0011_0_1_0 = 0x1A
			// Payload byte: [show_existing=0:1][frame_type=00:2] = 0x00
			"frame_header_obu_keyframe",
			[]byte{0x1A, 0x03, 0x00, 0xAA},
			true,
		},
		{
			// OBU_FRAME_HEADER (type=3) with frame_type=1 (INTER_FRAME)
			// Payload byte: [show_existing=0:1][frame_type=01:2] = 0x20
			"frame_header_obu_inter",
			[]byte{0x1A, 0x03, 0x20, 0xAA},
			false,
		},
		{
			// OBU_FRAME (type=6) with show_existing_frame=1 → not a keyframe
			// Payload byte: [show_existing=1:1][...] = 0x80
			"frame_obu_show_existing",
			[]byte{0x32, 0x02, 0x80, 0x00},
			false,
		},
		{
			// OBU_FRAME (type=6) with extension flag set, frame_type=0 (KEY_FRAME)
			// OBU header: [forbidden=0:1][type=0110:4][ext=1:1][has_size=1:1][reserved=0:1]
			// = 0b0_0110_1_1_0 = 0x36
			// Extension byte: 0x00 (temporal_id=0, spatial_id=0)
			// Size: LEB128 = 0x05
			// Payload: [show_existing=0:1][frame_type=00:2] = 0x00
			"frame_obu_with_extension_keyframe",
			[]byte{0x36, 0x00, 0x05, 0x00, 0xAA, 0xBB},
			true,
		},
		{
			// OBU_FRAME (type=6) with no has_size flag, frame_type=0 (KEY_FRAME)
			// OBU header: [forbidden=0:1][type=0110:4][ext=0:1][has_size=0:1][reserved=0:1]
			// = 0b0_0110_0_0_0 = 0x30
			// No size field → payload immediately follows
			// Payload: [show_existing=0:1][frame_type=00:2] = 0x00
			"frame_obu_no_size_keyframe",
			[]byte{0x30, 0x00, 0xAA, 0xBB},
			true,
		},
		{
			// Unknown OBU type (type=7 = OBU_TILE_GROUP) → not a keyframe
			// OBU header: [forbidden=0:1][type=0111:4][ext=0:1][has_size=1:1][reserved=0:1]
			// = 0b0_0111_0_1_0 = 0x3A
			"tile_group_obu",
			[]byte{0x3A, 0x03, 0x00},
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
			got := IsAV1Keyframe(tc.data)
			if got != tc.want {
				t.Errorf("IsAV1Keyframe(%v) = %v, want %v", tc.data, got, tc.want)
			}
		})
	}
}

package codec

import (
	"testing"
)

// TestH265NALUType verifies the H.265 NALU type extraction.
func TestH265NALUType(t *testing.T) {
	tests := []struct {
		name     string
		naluByte byte
		wantType uint8
	}{
		// H.265 encodes NALU type in bits [6:1]
		// byte = [forbidden(1)][type(6)][temporal_id_plus1(1)]

		// VPS: type=32, first byte = 0b0_100000_x = 0x40 or 0x41
		{"vps_even", 0x40, 32},
		{"vps_odd", 0x41, 32},

		// SPS: type=33, first byte = 0b0_100001_x = 0x42 or 0x43
		{"sps_even", 0x42, 33},
		{"sps_odd", 0x43, 33},

		// PPS: type=34, first byte = 0b0_100010_x = 0x44 or 0x45
		{"pps_even", 0x44, 34},
		{"pps_odd", 0x45, 34},

		// IDR: type=19, first byte = 0b0_010011_x = 0x26 or 0x27
		{"idr_even", 0x26, 19},
		{"idr_odd", 0x27, 19},

		// Non-IDR (inter-frame): type=1, first byte = 0b0_000001_x = 0x02 or 0x03
		{"inter_even", 0x02, 1},
		{"inter_odd", 0x03, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			nalu := []byte{tc.naluByte}
			got := H265NALUType(nalu)
			if got != tc.wantType {
				t.Errorf("H265NALUType(0x%02X) = %d, want %d", tc.naluByte, got, tc.wantType)
			}
		})
	}
}

// TestH265NALUType_EmptyNALU verifies that empty NALUs return type 0.
func TestH265NALUType_EmptyNALU(t *testing.T) {
	got := H265NALUType([]byte{})
	if got != 0 {
		t.Errorf("H265NALUType([]) = %d, want 0", got)
	}
}

// TestIsH265KeyframeNALU verifies IDR frame detection.
func TestIsH265KeyframeNALU(t *testing.T) {
	tests := []struct {
		name      string
		naluByte  byte
		wantIsIDR bool
	}{
		// IDR frames: types 16-21
		{"idr_16", 0x20, true},   // type=16, byte=0b0_010000_0
		{"idr_19", 0x26, true},   // type=19, byte=0b0_010011_0
		{"idr_21", 0x2A, true},   // type=21, byte=0b0_010101_0

		// Non-IDR frames: types 0-15
		{"non_idr_1", 0x02, false},   // type=1
		{"non_idr_15", 0x1E, false},  // type=15

		// Parameter sets (types 32-34)
		{"vps", 0x40, false},
		{"sps", 0x42, false},
		{"pps", 0x44, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			nalu := []byte{tc.naluByte}
			got := IsH265KeyframeNALU(nalu)
			if got != tc.wantIsIDR {
				t.Errorf("IsH265KeyframeNALU(0x%02X) = %v, want %v", tc.naluByte, got, tc.wantIsIDR)
			}
		})
	}
}

// TestIsH265ParameterSet verifies parameter set detection.
func TestIsH265ParameterSet(t *testing.T) {
	tests := []struct {
		name          string
		naluByte      byte
		wantParamSet  bool
	}{
		// Parameter sets
		{"vps", 0x40, true},
		{"sps", 0x42, true},
		{"pps", 0x44, true},

		// Non-parameter sets
		{"idr", 0x26, false},
		{"inter", 0x02, false},
		{"aud", 0x46, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			nalu := []byte{tc.naluByte}
			got := IsH265ParameterSet(nalu)
			if got != tc.wantParamSet {
				t.Errorf("IsH265ParameterSet(0x%02X) = %v, want %v", tc.naluByte, got, tc.wantParamSet)
			}
		})
	}
}

// TestSplitH265AnnexB verifies H.265 Annex B splitting.
func TestSplitH265AnnexB(t *testing.T) {
	tests := []struct {
		name      string
		data      []byte
		wantCount int
	}{
		// Single NALU with 4-byte start code
		{
			"single_4byte_start",
			[]byte{0x00, 0x00, 0x00, 0x01, 0x42, 0x00, 0x00},
			1,
		},
		// Single NALU with 3-byte start code
		{
			"single_3byte_start",
			[]byte{0x00, 0x00, 0x01, 0x42, 0x00},
			1,
		},
		// Two NALUs
		{
			"two_nalus",
			[]byte{
				0x00, 0x00, 0x00, 0x01, 0x42, 0x00,       // NALU 1 (SPS-like)
				0x00, 0x00, 0x01, 0x44, 0x01,             // NALU 2 (PPS-like)
			},
			2,
		},
		// Empty data
		{
			"empty",
			[]byte{},
			0,
		},
		// Data too short for start code
		{
			"too_short",
			[]byte{0x00, 0x00},
			0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SplitH265AnnexB(tc.data)
			if len(got) != tc.wantCount {
				t.Errorf("SplitH265AnnexB() returned %d NALUs, want %d", len(got), tc.wantCount)
			}
		})
	}
}

// TestExtractH265VPSSPSPPS verifies VPS/SPS/PPS extraction.
func TestExtractH265VPSSPSPPS(t *testing.T) {
	// Create mock NALUs with appropriate type bytes
	vpsNALU := []byte{0x40, 0x01, 0x02, 0x03}       // VPS type=32
	spsNALU := []byte{0x42, 0x11, 0x12, 0x13}       // SPS type=33
	ppsNALU := []byte{0x44, 0x21, 0x22, 0x23}       // PPS type=34
	interNALU := []byte{0x02, 0x31, 0x32, 0x33}     // Non-IDR type=1
	idrNALU := []byte{0x26, 0x41, 0x42, 0x43}       // IDR type=19

	tests := []struct {
		name       string
		nalus      [][]byte
		wantFound  bool
		checkVPS   bool
		checkSPS   bool
		checkPPS   bool
	}{
		{
			"all_present",
			[][]byte{vpsNALU, spsNALU, ppsNALU, idrNALU},
			true,
			true,
			true,
			true,
		},
		{
			"only_sps_pps",
			[][]byte{spsNALU, ppsNALU, idrNALU},
			false,  // missing VPS
			false,
			true,
			true,
		},
		{
			"mixed_order",
			[][]byte{interNALU, ppsNALU, vpsNALU, idrNALU, spsNALU},
			true,  // found all three despite mixed order
			true,
			true,
			true,
		},
		{
			"empty",
			[][]byte{},
			false,
			false,
			false,
			false,
		},
		{
			"only_video_frames",
			[][]byte{idrNALU, interNALU},
			false,
			false,
			false,
			false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vps, sps, pps, found := ExtractH265VPSSPSPPS(tc.nalus)

			if found != tc.wantFound {
				t.Errorf("ExtractH265VPSSPSPPS() found=%v, want %v", found, tc.wantFound)
			}

			if tc.checkVPS && vps == nil {
				t.Errorf("ExtractH265VPSSPSPPS() VPS is nil, want non-nil")
			}
			if !tc.checkVPS && vps != nil {
				t.Errorf("ExtractH265VPSSPSPPS() VPS is non-nil, want nil")
			}

			if tc.checkSPS && sps == nil {
				t.Errorf("ExtractH265VPSSPSPPS() SPS is nil, want non-nil")
			}
			if !tc.checkSPS && sps != nil {
				t.Errorf("ExtractH265VPSSPSPPS() SPS is non-nil, want nil")
			}

			if tc.checkPPS && pps == nil {
				t.Errorf("ExtractH265VPSSPSPPS() PPS is nil, want non-nil")
			}
			if !tc.checkPPS && pps != nil {
				t.Errorf("ExtractH265VPSSPSPPS() PPS is non-nil, want nil")
			}
		})
	}
}

// TestBuildHEVCSequenceHeader verifies sequence header structure.
func TestBuildHEVCSequenceHeader(t *testing.T) {
	// Mock VPS (4 bytes): NAL header (2 bytes) + minimal data
	vps := []byte{0x40, 0x01, 0xAA, 0xBB}

	// Mock SPS (15+ bytes): NAL header (2 bytes) + vps_id/layers byte + profile_tier_level (12 bytes)
	// SPS[0-1] = NAL header (type=33)
	// SPS[2]   = sps_video_parameter_set_id (4b) | sps_max_sub_layers_minus1 (3b) | temporal_id_nesting (1b)
	// SPS[3]   = general_profile_space (2b) | general_tier_flag (1b) | general_profile_idc (5b)
	// SPS[4:8] = general_profile_compatibility_flags (32 bits)
	// SPS[8:14] = general_constraint_indicator_flags (48 bits)
	// SPS[14]  = general_level_idc
	sps := []byte{
		0x42, 0x01, // NAL header: type=33 (SPS)
		0x01,       // vps_id=0, max_sub_layers=0, temporal_nesting=1
		0x01,       // profile_space=0, tier=0, profile_idc=1 (Main)
		0x60, 0x00, 0x00, 0x00, // profile_compatibility_flags
		0xB0, 0x00, 0x00, 0x00, 0x00, 0x00, // constraint_indicator_flags
		0x5D, // level_idc = 93 (Level 3.1)
		0x00, 0x00, // extra bytes for safety
	}
	pps := []byte{0x44, 0x01, 0xC0}

	got := BuildHEVCSequenceHeader(vps, sps, pps)

	// Verify Enhanced RTMP header (5 bytes: ExHeader + FourCC)
	if len(got) < 5 {
		t.Fatalf("BuildHEVCSequenceHeader() returned %d bytes, want at least 5", len(got))
	}

	// Byte 0: Enhanced RTMP header = 0x90
	// [IsExHeader=1][FrameType=001 (key)][PacketType=0000 (SequenceStart)]
	if got[0] != 0x90 {
		t.Errorf("byte[0] = 0x%02X, want 0x90 (Enhanced RTMP SequenceStart)", got[0])
	}

	// Bytes 1-4: FourCC = "hvc1"
	if string(got[1:5]) != "hvc1" {
		t.Errorf("FourCC = %q, want \"hvc1\"", string(got[1:5]))
	}

	// Byte 5: ConfigurationVersion = 1
	if got[5] != 0x01 {
		t.Errorf("configurationVersion = 0x%02X, want 0x01", got[5])
	}

	// Byte 6: Profile from SPS[3] = 0x01 (Main profile)
	if got[6] != 0x01 {
		t.Errorf("general_profile_idc = 0x%02X, want 0x01", got[6])
	}

	// Bytes 7-10: Profile compatibility from SPS[4:8]
	if got[7] != 0x60 || got[8] != 0x00 || got[9] != 0x00 || got[10] != 0x00 {
		t.Errorf("profile_compat = [0x%02X,0x%02X,0x%02X,0x%02X], want [0x60,0x00,0x00,0x00]",
			got[7], got[8], got[9], got[10])
	}

	// Byte 17: general_level_idc from SPS[14] = 0x5D (Level 3.1)
	if got[17] != 0x5D {
		t.Errorf("general_level_idc = 0x%02X, want 0x5D", got[17])
	}

	// Check reserved bits fields (after the 12-byte profile/tier/level section)
	// Offset 18-19: min_spatial_segmentation = 0xF000 (4 reserved 1-bits + 12-bit value=0)
	if got[18] != 0xF0 || got[19] != 0x00 {
		t.Errorf("min_spatial_seg = [0x%02X,0x%02X], want [0xF0,0x00]", got[18], got[19])
	}

	// Offset 20: parallelismType = 0xFC (6 reserved 1-bits + 2-bit value=0)
	if got[20] != 0xFC {
		t.Errorf("parallelismType = 0x%02X, want 0xFC", got[20])
	}

	// Offset 21: chromaFormatIdc = 0xFD (6 reserved 1-bits + 2-bit value=1 for 4:2:0)
	if got[21] != 0xFD {
		t.Errorf("chromaFormatIdc = 0x%02X, want 0xFD", got[21])
	}

	// Offset 22: bitDepthLumaMinus8 = 0xF8 (5 reserved 1-bits + 3-bit value=0)
	if got[22] != 0xF8 {
		t.Errorf("bitDepthLuma = 0x%02X, want 0xF8", got[22])
	}

	// Offset 23: bitDepthChromaMinus8 = 0xF8
	if got[23] != 0xF8 {
		t.Errorf("bitDepthChroma = 0x%02X, want 0xF8", got[23])
	}

	// Offset 27: numOfArrays = 3 (VPS, SPS, PPS)
	// 5 bytes Enhanced RTMP header + 22 bytes fixed hvcC fields = offset 27
	numArraysOffset := 5 + 22
	if numArraysOffset >= len(got) {
		t.Fatalf("output too short (%d bytes) to contain numOfArrays at offset %d", len(got), numArraysOffset)
	}
	if got[numArraysOffset] != 3 {
		t.Errorf("numOfArrays = %d, want 3", got[numArraysOffset])
	}

	// Verify VPS array header (offset 28): type byte = 0xA0 (completeness=1, type=32)
	if got[numArraysOffset+1] != 0xA0 {
		t.Errorf("VPS array type = 0x%02X, want 0xA0", got[numArraysOffset+1])
	}
}

// TestBuildHEVCSequenceHeader_ShortSPS verifies fallback for SPS < 15 bytes.
func TestBuildHEVCSequenceHeader_ShortSPS(t *testing.T) {
	vps := []byte{0x40, 0x01, 0x02, 0x03}
	sps := []byte{0x42, 0x01, 0x01, 0x01} // Only 4 bytes, too short for profile extraction
	pps := []byte{0x44, 0x01}

	got := BuildHEVCSequenceHeader(vps, sps, pps)

	// Should still produce valid output with fallback defaults
	if len(got) < 5 {
		t.Fatalf("returned %d bytes, want at least 5", len(got))
	}
	if got[0] != 0x90 {
		t.Errorf("byte[0] = 0x%02X, want 0x90", got[0])
	}
	if string(got[1:5]) != "hvc1" {
		t.Errorf("FourCC = %q, want \"hvc1\"", string(got[1:5]))
	}
	// Fallback profile byte: 0x01 (Main profile)
	if got[6] != 0x01 {
		t.Errorf("fallback profile = 0x%02X, want 0x01", got[6])
	}
}

// TestBuildHEVCVideoFrame verifies video frame tag structure.
func TestBuildHEVCVideoFrame(t *testing.T) {
	tests := []struct {
		name        string
		nalus       [][]byte
		isKeyframe  bool
		cts         int32
		wantByte0   byte
	}{
		{
			"keyframe",
			[][]byte{{0x26, 0xAB, 0xCD}},
			true,
			0,
			0x91, // Enhanced RTMP: IsExHeader=1, Keyframe=1, CodedFrames=1
		},
		{
			"inter_frame",
			[][]byte{{0x02, 0xEF, 0x01}},
			false,
			100,
			0xA1, // Enhanced RTMP: IsExHeader=1, Inter=2, CodedFrames=1
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildHEVCVideoFrame(tc.nalus, tc.isKeyframe, tc.cts)

			// Enhanced RTMP: 1 byte header + 4 bytes FourCC + 3 bytes CTS + AVCC data = 8+ bytes
			if len(got) < 8 {
				t.Fatalf("BuildHEVCVideoFrame() returned %d bytes, want at least 8", len(got))
			}

			// Check Enhanced RTMP header byte
			if got[0] != tc.wantByte0 {
				t.Errorf("byte[0] = 0x%02X, want 0x%02X", got[0], tc.wantByte0)
			}

			// Check FourCC = "hvc1"
			if string(got[1:5]) != "hvc1" {
				t.Errorf("FourCC = %q, want \"hvc1\"", string(got[1:5]))
			}

			// Check composition time offset (bytes 5-7)
			gotCTS := (int32(got[5]) << 16) | (int32(got[6]) << 8) | int32(got[7])
			if gotCTS != tc.cts {
				t.Errorf("CTS = %d, want %d", gotCTS, tc.cts)
			}
		})
	}
}

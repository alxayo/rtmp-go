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
	// Mock VPS, SPS, PPS with minimal content
	vps := []byte{0x40, 0x00, 0x00, 0x00}
	sps := []byte{0x42, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x00}
	pps := []byte{0x44, 0x01, 0x01}

	got := BuildHEVCSequenceHeader(vps, sps, pps)

	// Verify the tag header
	if len(got) < 5 {
		t.Fatalf("BuildHEVCSequenceHeader() returned %d bytes, want at least 5", len(got))
	}

	// Check frame type and codec ID (byte 0)
	// Should be 0x1C: keyframe (1) << 4 | HEVC (12)
	if got[0] != 0x1C {
		t.Errorf("BuildHEVCSequenceHeader() byte[0] = 0x%02X, want 0x1C", got[0])
	}

	// Check packet type (byte 1)
	// Should be 0x00 for sequence header
	if got[1] != 0x00 {
		t.Errorf("BuildHEVCSequenceHeader() byte[1] = 0x%02X, want 0x00", got[1])
	}

	// Check composition time offset (bytes 2-4)
	// Should be 0x000000 for sequence header
	if got[2] != 0x00 || got[3] != 0x00 || got[4] != 0x00 {
		t.Errorf("BuildHEVCSequenceHeader() bytes[2:5] = [0x%02X, 0x%02X, 0x%02X], want [0x00, 0x00, 0x00]",
			got[2], got[3], got[4])
	}

	// Check configuration version (byte 5)
	// Should be 0x01
	if got[5] != 0x01 {
		t.Errorf("BuildHEVCSequenceHeader() byte[5] = 0x%02X, want 0x01", got[5])
	}

	// Check number of arrays (should be 3: VPS, SPS, PPS)
	// This is at offset 5 + 23 bytes
	arrayCountOffset := 5 + 23
	if arrayCountOffset < len(got) {
		if got[arrayCountOffset] != 3 {
			t.Errorf("BuildHEVCSequenceHeader() num_arrays = %d, want 3", got[arrayCountOffset])
		}
	} else {
		t.Errorf("BuildHEVCSequenceHeader() returned %d bytes, not enough for array count", len(got))
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
			0x1C,  // keyframe (1) << 4 | HEVC (12)
		},
		{
			"inter_frame",
			[][]byte{{0x02, 0xEF, 0x01}},
			false,
			100,
			0x2C,  // inter (2) << 4 | HEVC (12)
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildHEVCVideoFrame(tc.nalus, tc.isKeyframe, tc.cts)

			if len(got) < 5 {
				t.Fatalf("BuildHEVCVideoFrame() returned %d bytes, want at least 5", len(got))
			}

			// Check frame type and codec ID
			if got[0] != tc.wantByte0 {
				t.Errorf("BuildHEVCVideoFrame() byte[0] = 0x%02X, want 0x%02X", got[0], tc.wantByte0)
			}

			// Check packet type (should be 0x01 for NALU data)
			if got[1] != 0x01 {
				t.Errorf("BuildHEVCVideoFrame() byte[1] = 0x%02X, want 0x01", got[1])
			}

			// Check composition time offset
			gotCTS := (int32(got[2]) << 16) | (int32(got[3]) << 8) | int32(got[4])
			if gotCTS != tc.cts {
				t.Errorf("BuildHEVCVideoFrame() CTS = %d, want %d", gotCTS, tc.cts)
			}
		})
	}
}

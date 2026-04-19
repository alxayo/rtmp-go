package codec

import (
	"encoding/binary"
	"testing"
)

// TestVVCNALUType verifies the VVC NALU type extraction.
func TestVVCNALUType(t *testing.T) {
	tests := []struct {
		name     string
		naluByte byte
		wantType uint8
	}{
		// VVC encodes NALU type in bits [6:1]
		// byte = [forbidden(1)][type(6)][nuh_temporal_id_plus1 high bit(1)]

		// VPS: type=32, first byte = 0b0_100000_0 = 0x40
		{"vps", 0x40, 32},

		// SPS: type=33, first byte = 0b0_100001_0 = 0x42
		{"sps", 0x42, 33},

		// PPS: type=34, first byte = 0b0_100010_0 = 0x44
		{"pps", 0x44, 34},

		// APS: type=35, first byte = 0b0_100011_0 = 0x46
		{"aps", 0x46, 35},

		// IDR_W_RADL: type=19, first byte = 0b0_010011_0 = 0x26
		{"idr_w_radl", 0x26, 19},

		// IDR_N_LP: type=20, first byte = 0b0_010100_0 = 0x28
		{"idr_n_lp", 0x28, 20},

		// CRA: type=21, first byte = 0b0_010101_0 = 0x2A
		{"cra", 0x2A, 21},

		// GDR: type=22, first byte = 0b0_010110_0 = 0x2C
		{"gdr", 0x2C, 22},

		// AUD: type=38, first byte = 0b0_100110_0 = 0x4C
		{"aud", 0x4C, 38},

		// Non-IDR slice: type=1, first byte = 0b0_000001_0 = 0x02
		{"non_idr", 0x02, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			nalu := []byte{tc.naluByte}
			got := VVCNALUType(nalu)
			if got != tc.wantType {
				t.Errorf("VVCNALUType(0x%02X) = %d, want %d", tc.naluByte, got, tc.wantType)
			}
		})
	}
}

// TestVVCNALUType_EmptyNALU verifies that empty and nil NALUs return type 0.
func TestVVCNALUType_EmptyNALU(t *testing.T) {
	tests := []struct {
		name string
		nalu []byte
	}{
		{"empty", []byte{}},
		{"nil", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := VVCNALUType(tc.nalu)
			if got != 0 {
				t.Errorf("VVCNALUType(%v) = %d, want 0", tc.nalu, got)
			}
		})
	}
}

// TestIsVVCKeyframeNALU verifies IRAP/GDR keyframe detection.
func TestIsVVCKeyframeNALU(t *testing.T) {
	tests := []struct {
		name      string
		naluByte  byte
		wantIsKey bool
	}{
		// Keyframes: types 19-22
		{"idr_w_radl_19", 0x26, true},  // type=19
		{"idr_n_lp_20", 0x28, true},    // type=20
		{"cra_21", 0x2A, true},         // type=21
		{"gdr_22", 0x2C, true},         // type=22

		// Non-keyframes
		{"non_idr_1", 0x02, false},     // type=1
		{"vps_32", 0x40, false},        // type=32
		{"sps_33", 0x42, false},        // type=33
		{"aud_38", 0x4C, false},        // type=38
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			nalu := []byte{tc.naluByte}
			got := IsVVCKeyframeNALU(nalu)
			if got != tc.wantIsKey {
				t.Errorf("IsVVCKeyframeNALU(0x%02X) = %v, want %v", tc.naluByte, got, tc.wantIsKey)
			}
		})
	}
}

// TestIsVVCKeyframeNALU_Empty verifies empty input returns false.
func TestIsVVCKeyframeNALU_Empty(t *testing.T) {
	if IsVVCKeyframeNALU([]byte{}) {
		t.Error("IsVVCKeyframeNALU([]) = true, want false")
	}
}

// TestIsVVCParameterSet verifies parameter set detection.
func TestIsVVCParameterSet(t *testing.T) {
	tests := []struct {
		name         string
		naluByte     byte
		wantParamSet bool
	}{
		// Parameter sets: VPS(32), SPS(33), PPS(34), APS(35)
		{"vps", 0x40, true},
		{"sps", 0x42, true},
		{"pps", 0x44, true},
		{"aps", 0x46, true},

		// Non-parameter sets
		{"aud", 0x4C, false},
		{"idr", 0x26, false},
		{"non_idr", 0x02, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			nalu := []byte{tc.naluByte}
			got := IsVVCParameterSet(nalu)
			if got != tc.wantParamSet {
				t.Errorf("IsVVCParameterSet(0x%02X) = %v, want %v", tc.naluByte, got, tc.wantParamSet)
			}
		})
	}
}

// TestIsVVCParameterSet_Empty verifies empty input returns false.
func TestIsVVCParameterSet_Empty(t *testing.T) {
	if IsVVCParameterSet([]byte{}) {
		t.Error("IsVVCParameterSet([]) = true, want false")
	}
}

// TestSplitVVCAnnexB verifies VVC Annex B splitting.
func TestSplitVVCAnnexB(t *testing.T) {
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
		// Multiple NALUs with mixed 3/4-byte start codes
		{
			"mixed_start_codes",
			[]byte{
				0x00, 0x00, 0x00, 0x01, 0x40, 0x01, // VPS with 4-byte start code
				0x00, 0x00, 0x01, 0x42, 0x02,        // SPS with 3-byte start code
				0x00, 0x00, 0x00, 0x01, 0x44, 0x03,  // PPS with 4-byte start code
			},
			3,
		},
		// Empty data
		{
			"empty",
			[]byte{},
			0,
		},
		// Too short for start code
		{
			"too_short",
			[]byte{0x00, 0x00},
			0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SplitVVCAnnexB(tc.data)
			if len(got) != tc.wantCount {
				t.Errorf("SplitVVCAnnexB() returned %d NALUs, want %d", len(got), tc.wantCount)
			}
		})
	}
}

// TestExtractVVCVPSSPSPPS verifies VPS/SPS/PPS extraction.
func TestExtractVVCVPSSPSPPS(t *testing.T) {
	vpsNALU := []byte{0x40, 0x01, 0x02, 0x03}   // VPS type=32
	spsNALU := []byte{0x42, 0x11, 0x12, 0x13}   // SPS type=33
	ppsNALU := []byte{0x44, 0x21, 0x22, 0x23}   // PPS type=34
	interNALU := []byte{0x02, 0x31, 0x32, 0x33} // Non-IDR type=1
	idrNALU := []byte{0x26, 0x41, 0x42, 0x43}   // IDR type=19

	tests := []struct {
		name      string
		nalus     [][]byte
		wantFound bool
		checkVPS  bool
		checkSPS  bool
		checkPPS  bool
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
			"missing_vps",
			[][]byte{spsNALU, ppsNALU, idrNALU},
			false,
			false,
			true,
			true,
		},
		{
			"mixed_order",
			[][]byte{interNALU, ppsNALU, vpsNALU, idrNALU, spsNALU},
			true,
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
			vps, sps, pps, found := ExtractVVCVPSSPSPPS(tc.nalus)

			if found != tc.wantFound {
				t.Errorf("ExtractVVCVPSSPSPPS() found=%v, want %v", found, tc.wantFound)
			}

			if tc.checkVPS && vps == nil {
				t.Errorf("ExtractVVCVPSSPSPPS() VPS is nil, want non-nil")
			}
			if !tc.checkVPS && vps != nil {
				t.Errorf("ExtractVVCVPSSPSPPS() VPS is non-nil, want nil")
			}

			if tc.checkSPS && sps == nil {
				t.Errorf("ExtractVVCVPSSPSPPS() SPS is nil, want non-nil")
			}
			if !tc.checkSPS && sps != nil {
				t.Errorf("ExtractVVCVPSSPSPPS() SPS is non-nil, want nil")
			}

			if tc.checkPPS && pps == nil {
				t.Errorf("ExtractVVCVPSSPSPPS() PPS is nil, want non-nil")
			}
			if !tc.checkPPS && pps != nil {
				t.Errorf("ExtractVVCVPSSPSPPS() PPS is non-nil, want nil")
			}
		})
	}
}

// TestExtractVVCVPSSPSPPS_DuplicateVPS verifies first VPS wins.
func TestExtractVVCVPSSPSPPS_DuplicateVPS(t *testing.T) {
	vps1 := []byte{0x40, 0x01, 0xAA}
	vps2 := []byte{0x40, 0x01, 0xBB}
	sps := []byte{0x42, 0x11, 0x12}
	pps := []byte{0x44, 0x21, 0x22}

	vps, _, _, found := ExtractVVCVPSSPSPPS([][]byte{vps1, vps2, sps, pps})
	if !found {
		t.Fatal("ExtractVVCVPSSPSPPS() found=false, want true")
	}
	// First VPS should win
	if len(vps) != len(vps1) || vps[2] != 0xAA {
		t.Errorf("ExtractVVCVPSSPSPPS() returned second VPS, want first")
	}
}

// TestBuildVVCSequenceHeader verifies sequence header structure.
func TestBuildVVCSequenceHeader(t *testing.T) {
	vps := []byte{0x40, 0x01, 0xAA, 0xBB}
	sps := []byte{0x42, 0x01, 0xCC, 0xDD}
	pps := []byte{0x44, 0x01, 0xEE}

	got := BuildVVCSequenceHeader(vps, sps, pps)

	// Minimum: 5 bytes Enhanced RTMP header + 3 bytes config header + 3 arrays
	if len(got) < 8 {
		t.Fatalf("BuildVVCSequenceHeader() returned %d bytes, want at least 8", len(got))
	}

	// Byte 0: Enhanced RTMP header = 0x90
	// [IsExHeader=1][FrameType=001 (key)][PacketType=0000 (SequenceStart)]
	if got[0] != 0x90 {
		t.Errorf("byte[0] = 0x%02X, want 0x90 (Enhanced RTMP SequenceStart)", got[0])
	}

	// Bytes 1-4: FourCC = "vvc1"
	if string(got[1:5]) != "vvc1" {
		t.Errorf("FourCC = %q, want \"vvc1\"", string(got[1:5]))
	}

	// Byte 5: configurationVersion = 1
	if got[5] != 0x01 {
		t.Errorf("configurationVersion = 0x%02X, want 0x01", got[5])
	}

	// Byte 6: flags = 0xDF (lengthSizeMinusOne=3, ptl_present_flag=0, reserved=0x1F)
	if got[6] != 0xDF {
		t.Errorf("config flags = 0x%02X, want 0xDF", got[6])
	}

	// Byte 7: numOfArrays = 3
	if got[7] != 3 {
		t.Errorf("numOfArrays = %d, want 3", got[7])
	}

	// Verify VPS array header: type byte = 0xA0 (completeness=1, type=32)
	if got[8] != 0xA0 {
		t.Errorf("VPS array type = 0x%02X, want 0xA0", got[8])
	}

	// VPS numNalus = 1
	vpsNumNalus := binary.BigEndian.Uint16(got[9:11])
	if vpsNumNalus != 1 {
		t.Errorf("VPS numNalus = %d, want 1", vpsNumNalus)
	}

	// VPS length
	vpsLen := binary.BigEndian.Uint16(got[11:13])
	if vpsLen != uint16(len(vps)) {
		t.Errorf("VPS length = %d, want %d", vpsLen, len(vps))
	}

	// Verify total output size
	wantLen := 5 + 3 + // Enhanced RTMP header + config header
		(1 + 2 + 2 + len(vps)) + // VPS array
		(1 + 2 + 2 + len(sps)) + // SPS array
		(1 + 2 + 2 + len(pps)) // PPS array
	if len(got) != wantLen {
		t.Errorf("total length = %d, want %d", len(got), wantLen)
	}
}

// TestBuildVVCVideoFrame verifies video frame tag structure.
func TestBuildVVCVideoFrame(t *testing.T) {
	tests := []struct {
		name       string
		nalus      [][]byte
		isKeyframe bool
		cts        int32
		wantByte0  byte
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
			got := BuildVVCVideoFrame(tc.nalus, tc.isKeyframe, tc.cts)

			// Enhanced RTMP: 1 byte header + 4 bytes FourCC + 3 bytes CTS + AVCC data = 8+ bytes
			if len(got) < 8 {
				t.Fatalf("BuildVVCVideoFrame() returned %d bytes, want at least 8", len(got))
			}

			// Check Enhanced RTMP header byte
			if got[0] != tc.wantByte0 {
				t.Errorf("byte[0] = 0x%02X, want 0x%02X", got[0], tc.wantByte0)
			}

			// Check FourCC = "vvc1"
			if string(got[1:5]) != "vvc1" {
				t.Errorf("FourCC = %q, want \"vvc1\"", string(got[1:5]))
			}

			// Check composition time offset (bytes 5-7)
			gotCTS := (int32(got[5]) << 16) | (int32(got[6]) << 8) | int32(got[7])
			if gotCTS != tc.cts {
				t.Errorf("CTS = %d, want %d", gotCTS, tc.cts)
			}
		})
	}
}

// TestIsVVCKeyframe verifies keyframe detection in AVCC-formatted data.
func TestIsVVCKeyframe(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantKey bool
	}{
		{
			"idr_nalu",
			buildAVCCData([]byte{0x26, 0xAB, 0xCD}), // IDR_W_RADL type=19
			true,
		},
		{
			"cra_nalu",
			buildAVCCData([]byte{0x2A, 0x01, 0x02}), // CRA type=21
			true,
		},
		{
			"gdr_nalu",
			buildAVCCData([]byte{0x2C, 0x01, 0x02}), // GDR type=22
			true,
		},
		{
			"non_idr_nalu",
			buildAVCCData([]byte{0x02, 0xEF, 0x01}), // Non-IDR type=1
			false,
		},
		{
			"empty",
			[]byte{},
			false,
		},
		{
			"corrupt_short",
			[]byte{0x00, 0x00},
			false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsVVCKeyframe(tc.data)
			if got != tc.wantKey {
				t.Errorf("IsVVCKeyframe() = %v, want %v", got, tc.wantKey)
			}
		})
	}
}

// TestParseVVCDecoderConfig verifies config record parsing.
func TestParseVVCDecoderConfig(t *testing.T) {
	vpsData := []byte{0x40, 0x01, 0xAA}
	spsData := []byte{0x42, 0x01, 0xBB}
	ppsData := []byte{0x44, 0x01, 0xCC}

	t.Run("valid_minimal_config", func(t *testing.T) {
		config := buildVVCConfigRecord(vpsData, spsData, ppsData)

		parsed, err := ParseVVCDecoderConfig(config)
		if err != nil {
			t.Fatalf("ParseVVCDecoderConfig() error = %v", err)
		}

		if parsed.NALULengthSize != 4 {
			t.Errorf("NALULengthSize = %d, want 4", parsed.NALULengthSize)
		}

		if len(parsed.VPS) != len(vpsData) {
			t.Errorf("VPS length = %d, want %d", len(parsed.VPS), len(vpsData))
		}
		if len(parsed.SPS) != len(spsData) {
			t.Errorf("SPS length = %d, want %d", len(parsed.SPS), len(spsData))
		}
		if len(parsed.PPS) != len(ppsData) {
			t.Errorf("PPS length = %d, want %d", len(parsed.PPS), len(ppsData))
		}
	})

	t.Run("wrong_version", func(t *testing.T) {
		config := buildVVCConfigRecord(vpsData, spsData, ppsData)
		config[0] = 2 // invalid version

		_, err := ParseVVCDecoderConfig(config)
		if err == nil {
			t.Error("ParseVVCDecoderConfig() expected error for version != 1")
		}
	})

	t.Run("truncated", func(t *testing.T) {
		_, err := ParseVVCDecoderConfig([]byte{0x01})
		if err == nil {
			t.Error("ParseVVCDecoderConfig() expected error for truncated data")
		}
	})

	t.Run("missing_vps", func(t *testing.T) {
		// Config record with only SPS and PPS arrays (no VPS)
		config := buildVVCConfigRecordArrays(
			vvcArray{naluType: VVCNALUTypeSPS, data: spsData},
			vvcArray{naluType: VVCNALUTypePPS, data: ppsData},
		)

		_, err := ParseVVCDecoderConfig(config)
		if err == nil {
			t.Error("ParseVVCDecoderConfig() expected error for missing VPS")
		}
	})
}

// --- helpers ---

// buildAVCCData creates AVCC-formatted data from a single NALU (4-byte length prefix).
func buildAVCCData(nalu []byte) []byte {
	buf := make([]byte, 4+len(nalu))
	binary.BigEndian.PutUint32(buf, uint32(len(nalu)))
	copy(buf[4:], nalu)
	return buf
}

// vvcArray describes one NALU array for buildVVCConfigRecordArrays.
type vvcArray struct {
	naluType uint8
	data     []byte
}

// buildVVCConfigRecord builds a minimal VVCDecoderConfigurationRecord
// (ptl_present_flag=0) containing VPS, SPS, and PPS.
func buildVVCConfigRecord(vps, sps, pps []byte) []byte {
	return buildVVCConfigRecordArrays(
		vvcArray{naluType: VVCNALUTypeVPS, data: vps},
		vvcArray{naluType: VVCNALUTypeSPS, data: sps},
		vvcArray{naluType: VVCNALUTypePPS, data: pps},
	)
}

// buildVVCConfigRecordArrays builds a VVCDecoderConfigurationRecord with
// the specified NALU arrays.
func buildVVCConfigRecordArrays(arrays ...vvcArray) []byte {
	// Calculate total size
	size := 3 // configVersion + flags + numOfArrays
	for _, a := range arrays {
		size += 1 + 2 + 2 + len(a.data) // type + numNalus + length + data
	}

	buf := make([]byte, size)
	buf[0] = 1    // configurationVersion
	buf[1] = 0xDF // lengthSizeMinusOne=3, ptl_present_flag=0, reserved=0x1F
	buf[2] = byte(len(arrays))

	off := 3
	for _, a := range arrays {
		buf[off] = 0x80 | (a.naluType & 0x3F) // array_completeness=1
		off++
		binary.BigEndian.PutUint16(buf[off:], 1) // numNalus = 1
		off += 2
		binary.BigEndian.PutUint16(buf[off:], uint16(len(a.data)))
		off += 2
		copy(buf[off:], a.data)
		off += len(a.data)
	}

	return buf
}

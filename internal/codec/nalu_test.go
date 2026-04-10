package codec

import (
	"bytes"
	"testing"
)

// TestSplitAnnexB_FourByteStartCodes tests splitting with 4-byte start codes.
func TestSplitAnnexB_FourByteStartCodes(t *testing.T) {
	// Two NALUs separated by 4-byte start codes: 0x00000001
	data := []byte{
		0x00, 0x00, 0x00, 0x01, // Start code
		0x67, 0x42, 0x00, 0x1E, // NALU 1 (SPS: type 7)
		0x00, 0x00, 0x00, 0x01, // Start code
		0x68, 0xCE, 0x38, 0x80, // NALU 2 (PPS: type 8)
	}

	nalus := SplitAnnexB(data)
	if len(nalus) != 2 {
		t.Fatalf("expected 2 NALUs, got %d", len(nalus))
	}

	if NALUType(nalus[0]) != NALUTypeSPS {
		t.Errorf("NALU 0: expected SPS (7), got %d", NALUType(nalus[0]))
	}
	if NALUType(nalus[1]) != NALUTypePPS {
		t.Errorf("NALU 1: expected PPS (8), got %d", NALUType(nalus[1]))
	}
}

// TestSplitAnnexB_ThreeByteStartCodes tests splitting with 3-byte start codes.
func TestSplitAnnexB_ThreeByteStartCodes(t *testing.T) {
	data := []byte{
		0x00, 0x00, 0x01, // 3-byte start code
		0x65, 0xAA, 0xBB, // NALU (IDR: type 5)
		0x00, 0x00, 0x01, // 3-byte start code
		0x06, 0xCC, // NALU (SEI: type 6)
	}

	nalus := SplitAnnexB(data)
	if len(nalus) != 2 {
		t.Fatalf("expected 2 NALUs, got %d", len(nalus))
	}

	if NALUType(nalus[0]) != NALUTypeIDR {
		t.Errorf("NALU 0: expected IDR (5), got %d", NALUType(nalus[0]))
	}
	if NALUType(nalus[1]) != NALUTypeSEI {
		t.Errorf("NALU 1: expected SEI (6), got %d", NALUType(nalus[1]))
	}
}

// TestSplitAnnexB_MixedStartCodes tests a mix of 3 and 4 byte start codes.
func TestSplitAnnexB_MixedStartCodes(t *testing.T) {
	data := []byte{
		0x00, 0x00, 0x00, 0x01, // 4-byte
		0x67, 0x42, // SPS
		0x00, 0x00, 0x01, // 3-byte
		0x68, 0xCE, // PPS
	}

	nalus := SplitAnnexB(data)
	if len(nalus) != 2 {
		t.Fatalf("expected 2 NALUs, got %d", len(nalus))
	}
}

// TestSplitAnnexB_Empty tests with empty or too-short data.
func TestSplitAnnexB_Empty(t *testing.T) {
	if nalus := SplitAnnexB(nil); len(nalus) != 0 {
		t.Errorf("expected 0 NALUs for nil, got %d", len(nalus))
	}
	if nalus := SplitAnnexB([]byte{0x00, 0x00}); len(nalus) != 0 {
		t.Errorf("expected 0 NALUs for short data, got %d", len(nalus))
	}
}

// TestNALUType tests NALU type extraction.
func TestNALUType(t *testing.T) {
	tests := []struct {
		name string
		nalu []byte
		want uint8
	}{
		{"SPS", []byte{0x67}, NALUTypeSPS},
		{"PPS", []byte{0x68}, NALUTypePPS},
		{"IDR", []byte{0x65}, NALUTypeIDR},
		{"Slice", []byte{0x41}, NALUTypeSlice},
		{"AUD", []byte{0x09}, NALUTypeAUD},
		{"empty", nil, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NALUType(tt.nalu); got != tt.want {
				t.Errorf("NALUType(%v) = %d, want %d", tt.nalu, got, tt.want)
			}
		})
	}
}

// TestToAVCC tests conversion from NALUs to AVCC format.
func TestToAVCC(t *testing.T) {
	nalus := [][]byte{
		{0x67, 0x42, 0x00}, // 3 bytes
		{0x68, 0xCE},       // 2 bytes
	}

	avcc := ToAVCC(nalus)

	// Expected: [00 00 00 03] [67 42 00] [00 00 00 02] [68 CE]
	expected := []byte{
		0x00, 0x00, 0x00, 0x03, 0x67, 0x42, 0x00,
		0x00, 0x00, 0x00, 0x02, 0x68, 0xCE,
	}

	if !bytes.Equal(avcc, expected) {
		t.Errorf("ToAVCC:\ngot:  %X\nwant: %X", avcc, expected)
	}
}

// TestExtractSPSPPS tests SPS/PPS extraction from a list of NALUs.
func TestExtractSPSPPS(t *testing.T) {
	nalus := [][]byte{
		{0x09, 0x10},       // AUD (should be skipped)
		{0x67, 0x42, 0x00}, // SPS
		{0x68, 0xCE},       // PPS
		{0x65, 0xAA, 0xBB}, // IDR (should be skipped)
	}

	sps, pps, found := ExtractSPSPPS(nalus)
	if !found {
		t.Fatal("expected SPS and PPS to be found")
	}
	if NALUType(sps) != NALUTypeSPS {
		t.Errorf("SPS NALU type: got %d, want %d", NALUType(sps), NALUTypeSPS)
	}
	if NALUType(pps) != NALUTypePPS {
		t.Errorf("PPS NALU type: got %d, want %d", NALUType(pps), NALUTypePPS)
	}
}

// TestExtractSPSPPS_MissingSPS tests when SPS is missing.
func TestExtractSPSPPS_MissingSPS(t *testing.T) {
	nalus := [][]byte{
		{0x68, 0xCE}, // PPS only
	}
	_, _, found := ExtractSPSPPS(nalus)
	if found {
		t.Error("should not find SPS+PPS when SPS is missing")
	}
}

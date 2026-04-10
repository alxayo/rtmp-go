package codec

// This file implements H.265/HEVC (High Efficiency Video Coding) NAL Unit
// parsing and conversion for RTMP.
//
// H.265 is the successor to H.264 and offers significantly better compression
// at the cost of higher encoding complexity. The NALU (Network Abstraction
// Layer Unit) concepts are similar to H.264, but the NAL unit types and
// parameter set structures differ.
//
// Like H.264, H.265 video is composed of NAL Units that are transmitted in
// two possible formats:
//
//   - Annex B format: NALUs are separated by "start codes" (0x000001 or
//     0x00000001). This is what MPEG-TS and raw H.265 bitstreams use.
//
//   - AVCC format: Each NALU is preceded by a 4-byte big-endian length field.
//     This is what RTMP uses (via the HEVCDecoderConfigurationRecord).
//
// The key difference from H.264 is the addition of VPS (Video Parameter Set),
// which is a new parameter set in H.265. H.265 requires all three of:
// - VPS (Video Parameter Set) — contains encoding options for multiple profiles
// - SPS (Sequence Parameter Set) — contains resolution, frame rate, etc.
// - PPS (Picture Parameter Set) — contains picture-level parameters
//
// This file provides functions to identify H.265 NAL unit types and help with
// parsing the H.265 bitstream structure.

// H.265 NAL unit type constants. These are found in bits [6:1] of the first
// byte of each NAL unit (note: different bit positions than H.264).
const (
	// H265NALUTypeVPS is a Video Parameter Set.
	// Contains encoding options and profile/level information.
	// Must be sent before SPS/PPS.
	H265NALUTypeVPS uint8 = 32 // 0x40 >> 1

	// H265NALUTypeSPS is a Sequence Parameter Set.
	// Contains resolution, frame rate, and other sequence-level parameters.
	// Must be sent before PPS and any video frames.
	H265NALUTypeSPS uint8 = 33 // 0x42 >> 1

	// H265NALUTypePPS is a Picture Parameter Set.
	// Contains picture-level parameters.
	// Must be sent before video frames.
	H265NALUTypePPS uint8 = 34 // 0x44 >> 1

	// H265NALUTypeAUD is an Access Unit Delimiter.
	// Marks the boundary between video frames. Often stripped for RTMP output.
	H265NALUTypeAUD uint8 = 35 // 0x46 >> 1

	// H265NALUTypeIDR is a coded slice of an IDR (Instantaneous Decoder Refresh) picture.
	// This is a keyframe that can be decoded independently.
	// Range: 16-21 (0x20-0x2A >> 1)
	H265NALUTypeIDR uint8 = 19 // 0x26 >> 1 (keyframe)

	// H265NALUTypeNonIDR is a coded slice of a non-IDR picture (inter-frame).
	// Range: 0-15 (0x00-0x1E >> 1)
	H265NALUTypeNonIDR uint8 = 1 // example, actual range is 0-15
)

// H265NALUType extracts the NAL unit type from the first byte of an H.265 NALU.
// In H.265, the NALU type is stored in bits [6:1] of the first byte.
// Bit 7 is forbidden_zero_bit, bits [6:1] are nal_unit_type, bit 0 is nuh_temporal_id_plus1[7].
//
// For convenience, this function extracts the type and shifts it to the rightmost position.
// Example: if the first byte is 0x40 (VPS), this returns 32.
func H265NALUType(nalu []byte) uint8 {
	if len(nalu) == 0 {
		return 0
	}
	// Extract bits [6:1] and shift to bits [5:0]
	return (nalu[0] >> 1) & 0x3F
}

// IsH265KeyframeNALU checks if a NAL unit is an IDR (Instantaneous Decoder Refresh) frame.
// IDR frames are keyframes that can be decoded independently.
//
// In H.265, IDR frames have NAL unit types in the range 16-21 (0x20-0x2A >> 1).
// The exact type depends on the picture type, but all are in this range.
func IsH265KeyframeNALU(nalu []byte) bool {
	if len(nalu) == 0 {
		return false
	}
	naluType := H265NALUType(nalu)
	// H.265 IDR NAL units: types 16-21
	return naluType >= 16 && naluType <= 21
}

// IsH265ParameterSet checks if a NAL unit is a parameter set (VPS, SPS, or PPS).
// Parameter sets are metadata that must be sent before video frames.
func IsH265ParameterSet(nalu []byte) bool {
	if len(nalu) == 0 {
		return false
	}
	naluType := H265NALUType(nalu)
	// VPS=32, SPS=33, PPS=34
	return naluType == 32 || naluType == 33 || naluType == 34
}

// FindH265StartCodes locates all Annex B start codes in a byte stream.
// Returns the byte positions where each start code begins.
// This is identical to findStartCodes used by H.264; H.265 uses the same
// Annex B format with 0x000001 or 0x00000001 start codes.
func FindH265StartCodes(data []byte) []int {
	return findStartCodes(data)
}

// SplitH265AnnexB splits an H.265 Annex B byte stream into individual NALUs.
// This is essentially the same as SplitAnnexB for H.264, but provided for
// clarity when working with H.265 streams.
//
// It scans the data for start codes (0x000001 or 0x00000001) and returns
// a slice of NALUs. Each returned NALU does NOT include the start code.
//
// The returned slices point into the original data (no copies are made).
func SplitH265AnnexB(data []byte) [][]byte {
	if len(data) < 4 {
		return nil
	}

	var nalus [][]byte

	// Find all start code positions in the data
	positions := FindH265StartCodes(data)
	if len(positions) == 0 {
		return nil
	}

	// Extract NALUs between consecutive start code positions
	for i, pos := range positions {
		// Determine the start code length (3 or 4 bytes)
		scLen := startCodeLen(data, pos)
		naluStart := pos + scLen

		// Determine the end position (start of next NALU or end of data)
		naluEnd := len(data)
		if i+1 < len(positions) {
			naluEnd = positions[i+1]
		}

		// Extract this NALU (skip the start code, include the data up to next start code)
		nalu := data[naluStart:naluEnd]

		// Filter out empty NALUs
		if len(nalu) > 0 {
			nalus = append(nalus, nalu)
		}
	}

	return nalus
}

package codec

// This file implements NAL Unit (NALU) parsing and conversion for H.264 video.
//
// Background: H.264 video is composed of Network Abstraction Layer (NAL) Units.
// Each NALU is a chunk of video data — it might be a video frame, a sequence
// parameter set (SPS), a picture parameter set (PPS), etc.
//
// The tricky part is that different containers use different ways to delimit
// where one NALU ends and the next begins:
//
//   - Annex B format: NALUs are separated by "start codes" — a sequence of
//     0x00 0x00 0x01 (3 bytes) or 0x00 0x00 0x00 0x01 (4 bytes). This is what
//     MPEG-TS and raw H.264 bitstreams use.
//
//   - AVCC format: Each NALU is preceded by a 4-byte big-endian length field
//     that says how many bytes the NALU contains. This is what RTMP and MP4 use.
//
// This file provides functions to:
//   1. Split an Annex B byte stream into individual NALUs
//   2. Identify what type each NALU is (frame, SPS, PPS, etc.)
//   3. Convert NALUs from Annex B to AVCC format

import "encoding/binary"

// H.264 NALU type constants (lower 5 bits of the first byte of each NALU).
// These tell us what kind of data the NALU contains.
const (
	// NALUTypeSlice is a coded slice of a non-IDR picture (a regular video frame).
	NALUTypeSlice uint8 = 1

	// NALUTypeDPA is a coded slice data partition A.
	NALUTypeDPA uint8 = 2

	// NALUTypeIDR is a coded slice of an IDR picture (a keyframe / I-frame).
	// IDR frames are special because they can be decoded independently —
	// they don't reference any other frames.
	NALUTypeIDR uint8 = 5

	// NALUTypeSEI is Supplemental Enhancement Information.
	// Contains metadata like timing info, display hints, etc.
	NALUTypeSEI uint8 = 6

	// NALUTypeSPS is a Sequence Parameter Set.
	// Contains global encoding parameters like resolution, profile, level.
	// Must be sent before any video frames.
	NALUTypeSPS uint8 = 7

	// NALUTypePPS is a Picture Parameter Set.
	// Contains per-picture encoding parameters.
	// Must be sent before any video frames (after SPS).
	NALUTypePPS uint8 = 8

	// NALUTypeAUD is an Access Unit Delimiter.
	// Marks the boundary between video frames. Useful for segmenting
	// but not needed for RTMP output, so we typically strip these.
	NALUTypeAUD uint8 = 9
)

// NALUType returns the H.264 NALU type from the first byte of a NALU.
// In H.264, the NALU type is stored in the lower 5 bits of the first byte.
// The upper 3 bits are forbidden_zero_bit (1 bit) and nal_ref_idc (2 bits).
func NALUType(nalu []byte) uint8 {
	if len(nalu) == 0 {
		return 0
	}
	return nalu[0] & 0x1F
}

// SplitAnnexB splits an H.264 Annex B byte stream into individual NALUs.
//
// It scans the data for start codes (0x000001 or 0x00000001) and returns
// a slice of NALUs. Each returned NALU does NOT include the start code.
//
// The returned slices point into the original data (no copies are made),
// so modifying the original data will affect the returned NALUs.
//
// Example:
//
//	input:  [0x00 0x00 0x00 0x01 <nalu1 bytes> 0x00 0x00 0x01 <nalu2 bytes>]
//	output: [<nalu1 bytes>, <nalu2 bytes>]
func SplitAnnexB(data []byte) [][]byte {
	if len(data) < 4 {
		return nil
	}

	var nalus [][]byte

	// Find all start code positions in the data
	positions := findStartCodes(data)
	if len(positions) == 0 {
		return nil
	}

	// Extract NALUs between consecutive start code positions
	for i, pos := range positions {
		// Determine the start code length (3 or 4 bytes)
		scLen := startCodeLen(data, pos)
		naluStart := pos + scLen

		// The NALU extends from after this start code to the next start code
		// (or the end of data for the last NALU)
		var naluEnd int
		if i+1 < len(positions) {
			naluEnd = positions[i+1]
		} else {
			naluEnd = len(data)
		}

		// Skip empty NALUs
		if naluStart < naluEnd {
			// Trim trailing zero bytes that might be part of the next start code padding
			nalu := data[naluStart:naluEnd]
			nalu = trimTrailingZeros(nalu)
			if len(nalu) > 0 {
				nalus = append(nalus, nalu)
			}
		}
	}

	return nalus
}

// ToAVCC converts a list of NALUs to AVCC format.
// In AVCC format, each NALU is prefixed with a 4-byte big-endian length.
// This is the format that RTMP and MP4 containers expect.
//
// Example:
//
//	input NALUs: [<5 bytes>, <10 bytes>]
//	output: [0x00 0x00 0x00 0x05 <5 bytes> 0x00 0x00 0x00 0x0A <10 bytes>]
func ToAVCC(nalus [][]byte) []byte {
	// Calculate total size needed: 4 bytes length prefix per NALU + NALU data
	totalSize := 0
	for _, nalu := range nalus {
		totalSize += 4 + len(nalu)
	}

	// Allocate the output buffer
	buf := make([]byte, 0, totalSize)

	// Write each NALU with its 4-byte length prefix
	for _, nalu := range nalus {
		lenBytes := make([]byte, 4)
		binary.BigEndian.PutUint32(lenBytes, uint32(len(nalu)))
		buf = append(buf, lenBytes...)
		buf = append(buf, nalu...)
	}

	return buf
}

// ExtractSPSPPS scans a list of NALUs for Sequence Parameter Set (SPS)
// and Picture Parameter Set (PPS). These are required to build the AVCC
// decoder configuration record (sequence header) for RTMP.
//
// Returns the first SPS and first PPS found. The 'found' return value
// is true only if BOTH SPS and PPS were found.
func ExtractSPSPPS(nalus [][]byte) (sps, pps []byte, found bool) {
	for _, nalu := range nalus {
		switch NALUType(nalu) {
		case NALUTypeSPS:
			if sps == nil {
				sps = nalu
			}
		case NALUTypePPS:
			if pps == nil {
				pps = nalu
			}
		}
	}
	return sps, pps, sps != nil && pps != nil
}

// findStartCodes returns the byte positions of all Annex B start codes
// in the data. A start code is either 0x000001 (3 bytes) or 0x00000001 (4 bytes).
func findStartCodes(data []byte) []int {
	var positions []int

	i := 0
	for i < len(data)-2 {
		// Look for the 3-byte start code pattern: 0x00 0x00 0x01
		if data[i] == 0x00 && data[i+1] == 0x00 && data[i+2] == 0x01 {
			// Check if this is actually a 4-byte start code (0x00 0x00 0x00 0x01)
			if i > 0 && data[i-1] == 0x00 {
				// 4-byte start code — the position should be at i-1
				// Only add if we haven't already added this position
				if len(positions) == 0 || positions[len(positions)-1] != i-1 {
					positions = append(positions, i-1)
				}
			} else {
				positions = append(positions, i)
			}
			i += 3 // Skip past the start code
		} else {
			i++
		}
	}

	return positions
}

// startCodeLen returns the length of the start code at the given position.
// Returns 4 for 0x00000001 and 3 for 0x000001.
func startCodeLen(data []byte, pos int) int {
	if pos+3 < len(data) &&
		data[pos] == 0x00 && data[pos+1] == 0x00 &&
		data[pos+2] == 0x00 && data[pos+3] == 0x01 {
		return 4
	}
	return 3 // 0x00 0x00 0x01
}

// trimTrailingZeros removes trailing zero bytes from a NALU.
// These zeros can appear as padding between NALUs in Annex B streams.
func trimTrailingZeros(nalu []byte) []byte {
	end := len(nalu)
	for end > 0 && nalu[end-1] == 0x00 {
		end--
	}
	return nalu[:end]
}

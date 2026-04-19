package codec

// This file implements VVC/H.266 (Versatile Video Coding) NAL Unit parsing,
// sequence header building, and decoder configuration parsing for RTMP.
//
// VVC is the successor to H.265/HEVC and offers ~50% better compression
// efficiency. The NALU structure is very similar to H.265:
//
//   - NAL unit type is extracted from the same bit position: (byte0 >> 1) & 0x3F
//   - Annex B format uses the same start codes (0x000001 or 0x00000001)
//   - Parameter sets use the same type codes: VPS=32, SPS=33, PPS=34
//   - AVCC format uses 4-byte length-prefixed NALUs (same as H.264/H.265)
//
// Key differences from H.265:
//   - FourCC is "vvc1" (not "hvc1")
//   - VVCDecoderConfigurationRecord has a different binary layout than HEVC
//   - VVC adds APS (Adaptation Parameter Set, type 35) as a new parameter set
//   - AUD (Access Unit Delimiter) is type 38 (vs. 35 in H.265)
//   - Keyframes include IDR (19-20), CRA (21), and GDR (22) types
//
// Like H.264 and H.265, VVC video is composed of NAL Units transmitted in
// two possible formats:
//
//   - Annex B format: NALUs separated by start codes (0x000001 or 0x00000001).
//     Used by MPEG-TS and raw bitstreams.
//
//   - AVCC format: Each NALU preceded by a 4-byte big-endian length field.
//     Used by RTMP (via the VVCDecoderConfigurationRecord).
//
// VVC requires three parameter sets for decoding:
//   - VPS (Video Parameter Set) — encoding options, profile/tier/level
//   - SPS (Sequence Parameter Set) — resolution, frame rate, etc.
//   - PPS (Picture Parameter Set) — picture-level parameters
//
// This file provides functions to parse VVC NALUs, build RTMP sequence headers
// and video frames, and parse VVCDecoderConfigurationRecords from containers
// like MKV and MP4.

import (
	"encoding/binary"
	"fmt"
)

// VVC NAL unit type constants. Like H.265, the type is found in bits [6:1]
// of the first byte of each NAL unit.
const (
	// VVCNALUTypeIDR_W_RADL is a coded slice of an IDR picture with
	// associated RADL pictures. This is a keyframe.
	VVCNALUTypeIDR_W_RADL uint8 = 19

	// VVCNALUTypeIDR_N_LP is a coded slice of an IDR picture without
	// leading pictures. This is a keyframe.
	VVCNALUTypeIDR_N_LP uint8 = 20

	// VVCNALUTypeCRA is a coded slice of a CRA (Clean Random Access) picture.
	// CRA pictures are random access points (keyframes).
	VVCNALUTypeCRA uint8 = 21

	// VVCNALUTypeGDR is a coded slice of a GDR (Gradual Decoder Refresh) picture.
	// GDR pictures provide a gradual refresh mechanism and are treated as keyframes.
	VVCNALUTypeGDR uint8 = 22

	// VVCNALUTypeVPS is a Video Parameter Set.
	// Contains encoding options and profile/tier/level information.
	// Must be sent before SPS/PPS.
	VVCNALUTypeVPS uint8 = 32

	// VVCNALUTypeSPS is a Sequence Parameter Set.
	// Contains resolution, frame rate, and other sequence-level parameters.
	// Must be sent before PPS and any video frames.
	VVCNALUTypeSPS uint8 = 33

	// VVCNALUTypePPS is a Picture Parameter Set.
	// Contains picture-level parameters.
	// Must be sent before video frames.
	VVCNALUTypePPS uint8 = 34

	// VVCNALUTypeAPS is an Adaptation Parameter Set.
	// New in VVC — contains adaptive loop filter and other parameters.
	// Stripped from coded frames but not included in the config record
	// for basic support.
	VVCNALUTypeAPS uint8 = 35

	// VVCNALUTypeAUD is an Access Unit Delimiter.
	// Marks the boundary between video frames. Often stripped for RTMP output.
	VVCNALUTypeAUD uint8 = 38
)

// VVCNALUType extracts the NAL unit type from the first byte of a VVC NALU.
// VVC uses the same bit layout as H.265: the type is stored in bits [6:1]
// of the first byte.
// Bit 7 is forbidden_zero_bit, bits [6:1] are nal_unit_type, bit 0 is
// nuh_temporal_id_plus1[highest bit].
//
// Example: if the first byte is 0x40 (VPS), this returns 32.
func VVCNALUType(nalu []byte) uint8 {
	if len(nalu) == 0 {
		return 0
	}
	return (nalu[0] >> 1) & 0x3F
}

// IsVVCKeyframeNALU checks if a NAL unit is an IRAP (Intra Random Access Point)
// or GDR frame. These are keyframe-equivalent pictures that can be used as
// random access points in VVC.
//
// VVC keyframe types:
//   - IDR_W_RADL (19): IDR with associated RADL pictures
//   - IDR_N_LP (20): IDR without leading pictures
//   - CRA (21): Clean Random Access
//   - GDR (22): Gradual Decoder Refresh
func IsVVCKeyframeNALU(nalu []byte) bool {
	if len(nalu) == 0 {
		return false
	}
	naluType := VVCNALUType(nalu)
	return naluType >= VVCNALUTypeIDR_W_RADL && naluType <= VVCNALUTypeGDR
}

// IsVVCParameterSet checks if a NAL unit is a parameter set (VPS, SPS, PPS,
// or APS). Parameter sets are metadata that must be sent before video frames.
func IsVVCParameterSet(nalu []byte) bool {
	if len(nalu) == 0 {
		return false
	}
	naluType := VVCNALUType(nalu)
	return naluType >= VVCNALUTypeVPS && naluType <= VVCNALUTypeAPS
}

// SplitVVCAnnexB splits a VVC Annex B byte stream into individual NALUs.
// VVC uses the same Annex B start code format as H.264 and H.265
// (0x000001 or 0x00000001), so this reuses the shared start code scanner.
//
// The returned slices point into the original data (no copies are made).
func SplitVVCAnnexB(data []byte) [][]byte {
	if len(data) < 4 {
		return nil
	}

	var nalus [][]byte

	positions := findStartCodes(data)
	if len(positions) == 0 {
		return nil
	}

	for i, pos := range positions {
		scLen := startCodeLen(data, pos)
		naluStart := pos + scLen

		naluEnd := len(data)
		if i+1 < len(positions) {
			naluEnd = positions[i+1]
		}

		if naluStart < naluEnd {
			nalu := data[naluStart:naluEnd]
			nalu = trimTrailingZeros(nalu)
			if len(nalu) > 0 {
				nalus = append(nalus, nalu)
			}
		}
	}

	return nalus
}

// ExtractVVCVPSSPSPPS extracts VPS, SPS, and PPS NALUs from a collection
// of NALUs, typically obtained by splitting an Annex B bitstream with
// SplitVVCAnnexB.
//
// Returns the first occurrence of each parameter set type. The 'found'
// return value is true only if all three were found.
//
// APS NALUs (type 35) are intentionally not extracted here — they are
// part of the coded bitstream rather than the decoder configuration.
func ExtractVVCVPSSPSPPS(nalus [][]byte) (vps, sps, pps []byte, found bool) {
	for _, nalu := range nalus {
		naluType := VVCNALUType(nalu)

		switch naluType {
		case VVCNALUTypeVPS:
			if vps == nil {
				vps = nalu
			}
		case VVCNALUTypeSPS:
			if sps == nil {
				sps = nalu
			}
		case VVCNALUTypePPS:
			if pps == nil {
				pps = nalu
			}
		}

		if vps != nil && sps != nil && pps != nil {
			return vps, sps, pps, true
		}
	}

	found = (vps != nil && sps != nil && pps != nil)
	return vps, sps, pps, found
}

// BuildVVCSequenceHeader builds the RTMP video tag payload for a VVC
// sequence header. This must be sent before any VVC video frames so the
// decoder knows how to interpret the video data.
//
// The payload uses Enhanced RTMP format:
//
//	Byte 0: [IsExHeader:1][FrameType:3][PacketType:4]
//	  IsExHeader = 1, FrameType = 1 (keyframe), PacketType = 0 (SequenceStart)
//	  = 0x90
//	Bytes 1-4: FourCC = "vvc1" (identifies VVC codec)
//	Remaining: VVCDecoderConfigurationRecord
//
// The VVCDecoderConfigurationRecord (ISO/IEC 14496-15) contains VPS, SPS,
// and PPS parameter sets needed to initialize the decoder.
//
// We build a minimal configuration record with ptl_present_flag=0 (no
// profile/tier/level signaling in the config), which keeps the record
// simple while remaining spec-compliant. Decoders extract profile/level
// info from the SPS NALU directly.
//
// VVCDecoderConfigurationRecord layout (minimal, ptl_present_flag=0):
//
//	Byte 0: configurationVersion = 1
//	Byte 1: [lengthSizeMinusOne:2][ptl_present_flag:1][reserved:5]
//	         = (3 << 6) | (0 << 5) | 0x1F = 0xDF
//	Byte 2: numOfArrays
//	Then for each array:
//	  Byte: [array_completeness:1][reserved:1][NAL_unit_type:6]
//	  2 bytes: numNalus (big-endian)
//	  For each NALU:
//	    2 bytes: nalUnitLength (big-endian)
//	    N bytes: NAL unit data
func BuildVVCSequenceHeader(vps, sps, pps []byte) []byte {
	// Calculate sizes:
	// Enhanced RTMP header: 5 bytes (1 byte ExHeader + 4 bytes FourCC "vvc1")
	// VVCDecoderConfigurationRecord: 3 bytes fixed header + 3 NAL arrays
	//   Each array: 1 byte type + 2 bytes count + (2 bytes length + data) per NALU
	recordLen := 3 + // Fixed header (configVersion, flags, numOfArrays)
		(1 + 2 + 2 + len(vps)) + // VPS array
		(1 + 2 + 2 + len(sps)) + // SPS array
		(1 + 2 + 2 + len(pps)) // PPS array

	buf := make([]byte, 5+recordLen)

	// -- Enhanced RTMP video tag header (5 bytes) --
	// Byte 0: [IsExHeader:1][FrameType:3][PacketType:4]
	//   IsExHeader = 1 (bit 7) — signals Enhanced RTMP format
	//   FrameType  = 1 (bits 6-4) — keyframe (sequence headers are always keyframes)
	//   PacketType = 0 (bits 3-0) — SequenceStart (codec configuration record)
	// = 0b1_001_0000 = 0x90
	buf[0] = 0x90

	// Bytes 1-4: FourCC = "vvc1" (identifies VVC codec)
	buf[1] = 'v'
	buf[2] = 'v'
	buf[3] = 'c'
	buf[4] = '1'

	// -- VVCDecoderConfigurationRecord (ISO/IEC 14496-15) --
	off := 5

	// Byte 0: configurationVersion = 1 (always 1 for current spec)
	buf[off] = 1
	off++

	// Byte 1: [lengthSizeMinusOne:2][ptl_present_flag:1][reserved:5]
	//   lengthSizeMinusOne = 3 (4-byte NALU lengths) → bits [7:6] = 0b11
	//   ptl_present_flag   = 0 (skip profile/tier/level) → bit 5 = 0
	//   reserved           = 0b11111 → bits [4:0] = 0x1F
	// = 0b11_0_11111 = 0xDF
	buf[off] = 0xDF
	off++

	// Byte 2: numOfArrays = 3 (VPS + SPS + PPS)
	buf[off] = 3
	off++

	// Array 1: VPS
	// Byte: [array_completeness:1][reserved:1][NAL_unit_type:6]
	//   array_completeness=1 | reserved=0 | nal_unit_type=32 (VPS)
	// = 0b1_0_100000 = 0xA0
	buf[off] = 0xA0
	off++
	binary.BigEndian.PutUint16(buf[off:], 1) // numNalus = 1
	off += 2
	binary.BigEndian.PutUint16(buf[off:], uint16(len(vps))) // VPS length
	off += 2
	copy(buf[off:], vps)
	off += len(vps)

	// Array 2: SPS
	// array_completeness=1 | reserved=0 | nal_unit_type=33 (SPS)
	// = 0b1_0_100001 = 0xA1
	buf[off] = 0xA1
	off++
	binary.BigEndian.PutUint16(buf[off:], 1) // numNalus = 1
	off += 2
	binary.BigEndian.PutUint16(buf[off:], uint16(len(sps))) // SPS length
	off += 2
	copy(buf[off:], sps)
	off += len(sps)

	// Array 3: PPS
	// array_completeness=1 | reserved=0 | nal_unit_type=34 (PPS)
	// = 0b1_0_100010 = 0xA2
	buf[off] = 0xA2
	off++
	binary.BigEndian.PutUint16(buf[off:], 1) // numNalus = 1
	off += 2
	binary.BigEndian.PutUint16(buf[off:], uint16(len(pps))) // PPS length
	off += 2
	copy(buf[off:], pps)

	return buf
}

// BuildVVCVideoFrame builds the RTMP video tag payload for a VVC frame.
//
// Parameters:
//   - nalus: The NAL units that make up this frame (already split from Annex B).
//     These should be VCL (video coding layer) NALUs only (no VPS/SPS/PPS/AUD).
//   - isKeyframe: true if this is an IRAP/GDR frame (keyframe)
//   - cts: Composition Time Offset in milliseconds (PTS - DTS).
//     This is 0 for streams without B-frames, which is typical for live.
func BuildVVCVideoFrame(nalus [][]byte, isKeyframe bool, cts int32) []byte {
	// Convert NALUs to AVCC format (4-byte length prefix each)
	avccData := ToAVCC(nalus)

	// Allocate: 8 bytes Enhanced RTMP header + AVCC data
	// Header: 1 byte ExHeader + 4 bytes FourCC + 3 bytes CTS
	buf := make([]byte, 8+len(avccData))

	// -- Enhanced RTMP video tag header (8 bytes for CodedFrames) --
	// Byte 0: [IsExHeader:1][FrameType:3][PacketType:4]
	//   IsExHeader = 1 (bit 7) — Enhanced RTMP format
	//   FrameType  = 1 (keyframe) or 2 (inter-frame), in bits 6-4
	//   PacketType = 1 (CodedFrames), in bits 3-0
	if isKeyframe {
		// 0b1_001_0001 = 0x91 (IsExHeader=1, Keyframe=1, CodedFrames=1)
		buf[0] = 0x91
	} else {
		// 0b1_010_0001 = 0xA1 (IsExHeader=1, Inter=2, CodedFrames=1)
		buf[0] = 0xA1
	}

	// Bytes 1-4: FourCC = "vvc1" (VVC codec identifier)
	buf[1] = 'v'
	buf[2] = 'v'
	buf[3] = 'c'
	buf[4] = '1'

	// Bytes 5-7: CompositionTimeOffset (signed 24-bit, big-endian)
	// CTS = PTS - DTS, in milliseconds. Tells the player how to reorder
	// frames for display when B-frames are present.
	buf[5] = byte(cts >> 16)
	buf[6] = byte(cts >> 8)
	buf[7] = byte(cts)

	// Copy the AVCC-formatted NAL units after the header
	copy(buf[8:], avccData)

	return buf
}

// IsVVCKeyframe examines AVCC-formatted VVC frame data to determine if it
// contains a keyframe (IRAP or GDR picture).
//
// The data is expected to be in AVCC format: a sequence of length-prefixed
// NALUs where each NALU is preceded by a 4-byte big-endian length. This
// function walks through the NALUs checking their types against the VVC
// keyframe type range (IDR, CRA, GDR).
func IsVVCKeyframe(avccData []byte) bool {
	offset := 0
	for offset+4 < len(avccData) {
		naluLen := int(binary.BigEndian.Uint32(avccData[offset:]))
		offset += 4

		if naluLen <= 0 || offset+naluLen > len(avccData) {
			break
		}

		if IsVVCKeyframeNALU(avccData[offset : offset+naluLen]) {
			return true
		}

		offset += naluLen
	}
	return false
}

// VVCDecoderConfig holds the parsed contents of a VVCDecoderConfigurationRecord.
// This is extracted from MKV CodecPrivate data or MP4 vvcC boxes for VVC tracks.
type VVCDecoderConfig struct {
	// NALULengthSize is the number of bytes used for NALU length fields
	// in frame data (1, 2, 3, or 4 bytes). Usually 4.
	NALULengthSize int

	// VPS is the first Video Parameter Set from the config record.
	// Contains encoding options, profile, tier, and level information.
	VPS []byte

	// SPS is the first Sequence Parameter Set from the config record.
	// Contains resolution, frame rate, and other sequence-level parameters.
	SPS []byte

	// PPS is the first Picture Parameter Set from the config record.
	// Contains picture-level encoding parameters.
	PPS []byte
}

// ParseVVCDecoderConfig parses a VVCDecoderConfigurationRecord (typically
// from MKV CodecPrivate data or an MP4 vvcC box) and extracts VPS, SPS,
// PPS, and the NALU length field size.
//
// VVCDecoderConfigurationRecord layout (ISO/IEC 14496-15):
//
//	Byte 0:    configurationVersion (must be 1)
//	Byte 1:    [lengthSizeMinusOne:2][ptl_present_flag:1][reserved:5]
//	           lengthSizeMinusOne is in bits [7:6]
//	           ptl_present_flag is bit 5
//	If ptl_present_flag == 1:
//	  Variable-length profile_tier_level data follows (skipped)
//	Byte N:    numOfArrays
//	For each array:
//	  Byte:    [array_completeness:1][reserved:1][NAL_unit_type:6]
//	  2 bytes: numNalus (big-endian)
//	  For each NALU:
//	    2 bytes: naluLength (big-endian)
//	    N bytes: NALU data
//
// The returned parameter sets are copies (safe to keep after the input is freed).
// Returns an error if the data is too short or malformed.
func ParseVVCDecoderConfig(data []byte) (*VVCDecoderConfig, error) {
	// Minimum size: 3 bytes (configVersion + flags + numOfArrays)
	if len(data) < 3 {
		return nil, fmt.Errorf("VVC config too short: %d bytes, need at least 3", len(data))
	}

	// Byte 0: configurationVersion — must be 1
	if data[0] != 1 {
		return nil, fmt.Errorf("unsupported VVC config version: %d", data[0])
	}

	// Byte 1: [lengthSizeMinusOne:2][ptl_present_flag:1][reserved:5]
	naluLengthSize := int(data[1]>>6) + 1
	ptlPresentFlag := (data[1] >> 5) & 0x01

	offset := 2

	// If ptl_present_flag is set, skip the profile_tier_level data.
	// The PTL record in VVCDecoderConfigurationRecord has a complex variable
	// structure. We parse just enough to skip past it.
	if ptlPresentFlag == 1 {
		var err error
		offset, err = skipVVCPTL(data, offset)
		if err != nil {
			return nil, err
		}
	}

	// numOfArrays
	if offset >= len(data) {
		return nil, fmt.Errorf("VVC config truncated before numOfArrays")
	}
	numArrays := int(data[offset])
	offset++

	config := &VVCDecoderConfig{
		NALULengthSize: naluLengthSize,
	}

	// Walk through each NALU array looking for VPS (32), SPS (33), PPS (34)
	for i := 0; i < numArrays; i++ {
		if offset+3 > len(data) {
			return nil, fmt.Errorf("VVC config truncated at array %d header", i)
		}

		// Extract NAL unit type from bottom 6 bits of the first byte
		naluType := data[offset] & 0x3F
		offset++

		// Number of NALUs in this array
		numNalus := int(binary.BigEndian.Uint16(data[offset:]))
		offset += 2

		for j := 0; j < numNalus; j++ {
			if offset+2 > len(data) {
				return nil, fmt.Errorf("VVC config truncated at array %d NALU %d length", i, j)
			}
			naluLen := int(binary.BigEndian.Uint16(data[offset:]))
			offset += 2

			if offset+naluLen > len(data) {
				return nil, fmt.Errorf("VVC config truncated at array %d NALU %d data (need %d, have %d)",
					i, j, naluLen, len(data)-offset)
			}

			// Copy the first NALU of each type we care about
			naluData := make([]byte, naluLen)
			copy(naluData, data[offset:offset+naluLen])

			switch naluType {
			case VVCNALUTypeVPS:
				if config.VPS == nil {
					config.VPS = naluData
				}
			case VVCNALUTypeSPS:
				if config.SPS == nil {
					config.SPS = naluData
				}
			case VVCNALUTypePPS:
				if config.PPS == nil {
					config.PPS = naluData
				}
			}

			offset += naluLen
		}
	}

	// Validate that we found all three required parameter sets
	if config.VPS == nil {
		return nil, fmt.Errorf("VVC config missing VPS")
	}
	if config.SPS == nil {
		return nil, fmt.Errorf("VVC config missing SPS")
	}
	if config.PPS == nil {
		return nil, fmt.Errorf("VVC config missing PPS")
	}

	return config, nil
}

// skipVVCPTL skips past the profile_tier_level data in a
// VVCDecoderConfigurationRecord when ptl_present_flag is set.
//
// The PTL record in the VVC config has this structure:
//
//	ols_idx:                  9 bits
//	num_sublayers:            3 bits
//	constant_frame_rate:      2 bits
//	chroma_format_idc:        2 bits   → 2 bytes so far
//	bit_depth_minus8:         3 bits
//	reserved:                 5 bits   → 1 more byte (3 total)
//
//	-- VvcPTLRecord(num_sublayers) --
//	reserved:                 2 bits
//	num_bytes_constraint_info: 6 bits
//	general_profile_idc:      7 bits
//	general_tier_flag:        1 bit    → 1 byte
//	general_level_idc:        8 bits   → 1 byte
//	ptl_frame_only_constraint: 1 bit
//	ptl_multilayer_enabled:   1 bit
//	general_constraint_info:  num_bytes_constraint_info bytes (variable)
//	sublayer_level_idc:       (num_sublayers - 2) bytes if num_sublayers > 1
//	ptl_num_sub_profiles:     8 bits
//	general_sub_profile_idc:  ptl_num_sub_profiles * 32 bits
//
// Returns the new offset past the PTL data, or an error if truncated.
func skipVVCPTL(data []byte, offset int) (int, error) {
	// Need at least 3 bytes for the fixed fields before VvcPTLRecord
	if offset+3 > len(data) {
		return 0, fmt.Errorf("VVC config truncated in PTL fixed fields")
	}

	// First 2 bytes: [ols_idx:9][num_sublayers:3][constant_frame_rate:2][chroma_format_idc:2]
	numSublayers := int((data[offset+1] >> 4) & 0x07)
	offset += 2

	// Third byte: [bit_depth_minus8:3][reserved:5]
	offset++

	// -- VvcPTLRecord --
	// First byte: [reserved:2][num_bytes_constraint_info:6]
	if offset >= len(data) {
		return 0, fmt.Errorf("VVC config truncated at PTL record start")
	}
	numBytesConstraintInfo := int(data[offset] & 0x3F)
	offset++

	// general_profile_idc (7 bits) + general_tier_flag (1 bit) = 1 byte
	if offset >= len(data) {
		return 0, fmt.Errorf("VVC config truncated at PTL profile_idc")
	}
	offset++

	// general_level_idc = 1 byte
	if offset >= len(data) {
		return 0, fmt.Errorf("VVC config truncated at PTL level_idc")
	}
	offset++

	// ptl_frame_only_constraint (1 bit) + ptl_multilayer_enabled (1 bit)
	// + general_constraint_info (num_bytes_constraint_info bytes)
	// The first 2 bits are packed into the first constraint info byte,
	// so the total is num_bytes_constraint_info bytes.
	if offset+numBytesConstraintInfo > len(data) {
		return 0, fmt.Errorf("VVC config truncated at PTL constraint info")
	}
	offset += numBytesConstraintInfo

	// sublayer_level_idc: present for each sublayer from (num_sublayers-2) down to 0,
	// but only if num_sublayers > 1. Each sublayer has a flag bit and conditionally
	// a level byte. In the config record, this is simplified to (num_sublayers - 1) bytes
	// if num_sublayers > 1.
	if numSublayers > 1 {
		sublayerBytes := numSublayers - 1
		if offset+sublayerBytes > len(data) {
			return 0, fmt.Errorf("VVC config truncated at PTL sublayer levels")
		}
		offset += sublayerBytes
	}

	// ptl_num_sub_profiles (8 bits)
	if offset >= len(data) {
		return 0, fmt.Errorf("VVC config truncated at PTL num sub profiles")
	}
	numSubProfiles := int(data[offset])
	offset++

	// general_sub_profile_idc: 4 bytes each
	subProfileBytes := numSubProfiles * 4
	if offset+subProfileBytes > len(data) {
		return 0, fmt.Errorf("VVC config truncated at PTL sub profile IDCs")
	}
	offset += subProfileBytes

	return offset, nil
}

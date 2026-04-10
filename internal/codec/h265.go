package codec

// This file implements H.265/HEVC video conversion for RTMP.
//
// H.265 (HEVC) is the successor to H.264/AVC. Like H.264, H.265 video
// must include parameter sets for the decoder to work properly.
//
// Key differences from H.264:
//   - H.265 requires THREE parameter sets: VPS, SPS, PPS (vs. H.264's SPS, PPS)
//   - VPS = Video Parameter Set (new in H.265): contains general encoding options
//   - SPS = Sequence Parameter Set: contains resolution, frame rate, etc.
//   - PPS = Picture Parameter Set: contains picture-level parameters
//
// The HEVCDecoderConfigurationRecord (per ISO/IEC 14496-15) is built from
// VPS, SPS, and PPS. It tells the RTMP decoder:
//   - Which H.265 profile and level is used
//   - The video resolution and frame rate
//   - The picture parameters
//
// When an RTMP client publishes H.265, we intercept the sequence header
// (which contains VPS+SPS+PPS) and forward it to subscribers. When SRT
// delivers H.265 via MPEG-TS, we extract the VPS, SPS, PPS separately
// and reconstruct this configuration record.

import "encoding/binary"

// BuildHEVCSequenceHeader builds the RTMP video tag payload for an H.265
// sequence header. This must be sent before any H.265 video frames so the
// decoder knows how to interpret the video data.
//
// The payload contains a HEVCDecoderConfigurationRecord per ISO/IEC 14496-15:
//
//	ConfigurationVersion:        1 (always)
//	GeneralProfileSpace:         2 bits
//	GeneralTierFlag:             1 bit
//	GeneralProfileIdc:           5 bits
//	GeneralProfileCompatibilityFlags: 32 bits
//	GeneralConstraintIndicatorFlags: 48 bits
//	GeneralLevelIdc:             8 bits
//	MinSpatialSegmentationIdc:   12 bits (with 4-bit reserved)
//	ParallelismType:             2 bits (with 6-bit reserved)
//	ChromaFormatIdc:             2 bits (with 6-bit reserved)
//	BitDepthLumaMinus8:          3 bits (with 5-bit reserved)
//	BitDepthChromaMinus8:        3 bits (with 5-bit reserved)
//	AvgFrameRate:                16 bits
//	ConstantFrameRate:           2 bits (with 6-bit reserved)
//	NumTemporalLayers:           3 bits (with 5-bit reserved)
//	TemporalIdNested:            1 bit
//	LengthSizeMinusOne:          2 bits (with 6-bit reserved)
//	NumOfArrays:                 8 bits
//	Then for each array:
//	  - ArrayCompleteness:       1 bit
//	  - Reserved:                1 bit
//	  - NAL_unit_type:           6 bits
//	  - NumNalus:                16 bits
//	  - For each NALU:
//	    - NAL_unit_length:       16 bits
//	    - NAL_unit_data:         variable
//
// For simplicity and consistency with how RTMP clients typically send this,
// we build a minimal configuration record with one VPS, one SPS, and one PPS.
// This matches how ffmpeg encodes H.265 for RTMP output.
func BuildHEVCSequenceHeader(vps, sps, pps []byte) []byte {
	// Calculate sizes:
	// Enhanced RTMP header: 5 bytes (1 byte ExHeader + 4 bytes FourCC "hvc1")
	// HEVCDecoderConfigurationRecord: 23 bytes fixed header + 3 NAL arrays
	//   Each array: 1 byte type + 2 bytes count + (2 bytes length + data) per NALU
	recordLen := 23 + // Fixed header (configVersion through numOfArrays)
		(1 + 2 + 2 + len(vps)) + // VPS array: type byte, count, VPS length, VPS data
		(1 + 2 + 2 + len(sps)) + // SPS array
		(1 + 2 + 2 + len(pps))   // PPS array

	// Enhanced RTMP header is 5 bytes (no CTS for SequenceStart)
	buf := make([]byte, 5+recordLen)

	// -- Enhanced RTMP video tag header (5 bytes) --
	// Byte 0: [IsExHeader:1][FrameType:3][PacketType:4]
	//   IsExHeader = 1 (bit 7) — signals Enhanced RTMP format
	//   FrameType  = 1 (bits 6-4) — keyframe (sequence headers are always keyframes)
	//   PacketType = 0 (bits 3-0) — SequenceStart (codec configuration record)
	// = 0b1_001_0000 = 0x90
	buf[0] = 0x90

	// Bytes 1-4: FourCC = "hvc1" (identifies H.265/HEVC codec)
	// This is the standard Enhanced RTMP signaling for HEVC, replacing the
	// non-standard legacy CodecID=12 approach.
	buf[1] = 'h'
	buf[2] = 'v'
	buf[3] = 'c'
	buf[4] = '1'

	// -- HEVCDecoderConfigurationRecord (ISO/IEC 14496-15 §8.3.3.1) --
	// This record describes the H.265 decoder configuration and embeds the
	// three parameter sets (VPS, SPS, PPS) needed to initialize the decoder.
	off := 5

	// ConfigurationVersion = 1 (always 1 for current spec)
	buf[off] = 1
	off++

	// Profile/tier/level information extracted from the SPS NALU.
	//
	// H.265 SPS NALU structure (after start code removal):
	//   Bytes 0-1: NAL unit header (2 bytes: type=33 in bits [6:1] of byte 0)
	//   Byte 2:    sps_video_parameter_set_id (4 bits) |
	//              sps_max_sub_layers_minus1 (3 bits) |
	//              sps_temporal_id_nesting_flag (1 bit)
	//   Byte 3:    profile_tier_level starts here:
	//              general_profile_space (2 bits) |
	//              general_tier_flag (1 bit) |
	//              general_profile_idc (5 bits)
	//   Bytes 4-7: general_profile_compatibility_flags (32 bits)
	//   Bytes 8-13: general_constraint_indicator_flags (48 bits)
	//   Byte 14:   general_level_idc (8 bits)
	//
	// We need at least 15 bytes of SPS to read all profile/tier/level fields.
	if len(sps) >= 15 {
		// general_profile_space (2) | general_tier_flag (1) | general_profile_idc (5)
		buf[off] = sps[3]
		off++

		// general_profile_compatibility_flags (32 bits)
		copy(buf[off:off+4], sps[4:8])
		off += 4

		// general_constraint_indicator_flags (48 bits)
		copy(buf[off:off+6], sps[8:14])
		off += 6

		// general_level_idc
		buf[off] = sps[14]
		off++
	} else {
		// Fallback: SPS too short to extract profile/tier/level.
		// Write Main profile defaults so decoders have something to work with.
		buf[off] = 0x01 // Main profile (profile_idc=1)
		off++
		// Profile compatibility: set bit for Main profile
		buf[off] = 0x60 // bits for Main profile compatibility
		off++
		buf[off] = 0x00
		off++
		buf[off] = 0x00
		off++
		buf[off] = 0x00
		off++
		// Constraint indicator flags (6 bytes, all zero = no constraints)
		for i := 0; i < 6; i++ {
			buf[off] = 0x00
			off++
		}
		// Level 3.1 (93) as a safe default
		buf[off] = 93
		off++
	}

	// min_spatial_segmentation_idc: 4 reserved bits (1111) + 12-bit value
	// Per spec, reserved bits MUST be 1111. Value 0 = no segmentation info.
	// Layout: [1111][12-bit value] → 0xF000 when value=0
	binary.BigEndian.PutUint16(buf[off:], 0xF000)
	off += 2

	// parallelismType: 6 reserved bits (111111) + 2-bit value
	// Per spec, reserved bits MUST be 111111. Value 0 = unknown.
	// Layout: [111111][2-bit value] → 0xFC when value=0
	buf[off] = 0xFC
	off++

	// chromaFormatIdc: 6 reserved bits (111111) + 2-bit value
	// Per spec, reserved bits MUST be 111111. Value 1 = 4:2:0 (most common).
	// Layout: [111111][2-bit value] → 0xFC | 0x01 = 0xFD for 4:2:0
	buf[off] = 0xFD
	off++

	// bitDepthLumaMinus8: 5 reserved bits (11111) + 3-bit value
	// Per spec, reserved bits MUST be 11111. Value 0 = 8-bit luma.
	// Layout: [11111][3-bit value] → 0xF8 when value=0 (8-bit)
	buf[off] = 0xF8
	off++

	// bitDepthChromaMinus8: 5 reserved bits (11111) + 3-bit value
	// Same as luma. Value 0 = 8-bit chroma.
	// Layout: [11111][3-bit value] → 0xF8 when value=0 (8-bit)
	buf[off] = 0xF8
	off++

	// avgFrameRate (16 bits, big-endian): 0 = unspecified
	binary.BigEndian.PutUint16(buf[off:], 0)
	off += 2

	// Combined flags byte:
	//   constantFrameRate (2 bits) | numTemporalLayers (3 bits) |
	//   temporalIdNested (1 bit) | lengthSizeMinusOne (2 bits)
	// Values: constantFrameRate=0, numTemporalLayers=1, temporalIdNested=1,
	//         lengthSizeMinusOne=3 (4-byte NALU lengths)
	// = 0b00_001_1_11 = 0x0F
	buf[off] = 0x0F
	off++

	// Number of NAL unit arrays (8 bits)
	// We have 3 arrays: VPS, SPS, PPS
	buf[off] = 3
	off++

	// Array 1: VPS
	// Byte: array_completeness (1 bit) | reserved (1 bit) | nal_unit_type (6 bits)
	// array_completeness=1 (complete set) | reserved=0 | nal_unit_type=32 (VPS)
	// = 0b1_0_100000 = 0xA0
	buf[off] = 0xA0
	off++
	// NumNalus (16 bits, big-endian) = 1
	binary.BigEndian.PutUint16(buf[off:], 1)
	off += 2
	// VPS length (16 bits, big-endian)
	binary.BigEndian.PutUint16(buf[off:], uint16(len(vps)))
	off += 2
	// VPS data
	copy(buf[off:], vps)
	off += len(vps)

	// Array 2: SPS
	// Byte: array_completeness=1 | reserved=0 | nal_unit_type=33 (SPS)
	// = 0b1_0_100001 = 0xA1
	buf[off] = 0xA1
	off++
	// NumNalus = 1
	binary.BigEndian.PutUint16(buf[off:], 1)
	off += 2
	// SPS length
	binary.BigEndian.PutUint16(buf[off:], uint16(len(sps)))
	off += 2
	// SPS data
	copy(buf[off:], sps)
	off += len(sps)

	// Array 3: PPS
	// Byte: array_completeness=1 | reserved=0 | nal_unit_type=34 (PPS)
	// = 0b1_0_100010 = 0xA2
	buf[off] = 0xA2
	off++
	// NumNalus = 1
	binary.BigEndian.PutUint16(buf[off:], 1)
	off += 2
	// PPS length
	binary.BigEndian.PutUint16(buf[off:], uint16(len(pps)))
	off += 2
	// PPS data
	copy(buf[off:], pps)

	return buf
}

// BuildHEVCVideoFrame builds the RTMP video tag payload for an H.265 frame.
//
// Parameters:
//   - nalus: The NAL units that make up this frame (already split from Annex B).
//     These should be VCL (video coding layer) NALUs only (no VPS/SPS/PPS/AUD).
//   - isKeyframe: true if this is an IDR frame (I-frame / keyframe)
//   - cts: Composition Time Offset in milliseconds (PTS - DTS).
//     This is 0 for streams without B-frames, which is typical for live.
func BuildHEVCVideoFrame(nalus [][]byte, isKeyframe bool, cts int32) []byte {
	// Convert NALUs to AVCC format (4-byte length prefix each)
	// H.265 uses the same AVCC format as H.264
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

	// Bytes 1-4: FourCC = "hvc1" (H.265/HEVC codec identifier)
	buf[1] = 'h'
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

// ExtractH265VPSSPSPPS extracts VPS, SPS, and PPS NALUs from a collection
// of NALUs, typically obtained by splitting an Annex B bitstream with SplitH265AnnexB.
//
// Returns:
//   - vps: The Video Parameter Set NALU (or nil if not found)
//   - sps: The Sequence Parameter Set NALU (or nil if not found)
//   - pps: The Picture Parameter Set NALU (or nil if not found)
//   - found: true if all three parameter sets were found
//
// This function searches through the provided NALUs and extracts the first
// occurrence of each parameter set type. If multiple VPS/SPS/PPS are present,
// only the first of each is returned.
//
// This is used by the SRT bridge to extract parameters from MPEG-TS streams
// and build sequence headers for RTMP subscribers.
func ExtractH265VPSSPSPPS(nalus [][]byte) (vps, sps, pps []byte, found bool) {
	for _, nalu := range nalus {
		naluType := H265NALUType(nalu)

		switch naluType {
		case 32: // VPS
			if vps == nil {
				vps = nalu
			}
		case 33: // SPS
			if sps == nil {
				sps = nalu
			}
		case 34: // PPS
			if pps == nil {
				pps = nalu
			}
		}

		// Early exit if we've found all three
		if vps != nil && sps != nil && pps != nil {
			return vps, sps, pps, true
		}
	}

	found = (vps != nil && sps != nil && pps != nil)
	return vps, sps, pps, found
}

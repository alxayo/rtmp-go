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
	// RTMP tag header: 5 bytes
	// HEVCDecoderConfigurationRecord: 23 bytes minimum + 3 NAL arrays
	//   Each array: 1 byte type + 2 bytes count + (2 bytes length + data) per NALU
	recordLen := 23 + // Fixed header through LengthSizeMinusOne
		1 + // NumOfArrays
		(1 + 2 + 2 + len(vps)) + // VPS array: type byte, count, VPS length, VPS data
		(1 + 2 + 2 + len(sps)) + // SPS array
		(1 + 2 + 2 + len(pps))   // PPS array

	buf := make([]byte, 5+recordLen)

	// -- RTMP video tag header (5 bytes) --
	// Byte 0: FrameType=1 (keyframe) << 4 | CodecID=12 (HEVC legacy) or use Enhanced RTMP
	// For Enhanced RTMP (E-RTMP), we use CodecID=0xF with IsExHeader=1 and FourCC="hvc1"
	// Here we use the legacy CodecID=12 approach for compatibility:
	buf[0] = 0x1C // FrameType=1 (keyframe) << 4 | CodecID=12 (HEVC)
	// Byte 1: PacketType=0 (sequence header)
	buf[1] = 0x00
	// Bytes 2-4: CompositionTimeOffset = 0 for sequence headers
	buf[2] = 0x00
	buf[3] = 0x00
	buf[4] = 0x00

	// -- HEVCDecoderConfigurationRecord --
	off := 5

	// ConfigurationVersion = 1
	buf[off] = 1
	off++

	// General profile/tier/level (from SPS bytes 1-7)
	// For simplicity, extract what we can from SPS if available
	if len(sps) >= 13 {
		// Byte 1: profile_space (2 bits) | tier_flag (1 bit) | profile_idc (5 bits)
		buf[off] = sps[1]
		off++

		// Bytes 2-5: General profile compatibility flags (4 bytes, from SPS)
		copy(buf[off:off+4], sps[2:6])
		off += 4

		// Bytes 6-11: General constraint indicator flags (6 bytes, from SPS)
		copy(buf[off:off+6], sps[6:12])
		off += 6

		// Byte 12: General level Idc (from SPS byte 12)
		buf[off] = sps[12]
		off++
	} else {
		// Fallback: use minimal defaults if SPS is too short
		// Write dummy profile/tier/level
		buf[off] = 0x00 // profile
		off++
		for i := 0; i < 4; i++ {
			buf[off] = 0x00 // profile compatibility
			off++
		}
		for i := 0; i < 6; i++ {
			buf[off] = 0x00 // constraint flags
			off++
		}
		buf[off] = 0x00 // level
		off++
	}

	// Min spatial segmentation Idc (12 bits) + reserved (4 bits)
	// Typically 0; encode as 0xF000 in 16-bit big-endian
	binary.BigEndian.PutUint16(buf[off:], 0xF000)
	off += 2

	// Parallelism type (2 bits) + reserved (6 bits)
	// Typically 0; encode as 0x00
	buf[off] = 0x00
	off++

	// Chroma format Idc (2 bits) + reserved (6 bits)
	// Typically 1 (4:2:0); encode as 0x01
	buf[off] = 0x01
	off++

	// Bit depth luma minus 8 (3 bits) + reserved (5 bits)
	// For 8-bit video: 0; encode as 0x00
	// For 10-bit video: 2; encode as 0x02
	// Default to 8-bit (0)
	buf[off] = 0x00
	off++

	// Bit depth chroma minus 8 (3 bits) + reserved (5 bits)
	// Same as luma; default to 0
	buf[off] = 0x00
	off++

	// Average frame rate (16 bits, big-endian)
	// 0 = unspecified; commonly used value
	binary.BigEndian.PutUint16(buf[off:], 0)
	off += 2

	// Constant frame rate (2 bits) | reserved (4 bits) | num temporal layers (3 bits) | temporal ID nested (1 bit)
	// Encode as: constant_frame_rate=0 (2 bits) | reserved=0xF (4 bits) | num_temporal_layers=1 (3 bits) | temporal_id_nested=1 (1 bit)
	// = 0b00_1111_001_1 = 0x3C
	buf[off] = 0x3C
	off++

	// Length size minus one (2 bits) + reserved (6 bits)
	// LengthSize = 4 bytes, so LengthSizeM1 = 3
	// Encode as 0xFC | 0x03 = 0xFF
	buf[off] = 0xFF
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

	// Allocate: 5 bytes RTMP header + AVCC data
	buf := make([]byte, 5+len(avccData))

	// -- RTMP video tag header (5 bytes) --
	// Byte 0: [FrameType:4][CodecID:4]
	// For legacy H.265: CodecID = 12
	if isKeyframe {
		buf[0] = 0x1C // Keyframe (1) + HEVC (12)
	} else {
		buf[0] = 0x2C // Inter-frame (2) + HEVC (12)
	}

	// Byte 1: HEVCPacketType = 1 (NALU data)
	// (Similar to AVC packet type, value 1 means "data")
	buf[1] = 0x01

	// Bytes 2-4: CompositionTimeOffset (signed 24-bit, big-endian)
	// CTS = PTS - DTS, in milliseconds
	buf[2] = byte(cts >> 16)
	buf[3] = byte(cts >> 8)
	buf[4] = byte(cts)

	// Copy the AVCC data after the header
	copy(buf[5:], avccData)

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

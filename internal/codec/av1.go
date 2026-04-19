package codec

// This file implements AV1 video conversion for Enhanced RTMP.
//
// AV1 is an open, royalty-free video codec developed by the Alliance for Open
// Media (AOMedia). AV1 uses OBU (Open Bitstream Unit) format where the
// bitstream is a sequence of OBUs, each with a type and optional size field.
//
// Enhanced RTMP uses a FourCC-based tag format for AV1 video:
//
//   Byte 0: [IsExHeader:1bit][FrameType:3bits][PacketType:4bits]
//     - IsExHeader = 1 (bit 7, always set for Enhanced RTMP)
//     - FrameType: 1 = keyframe, 2 = inter-frame
//     - PacketType: 0 = SequenceStart (codec config)
//     - PacketType: 3 = CodedFramesX (no composition time offset)
//
//   Bytes 1-4: FourCC identifier ("av01" for AV1)
//
//   Bytes 5+: Either AV1CodecConfigurationRecord (for SequenceStart)
//             or raw AV1 OBU data (for CodedFramesX)
//
// AV1 uses CodedFramesX (PacketType=3) because AV1's frame timing model
// does not require a separate composition time offset field.
//
// AV1 OBU (Open Bitstream Unit) header format:
//
//   Byte 0: [forbidden:1][obu_type:4][obu_extension_flag:1][obu_has_size_field:1][reserved:1]
//
// OBU types relevant for keyframe detection:
//   - 1 = OBU_SEQUENCE_HEADER (always precedes keyframes in AV1)
//   - 3 = OBU_FRAME_HEADER
//   - 6 = OBU_FRAME (contains both header and data)
//
// AV1 frame_type values (in frame header OBU payload):
//   - 0 = KEY_FRAME
//   - 1 = INTER_FRAME
//   - 2 = INTRA_ONLY_FRAME
//   - 3 = SWITCH_FRAME

// av1FourCC is the 4-byte FourCC identifier for AV1 in Enhanced RTMP.
// This is placed in bytes 1-4 of the Enhanced RTMP video tag.
var av1FourCC = [4]byte{'a', 'v', '0', '1'}

// AV1 OBU type constants extracted from the OBU header.
const (
	// obuTypeSequenceHeader is the OBU type for a sequence header.
	// A sequence header OBU always precedes a keyframe in an AV1 bitstream.
	obuTypeSequenceHeader = 1

	// obuTypeFrameHeader is the OBU type for a frame header (without tile data).
	obuTypeFrameHeader = 3

	// obuTypeFrame is the OBU type for a complete frame (header + tile data).
	obuTypeFrame = 6
)

// BuildAV1SequenceHeader builds the Enhanced RTMP video SequenceStart tag
// for AV1. The configRecord is the AV1CodecConfigurationRecord from the
// Matroska CodecPrivate, which contains the sequence header OBU needed
// to initialize the decoder.
//
// Wire format:
//
//	Byte 0:    0x90 = [IsExHeader=1:1][FrameType=1(keyframe):3][PacketType=0(SequenceStart):4]
//	Bytes 1-4: FourCC "av01"
//	Bytes 5+:  AV1CodecConfigurationRecord
//
// The AV1CodecConfigurationRecord (per AV1-ISOBMFF spec) contains:
//   - marker (1 bit, must be 1)
//   - version (7 bits, must be 1)
//   - seq_profile (3 bits)
//   - seq_level_idx_0 (5 bits)
//   - seq_tier_0 (1 bit)
//   - high_bitdepth (1 bit)
//   - twelve_bit (1 bit)
//   - monochrome (1 bit)
//   - chroma_subsampling_x (1 bit)
//   - chroma_subsampling_y (1 bit)
//   - chroma_sample_position (2 bits)
//   - initial_presentation_delay (4 bits)
//   - configOBUs[] (sequence header OBU and optional metadata OBUs)
func BuildAV1SequenceHeader(configRecord []byte) []byte {
	// Total size: 1 byte header + 4 bytes FourCC + config record
	buf := make([]byte, 5+len(configRecord))

	// Byte 0: Enhanced RTMP header
	// [IsExHeader=1][FrameType=001(keyframe)][PacketType=0000(SequenceStart)]
	// = 0b1_001_0000 = 0x90
	buf[0] = 0x90

	// Bytes 1-4: FourCC "av01"
	copy(buf[1:5], av1FourCC[:])

	// Bytes 5+: AV1CodecConfigurationRecord
	if len(configRecord) > 0 {
		copy(buf[5:], configRecord)
	}

	return buf
}

// BuildAV1VideoFrame builds an Enhanced RTMP video CodedFramesX tag for AV1.
// CodedFramesX (PacketType=3) has no composition time offset field.
//
// Wire format:
//
//	Byte 0:    [IsExHeader=1:1][FrameType:3][PacketType=3(CodedFramesX):4]
//	           Keyframe:    0x93 = [1][001][0011]
//	           Inter-frame: 0xA3 = [1][010][0011]
//	Bytes 1-4: FourCC "av01"
//	Bytes 5+:  Raw AV1 OBU data (one or more OBUs comprising the frame)
//
// Parameters:
//   - data: raw AV1 OBU data from the Matroska container
//   - isKeyframe: true if this is a keyframe
func BuildAV1VideoFrame(data []byte, isKeyframe bool) []byte {
	// Total size: 1 byte header + 4 bytes FourCC + raw OBU data
	buf := make([]byte, 5+len(data))

	// Byte 0: Enhanced RTMP header with CodedFramesX packet type
	if isKeyframe {
		// [IsExHeader=1][FrameType=001(keyframe)][PacketType=0011(CodedFramesX)]
		// = 0b1_001_0011 = 0x93
		buf[0] = 0x93
	} else {
		// [IsExHeader=1][FrameType=010(inter)][PacketType=0011(CodedFramesX)]
		// = 0b1_010_0011 = 0xA3
		buf[0] = 0xA3
	}

	// Bytes 1-4: FourCC "av01"
	copy(buf[1:5], av1FourCC[:])

	// Bytes 5+: Raw AV1 OBU data
	copy(buf[5:], data)

	return buf
}

// IsAV1Keyframe checks if an AV1 frame is a keyframe by examining the
// OBU (Open Bitstream Unit) structure.
//
// AV1 OBU header (byte 0 of each OBU):
//
//	[forbidden:1][obu_type:4][extension_flag:1][has_size:1][reserved:1]
//
// Keyframe detection strategy:
//  1. If the first OBU is a SEQUENCE_HEADER (type=1), this is a keyframe.
//     In AV1 bitstreams, a sequence header OBU always appears before a keyframe.
//  2. If the first OBU is a FRAME (type=6) or FRAME_HEADER (type=3),
//     parse the first 2 bits of the OBU payload to get frame_type:
//     - frame_type=0 → KEY_FRAME
//     - frame_type=1 → INTER_FRAME
//     - frame_type=2 → INTRA_ONLY_FRAME
//     - frame_type=3 → SWITCH_FRAME
//
// This function checks the first OBU only. In practice, keyframes in AV1
// always start with a sequence header OBU.
func IsAV1Keyframe(data []byte) bool {
	// Need at least 1 byte for the OBU header
	if len(data) == 0 {
		return false
	}

	// Extract the OBU type from the first byte:
	// obu_type is in bits [6:3] (4 bits), which is (byte >> 3) & 0x0F
	obuType := (data[0] >> 3) & 0x0F

	// Strategy 1: If the first OBU is a sequence header, treat as keyframe.
	// In AV1, a sequence header OBU always precedes a keyframe.
	if obuType == obuTypeSequenceHeader {
		return true
	}

	// Strategy 2: If the first OBU is a FRAME or FRAME_HEADER, parse frame_type.
	if obuType == obuTypeFrame || obuType == obuTypeFrameHeader {
		// Determine OBU header size (1 byte, or 2 if extension flag is set)
		obuHeaderSize := 1
		hasExtension := (data[0] & 0x04) != 0 // bit 2 = obu_extension_flag
		if hasExtension {
			obuHeaderSize = 2
		}

		// Check if has_size field is present (bit 1)
		hasSize := (data[0] & 0x02) != 0

		// Calculate the offset to the OBU payload
		payloadOffset := obuHeaderSize
		if hasSize {
			// Skip the LEB128-encoded size field.
			// LEB128 uses 7 bits per byte with bit 7 as continuation flag.
			for i := payloadOffset; i < len(data); i++ {
				payloadOffset++
				// If bit 7 is 0, this is the last byte of the LEB128 value
				if (data[i] & 0x80) == 0 {
					break
				}
			}
		}

		// Need at least 1 byte of payload to read frame_type
		if payloadOffset >= len(data) {
			return false
		}

		// The first bit of the frame header OBU payload is show_existing_frame.
		// If show_existing_frame=1, the rest is just a frame index — not a keyframe.
		showExisting := (data[payloadOffset] >> 7) & 0x01
		if showExisting == 1 {
			return false
		}

		// frame_type is the next 2 bits after show_existing_frame:
		// bits [6:5] of the payload byte
		frameType := (data[payloadOffset] >> 5) & 0x03

		// frame_type=0 means KEY_FRAME
		return frameType == 0
	}

	return false
}

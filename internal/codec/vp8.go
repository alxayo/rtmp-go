package codec

// This file implements VP8 video conversion for Enhanced RTMP.
//
// VP8 is a video codec developed by Google (used in WebM). Unlike H.264/H.265,
// VP8 is self-describing — each frame contains all the information needed to
// decode it, so no out-of-band decoder configuration record is required.
//
// Enhanced RTMP uses a FourCC-based tag format for VP8 video:
//
//   Byte 0: [IsExHeader:1bit][FrameType:3bits][PacketType:4bits]
//     - IsExHeader = 1 (bit 7, always set for Enhanced RTMP)
//     - FrameType: 1 = keyframe, 2 = inter-frame
//     - PacketType: 0 = SequenceStart (codec config)
//     - PacketType: 3 = CodedFramesX (no composition time offset)
//
//   Bytes 1-4: FourCC identifier ("vp08" for VP8)
//
//   Bytes 5+: Either empty (for SequenceStart) or raw VP8 frame data
//
// VP8 uses CodedFramesX (PacketType=3) instead of CodedFrames (PacketType=1)
// because VP8 does not use B-frames, so DTS always equals PTS and there is
// no need for a composition time offset field.
//
// VP8 keyframe detection:
// The first byte of a VP8 frame contains a 1-bit frame_type flag at bit 0:
//   - 0 = keyframe (intra-frame, can be decoded independently)
//   - 1 = inter-frame (depends on previous frames)
// Note: this is inverted from what you might expect (0 = key, not 1).

// vp8FourCC is the 4-byte FourCC identifier for VP8 in Enhanced RTMP.
// This is placed in bytes 1-4 of the Enhanced RTMP video tag.
var vp8FourCC = [4]byte{'v', 'p', '0', '8'}

// BuildVP8SequenceHeader builds the Enhanced RTMP video SequenceStart tag
// for VP8. VP8 is self-describing (no decoder configuration record needed),
// so the payload after the FourCC is empty.
//
// Wire format (5 bytes total):
//
//	Byte 0:    0x90 = [IsExHeader=1:1][FrameType=1(keyframe):3][PacketType=0(SequenceStart):4]
//	Bytes 1-4: FourCC "vp08"
//
// This must be sent once before any VP8 video frames so that subscribers
// know the codec type. Even though VP8 doesn't need a config record,
// the SequenceStart tag is required by the Enhanced RTMP protocol.
func BuildVP8SequenceHeader() []byte {
	// Total size: 1 byte header + 4 bytes FourCC = 5 bytes
	buf := make([]byte, 5)

	// Byte 0: Enhanced RTMP header
	// [IsExHeader=1][FrameType=001(keyframe)][PacketType=0000(SequenceStart)]
	// = 0b1_001_0000 = 0x90
	buf[0] = 0x90

	// Bytes 1-4: FourCC "vp08"
	copy(buf[1:5], vp8FourCC[:])

	return buf
}

// BuildVP8VideoFrame builds an Enhanced RTMP video CodedFramesX tag for VP8.
// CodedFramesX (PacketType=3) has no composition time offset — VP8 doesn't
// use B-frames, so DTS always equals PTS.
//
// Wire format:
//
//	Byte 0:    [IsExHeader=1:1][FrameType:3][PacketType=3(CodedFramesX):4]
//	           Keyframe:    0x93 = [1][001][0011]
//	           Inter-frame: 0xA3 = [1][010][0011]
//	Bytes 1-4: FourCC "vp08"
//	Bytes 5+:  Raw VP8 frame data
//
// Parameters:
//   - data: raw VP8 frame data from the Matroska container
//   - isKeyframe: true if this is a keyframe (intra-frame)
func BuildVP8VideoFrame(data []byte, isKeyframe bool) []byte {
	// Total size: 1 byte header + 4 bytes FourCC + raw frame data
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

	// Bytes 1-4: FourCC "vp08"
	copy(buf[1:5], vp8FourCC[:])

	// Bytes 5+: Raw VP8 frame data
	copy(buf[5:], data)

	return buf
}

// IsVP8Keyframe checks if a VP8 frame is a keyframe by examining the
// frame header. In VP8, the keyframe flag is bit 0 of the first byte:
//   - 0 = keyframe (intra-frame, can be decoded independently)
//   - 1 = inter-frame (predicted from previous frames)
//
// Note: this is inverted from what you might expect — a 0 bit means keyframe.
//
// VP8 frame header layout (first 3 bytes of a keyframe):
//
//	Byte 0: [size0:5][show_frame:1][version:2][frame_type:1(bit 0)]
//	        frame_type = 0 → keyframe, frame_type = 1 → inter-frame
//
// For inter-frames, only the first byte matters for keyframe detection.
// For keyframes, bytes 1-2 are part of the first_part_size field,
// and bytes 3-5 contain the start code 0x9D 0x01 0x2A.
func IsVP8Keyframe(data []byte) bool {
	// Need at least 1 byte to check the frame type
	if len(data) == 0 {
		return false
	}

	// Bit 0 of byte 0: frame_type (0 = keyframe, 1 = inter-frame)
	return (data[0] & 0x01) == 0
}

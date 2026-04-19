package codec

// This file implements VP9 video conversion for Enhanced RTMP.
//
// VP9 is a video codec developed by Google (successor to VP8, used in WebM).
// VP9 supports an optional VPCodecConfigurationRecord for out-of-band
// signaling, but is also self-describing for basic streams.
//
// Enhanced RTMP uses a FourCC-based tag format for VP9 video:
//
//   Byte 0: [IsExHeader:1bit][FrameType:3bits][PacketType:4bits]
//     - IsExHeader = 1 (bit 7, always set for Enhanced RTMP)
//     - FrameType: 1 = keyframe, 2 = inter-frame
//     - PacketType: 0 = SequenceStart (codec config)
//     - PacketType: 3 = CodedFramesX (no composition time offset)
//
//   Bytes 1-4: FourCC identifier ("vp09" for VP9)
//
//   Bytes 5+: Either VPCodecConfigurationRecord (for SequenceStart)
//             or raw VP9 frame data (for CodedFramesX)
//
// VP9 uses CodedFramesX (PacketType=3) instead of CodedFrames (PacketType=1)
// because VP9 does not use B-frames in most configurations, so DTS always
// equals PTS and there is no need for a composition time offset field.
//
// VP9 keyframe detection:
// The VP9 uncompressed header starts with a 2-bit frame_marker (always 0b10),
// followed by profile bits, then show_existing_frame, and frame_type.
// frame_type=0 means KEY_FRAME. The exact bit layout depends on the profile
// and version, but for Profile 0 (the most common), a simplified check works:
// bit 2 of the first byte indicates the frame type when show_existing_frame=0.

// vp9FourCC is the 4-byte FourCC identifier for VP9 in Enhanced RTMP.
// This is placed in bytes 1-4 of the Enhanced RTMP video tag.
var vp9FourCC = [4]byte{'v', 'p', '0', '9'}

// BuildVP9SequenceHeader builds the Enhanced RTMP video SequenceStart tag
// for VP9. If codecPrivate is non-nil, it contains the VPCodecConfigurationRecord
// from the Matroska container. If nil, the config is empty (VP9 is self-describing
// for basic streams).
//
// Wire format:
//
//	Byte 0:    0x90 = [IsExHeader=1:1][FrameType=1(keyframe):3][PacketType=0(SequenceStart):4]
//	Bytes 1-4: FourCC "vp09"
//	Bytes 5+:  VPCodecConfigurationRecord (if present, otherwise empty)
//
// The VPCodecConfigurationRecord (when present) contains:
//   - Profile (uint8)
//   - Level (uint8)
//   - Bit depth (4 bits)
//   - Chroma subsampling (3 bits)
//   - Video full range flag (1 bit)
//   - Colour primaries (uint8)
//   - Transfer characteristics (uint8)
//   - Matrix coefficients (uint8)
//   - Codec initialization data length (uint16)
//   - Codec initialization data (variable)
func BuildVP9SequenceHeader(codecPrivate []byte) []byte {
	// Total size: 1 byte header + 4 bytes FourCC + optional config data
	buf := make([]byte, 5+len(codecPrivate))

	// Byte 0: Enhanced RTMP header
	// [IsExHeader=1][FrameType=001(keyframe)][PacketType=0000(SequenceStart)]
	// = 0b1_001_0000 = 0x90
	buf[0] = 0x90

	// Bytes 1-4: FourCC "vp09"
	copy(buf[1:5], vp9FourCC[:])

	// Bytes 5+: VPCodecConfigurationRecord (if provided)
	if len(codecPrivate) > 0 {
		copy(buf[5:], codecPrivate)
	}

	return buf
}

// BuildVP9VideoFrame builds an Enhanced RTMP video CodedFramesX tag for VP9.
// CodedFramesX (PacketType=3) has no composition time offset — VP9 typically
// doesn't use B-frames, so DTS always equals PTS.
//
// Wire format:
//
//	Byte 0:    [IsExHeader=1:1][FrameType:3][PacketType=3(CodedFramesX):4]
//	           Keyframe:    0x93 = [1][001][0011]
//	           Inter-frame: 0xA3 = [1][010][0011]
//	Bytes 1-4: FourCC "vp09"
//	Bytes 5+:  Raw VP9 frame data
//
// Parameters:
//   - data: raw VP9 frame data from the Matroska container
//   - isKeyframe: true if this is a keyframe (intra-frame)
func BuildVP9VideoFrame(data []byte, isKeyframe bool) []byte {
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

	// Bytes 1-4: FourCC "vp09"
	copy(buf[1:5], vp9FourCC[:])

	// Bytes 5+: Raw VP9 frame data
	copy(buf[5:], data)

	return buf
}

// IsVP9Keyframe checks if a VP9 frame is a keyframe by examining the
// uncompressed frame header.
//
// VP9 uncompressed header layout (first byte):
//
//	Bits [7:6]: frame_marker (must be 0b10 = decimal 2)
//	Bit  [5]:   profile_low_bit (Profile 0/2 → 0, Profile 1/3 → 1)
//	Bit  [4]:   For Profile 0/1: reserved_zero (0)
//	            For Profile 2/3: profile_high_bit (1)
//	Bit  [3]:   show_existing_frame flag
//	            If 1, the rest of the header is just a frame index to show
//	Bit  [2]:   frame_type (0 = KEY_FRAME, 1 = NON_KEY_FRAME)
//	            Only meaningful when show_existing_frame = 0
//
// For Profile 0 (the most common profile), the simplified check is:
//
//	frame_marker == 0b10 (bits 7:6)
//	show_existing_frame == 0 (bit 3)
//	frame_type == 0 means keyframe (bit 2)
//
// This simplified approach works correctly for Profile 0 and Profile 1 streams,
// which cover the vast majority of VP9 content.
func IsVP9Keyframe(data []byte) bool {
	// Need at least 1 byte to check the frame header
	if len(data) == 0 {
		return false
	}

	// Verify the 2-bit frame_marker is 0b10 (bits 7:6 of byte 0)
	// This must equal 2 (binary 10) for a valid VP9 frame.
	frameMarker := (data[0] >> 6) & 0x03
	if frameMarker != 0x02 {
		return false
	}

	// Check show_existing_frame flag (bit 3).
	// If set, this is just a reference to a previously decoded frame,
	// not a real coded frame — definitely not a keyframe.
	if (data[0] & 0x08) != 0 {
		return false
	}

	// Check frame_type (bit 2): 0 = KEY_FRAME, 1 = NON_KEY_FRAME
	return (data[0] & 0x04) == 0
}

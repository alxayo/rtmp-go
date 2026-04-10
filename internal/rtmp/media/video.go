package media

import (
	"encoding/binary"
	"fmt"
)

// Video codec identifiers. Legacy codecs use the 4-bit CodecID in the FLV spec.
// Enhanced RTMP codecs use 4-byte FourCC identifiers (see E-RTMP v2 spec).
const (
	VideoCodecAVC  = "H264" // H.264 / Advanced Video Coding (legacy CodecID 7, FourCC "avc1")
	VideoCodecHEVC = "H265" // H.265 / High Efficiency Video Coding (legacy CodecID 12, FourCC "hvc1")
	VideoCodecAV1  = "AV1"  // AV1 (FourCC "av01")
	VideoCodecVP9  = "VP9"  // VP9 (FourCC "vp09")
	VideoCodecVVC  = "VVC"  // H.266 / Versatile Video Coding (FourCC "vvc1")
)

// Frame type identifiers (high nibble of the first video payload byte for legacy;
// bits [6:4] for enhanced RTMP).
const (
	VideoFrameTypeKey   = "keyframe" // Complete frame that can be decoded independently (I-frame)
	VideoFrameTypeInter = "inter"    // Requires previous frames to decode (P/B-frame)
)

// Packet type constants shared by both legacy AVC and Enhanced RTMP.
// Legacy AVC uses only sequence_header (0) and nalu (1).
// Enhanced RTMP uses the full VideoPacketType enumeration (0–7).
const (
	AVCPacketTypeSequenceHeader = "sequence_header" // Contains SPS/PPS (decoder initialization data)
	AVCPacketTypeNALU           = "nalu"            // Network Abstraction Layer Unit (actual video data)

	// Enhanced RTMP VideoPacketType values (E-RTMP v2 spec)
	PacketTypeSequenceStart = "sequence_start" // Codec configuration record (SPS/PPS/VPS)
	PacketTypeCodedFrames   = "coded_frames"   // NALUs with 3-byte composition time offset
	PacketTypeSequenceEnd   = "sequence_end"   // End of stream signal
	PacketTypeCodedFramesX  = "coded_frames_x" // NALUs without composition time (DTS==PTS)
	PacketTypeMetadata      = "metadata"        // AMF-encoded metadata (e.g., colorInfo for HDR)
)

// Enhanced RTMP VideoPacketType numeric values on the wire.
const (
	videoPacketTypeSequenceStart uint8 = 0
	videoPacketTypeCodedFrames   uint8 = 1
	videoPacketTypeSequenceEnd   uint8 = 2
	videoPacketTypeCodedFramesX  uint8 = 3
	videoPacketTypeMetadata      uint8 = 4
)

// videoFourCCMap maps well-known video FourCC values (as big-endian uint32)
// to their canonical codec constant. See fourCC() in codec.go.
var videoFourCCMap = map[uint32]string{
	fourCC("avc1"): VideoCodecAVC,
	fourCC("hvc1"): VideoCodecHEVC,
	fourCC("av01"): VideoCodecAV1,
	fourCC("vp09"): VideoCodecVP9,
	fourCC("vvc1"): VideoCodecVVC,
}

// VideoMessage is a lightweight parsed representation of an RTMP video (message type 9) tag.
// It supports both legacy FLV tags (4-bit CodecID) and Enhanced RTMP tags (IsExHeader + FourCC).
//
// Legacy tag layout:
//
//	[VideoHeader(1B)][AVCPacketType?][CompositionTime?][Data...]
//
// Enhanced RTMP tag layout (IsExHeader=1):
//
//	[ExVideoHeader(1B)][FourCC(4B)][CompositionTime?][Data...]
//
// Error conditions are conservative so upstream logic can decide how to handle unsupported codecs.
type VideoMessage struct {
	Codec      string // One of VideoCodec* constants
	FrameType  string // keyframe / inter (others -> raw numeric string)
	PacketType string // sequence_header / nalu / sequence_start / coded_frames / etc.
	Payload    []byte // Raw payload after parsed header bytes
	Enhanced   bool   // True if parsed via Enhanced RTMP (IsExHeader) path
	FourCC     string // Raw FourCC string (e.g. "hvc1"), empty for legacy
}

// ParseVideoMessage parses the raw payload of an RTMP video message (type 9).
// It supports both legacy codecs (AVC CodecID=7, HEVC CodecID=12) and Enhanced
// RTMP codecs (HEVC/AV1/VP9/AVC/VVC via IsExHeader + FourCC).
func ParseVideoMessage(data []byte) (*VideoMessage, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("video.parse: empty payload")
	}

	b0 := data[0]
	isExHeader := (b0 >> 7) & 1

	if isExHeader == 1 {
		return parseEnhancedVideo(data)
	}
	return parseLegacyVideo(data)
}

// parseEnhancedVideo handles the Enhanced RTMP (E-RTMP) video tag format.
//
// Wire format (first byte):
//
//	bit [7]:   IsExHeader = 1
//	bits [6:4]: FrameType (3 bits: 1=keyframe, 2=inter)
//	bits [3:0]: VideoPacketType (4 bits)
//
// Followed by 4-byte FourCC (big-endian), then codec-specific payload.
func parseEnhancedVideo(data []byte) (*VideoMessage, error) {
	// Minimum: 1 byte header + 4 bytes FourCC = 5 bytes
	if len(data) < 5 {
		return nil, fmt.Errorf("video.parse: enhanced packet truncated (need 5 bytes, got %d)", len(data))
	}

	b0 := data[0]
	frameTypeID := (b0 >> 4) & 0x07 // 3-bit FrameType
	pktType := b0 & 0x0F            // 4-bit VideoPacketType

	fourCCVal := binary.BigEndian.Uint32(data[1:5])
	fourCCStr := string(data[1:5])

	vm := &VideoMessage{
		Enhanced: true,
		FourCC:   fourCCStr,
	}

	// Map frame type.
	switch frameTypeID {
	case 1:
		vm.FrameType = VideoFrameTypeKey
	case 2:
		vm.FrameType = VideoFrameTypeInter
	case 5:
		// VideoInfoCmd — not a real frame; used for metadata delivery.
		vm.FrameType = "command"
	default:
		vm.FrameType = fmt.Sprintf("unknown_%d", frameTypeID)
	}

	// Map FourCC to codec.
	codec, ok := videoFourCCMap[fourCCVal]
	if !ok {
		return nil, fmt.Errorf("video.parse: unsupported enhanced fourcc %q (0x%08x)", fourCCStr, fourCCVal)
	}
	vm.Codec = codec

	// Map VideoPacketType.
	switch pktType {
	case videoPacketTypeSequenceStart:
		vm.PacketType = PacketTypeSequenceStart
		vm.Payload = data[5:]
	case videoPacketTypeCodedFrames:
		vm.PacketType = PacketTypeCodedFrames
		// CodedFrames includes a 3-byte SI24 composition time offset after FourCC.
		if len(data) < 8 {
			vm.Payload = data[5:]
		} else {
			vm.Payload = data[8:] // skip 3-byte composition time
		}
	case videoPacketTypeSequenceEnd:
		vm.PacketType = PacketTypeSequenceEnd
		vm.Payload = data[5:]
	case videoPacketTypeCodedFramesX:
		vm.PacketType = PacketTypeCodedFramesX
		// CodedFramesX has no composition time (DTS==PTS optimization).
		vm.Payload = data[5:]
	case videoPacketTypeMetadata:
		vm.PacketType = PacketTypeMetadata
		vm.Payload = data[5:]
	default:
		vm.PacketType = fmt.Sprintf("enhanced_%d", pktType)
		vm.Payload = data[5:]
	}

	return vm, nil
}

// parseLegacyVideo handles the traditional FLV video tag format (4-bit FrameType + 4-bit CodecID).
func parseLegacyVideo(data []byte) (*VideoMessage, error) {
	b0 := data[0]
	frameTypeID := (b0 >> 4) & 0x0F
	codecID := b0 & 0x0F

	vm := &VideoMessage{}

	// Map frame type.
	switch frameTypeID {
	case 1:
		vm.FrameType = VideoFrameTypeKey
	case 2:
		vm.FrameType = VideoFrameTypeInter
	default:
		vm.FrameType = fmt.Sprintf("unknown_%d", frameTypeID)
	}

	// Codec handling.
	switch codecID {
	case 7: // AVC
		vm.Codec = VideoCodecAVC
		if len(data) < 2 {
			return nil, fmt.Errorf("video.parse: avc packet truncated (need avc packet type)")
		}
		pt := data[1]
		if pt == 0x00 {
			vm.PacketType = AVCPacketTypeSequenceHeader
		} else if pt == 0x01 {
			vm.PacketType = AVCPacketTypeNALU
		} else {
			vm.PacketType = fmt.Sprintf("unknown_%d", pt)
		}
		vm.Payload = data[2:]
	case 12: // HEVC (non-standard legacy extension)
		vm.Codec = VideoCodecHEVC
		if len(data) >= 2 {
			pt := data[1]
			if pt == 0x00 {
				vm.PacketType = AVCPacketTypeSequenceHeader
			} else if pt == 0x01 {
				vm.PacketType = AVCPacketTypeNALU
			} else {
				vm.PacketType = fmt.Sprintf("unknown_%d", pt)
			}
			vm.Payload = data[2:]
		} else {
			vm.Payload = data[1:]
		}
	default:
		return nil, fmt.Errorf("video.parse: unsupported codec id=%d", codecID)
	}

	return vm, nil
}

// IsVideoSequenceHeader checks whether raw video tag data represents a sequence header
// (codec configuration record). This works for both legacy and Enhanced RTMP formats.
// Used by the stream registry to cache sequence headers for late-joining subscribers.
func IsVideoSequenceHeader(data []byte) bool {
	if len(data) < 2 {
		return false
	}

	b0 := data[0]
	isExHeader := (b0 >> 7) & 1

	if isExHeader == 1 {
		// Enhanced RTMP: PacketType is in bits [3:0]
		pktType := b0 & 0x0F
		return pktType == videoPacketTypeSequenceStart
	}

	// Legacy: CodecID-specific checks
	codecID := b0 & 0x0F
	switch codecID {
	case 7, 12: // AVC or legacy HEVC
		return data[1] == 0x00 // AVCPacketType 0 = sequence header
	}
	return false
}

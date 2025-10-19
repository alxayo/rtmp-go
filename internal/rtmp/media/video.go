package media

import "fmt"

// Video codec string identifiers used for simple detection.
const (
	VideoCodecAVC  = "H264"
	VideoCodecHEVC = "H265"
)

// Frame type identifiers (returned as strings for readability / logging).
const (
	VideoFrameTypeKey   = "keyframe"
	VideoFrameTypeInter = "inter"
)

// AVC (H.264) packet types.
const (
	AVCPacketTypeSequenceHeader = "sequence_header"
	AVCPacketTypeNALU           = "nalu"
)

// VideoMessage is a lightweight parsed representation of an RTMP video (message type 9) tag.
// Only minimal metadata for codec / frame classification is extracted; the payload bytes
// (excluding the FLV header + avc packet type if present) are left untouched for transparent relay.
//
// Tag layout (FLV spec / RTMP encapsulated video tag data):
//
//	[VideoHeader][AVCPacketType?][CompositionTime?][Data...]
//
// For our limited purposes we only look at:
//   - VideoHeader first byte: frameType (bits 7-4), codecID (bits 3-0)
//   - If codecID == 7 (AVC): second byte AVCPacketType (0=Sequence Header, 1=NALU)
//
// We intentionally do not parse composition time / NALUs.
//
// Error conditions are conservative so upstream logic can decide how to handle unsupported codecs.
type VideoMessage struct {
	Codec      string // One of VideoCodec* constants
	FrameType  string // keyframe / inter (others -> raw numeric string)
	PacketType string // AVC only: sequence_header / nalu (empty for non-AVC)
	Payload    []byte // Raw payload after header (+ avc packet type if applicable)
}

// ParseVideoMessage parses the raw payload of an RTMP video message (type 9).
// It supports AVC (H.264) and HEVC (H.265) basic detection; other codecs return an error.
// We only differentiate keyframe (1) and inter (2) frame types; other frame types are kept
// as numeric string values for potential logging without rejecting the packet.
func ParseVideoMessage(data []byte) (*VideoMessage, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("video.parse: empty payload")
	}
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
		// Payload excludes first two bytes (header + avc packet type). We ignore composition time (next 3 bytes) intentionally.
		vm.Payload = data[2:]
	case 12: // HEVC (H.265)
		vm.Codec = VideoCodecHEVC
		vm.Payload = data[1:]
	default:
		return nil, fmt.Errorf("video.parse: unsupported codec id=%d", codecID)
	}

	return vm, nil
}

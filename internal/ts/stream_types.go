package ts

// This file defines constants for the "stream type" field found in MPEG-TS
// Program Map Tables (PMT). When a PMT describes an elementary stream, it
// includes a one-byte stream type code that tells the receiver which codec
// is used. These constants come from the ISO/IEC 13818-1 and 14496 standards.

// Stream type constants used in PMT to identify the codec of each elementary stream.
// For example, a PMT entry with StreamType=0x1B means that PID carries H.264 video.
const (
	// StreamTypeMPEG2Video identifies an MPEG-2 video elementary stream.
	StreamTypeMPEG2Video uint8 = 0x02

	// StreamTypeMPEG1Audio identifies an MPEG-1 audio elementary stream (MP1/MP2/MP3).
	StreamTypeMPEG1Audio uint8 = 0x03

	// StreamTypeMPEG2Audio identifies an MPEG-2 audio elementary stream.
	StreamTypeMPEG2Audio uint8 = 0x04

	// StreamTypeAAC_ADTS identifies an AAC audio stream with ADTS framing
	// as defined in ISO/IEC 13818-7. ADTS (Audio Data Transport Stream) adds
	// a small header before each AAC frame for sync and metadata.
	StreamTypeAAC_ADTS uint8 = 0x0F

	// StreamTypeAAC_LATM identifies an AAC audio stream with LATM transport
	// as defined in ISO/IEC 14496-3. LATM (Low-overhead Audio Transport
	// Multiplex) is another framing format for AAC, sometimes used in
	// broadcast environments.
	StreamTypeAAC_LATM uint8 = 0x11

	// StreamTypeH264 identifies an H.264/AVC video elementary stream.
	// This is the most common video codec used in live streaming today.
	StreamTypeH264 uint8 = 0x1B

	// StreamTypeH265 identifies an H.265/HEVC video elementary stream.
	// HEVC is the successor to H.264, offering better compression at the
	// cost of higher encoding complexity.
	StreamTypeH265 uint8 = 0x24

	// StreamTypeAC3 identifies an AC-3 (Dolby Digital) audio stream.
	// This uses the ATSC registration descriptor value (0x81), which is the
	// most common way AC-3 is signaled in MPEG-TS streams.
	StreamTypeAC3 uint8 = 0x81

	// StreamTypeEAC3 identifies an E-AC-3 (Dolby Digital Plus) audio stream.
	// This uses the ATSC registration descriptor value (0x87).
	StreamTypeEAC3 uint8 = 0x87
)

// StreamTypeName returns a human-readable name for a stream type constant.
// This is useful for logging and debugging — instead of printing "0x1B",
// you get "H.264" which is much more meaningful.
func StreamTypeName(st uint8) string {
	switch st {
	case StreamTypeMPEG2Video:
		return "MPEG-2 Video"
	case StreamTypeMPEG1Audio:
		return "MPEG-1 Audio"
	case StreamTypeMPEG2Audio:
		return "MPEG-2 Audio"
	case StreamTypeAAC_ADTS:
		return "AAC (ADTS)"
	case StreamTypeAAC_LATM:
		return "AAC (LATM)"
	case StreamTypeH264:
		return "H.264"
	case StreamTypeH265:
		return "H.265"
	case StreamTypeAC3:
		return "AC-3"
	case StreamTypeEAC3:
		return "E-AC-3"
	default:
		return "Unknown"
	}
}

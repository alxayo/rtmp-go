package codec

// This file implements Opus audio conversion for Enhanced RTMP.
//
// Opus is a versatile audio codec designed for interactive speech and music
// over the internet. It is widely used in WebRTC, WebM, and other modern
// media containers.
//
// In Matroska/WebM containers, the Opus codec private data is the "OpusHead"
// structure (typically 19 bytes), which contains the decoder initialization
// parameters. This OpusHead is sent as the codec config in the Enhanced RTMP
// SequenceStart tag.
//
// Enhanced RTMP uses a FourCC-based tag format for Opus audio:
//
//   Byte 0: [SoundFormat:4bits=9][AudioPacketType:4bits]
//     - SoundFormat 9 means "Enhanced RTMP, use FourCC"
//     - AudioPacketType 0 = SequenceStart (codec config)
//     - AudioPacketType 1 = CodedFrames (compressed audio)
//
//   Bytes 1-4: FourCC identifier ("Opus" for Opus)
//
//   Bytes 5+: Either OpusHead (for SequenceStart) or raw Opus packet data
//
// OpusHead structure (typically 19 bytes):
//
//   Bytes 0-7:   "OpusHead" magic string (8 bytes)
//   Byte 8:      Version (must be 1)
//   Byte 9:      Channel count (1-255)
//   Bytes 10-11: Pre-skip (little-endian uint16, number of samples to skip)
//   Bytes 12-15: Input sample rate (little-endian uint32, informational only)
//   Bytes 16-17: Output gain (little-endian int16, in 1/256 dB units)
//   Byte 18:     Channel mapping family (0 = mono/stereo, 1 = surround, 255 = discrete)

// opusFourCC is the 4-byte FourCC identifier for Opus in Enhanced RTMP.
// This is placed in bytes 1-4 of the Enhanced RTMP audio tag.
// Note: the FourCC is "Opus" with capital 'O' — this is case-sensitive.
var opusFourCC = [4]byte{'O', 'p', 'u', 's'}

// BuildOpusSequenceHeader builds the Enhanced RTMP audio SequenceStart tag
// for Opus. The opusHead parameter is the OpusHead structure from the
// Matroska CodecPrivate, typically 19 bytes.
//
// Wire format:
//
//	Byte 0:    0x90 = [SoundFormat=9:4bits][AudioPacketType=0(SequenceStart):4bits]
//	Bytes 1-4: FourCC "Opus"
//	Bytes 5+:  OpusHead structure (decoder initialization data)
//
// This must be sent once before any Opus audio frames so that subscribers'
// decoders can properly initialize with the correct channel count, sample
// rate, and pre-skip value.
func BuildOpusSequenceHeader(opusHead []byte) []byte {
	// Total size: 1 byte header + 4 bytes FourCC + OpusHead data
	buf := make([]byte, 5+len(opusHead))

	// Byte 0: Enhanced RTMP header
	// SoundFormat=9 (upper nibble) | AudioPacketType=0 SequenceStart (lower nibble)
	// = 0b1001_0000 = 0x90
	buf[0] = 0x90

	// Bytes 1-4: FourCC "Opus"
	copy(buf[1:5], opusFourCC[:])

	// Bytes 5+: OpusHead structure
	if len(opusHead) > 0 {
		copy(buf[5:], opusHead)
	}

	return buf
}

// BuildOpusAudioFrame builds an Enhanced RTMP audio CodedFrames tag for Opus.
// Each Opus packet is self-contained and can be independently decoded
// (given the OpusHead initialization data from the SequenceStart).
//
// Wire format:
//
//	Byte 0:    0x91 = [SoundFormat=9:4bits][AudioPacketType=1(CodedFrames):4bits]
//	Bytes 1-4: FourCC "Opus"
//	Bytes 5+:  Raw Opus packet data
//
// Parameters:
//   - data: raw Opus packet data from the Matroska container
func BuildOpusAudioFrame(data []byte) []byte {
	// Total size: 1 byte header + 4 bytes FourCC + raw audio data
	buf := make([]byte, 5+len(data))

	// Byte 0: Enhanced RTMP header
	// SoundFormat=9 (upper nibble) | AudioPacketType=1 CodedFrames (lower nibble)
	// = 0b1001_0001 = 0x91
	buf[0] = 0x91

	// Bytes 1-4: FourCC "Opus"
	copy(buf[1:5], opusFourCC[:])

	// Bytes 5+: Raw Opus packet data
	copy(buf[5:], data)

	return buf
}

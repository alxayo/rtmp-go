package codec

// This file implements FLAC audio conversion for Enhanced RTMP.
//
// FLAC (Free Lossless Audio Codec) is a lossless audio compression format.
// Unlike lossy codecs (Opus, AAC), FLAC preserves the original audio
// data perfectly, making it suitable for archival and high-fidelity use cases.
//
// In Matroska containers, the FLAC CodecPrivate contains:
//   - "fLaC" marker (4 bytes) — the FLAC stream marker
//   - METADATA_BLOCK_HEADER (4 bytes) — header for the STREAMINFO block
//   - STREAMINFO (34 bytes) — the mandatory first metadata block
//
// STREAMINFO (34 bytes) contains:
//   - Minimum block size (16 bits)
//   - Maximum block size (16 bits)
//   - Minimum frame size (24 bits)
//   - Maximum frame size (24 bits)
//   - Sample rate (20 bits)
//   - Number of channels minus 1 (3 bits)
//   - Bits per sample minus 1 (5 bits)
//   - Total samples in stream (36 bits)
//   - MD5 signature of unencoded audio data (128 bits)
//
// Enhanced RTMP uses a FourCC-based tag format for FLAC audio:
//
//   Byte 0: [SoundFormat:4bits=9][AudioPacketType:4bits]
//     - SoundFormat 9 means "Enhanced RTMP, use FourCC"
//     - AudioPacketType 0 = SequenceStart (codec config)
//     - AudioPacketType 1 = CodedFrames (compressed audio)
//
//   Bytes 1-4: FourCC identifier ("fLaC" for FLAC)
//
//   Bytes 5+: Either FLAC codec config (for SequenceStart) or raw FLAC frame data
//
// IMPORTANT: The FLAC FourCC is case-sensitive: "fLaC" (not "flac" or "FLAC").
// This matches the FLAC stream marker and is required by the Enhanced RTMP spec.

// flacFourCC is the 4-byte FourCC identifier for FLAC in Enhanced RTMP.
// This is placed in bytes 1-4 of the Enhanced RTMP audio tag.
// Note: case-sensitive — must be "fLaC" (lowercase f, uppercase L, lowercase a, uppercase C).
var flacFourCC = [4]byte{'f', 'L', 'a', 'C'}

// BuildFLACSequenceHeader builds the Enhanced RTMP audio SequenceStart tag
// for FLAC. The codecPrivate parameter is the full FLAC codec private data
// from the Matroska container, which typically contains the "fLaC" marker,
// the METADATA_BLOCK_HEADER, and the STREAMINFO block (total ~42 bytes).
//
// Wire format:
//
//	Byte 0:    0x90 = [SoundFormat=9:4bits][AudioPacketType=0(SequenceStart):4bits]
//	Bytes 1-4: FourCC "fLaC"
//	Bytes 5+:  FLAC codec private data (fLaC marker + METADATA_BLOCK_HEADER + STREAMINFO)
//
// This must be sent once before any FLAC audio frames so that subscribers'
// decoders can properly initialize with the stream parameters from STREAMINFO.
func BuildFLACSequenceHeader(codecPrivate []byte) []byte {
	// Total size: 1 byte header + 4 bytes FourCC + codec private data
	buf := make([]byte, 5+len(codecPrivate))

	// Byte 0: Enhanced RTMP header
	// SoundFormat=9 (upper nibble) | AudioPacketType=0 SequenceStart (lower nibble)
	// = 0b1001_0000 = 0x90
	buf[0] = 0x90

	// Bytes 1-4: FourCC "fLaC" (case-sensitive!)
	copy(buf[1:5], flacFourCC[:])

	// Bytes 5+: FLAC codec private data
	if len(codecPrivate) > 0 {
		copy(buf[5:], codecPrivate)
	}

	return buf
}

// BuildFLACAudioFrame builds an Enhanced RTMP audio CodedFrames tag for FLAC.
// Each FLAC frame is a complete, independently decodable audio block
// (given the STREAMINFO from the SequenceStart).
//
// Wire format:
//
//	Byte 0:    0x91 = [SoundFormat=9:4bits][AudioPacketType=1(CodedFrames):4bits]
//	Bytes 1-4: FourCC "fLaC"
//	Bytes 5+:  Raw FLAC frame data
//
// FLAC frames begin with a sync code (0xFFF8 or 0xFFF9) and contain:
//   - Frame header (variable size, contains blocking strategy, sample rate, etc.)
//   - Subframes (one per channel, containing the actual audio samples)
//   - Frame footer (CRC-16 for error detection)
//
// Parameters:
//   - data: raw FLAC frame data from the Matroska container
func BuildFLACAudioFrame(data []byte) []byte {
	// Total size: 1 byte header + 4 bytes FourCC + raw audio data
	buf := make([]byte, 5+len(data))

	// Byte 0: Enhanced RTMP header
	// SoundFormat=9 (upper nibble) | AudioPacketType=1 CodedFrames (lower nibble)
	// = 0b1001_0001 = 0x91
	buf[0] = 0x91

	// Bytes 1-4: FourCC "fLaC" (case-sensitive!)
	copy(buf[1:5], flacFourCC[:])

	// Bytes 5+: Raw FLAC frame data
	copy(buf[5:], data)

	return buf
}

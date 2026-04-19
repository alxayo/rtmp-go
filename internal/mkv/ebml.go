package mkv

// This file implements the low-level EBML (Extensible Binary Meta Language)
// binary parser. EBML is the underlying binary format that Matroska is built
// on — think of it like a binary XML where each element has a variable-length
// integer ID, a variable-length integer size, and then the element's data.
//
// The core concept here is the VINT (Variable-length Integer). Both element
// IDs and element sizes are encoded as VINTs. The width of a VINT is
// determined by counting the leading zero bits in the first byte:
//
//     1xxxxxxx  →  1 byte,  7 value bits
//     01xxxxxx  →  2 bytes, 14 value bits
//     001xxxxx  →  3 bytes, 21 value bits
//     0001xxxx  →  4 bytes, 28 value bits
//     ...and so on up to 8 bytes
//
// The critical difference between ID and size parsing:
//   - For IDs: the marker bit is KEPT as part of the value
//   - For sizes: the marker bit is MASKED OUT to get the pure numeric value
//
// For example, the byte 0xA3 (10100011) as an ID = 0xA3 (SimpleBlock),
// but as a size = 0x23 (35 bytes) because the leading '1' is masked out.

import (
	"encoding/binary"
	"errors"
	"math"
	"strings"
)

// UnknownSize is the sentinel value returned when an EBML element has
// indeterminate length. This happens when all the value bits in the size
// VINT are set to 1. Streaming elements like Segment and Cluster commonly
// use unknown size because their length isn't known in advance.
const UnknownSize int64 = -1

// ErrBufferTooShort is returned when there aren't enough bytes in the
// input buffer to complete the parse operation.
var ErrBufferTooShort = errors.New("mkv: buffer too short")

// ReadVINT reads an EBML variable-length integer from data, preserving
// the width marker bit. This is the correct function for reading element IDs,
// where the marker bit is part of the ID value.
//
// For example:
//   - [0xA3]             → value=0xA3,       width=1 (SimpleBlock ID)
//   - [0x42, 0x86]       → value=0x4286,     width=2 (EBMLVersion ID)
//   - [0x1A,0x45,0xDF,0xA3] → value=0x1A45DFA3, width=4 (EBML Header ID)
//
// Returns the raw VINT value, the number of bytes consumed, and any error.
func ReadVINT(data []byte) (value uint64, width int, err error) {
	if len(data) == 0 {
		return 0, 0, ErrBufferTooShort
	}

	// Determine the width by counting leading zero bits in the first byte.
	// The width marker is the first '1' bit from the left. For example:
	//   0x81 = 10000001 → zero leading zeros → width = 1
	//   0x42 = 01000010 → one leading zero   → width = 2
	//   0x2A = 00101010 → two leading zeros  → width = 3
	//   0x1A = 00011010 → three leading zeros → width = 4
	first := data[0]
	width = leadingZeros(first) + 1

	// Make sure we have enough bytes in the buffer.
	if len(data) < width {
		return 0, 0, ErrBufferTooShort
	}

	// Assemble the value from all the bytes, keeping the marker bit intact.
	// Start with the first byte, then shift in the remaining bytes.
	value = uint64(first)
	for i := 1; i < width; i++ {
		value = (value << 8) | uint64(data[i])
	}

	return value, width, nil
}

// ReadVINTValue reads an EBML VINT and masks out the width marker bit.
// This is the correct function for reading element sizes, where the marker
// bit is NOT part of the actual numeric value.
//
// For example:
//   - [0x85]       → value=5,           width=1 (size = 5 bytes)
//   - [0x41, 0x00] → value=256,         width=2 (size = 256 bytes)
//   - [0xFF]       → value=UnknownSize, width=1 (all value bits = 1)
//
// If all value bits are set to 1, this returns UnknownSize (-1), which
// signals an element with indeterminate length. This is common for
// streaming Segment and Cluster elements.
func ReadVINTValue(data []byte) (value int64, width int, err error) {
	if len(data) == 0 {
		return 0, 0, ErrBufferTooShort
	}

	first := data[0]
	width = leadingZeros(first) + 1

	if len(data) < width {
		return 0, 0, ErrBufferTooShort
	}

	// The marker bit is the (width)th bit from the left in the first byte.
	// We mask it out to get the pure value bits. For a 1-byte VINT, the
	// marker is bit 7 (0x80). For a 2-byte VINT, it's bit 6 (0x40), etc.
	mask := byte(0x80 >> (width - 1))
	firstMasked := first & ^mask

	// Assemble the value from the masked first byte and remaining bytes.
	raw := uint64(firstMasked)
	for i := 1; i < width; i++ {
		raw = (raw << 8) | uint64(data[i])
	}

	// Check for the "unknown size" sentinel: all value bits are 1.
	// For a 1-byte VINT, that's 0x7F (binary: 1_1111111 with marker masked = 1111111).
	// For a 2-byte VINT, that's 0x3FFF, etc.
	// The formula: (1 << (7 * width)) - 1 gives us the all-ones value.
	unknownSentinel := (uint64(1) << (7 * uint(width))) - 1
	if raw == unknownSentinel {
		return UnknownSize, width, nil
	}

	return int64(raw), width, nil
}

// ReadElementHeader reads an EBML element header consisting of an element
// ID followed by an element size. Returns the element ID (as a uint32),
// the data size (which may be UnknownSize), the total number of header
// bytes consumed, and any error.
//
// For example, parsing [0xA3, 0x85] returns:
//   - id=0xA3 (SimpleBlock)
//   - size=5
//   - headerLen=2 (1 byte for ID + 1 byte for size)
func ReadElementHeader(data []byte) (id uint32, size int64, headerLen int, err error) {
	// Read the element ID (marker bit preserved).
	idVal, idWidth, err := ReadVINT(data)
	if err != nil {
		return 0, 0, 0, err
	}

	// Read the element size (marker bit masked out).
	sizeVal, sizeWidth, err := ReadVINTValue(data[idWidth:])
	if err != nil {
		return 0, 0, 0, err
	}

	return uint32(idVal), sizeVal, idWidth + sizeWidth, nil
}

// ReadUint reads a big-endian unsigned integer of the given width (1–8 bytes)
// from data. This is used to read fixed-width unsigned integer elements in
// Matroska, such as TrackNumber, TrackType, and TimecodeScale.
//
// The width determines how many bytes to read. For example, width=2 reads
// a 16-bit big-endian value, width=4 reads a 32-bit value, etc.
func ReadUint(data []byte, width int) uint64 {
	var val uint64
	for i := 0; i < width && i < len(data); i++ {
		val = (val << 8) | uint64(data[i])
	}
	return val
}

// ReadString reads a UTF-8 string of the given length from data. Matroska
// string elements (like DocType and CodecID) are stored as raw UTF-8 bytes
// with no null terminator — the length is given by the element's size field.
// Trailing null bytes, if present, are trimmed.
func ReadString(data []byte, length int) string {
	if length > len(data) {
		length = len(data)
	}
	// Trim trailing null bytes — some encoders pad strings with zeros.
	return strings.TrimRight(string(data[:length]), "\x00")
}

// ReadFloat reads an IEEE 754 floating-point number from data. The width
// must be either 4 (float32) or 8 (float64). This is used for elements
// like SamplingFrequency and Duration that store decimal values.
//
// For width=4, the bytes are interpreted as a big-endian IEEE 754 float32.
// For width=8, the bytes are interpreted as a big-endian IEEE 754 float64.
// Any other width returns 0.
func ReadFloat(data []byte, width int) float64 {
	switch width {
	case 4:
		if len(data) < 4 {
			return 0
		}
		bits := binary.BigEndian.Uint32(data[:4])
		return float64(math.Float32frombits(bits))
	case 8:
		if len(data) < 8 {
			return 0
		}
		bits := binary.BigEndian.Uint64(data[:8])
		return math.Float64frombits(bits)
	default:
		return 0
	}
}

// leadingZeros counts the number of leading zero bits in a byte.
// For EBML, this determines the VINT width: width = leadingZeros + 1.
//
// Examples:
//   - 0xFF (11111111) → 0 leading zeros → width 1
//   - 0x40 (01000000) → 1 leading zero  → width 2
//   - 0x20 (00100000) → 2 leading zeros → width 3
//   - 0x10 (00010000) → 3 leading zeros → width 4
//   - 0x00 (00000000) → 8 leading zeros (invalid VINT)
func leadingZeros(b byte) int {
	for i := 0; i < 8; i++ {
		if b&(0x80>>uint(i)) != 0 {
			return i
		}
	}
	return 8
}

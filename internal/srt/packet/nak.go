package packet

// This file implements NAK (Negative Acknowledgment) loss report encoding
// and decoding. When the receiver detects missing packets (gaps in sequence
// numbers), it sends a NAK packet containing a list of lost sequence number
// ranges. The sender then retransmits those packets.
//
// NAK loss report encoding format:
//
// The loss report is a compact binary encoding of sequence number ranges.
// Each entry is either:
//
//  1. A single lost packet: one 32-bit sequence number (bit 31 clear)
//     +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//     |0|               Lost Sequence Number                         |
//     +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//
//  2. A range of lost packets: two 32-bit words
//     +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//     |1|             Start Sequence Number (range start)            |
//     +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//     |0|              End Sequence Number (range end)               |
//     +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//
// The high bit (bit 31) of the first word acts as a flag:
//   - 0 = this is a single lost sequence number
//   - 1 = this is the start of a range, and the next word is the end

import (
	"encoding/binary"
)

// rangeFlagBit is the bitmask for bit 31, used to distinguish single loss
// entries from range entries in the NAK loss report encoding.
const rangeFlagBit = 0x80000000

// EncodeLossRanges converts a list of lost sequence number ranges into
// the compact binary format used in NAK packets.
//
// Each range is a [2]uint32 where:
//   - range[0] = first lost sequence number
//   - range[1] = last lost sequence number
//
// If range[0] == range[1], it's encoded as a single entry (4 bytes).
// If range[0] != range[1], it's encoded as a range (8 bytes: start | 0x80000000, end).
func EncodeLossRanges(ranges [][2]uint32) []byte {
	// Pre-calculate the total buffer size needed.
	// Each range is either 4 bytes (single) or 8 bytes (range).
	totalSize := 0
	for _, r := range ranges {
		if r[0] == r[1] {
			totalSize += 4 // Single lost packet: one 32-bit word
		} else {
			totalSize += 8 // Range: two 32-bit words (start + end)
		}
	}

	buf := make([]byte, totalSize)
	offset := 0

	for _, r := range ranges {
		if r[0] == r[1] {
			// Single lost packet: write sequence number with bit 31 clear.
			binary.BigEndian.PutUint32(buf[offset:offset+4], r[0]&^uint32(rangeFlagBit))
			offset += 4
		} else {
			// Range of lost packets:
			// First word: start sequence number with bit 31 SET (range flag).
			binary.BigEndian.PutUint32(buf[offset:offset+4], r[0]|rangeFlagBit)
			// Second word: end sequence number with bit 31 CLEAR.
			binary.BigEndian.PutUint32(buf[offset+4:offset+8], r[1]&^uint32(rangeFlagBit))
			offset += 8
		}
	}

	return buf
}

// DecodeLossRanges parses the compact binary loss report from a NAK packet
// back into a list of sequence number ranges.
//
// Returns a slice of [2]uint32 ranges where each range[0] is the first
// lost sequence number and range[1] is the last. For single-packet losses,
// range[0] == range[1].
func DecodeLossRanges(buf []byte) [][2]uint32 {
	var ranges [][2]uint32
	offset := 0

	// Process entries until we run out of data. Each entry starts with
	// a 32-bit word. If bit 31 is set, the next word is the range end.
	for offset+4 <= len(buf) {
		word := binary.BigEndian.Uint32(buf[offset : offset+4])

		if (word & rangeFlagBit) != 0 {
			// Bit 31 is set → this is the start of a range.
			// Strip the range flag to get the actual sequence number.
			start := word & ^uint32(rangeFlagBit)

			// The next 4 bytes are the end of the range.
			if offset+8 > len(buf) {
				// Truncated range — treat start as a single entry.
				ranges = append(ranges, [2]uint32{start, start})
				offset += 4
				continue
			}
			end := binary.BigEndian.Uint32(buf[offset+4 : offset+8])
			ranges = append(ranges, [2]uint32{start, end})
			offset += 8
		} else {
			// Bit 31 is clear → this is a single lost packet.
			ranges = append(ranges, [2]uint32{word, word})
			offset += 4
		}
	}

	return ranges
}

// Package packet implements binary encoding and decoding for SRT protocol
// packets. SRT uses UDP as its transport, and every UDP datagram begins with
// a 16-byte header that is common to both data packets and control packets.
//
// Wire layout of the 16-byte SRT packet header:
//
//	 0                   1                   2                   3
//	 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|F|                    (type-specific)                          |  Bytes 0-3
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                    (type-specific)                            |  Bytes 4-7
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                        Timestamp                             |  Bytes 8-11
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                  Destination Socket ID                       |  Bytes 12-15
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//
// The F bit (bit 0 of byte 0) distinguishes data packets (F=0) from
// control packets (F=1). Bytes 0-7 carry type-specific fields, while
// bytes 8-15 are always timestamp + destination socket ID.
package packet

import (
	"encoding/binary"
	"fmt"
)

// HeaderSize is the fixed size in bytes of every SRT packet header.
// Both data and control packets always start with exactly 16 bytes.
const HeaderSize = 16

// Header holds the fields common to every SRT packet (data or control).
// The IsControl flag (the F bit) tells us how to interpret the rest of
// the packet: data packets carry media payload, control packets carry
// protocol signaling like ACKs, NAKs, and handshakes.
type Header struct {
	// IsControl is the F bit: false = data packet, true = control packet.
	// It occupies the very first bit (bit 0) of the first byte on the wire.
	IsControl bool

	// Timestamp is a 32-bit microsecond clock value relative to the
	// connection start time. It wraps around after ~71 minutes.
	// Located at bytes 8-11 of the packet header.
	Timestamp uint32

	// DestSocketID identifies which SRT socket on the receiving side
	// should process this packet. Each SRT connection has a unique
	// socket ID assigned during the handshake.
	// Located at bytes 12-15 of the packet header.
	DestSocketID uint32
}

// ParseHeader extracts the common header fields from a raw packet buffer.
// The buffer must be at least HeaderSize (16) bytes long.
//
// It reads:
//   - The F bit from the most significant bit of byte 0
//   - The timestamp from bytes 8-11 (big-endian)
//   - The destination socket ID from bytes 12-15 (big-endian)
//
// The caller is responsible for parsing the type-specific fields in
// bytes 0-7 based on the IsControl flag.
func ParseHeader(buf []byte) (Header, error) {
	// Verify we have enough bytes to read the full header.
	if len(buf) < HeaderSize {
		return Header{}, fmt.Errorf("packet too short for header: need %d bytes, got %d", HeaderSize, len(buf))
	}

	var h Header

	// The F bit is the most significant bit (bit 7) of the first byte.
	// We use a bitmask (0x80 = 10000000 in binary) to isolate it.
	// If the bit is set (1), this is a control packet; if clear (0), data.
	h.IsControl = (buf[0] & 0x80) != 0

	// Timestamp lives at bytes 8-11, read as a big-endian 32-bit unsigned integer.
	// SRT uses big-endian (network byte order) for all multi-byte fields.
	h.Timestamp = binary.BigEndian.Uint32(buf[8:12])

	// Destination socket ID lives at bytes 12-15, also big-endian.
	h.DestSocketID = binary.BigEndian.Uint32(buf[12:16])

	return h, nil
}

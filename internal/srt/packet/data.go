package packet

// This file implements SRT data packets, which carry the actual media
// content (audio/video). Data packets are identified by the F bit being 0
// (clear) in the first byte.
//
// Wire layout of an SRT data packet:
//
//	 0                   1                   2                   3
//	 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|0|                Sequence Number (31 bits)                    |  Bytes 0-3
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|PP |O|KK|R|                Message Number (26 bits)           |  Bytes 4-7
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                        Timestamp                             |  Bytes 8-11
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                  Destination Socket ID                       |  Bytes 12-15
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                                                              |
//	~                        Payload                               ~  Bytes 16+
//	|                                                              |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+

import (
	"encoding/binary"
	"fmt"
)

// PacketPosition describes where this packet sits within a larger message.
// A single message can be split across multiple SRT data packets (similar
// to how a large file is split into chunks). The PP field (2 bits) tells
// the receiver how to reassemble the message.
type PacketPosition uint8

const (
	// PositionMiddle means this packet is somewhere in the middle of a
	// multi-packet message (not the first, not the last).
	PositionMiddle PacketPosition = 0b00

	// PositionLast means this is the final packet of a multi-packet message.
	PositionLast PacketPosition = 0b01

	// PositionFirst means this is the first packet of a multi-packet message.
	PositionFirst PacketPosition = 0b10

	// PositionSolo means the entire message fits in this single packet.
	PositionSolo PacketPosition = 0b11
)

// EncryptionFlag indicates whether (and with which key) the payload is
// encrypted. SRT supports AES encryption with two key slots (even/odd)
// to allow seamless key rotation without interrupting the stream.
type EncryptionFlag uint8

const (
	// EncryptionNone means the payload is sent in the clear (no encryption).
	EncryptionNone EncryptionFlag = 0b00

	// EncryptionEven means the payload is encrypted with the "even" key.
	EncryptionEven EncryptionFlag = 0b01

	// EncryptionOdd means the payload is encrypted with the "odd" key.
	EncryptionOdd EncryptionFlag = 0b10
)

// maxSequenceNumber is the largest value that fits in 31 bits (2^31 - 1).
// Sequence numbers wrap around after reaching this value.
const maxSequenceNumber = 0x7FFFFFFF

// maxMessageNumber is the largest value that fits in 26 bits (2^26 - 1).
// Message numbers wrap around after reaching this value.
const maxMessageNumber = 0x03FFFFFF

// DataPacket represents a parsed SRT data packet containing media payload.
// It embeds the common Header and adds data-specific fields parsed from
// the first 16 bytes plus the variable-length payload.
type DataPacket struct {
	// Header contains the common fields (IsControl=false, Timestamp, DestSocketID).
	Header

	// SequenceNumber is a 31-bit monotonically increasing counter (bits 1-31
	// of byte 0-3). Each data packet gets the next sequence number. The
	// receiver uses gaps in sequence numbers to detect packet loss.
	SequenceNumber uint32

	// Position (PP field, 2 bits) indicates where this packet sits within
	// a multi-packet message: first, middle, last, or solo.
	Position PacketPosition

	// InOrder (O bit) is a hint: if true, the packet should be delivered
	// to the application in order; if false, it can be delivered immediately.
	InOrder bool

	// Encryption (KK field, 2 bits) indicates whether and how the payload
	// is encrypted: none, even key, or odd key.
	Encryption EncryptionFlag

	// Retransmitted (R bit) is true if this packet is a retransmission of
	// a previously lost packet (as opposed to the original transmission).
	Retransmitted bool

	// MessageNumber is a 26-bit counter identifying which application-level
	// message this packet belongs to. Multiple packets can share the same
	// message number when a message is fragmented.
	MessageNumber uint32

	// Payload is the actual media data (e.g., MPEG-TS bytes carrying H.264
	// video or AAC audio). This is everything after the 16-byte header.
	Payload []byte
}

// MarshalBinary serializes the DataPacket into its wire format (big-endian).
// The returned byte slice is ready to be sent as a UDP datagram.
func (d *DataPacket) MarshalBinary() ([]byte, error) {
	// Validate that the sequence number fits in 31 bits.
	if d.SequenceNumber > maxSequenceNumber {
		return nil, fmt.Errorf("sequence number %d exceeds 31-bit max (%d)", d.SequenceNumber, maxSequenceNumber)
	}
	// Validate that the message number fits in 26 bits.
	if d.MessageNumber > maxMessageNumber {
		return nil, fmt.Errorf("message number %d exceeds 26-bit max (%d)", d.MessageNumber, maxMessageNumber)
	}

	// Allocate a buffer for the 16-byte header plus the payload.
	buf := make([]byte, HeaderSize+len(d.Payload))

	// --- Bytes 0-3: F bit (0) + Sequence Number (31 bits) ---
	// The F bit is 0 for data packets. We mask the sequence number to 31 bits
	// and write it as a big-endian uint32. Since F=0, the high bit stays clear.
	binary.BigEndian.PutUint32(buf[0:4], d.SequenceNumber&maxSequenceNumber)

	// --- Bytes 4-7: PP(2) | O(1) | KK(2) | R(1) | MsgNo(26) ---
	// We pack six fields into a single 32-bit word using bitwise OR and shifts:
	//   Bits 31-30: PP (packet position, 2 bits)
	//   Bit 29:     O  (in-order flag, 1 bit)
	//   Bits 28-27: KK (encryption flag, 2 bits)
	//   Bit 26:     R  (retransmitted flag, 1 bit)
	//   Bits 25-0:  Message Number (26 bits)
	var word1 uint32
	word1 |= uint32(d.Position&0x03) << 30   // PP in bits 31-30
	if d.InOrder {
		word1 |= 1 << 29 // O in bit 29
	}
	word1 |= uint32(d.Encryption&0x03) << 27 // KK in bits 28-27
	if d.Retransmitted {
		word1 |= 1 << 26 // R in bit 26
	}
	word1 |= d.MessageNumber & maxMessageNumber // MsgNo in bits 25-0
	binary.BigEndian.PutUint32(buf[4:8], word1)

	// --- Bytes 8-11: Timestamp (big-endian microseconds) ---
	binary.BigEndian.PutUint32(buf[8:12], d.Timestamp)

	// --- Bytes 12-15: Destination Socket ID (big-endian) ---
	binary.BigEndian.PutUint32(buf[12:16], d.DestSocketID)

	// --- Bytes 16+: Payload (raw media bytes) ---
	copy(buf[HeaderSize:], d.Payload)

	return buf, nil
}

// UnmarshalDataPacket parses a raw buffer into a DataPacket.
// The buffer must be at least HeaderSize (16) bytes. The F bit (byte 0,
// bit 7) must be 0, indicating this is a data packet, not a control packet.
func UnmarshalDataPacket(buf []byte) (*DataPacket, error) {
	// First, parse the common header to get timestamp and socket ID.
	hdr, err := ParseHeader(buf)
	if err != nil {
		return nil, fmt.Errorf("unmarshal data packet: %w", err)
	}

	// Verify this is actually a data packet (F bit must be 0).
	if hdr.IsControl {
		return nil, fmt.Errorf("unmarshal data packet: F bit is 1 (this is a control packet, not data)")
	}

	d := &DataPacket{
		Header: hdr,
	}

	// --- Bytes 0-3: Extract the 31-bit sequence number ---
	// Read the full 32-bit word, then mask off the F bit (bit 31) to get
	// just the 31-bit sequence number.
	word0 := binary.BigEndian.Uint32(buf[0:4])
	d.SequenceNumber = word0 & maxSequenceNumber

	// --- Bytes 4-7: Extract PP, O, KK, R, and MessageNumber ---
	word1 := binary.BigEndian.Uint32(buf[4:8])
	d.Position = PacketPosition((word1 >> 30) & 0x03)   // Bits 31-30: PP
	d.InOrder = (word1 & (1 << 29)) != 0                 // Bit 29: O
	d.Encryption = EncryptionFlag((word1 >> 27) & 0x03)  // Bits 28-27: KK
	d.Retransmitted = (word1 & (1 << 26)) != 0           // Bit 26: R
	d.MessageNumber = word1 & maxMessageNumber            // Bits 25-0: MsgNo

	// --- Bytes 16+: Payload ---
	// Everything after the 16-byte header is the media payload.
	if len(buf) > HeaderSize {
		d.Payload = make([]byte, len(buf)-HeaderSize)
		copy(d.Payload, buf[HeaderSize:])
	}

	return d, nil
}

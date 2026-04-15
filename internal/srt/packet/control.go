package packet

// This file implements SRT control packets, which carry protocol signaling
// (handshakes, acknowledgments, keep-alives, etc.). Control packets are
// identified by the F bit being 1 (set) in the first byte.
//
// Wire layout of an SRT control packet:
//
//	 0                   1                   2                   3
//	 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|1|         Control Type        |           Subtype             |  Bytes 0-3
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                     Type-Specific Info                        |  Bytes 4-7
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                         Timestamp                            |  Bytes 8-11
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                   Destination Socket ID                      |  Bytes 12-15
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                                                              |
//	~             Control Information Field (CIF)                  ~  Bytes 16+
//	|                                                              |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+

import (
	"encoding/binary"
	"fmt"
)

// ControlType identifies the kind of control message. Each type triggers
// different behavior in the SRT state machine. These are 15-bit values
// occupying bits 1-15 of the first header word.
type ControlType uint16

const (
	// CtrlHandshake is used during connection setup. The handshake CIF
	// carries version info, encryption settings, and peer capabilities.
	CtrlHandshake ControlType = 0x0000

	// CtrlKeepAlive is sent periodically when no other traffic flows,
	// to prevent NAT/firewall mappings from expiring and to detect dead peers.
	CtrlKeepAlive ControlType = 0x0001

	// CtrlACK acknowledges received data packets. The ACK CIF includes
	// the last acknowledged sequence number, RTT estimates, and bandwidth
	// measurements that drive congestion control.
	CtrlACK ControlType = 0x0002

	// CtrlNAK (Negative Acknowledgment) reports packet loss. The NAK CIF
	// contains a list of lost sequence number ranges, triggering retransmission.
	CtrlNAK ControlType = 0x0003

	// CtrlCongestion carries congestion warning information.
	// Reserved for congestion control algorithm extensions.
	CtrlCongestion ControlType = 0x0004

	// CtrlShutdown requests a graceful connection teardown.
	// After sending shutdown, the peer should stop transmitting.
	CtrlShutdown ControlType = 0x0005

	// CtrlACKACK acknowledges a received ACK. This completes the
	// three-way handshake for RTT measurement: data → ACK → ACKACK.
	CtrlACKACK ControlType = 0x0006

	// CtrlDropReq asks the peer to drop specific packets from its send
	// buffer (e.g., packets that are too old to be useful for live streaming).
	CtrlDropReq ControlType = 0x0007

	// CtrlPeerError signals a fatal error from the peer, with an
	// error code in the type-specific info field.
	CtrlPeerError ControlType = 0x0008

	// CtrlUserDefined is the user-defined control message type (0x7FFF).
	// SRT uses this for post-handshake Key Material (KM) messages during
	// key rotation. The TypeSpecific field carries the message subtype:
	//   - 3 = KMREQ (Key Material Request — new key from sender)
	//   - 4 = KMRSP (Key Material Response — acknowledgment from receiver)
	CtrlUserDefined ControlType = 0x7FFF
)

// User-defined control message subtypes. These are carried in the
// TypeSpecific field of a CtrlUserDefined control packet and match
// the SRT handshake extension type IDs for key material messages.
const (
	// UserSubtypeKMREQ identifies a post-handshake Key Material Request.
	// The sender sends this when rotating to a new Stream Encrypting Key.
	UserSubtypeKMREQ uint32 = 3

	// UserSubtypeKMRSP identifies a post-handshake Key Material Response.
	// The receiver sends this to acknowledge receipt of the new key.
	UserSubtypeKMRSP uint32 = 4
)

// ControlPacket represents a parsed SRT control packet used for protocol
// signaling. It embeds the common Header and adds control-specific fields.
type ControlPacket struct {
	// Header contains the common fields (IsControl=true, Timestamp, DestSocketID).
	Header

	// Type identifies what kind of control message this is (handshake, ACK, etc.).
	// It occupies 15 bits (bits 1-15 of the first header word).
	Type ControlType

	// Subtype provides additional classification for some control types.
	// Most control types set this to 0. It occupies 16 bits (bits 16-31
	// of the first header word).
	Subtype uint16

	// TypeSpecific is a 32-bit field whose meaning depends on the control
	// type. For example, in ACK packets it holds the ACK sequence number.
	// It occupies bytes 4-7 of the header.
	TypeSpecific uint32

	// CIF (Control Information Field) is the variable-length payload of the
	// control packet. Its format depends on the Type — for example, a
	// handshake CIF contains version and encryption info, while a NAK CIF
	// contains loss report ranges.
	CIF []byte
}

// MarshalBinary serializes the ControlPacket into its wire format (big-endian).
// The returned byte slice is ready to be sent as a UDP datagram.
func (c *ControlPacket) MarshalBinary() ([]byte, error) {
	// Validate that the control type fits in 15 bits (max 0x7FFF).
	if c.Type > 0x7FFF {
		return nil, fmt.Errorf("control type %d exceeds 15-bit max (0x7FFF)", c.Type)
	}

	// Allocate a buffer for the 16-byte header plus the CIF payload.
	buf := make([]byte, HeaderSize+len(c.CIF))

	// --- Bytes 0-3: F(1) | ControlType(15) | Subtype(16) ---
	// The F bit (bit 31 of the big-endian uint32) is always 1 for control packets.
	// The control type occupies bits 16-30, and the subtype occupies bits 0-15.
	var word0 uint32
	word0 |= 1 << 31                          // F bit = 1 (control packet)
	word0 |= uint32(c.Type&0x7FFF) << 16      // Control type in bits 30-16
	word0 |= uint32(c.Subtype)                // Subtype in bits 15-0
	binary.BigEndian.PutUint32(buf[0:4], word0)

	// --- Bytes 4-7: Type-Specific Info ---
	binary.BigEndian.PutUint32(buf[4:8], c.TypeSpecific)

	// --- Bytes 8-11: Timestamp ---
	binary.BigEndian.PutUint32(buf[8:12], c.Timestamp)

	// --- Bytes 12-15: Destination Socket ID ---
	binary.BigEndian.PutUint32(buf[12:16], c.DestSocketID)

	// --- Bytes 16+: CIF (Control Information Field) ---
	copy(buf[HeaderSize:], c.CIF)

	return buf, nil
}

// UnmarshalControlPacket parses a raw buffer into a ControlPacket.
// The buffer must be at least HeaderSize (16) bytes. The F bit (byte 0,
// bit 7) must be 1, indicating this is a control packet.
func UnmarshalControlPacket(buf []byte) (*ControlPacket, error) {
	// First, parse the common header to get timestamp and socket ID.
	hdr, err := ParseHeader(buf)
	if err != nil {
		return nil, fmt.Errorf("unmarshal control packet: %w", err)
	}

	// Verify this is actually a control packet (F bit must be 1).
	if !hdr.IsControl {
		return nil, fmt.Errorf("unmarshal control packet: F bit is 0 (this is a data packet, not control)")
	}

	c := &ControlPacket{
		Header: hdr,
	}

	// --- Bytes 0-3: Extract Control Type (bits 30-16) and Subtype (bits 15-0) ---
	word0 := binary.BigEndian.Uint32(buf[0:4])
	c.Type = ControlType((word0 >> 16) & 0x7FFF) // Mask off the F bit, take 15 bits
	c.Subtype = uint16(word0 & 0xFFFF)            // Lower 16 bits are the subtype

	// --- Bytes 4-7: Type-Specific Info ---
	c.TypeSpecific = binary.BigEndian.Uint32(buf[4:8])

	// --- Bytes 16+: CIF ---
	// Everything after the 16-byte header is the CIF payload.
	if len(buf) > HeaderSize {
		c.CIF = make([]byte, len(buf)-HeaderSize)
		copy(c.CIF, buf[HeaderSize:])
	}

	return c, nil
}

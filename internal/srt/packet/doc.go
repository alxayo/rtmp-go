// Package packet implements SRT protocol packet framing and parsing.
//
// # Packet Structure
//
// All SRT packets are UDP datagrams with this structure:
//
//	[SRT Header (16 bytes)] [Optional Extensions] [Payload]
//
// SRT Header:
//   Bytes 0-1:   Flags (X=extension, T=type, R=reserved, etc.)
//   Bytes 2-3:   Send Time Stamp (32-bit, 1us resolution)
//   Bytes 4-7:   Destination Socket ID (32-bit, little-endian quirk)
//   Bytes 8-15:  Sequence Number / Message Number / etc. (varies by type)
//
// Type (from Flags byte 0 bit 7):
//   0 = Data packet (audio/video payload)
//   1 = Control packet (handshake, ACK, NAK, keepalive, etc.)
//
// # Packet Types
//
// For clarity, packets fall into two categories:
//
// DATA packets (type=0):
//   - Contains audio/video/text frames
//   - Sequenced (sequence number increments for each packet)
//   - Payload is MPEG-TS data (audio/video elementary streams)
//   - Subject to retransmission if lost
//
// CONTROL packets (type=1):
//   - Handshake: Protocol negotiation (INDUCTION, CONCLUSION)
//   - ACK: Acknowledges received data packets
//   - NAK: Signals lost packets, triggers retransmission
//   - KEEPALIVE: Heartbeat, detects dead peer
//   - SHUTDOWN: Graceful close notification
//   - DROPREQ: Request to drop old packets from buffer
//
// # Extensions
//
// SRT packets can have optional extensions (marked by X flag in header).
// Extensions include:
//   - Cipher Info (CIF): Encryption key material exchange
//   - Key Material (KM): Actual AES-128 keys encrypted with passphrase
//
// This package provides:
//   - Serialize/deserialize data packets
//   - Serialize/deserialize control packets (all types)
//   - Parse extensions (for crypto)
//   - Helper functions for timestamp math, sequence checks
//
// # Data Packet Format
//
// After 16-byte header:
//   - Payload Offset (10 bits): How far into a frame this packet starts
//   - Reserved (1 bit): Always 0
//   - PP (2 bits): Packet Position in frame (first, middle, last, single)
//   - Ordered (1 bit): Delivery order preserved?
//   - Encryption (2 bits): 0=none, 1=AES-128-even, 2=AES-128-odd
//   - Sequence (4 bytes): Incremental sequence number
//   - Message Number (4 bytes): Which audio/video frame this belongs to
//   - Timestamp (4 bytes): Timestamp of this frame (90kHz scale)
//   - Destination Socket ID (4 bytes): Target socket (little-endian)
//   - Payload: Actual audio/video data
//
// # Control Packet Format (Handshake Example)
//
// After 16-byte header:
//   - Control Type (2 bytes): E.g., 0x0000 = INDUCTION
//   - Control Subtype (2 bytes): 0 for most types
//   - Checksum (4 bytes): CRC32 of the entire packet
//   - Time Stamp (4 bytes): When control packet was sent
//   - Dest Socket ID (4 bytes): Target socket (little-endian)
//   - Control Info (variable): Type-specific data
//     For INDUCTION: version, MTU, TSBPD, encryption info, etc.
//     For ACK: sequence numbers of received packets
//     For NAK: sequence numbers of lost packets
//
// # Timestamp Resolution
//
// Timestamps are in microseconds (1us = 1 / 1,000,000 second).
// Sequence numbers wrap at 2^31 (31-bit space), which is intentional
// to simplify modular arithmetic. Timestamps are 32-bit, wrapping
// after ~4294 seconds (~71 minutes).
//
// # Byte Order (Endianness)
//
// Most SRT header fields are BIG-ENDIAN (network byte order), EXCEPT:
//   - Destination Socket ID: LITTLE-ENDIAN (quirk from SRT design)
//
// Always use binary.BigEndian or binary.LittleEndian as appropriate.
// The package provides helpers to ensure consistency.
//
// # Integration Points
//
// - conn package: Calls Encode/Decode to read/write packets
// - listener package: Creates data packets for initial handshake
// - handshake package: Creates/parses control packets (INDUCTION, CONCLUSION)
// - crypto package: Accesses extension headers for KM exchange
//
// # Example: Decode a Received Packet
//
//	// buf contains a UDP payload
//	pkt, err := packet.Decode(buf)
//	if err != nil {
//	    return err
//	}
//
//	if pkt.IsControl {
//	    hs := handshake.NewServer()
//	    hs.HandleClientInduction(pkt)
//	} else {
//	    // Data packet
//	    conn.HandleDataPacket(pkt)
//	}
//
// # Example: Encode an ACK
//
//	ack := packet.NewACK(sequenceNumber, millis, destSocketID)
//	buf, err := ack.Encode()
//	conn.Send(buf)
package packet

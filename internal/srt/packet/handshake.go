package packet

// This file implements the SRT handshake Control Information Field (CIF),
// which is exchanged during connection setup. The handshake CIF carries
// version info, encryption settings, and peer capabilities. It has a
// 48-byte fixed portion followed by optional variable-length extensions.
//
// Wire layout of the Handshake CIF (48 bytes fixed):
//
//	 0                   1                   2                   3
//	 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                          Version                             |  Bytes 0-3
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|       Encryption Field        |       Extension Field        |  Bytes 4-7
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                   Initial Sequence Number                    |  Bytes 8-11
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                  Maximum Transmission Unit                   |  Bytes 12-15
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                Maximum Flow Window Size                      |  Bytes 16-19
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                      Handshake Type                          |  Bytes 20-23
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                       SRT Socket ID                          |  Bytes 24-27
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                         SYN Cookie                           |  Bytes 28-31
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                                                              |
//	+                        Peer IP (128 bits)                    +  Bytes 32-47
//	|                                                              |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                                                              |
//	~                   Extensions (variable)                      ~  Bytes 48+
//	|                                                              |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+

import (
	"encoding/binary"
	"fmt"
)

// HandshakeCIFSize is the fixed size of the handshake CIF (without extensions).
const HandshakeCIFSize = 48

// HandshakeType identifies the stage of the SRT handshake process. SRT uses
// a multi-step handshake: the caller sends Induction, the listener responds
// with Induction (with a cookie), then the caller sends Conclusion, and
// the listener confirms with Conclusion.
type HandshakeType uint32

const (
	// HSTypeWaveAHand is used in legacy UDT-style handshakes (SRT version < 5).
	HSTypeWaveAHand HandshakeType = 0x00000000

	// HSTypeInduction is the first step in the SRT v5 handshake.
	// The caller sends this to initiate a connection, and the listener
	// responds with a SYN cookie for DDoS protection.
	HSTypeInduction HandshakeType = 0x00000001

	// HSTypeConclusion is the final step where both sides confirm the
	// connection parameters. After this, the connection is established
	// and media can flow.
	HSTypeConclusion HandshakeType = 0xFFFFFFFF

	// HSTypeAgreement is used in rendezvous mode (both sides connect
	// simultaneously, as opposed to caller/listener mode).
	HSTypeAgreement HandshakeType = 0xFFFFFFFE

	// HSTypeDone signals that the handshake is complete. Not used on
	// the wire — it's an internal sentinel value.
	HSTypeDone HandshakeType = 0xFFFFFFFD
)

// HandshakeCIF represents the Control Information Field of an SRT handshake
// packet. This structure is exchanged during connection setup to negotiate
// capabilities and security parameters.
type HandshakeCIF struct {
	// Version is the SRT protocol version. SRT v5 uses the value 5.
	// Older UDT-based versions use 4.
	Version uint32

	// EncryptionField indicates the key size for AES encryption:
	//   0 = no encryption, 2 = AES-128, 3 = AES-192, 4 = AES-256.
	EncryptionField uint16

	// ExtensionField flags which handshake extensions are present.
	// In SRT v5 Induction responses this is set to 0x4A17 (the SRT
	// magic number). In Conclusion it indicates which extension blocks follow.
	ExtensionField uint16

	// InitialSeqNumber is the 31-bit initial sequence number for data
	// packets. The sender starts counting from this value.
	InitialSeqNumber uint32

	// MTU is the Maximum Transmission Unit — the largest packet (in bytes)
	// that can be sent without fragmentation. Typically 1500 for Ethernet.
	MTU uint32

	// FlowWindow is the maximum number of unacknowledged packets the
	// sender is allowed to have in flight. This provides flow control
	// to prevent overwhelming the receiver.
	FlowWindow uint32

	// Type identifies the handshake stage (Induction, Conclusion, etc.).
	Type HandshakeType

	// SocketID is the sender's SRT socket ID. The peer uses this value
	// as the DestSocketID when sending packets back.
	SocketID uint32

	// SYNCookie is a random value used for DDoS protection. The listener
	// generates it during Induction and the caller must echo it back
	// in the Conclusion step.
	SYNCookie uint32

	// PeerIP is the sender's IP address stored as 128 bits (16 bytes).
	// IPv4 addresses are stored in the first 4 bytes with the rest zeroed.
	PeerIP [16]byte

	// Extensions is a list of optional extension blocks that follow the
	// fixed 48-byte CIF. Extensions carry additional capabilities like
	// stream ID, congestion control type, and key material.
	Extensions []HSExtension
}

// HSExtension represents a single handshake extension block. Extensions
// allow SRT to negotiate optional features without changing the base
// handshake format.
//
// Wire layout of each extension:
//
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|       Extension Type          |     Length (4-byte blocks)    |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                                                              |
//	~                    Content (Length * 4 bytes)                 ~
//	|                                                              |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
type HSExtension struct {
	// Type identifies this extension (e.g., SRT_CMD_HSREQ, SRT_CMD_SID).
	Type uint16

	// Length is the size of the Content in 4-byte blocks. The actual
	// byte length of Content is Length * 4.
	Length uint16

	// Content is the raw extension data. Its interpretation depends on Type.
	Content []byte
}

// MarshalBinary serializes the HandshakeCIF into its wire format (big-endian).
// The returned slice contains the 48-byte fixed CIF followed by any extensions.
func (h *HandshakeCIF) MarshalBinary() ([]byte, error) {
	// Calculate total size: 48 bytes fixed + 4 bytes per extension header
	// + extension content for each extension.
	totalSize := HandshakeCIFSize
	for _, ext := range h.Extensions {
		// Each extension has a 4-byte header (2 bytes type + 2 bytes length)
		// plus Length*4 bytes of content.
		totalSize += 4 + int(ext.Length)*4
	}

	buf := make([]byte, totalSize)

	// --- Bytes 0-3: Version ---
	binary.BigEndian.PutUint32(buf[0:4], h.Version)

	// --- Bytes 4-5: Encryption Field ---
	binary.BigEndian.PutUint16(buf[4:6], h.EncryptionField)

	// --- Bytes 6-7: Extension Field ---
	binary.BigEndian.PutUint16(buf[6:8], h.ExtensionField)

	// --- Bytes 8-11: Initial Sequence Number (only 31 bits used) ---
	binary.BigEndian.PutUint32(buf[8:12], h.InitialSeqNumber&maxSequenceNumber)

	// --- Bytes 12-15: MTU ---
	binary.BigEndian.PutUint32(buf[12:16], h.MTU)

	// --- Bytes 16-19: Flow Window ---
	binary.BigEndian.PutUint32(buf[16:20], h.FlowWindow)

	// --- Bytes 20-23: Handshake Type ---
	binary.BigEndian.PutUint32(buf[20:24], uint32(h.Type))

	// --- Bytes 24-27: Socket ID ---
	binary.BigEndian.PutUint32(buf[24:28], h.SocketID)

	// --- Bytes 28-31: SYN Cookie ---
	binary.BigEndian.PutUint32(buf[28:32], h.SYNCookie)

	// --- Bytes 32-47: Peer IP (16 bytes, copied directly) ---
	copy(buf[32:48], h.PeerIP[:])

	// --- Bytes 48+: Extensions ---
	// Write each extension block sequentially after the fixed CIF.
	offset := HandshakeCIFSize
	for _, ext := range h.Extensions {
		// Extension header: 2 bytes type + 2 bytes length
		binary.BigEndian.PutUint16(buf[offset:offset+2], ext.Type)
		binary.BigEndian.PutUint16(buf[offset+2:offset+4], ext.Length)
		// Extension content: Length * 4 bytes
		contentLen := int(ext.Length) * 4
		copy(buf[offset+4:offset+4+contentLen], ext.Content)
		offset += 4 + contentLen
	}

	return buf, nil
}

// UnmarshalHandshakeCIF parses a raw buffer into a HandshakeCIF.
// The buffer must be at least HandshakeCIFSize (48) bytes for the fixed
// portion. Any bytes beyond 48 are parsed as extension blocks.
func UnmarshalHandshakeCIF(buf []byte) (*HandshakeCIF, error) {
	// Verify we have enough bytes for the fixed CIF.
	if len(buf) < HandshakeCIFSize {
		return nil, fmt.Errorf("handshake CIF too short: need %d bytes, got %d", HandshakeCIFSize, len(buf))
	}

	h := &HandshakeCIF{}

	// --- Parse the 48-byte fixed portion ---
	h.Version = binary.BigEndian.Uint32(buf[0:4])
	h.EncryptionField = binary.BigEndian.Uint16(buf[4:6])
	h.ExtensionField = binary.BigEndian.Uint16(buf[6:8])
	h.InitialSeqNumber = binary.BigEndian.Uint32(buf[8:12]) & maxSequenceNumber
	h.MTU = binary.BigEndian.Uint32(buf[12:16])
	h.FlowWindow = binary.BigEndian.Uint32(buf[16:20])
	h.Type = HandshakeType(binary.BigEndian.Uint32(buf[20:24]))
	h.SocketID = binary.BigEndian.Uint32(buf[24:28])
	h.SYNCookie = binary.BigEndian.Uint32(buf[28:32])
	copy(h.PeerIP[:], buf[32:48])

	// --- Parse extensions (bytes 48+) ---
	// Each extension block is: 2 bytes type + 2 bytes length + (length*4) bytes content.
	// We keep reading until we run out of bytes.
	offset := HandshakeCIFSize
	for offset+4 <= len(buf) {
		ext := HSExtension{}
		ext.Type = binary.BigEndian.Uint16(buf[offset : offset+2])
		ext.Length = binary.BigEndian.Uint16(buf[offset+2 : offset+4])

		// Calculate the content size in bytes (length field is in 4-byte blocks).
		contentLen := int(ext.Length) * 4

		// Verify we have enough bytes remaining for the content.
		if offset+4+contentLen > len(buf) {
			return nil, fmt.Errorf("handshake extension truncated at offset %d: need %d content bytes, have %d",
				offset, contentLen, len(buf)-offset-4)
		}

		// Copy the extension content into its own slice.
		if contentLen > 0 {
			ext.Content = make([]byte, contentLen)
			copy(ext.Content, buf[offset+4:offset+4+contentLen])
		}

		h.Extensions = append(h.Extensions, ext)
		offset += 4 + contentLen
	}

	return h, nil
}

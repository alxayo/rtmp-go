package handshake

// This file implements parsing and building of SRT handshake extensions.
//
// During the SRT v5 handshake Conclusion phase, both sides exchange
// extension blocks to negotiate capabilities. The three most important
// extensions are:
//
//   HSREQ (type 1): Caller sends SRT version, feature flags, and TSBPD delays.
//   HSRSP (type 2): Listener responds with negotiated version, flags, and delays.
//   SID   (type 5): Caller sends the Stream ID (like an RTMP stream key).
//
// Each extension block has a 4-byte header (2 bytes type + 2 bytes length)
// followed by content. The length field counts 4-byte words, not bytes.

import (
	"encoding/binary"
	"fmt"
)

// --- SRT Feature Flag Constants ---
//
// Each bit in the SRT flags field enables a specific protocol feature.
// Both sides advertise their supported flags, and the negotiated set is
// the intersection (bitwise AND) of what both sides support.

const (
	// FlagTSBPDSND indicates the sender supports Timestamp-Based Packet Delivery.
	// TSBPD adds a configurable delay at the receiver to absorb network jitter.
	FlagTSBPDSND uint32 = 0x00000001

	// FlagTSBPDRCV indicates the receiver supports TSBPD.
	FlagTSBPDRCV uint32 = 0x00000002

	// FlagCRYPT indicates encryption is supported (AES-128/192/256).
	FlagCRYPT uint32 = 0x00000004

	// FlagTLPKTDROP enables Too-Late Packet Drop — packets that arrive after
	// their TSBPD deadline are dropped instead of delivered out of order.
	FlagTLPKTDROP uint32 = 0x00000008

	// FlagPERIODICNAK enables periodic NAK reports. Instead of sending a NAK
	// immediately on loss detection, the receiver batches losses and reports
	// them periodically, reducing control overhead.
	FlagPERIODICNAK uint32 = 0x00000010

	// FlagREXMITFLG enables the retransmission flag in data packet headers.
	// When set, the R bit in data packets distinguishes original transmissions
	// from retransmissions, helping the receiver with statistics.
	FlagREXMITFLG uint32 = 0x00000020

	// FlagSTREAM enables stream mode (as opposed to message mode). In stream
	// mode, SRT delivers a continuous byte stream like TCP. In message mode,
	// SRT preserves message boundaries.
	FlagSTREAM uint32 = 0x00000040

	// FlagPACKETFILTER enables packet filter support for forward error
	// correction (FEC) or other packet-level processing.
	FlagPACKETFILTER uint32 = 0x00000080
)

// --- SRT Extension Type Constants ---
//
// These identify the type of each extension block in the handshake CIF.
// They appear in the 2-byte "Extension Type" field of each extension header.

const (
	// ExtTypeHSREQ is the Handshake Request extension. The caller sends this
	// with its SRT version, feature flags, and requested TSBPD delays.
	ExtTypeHSREQ uint16 = 1

	// ExtTypeHSRSP is the Handshake Response extension. The listener sends
	// this with the negotiated version, flags, and TSBPD delays.
	ExtTypeHSRSP uint16 = 2

	// ExtTypeKMREQ is the Key Material Request for encryption key exchange.
	ExtTypeKMREQ uint16 = 3

	// ExtTypeKMRSP is the Key Material Response for encryption key exchange.
	ExtTypeKMRSP uint16 = 4

	// ExtTypeSID carries the Stream ID (similar to an RTMP stream key).
	ExtTypeSID uint16 = 5

	// ExtTypeCONGEST carries the congestion controller type string.
	ExtTypeCONGEST uint16 = 6

	// ExtTypeFILTER carries the packet filter configuration.
	ExtTypeFILTER uint16 = 7

	// ExtTypeGROUP carries group membership information for socket groups.
	ExtTypeGROUP uint16 = 8
)

// hsReqSize is the fixed size of the HSREQ/HSRSP extension payload: 12 bytes
// (3 x 32-bit words: SRT version, flags, and packed TSBPD delays).
const hsReqSize = 12

// HSReqData contains the parsed HSREQ extension payload (12 bytes).
// This is sent by the caller during the Conclusion phase to negotiate
// SRT-specific parameters like TSBPD delays and feature flags.
type HSReqData struct {
	// SRTVersion is the peer's SRT library version, encoded as:
	//   major * 0x10000 + minor * 0x100 + patch
	// For example, SRT 1.5.0 = 0x00010500.
	SRTVersion uint32

	// SRTFlags is a bitmask of supported features (FlagTSBPDSND, etc.).
	SRTFlags uint32

	// RecvTSBPD is the receiver's requested TSBPD delay in milliseconds.
	// The final negotiated delay is max(sender_requested, receiver_requested).
	RecvTSBPD uint16

	// SenderTSBPD is the sender's requested TSBPD delay in milliseconds.
	SenderTSBPD uint16
}

// ParseHSReq parses an HSREQ extension payload (12 bytes) into an HSReqData
// struct. The payload contains 3 big-endian 32-bit words:
//   Word 0: SRT version
//   Word 1: Feature flags
//   Word 2: RecvTSBPD (high 16 bits) | SenderTSBPD (low 16 bits)
func ParseHSReq(data []byte) (*HSReqData, error) {
	// The HSREQ payload must be exactly 12 bytes (3 x uint32).
	if len(data) < hsReqSize {
		return nil, fmt.Errorf("HSREQ payload too short: need %d bytes, got %d", hsReqSize, len(data))
	}

	req := &HSReqData{}

	// Word 0: SRT version (e.g., 0x00010500 = v1.5.0).
	req.SRTVersion = binary.BigEndian.Uint32(data[0:4])

	// Word 1: Feature flag bitmask.
	req.SRTFlags = binary.BigEndian.Uint32(data[4:8])

	// Word 2: Two 16-bit TSBPD delays packed into one 32-bit word.
	// High 16 bits = receiver's delay, low 16 bits = sender's delay.
	req.RecvTSBPD = binary.BigEndian.Uint16(data[8:10])
	req.SenderTSBPD = binary.BigEndian.Uint16(data[10:12])

	return req, nil
}

// BuildHSRsp constructs an HSRSP extension payload (12 bytes).
// The format is identical to HSREQ — 3 big-endian 32-bit words containing
// the negotiated SRT version, flags, and TSBPD delays.
func BuildHSRsp(version uint32, flags uint32, recvDelay, sendDelay uint16) []byte {
	buf := make([]byte, hsReqSize)

	// Word 0: Negotiated SRT version.
	binary.BigEndian.PutUint32(buf[0:4], version)

	// Word 1: Negotiated feature flags (intersection of both sides).
	binary.BigEndian.PutUint32(buf[4:8], flags)

	// Word 2: Negotiated TSBPD delays.
	binary.BigEndian.PutUint16(buf[8:10], recvDelay)
	binary.BigEndian.PutUint16(buf[10:12], sendDelay)

	return buf
}

// ParseStreamIDExtension extracts a Stream ID string from an SID extension.
//
// SRT stores the stream ID as 32-bit words where the characters within
// each 4-byte group are reversed (byte-swapped within each word). This
// is because SRT writes the string as big-endian 32-bit integers, which
// reverses the byte order of each 4-character group.
//
// Example: The string "abcd" is stored as the bytes [d, c, b, a].
//
// After reversing, any trailing null bytes (padding) are stripped.
func ParseStreamIDExtension(data []byte) string {
	if len(data) == 0 {
		return ""
	}

	// Make a copy so we don't modify the original data.
	result := make([]byte, len(data))
	copy(result, data)

	// Reverse the bytes within each 4-byte word. SRT encodes the stream
	// ID by writing characters into 32-bit words in network byte order,
	// which reverses each group of 4 characters.
	for i := 0; i+3 < len(result); i += 4 {
		result[i], result[i+1], result[i+2], result[i+3] =
			result[i+3], result[i+2], result[i+1], result[i]
	}

	// Strip trailing null bytes that were added as padding to reach a
	// 4-byte boundary.
	end := len(result)
	for end > 0 && result[end-1] == 0 {
		end--
	}

	return string(result[:end])
}

// BuildStreamIDExtension encodes a stream ID string for the SID extension.
// The string is padded to a 4-byte boundary with null bytes, then each
// 4-byte group is byte-reversed to match SRT's encoding convention.
func BuildStreamIDExtension(streamID string) []byte {
	if len(streamID) == 0 {
		return nil
	}

	// Pad the string to a multiple of 4 bytes with null bytes.
	// SRT extension content must be aligned to 4-byte (32-bit) boundaries.
	padded := len(streamID)
	if padded%4 != 0 {
		padded += 4 - (padded % 4)
	}

	buf := make([]byte, padded)
	copy(buf, streamID)

	// Reverse the bytes within each 4-byte word to match SRT's encoding.
	for i := 0; i+3 < len(buf); i += 4 {
		buf[i], buf[i+1], buf[i+2], buf[i+3] =
			buf[i+3], buf[i+2], buf[i+1], buf[i]
	}

	return buf
}

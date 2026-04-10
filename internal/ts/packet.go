package ts

// This file implements parsing of the fundamental unit of MPEG-TS: the 188-byte
// transport stream packet. Every piece of data in an MPEG-TS stream — video,
// audio, metadata tables — is carried inside these fixed-size packets.
//
// The fixed size is crucial for error recovery: if a receiver loses sync (e.g.,
// due to network errors), it can simply scan for the sync byte (0x47) and then
// verify that subsequent sync bytes appear exactly 188 bytes apart.

import (
	"fmt"
)

// Fundamental MPEG-TS packet constants.
const (
	// PacketSize is the fixed size of every MPEG-TS packet in bytes.
	// This never varies — it's defined by the ISO 13818-1 standard.
	PacketSize = 188

	// SyncByte is the magic byte (0x47) at the start of every TS packet.
	// Receivers use this to find packet boundaries in a stream of bytes.
	SyncByte = 0x47

	// NullPID is a special PID value (0x1FFF) used for padding/null packets.
	// These packets carry no useful data and exist only to maintain a
	// constant bitrate when there isn't enough real data to fill the stream.
	NullPID = 0x1FFF

	// PATPID is the well-known PID (0) that always carries the Program
	// Association Table. This is the entry point for discovering what
	// programs and streams are available in the transport stream.
	PATPID = 0x0000
)

// Packet represents a parsed 188-byte MPEG-TS packet.
// Each field corresponds to a specific part of the 4-byte packet header,
// plus the optional adaptation field and payload that follow.
type Packet struct {
	// TEI (Transport Error Indicator) is set to true if the demodulator
	// could not correct errors in this packet. A receiver should discard
	// or skip packets with TEI=true.
	TEI bool

	// PayloadUnitStart (PUSI) signals the start of a new higher-level
	// data unit. For PES packets, it means a new PES packet starts in
	// this TS packet. For PSI tables (PAT/PMT), it means a new section
	// starts here (preceded by a pointer field).
	PayloadUnitStart bool

	// Priority indicates this packet has higher priority than other
	// packets with the same PID. Rarely used in practice.
	Priority bool

	// PID is the 13-bit Packet Identifier (0-8191). It tells the demuxer
	// which logical stream this packet belongs to. Special PIDs include
	// 0x0000 (PAT) and 0x1FFF (null/padding).
	PID uint16

	// Scrambling indicates the scrambling mode (0=not scrambled).
	// Values 1-3 indicate various scrambling modes used by conditional
	// access systems (e.g., pay-TV encryption). We only handle 0.
	Scrambling uint8

	// HasAdaptation is true when the packet contains an adaptation field.
	// The adaptation field carries timing info (PCR) and stream flags.
	HasAdaptation bool

	// HasPayload is true when the packet contains payload data.
	// Some packets only have an adaptation field (used for PCR-only packets).
	HasPayload bool

	// ContinuityCounter is a 4-bit counter (0-15) that increments with
	// each packet for a given PID. The receiver uses this to detect
	// dropped packets — if the counter jumps by more than 1, packets
	// were lost.
	ContinuityCounter uint8

	// AdaptationField contains optional timing and control information.
	// It's only present when HasAdaptation is true.
	AdaptationField *AdaptationField

	// Payload holds the actual data carried by this packet. For PES
	// packets, this is a chunk of audio/video data. For PSI tables,
	// this is table data. Can be up to 184 bytes (188 minus 4-byte header).
	Payload []byte
}

// AdaptationField carries timing and stream control information that sits
// between the 4-byte packet header and the payload. The most important
// field here is the PCR (Program Clock Reference), which is used by the
// decoder to synchronize its clock with the encoder's clock.
type AdaptationField struct {
	// Length is the number of bytes in the adaptation field, not counting
	// the length byte itself. A length of 0 means only the length byte
	// is present (used for padding).
	Length uint8

	// Discontinuity signals that the continuity counter and/or PCR may
	// have a discontinuity at this point (e.g., after a channel switch).
	Discontinuity bool

	// RandomAccess indicates this packet is at a random access point —
	// typically a video keyframe (I-frame). A decoder can start decoding
	// from this point without needing earlier data.
	RandomAccess bool

	// PCR (Program Clock Reference) is a timestamp in 90kHz units
	// derived from the 33-bit base field of the 48-bit PCR value.
	// It's used for clock synchronization between encoder and decoder.
	// Set to -1 if no PCR is present in this adaptation field.
	PCR int64
}

// ParsePacket parses a raw 188-byte buffer into a structured Packet.
//
// The packet header layout (4 bytes):
//
//	Byte 0:    sync byte (must be 0x47)
//	Byte 1-2:  TEI(1 bit) | PUSI(1 bit) | Priority(1 bit) | PID(13 bits)
//	Byte 3:    Scrambling(2 bits) | AdaptationControl(2 bits) | CC(4 bits)
//
// After the header, an optional adaptation field and payload follow based
// on the AdaptationControl bits.
func ParsePacket(data [PacketSize]byte) (*Packet, error) {
	// Step 1: Verify the sync byte. Every valid TS packet must start with 0x47.
	if data[0] != SyncByte {
		return nil, fmt.Errorf("ts: invalid sync byte: 0x%02X (expected 0x47)", data[0])
	}

	// Step 2: Parse the 4-byte header.
	//
	// Bytes 1-2 contain three single-bit flags followed by the 13-bit PID.
	// We use bitwise operations to extract each field:
	//   - bit 7 of byte 1 = TEI
	//   - bit 6 of byte 1 = PUSI
	//   - bit 5 of byte 1 = Priority
	//   - bits 4-0 of byte 1 + all of byte 2 = PID (13 bits)
	pkt := &Packet{
		TEI:              data[1]&0x80 != 0,
		PayloadUnitStart: data[1]&0x40 != 0,
		Priority:         data[1]&0x20 != 0,
		PID:              uint16(data[1]&0x1F)<<8 | uint16(data[2]),
	}

	// Byte 3 packs three fields:
	//   - bits 7-6: scrambling control (2 bits)
	//   - bits 5-4: adaptation field control (2 bits)
	//   - bits 3-0: continuity counter (4 bits)
	pkt.Scrambling = (data[3] >> 6) & 0x03
	adaptationControl := (data[3] >> 4) & 0x03
	pkt.ContinuityCounter = data[3] & 0x0F

	// Decode the adaptation field control bits:
	//   00 = reserved (invalid)
	//   01 = payload only (no adaptation field)
	//   10 = adaptation field only (no payload)
	//   11 = adaptation field followed by payload
	switch adaptationControl {
	case 0x00:
		// Reserved value — technically invalid, but some broken encoders
		// produce these. Treat as no adaptation and no payload.
	case 0x01:
		pkt.HasPayload = true
	case 0x02:
		pkt.HasAdaptation = true
	case 0x03:
		pkt.HasAdaptation = true
		pkt.HasPayload = true
	}

	// Step 3: Parse the adaptation field if present.
	// The adaptation field starts at byte 4 (right after the header).
	offset := 4 // Current position in the packet data
	if pkt.HasAdaptation {
		af, err := parseAdaptationField(data[:], offset)
		if err != nil {
			return nil, err
		}
		pkt.AdaptationField = af
		// Skip past the adaptation field: 1 byte for the length field itself,
		// plus however many bytes the adaptation field contains.
		offset += 1 + int(af.Length)
	}

	// Step 4: Extract the payload (everything from current offset to end of packet).
	if pkt.HasPayload && offset < PacketSize {
		pkt.Payload = data[offset:PacketSize]
	}

	return pkt, nil
}

// parseAdaptationField parses the adaptation field starting at the given offset.
//
// The adaptation field layout:
//
//	Byte 0:   adaptation field length (number of bytes following this byte)
//	Byte 1:   flags byte (only present if length > 0)
//	          bit 7: discontinuity indicator
//	          bit 6: random access indicator
//	          bit 5: ES priority indicator
//	          bit 4: PCR flag (1 = PCR fields are present)
//	          bit 3: OPCR flag
//	          bit 2: splicing point flag
//	          bit 1: transport private data flag
//	          bit 0: adaptation field extension flag
//	Bytes 2+: optional fields (PCR, OPCR, etc.) based on flags
func parseAdaptationField(data []byte, offset int) (*AdaptationField, error) {
	// Safety check: make sure we can read the length byte.
	if offset >= PacketSize {
		return nil, fmt.Errorf("ts: adaptation field offset %d out of bounds", offset)
	}

	af := &AdaptationField{
		Length: data[offset],
		PCR:    -1, // Default: no PCR present
	}

	// If the adaptation field length is 0, there are no flags or optional
	// fields — it's just used as padding.
	if af.Length == 0 {
		return af, nil
	}

	// Ensure we have enough data for the flags byte.
	flagsOffset := offset + 1
	if flagsOffset >= PacketSize {
		return nil, fmt.Errorf("ts: adaptation field truncated at flags byte")
	}

	flags := data[flagsOffset]
	af.Discontinuity = flags&0x80 != 0
	af.RandomAccess = flags&0x40 != 0

	// Check if PCR is present (bit 4 of the flags byte).
	hasPCR := flags&0x10 != 0
	if hasPCR {
		// PCR requires 6 bytes, starting 2 bytes after the adaptation field start
		// (1 byte for length + 1 byte for flags = offset+2).
		pcrOffset := offset + 2
		if pcrOffset+6 > PacketSize {
			return nil, fmt.Errorf("ts: adaptation field truncated at PCR")
		}
		af.PCR = parsePCR(data[pcrOffset : pcrOffset+6])
	}

	return af, nil
}

// parsePCR extracts the 33-bit PCR base from the 6-byte PCR field.
//
// The full 48-bit PCR field is laid out as:
//
//	33 bits:  PCR base (counts at 90kHz — same rate as PTS/DTS timestamps)
//	 6 bits:  reserved
//	 9 bits:  PCR extension (counts at 27MHz for fine-grained timing)
//
// For our purposes, we only need the 33-bit base value in 90kHz units,
// which is sufficient for synchronizing audio and video streams.
func parsePCR(data []byte) int64 {
	// The 33-bit base is spread across the first 4 bytes plus 1 bit of byte 4.
	// Byte layout:
	//   data[0]: base[32:25]  (8 bits)
	//   data[1]: base[24:17]  (8 bits)
	//   data[2]: base[16:9]   (8 bits)
	//   data[3]: base[8:1]    (8 bits)
	//   data[4]: base[0](1 bit) | reserved(6 bits) | ext[8](1 bit)
	base := int64(data[0])<<25 |
		int64(data[1])<<17 |
		int64(data[2])<<9 |
		int64(data[3])<<1 |
		int64(data[4]>>7)

	return base
}

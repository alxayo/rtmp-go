package chunk

// Chunk Header Parsing
// ====================
// Every RTMP chunk starts with a header that describes the message it carries.
// The header has three parts:
//
//   1. Basic Header (1-3 bytes): Contains the FMT type (0-3) and Chunk Stream ID (CSID).
//   2. Message Header (0/3/7/11 bytes): Contains timestamp, message length, type, and stream ID.
//      The size depends on FMT — higher FMT values omit fields that haven't changed.
//   3. Extended Timestamp (0 or 4 bytes): Present when timestamp >= 0xFFFFFF (16,777,215 ms ≈ 4.66 hours).
//
// FMT types provide header compression:
//   FMT 0 (11 bytes): Full header — all fields present (used for first message on a stream)
//   FMT 1 (7 bytes):  Timestamp delta + length + type (stream ID reused from previous)
//   FMT 2 (3 bytes):  Timestamp delta only (length, type, stream ID all reused)
//   FMT 3 (0 bytes):  No header — all fields inherited (used for continuation chunks)

import (
	"encoding/binary"
	"fmt"
	"io"
)

// extendedTimestampMarker is the sentinel value (0xFFFFFF) placed in the 3-byte
// timestamp field when the actual timestamp exceeds 24 bits. When the reader
// encounters this value, it knows an additional 4-byte extended timestamp follows.
const (
	extendedTimestampMarker = 0xFFFFFF
)

// ChunkHeader represents the parsed header for a single RTMP chunk.
//
// RTMP uses header compression: consecutive messages on the same Chunk Stream ID
// (CSID) can omit fields that haven't changed. The FMT field controls which
// fields are present in the wire format:
//
//   FMT 0: All fields present (Timestamp is absolute)
//   FMT 1: Timestamp delta, MessageLength, MessageTypeID (MessageStreamID inherited)
//   FMT 2: Timestamp delta only (all other fields inherited)
//   FMT 3: No fields (everything inherited — used for continuation chunks)
//
// IsDelta indicates whether Timestamp holds a delta (FMT 1/2) or absolute value (FMT 0).
type ChunkHeader struct {
	FMT                    uint8  // Header format type (0-3), controls which fields are present
	CSID                   uint32 // Chunk Stream ID — identifies the logical chunk stream
	Timestamp              uint32 // Absolute timestamp (FMT0) or delta from previous (FMT1/2)
	MessageLength          uint32 // Total size of the message payload in bytes
	MessageTypeID          uint8  // What kind of message: 1-6=control, 8=audio, 9=video, 20=command
	MessageStreamID        uint32 // Application-level stream ID (little-endian on wire — RTMP quirk)
	HasExtendedTimestamp   bool   // True if a 4-byte extended timestamp was read from the wire
	ExtendedTimestampValue uint32 // The extended timestamp value (replaces the 3-byte field)
	IsDelta                bool   // True when Timestamp is relative to the previous message
	headerBytes            int    // Total bytes consumed reading this header (for offset tracking)
}

// HeaderBytes returns number of bytes consumed for this header (basic + message + extended timestamp if any).
func (h *ChunkHeader) HeaderBytes() int { return h.headerBytes }

// parseBasicHeader reads the Basic Header (1-3 bytes) from the wire.
//
// The Basic Header encodes two values:
//   - FMT (2 bits): The header format type (0-3)
//   - CSID (6+ bits): The Chunk Stream ID
//
// CSID encoding uses three forms depending on the value:
//   - 1-byte form: CSID 2-63 (6 bits in first byte)
//   - 2-byte form: CSID 64-319 (first byte has 0 in low 6 bits, second byte + 64)
//   - 3-byte form: CSID 320-65599 (first byte has 1 in low 6 bits, next 2 bytes + 64)
//
// Returns the FMT value, CSID, number of bytes consumed, and any error.
func parseBasicHeader(r io.Reader) (fmtVal uint8, csid uint32, n int, err error) {
	var b [1]byte
	if _, err = io.ReadFull(r, b[:]); err != nil {
		return 0, 0, 0, fmt.Errorf("basic header: %w", err)
	}
	n = 1
	fmtVal = b[0] >> 6
	raw := b[0] & 0x3F
	switch raw {
	case 0: // 2-byte form (csid 64-319)
		var b1 [1]byte
		if _, err = io.ReadFull(r, b1[:]); err != nil {
			return 0, 0, n, fmt.Errorf("basic header (2-byte) continuation: %w", err)
		}
		n++
		csid = uint32(b1[0]) + 64
	case 1: // 3-byte form (csid 320-65599)
		var b2 [2]byte
		if _, err = io.ReadFull(r, b2[:]); err != nil {
			return 0, 0, n, fmt.Errorf("basic header (3-byte) continuation: %w", err)
		}
		n += 2
		csid = uint32(b2[0]) + 64 + (uint32(b2[1]) << 8)
	default:
		csid = uint32(raw)
	}
	return
}

// readUint24 decodes a 24-bit (3-byte) big-endian unsigned integer.
// RTMP uses 24-bit integers for timestamps and message lengths in chunk headers.
func readUint24(b []byte) uint32 { return uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2]) }

// ParseChunkHeader parses a single chunk header (Basic + Message + ExtendedTimestamp) from r.
// prev is the previous header for the same CSID (required for FMT 3 to inherit fields; optional otherwise).
// On success returns a fully populated header struct (for FMT3 inherited fields are copied).
func ParseChunkHeader(r io.Reader, prev *ChunkHeader) (*ChunkHeader, error) {
	fmtVal, csid, basicBytes, err := parseBasicHeader(r)
	if err != nil {
		return nil, err
	}

	h := &ChunkHeader{FMT: fmtVal, CSID: csid, headerBytes: basicBytes}

	switch fmtVal {
	case 0:
		if err := h.parseFMT0(r); err != nil {
			return nil, err
		}
	case 1:
		if err := h.parseFMT1(r, prev); err != nil {
			return nil, err
		}
	case 2:
		if err := h.parseFMT2(r, prev); err != nil {
			return nil, err
		}
	case 3:
		if err := h.parseFMT3(r, prev, basicBytes); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported FMT value %d", fmtVal)
	}
	return h, nil
}

// readExtendedTimestamp reads a 4-byte extended timestamp if rawTS equals the
// extended timestamp marker (0xFFFFFF). It updates the header fields accordingly.
func (h *ChunkHeader) readExtendedTimestamp(r io.Reader, rawTS uint32) error {
	if rawTS != extendedTimestampMarker {
		return nil
	}
	var ext [4]byte
	if _, err := io.ReadFull(r, ext[:]); err != nil {
		return fmt.Errorf("extended timestamp FMT%d: %w", h.FMT, err)
	}
	h.headerBytes += 4
	h.HasExtendedTimestamp = true
	val := binary.BigEndian.Uint32(ext[:])
	h.ExtendedTimestampValue = val
	h.Timestamp = val
	return nil
}

// parseFMT0 reads an 11-byte message header (absolute timestamp, length, type, stream ID).
func (h *ChunkHeader) parseFMT0(r io.Reader) error {
	var mh [11]byte
	if _, err := io.ReadFull(r, mh[:]); err != nil {
		return fmt.Errorf("message header FMT0: %w", err)
	}
	h.headerBytes += 11
	ts := readUint24(mh[0:3])
	h.Timestamp = ts
	h.MessageLength = readUint24(mh[3:6])
	h.MessageTypeID = mh[6]
	h.MessageStreamID = binary.LittleEndian.Uint32(mh[7:11])
	return h.readExtendedTimestamp(r, ts)
}

// parseFMT1 reads a 7-byte message header (timestamp delta, length, type).
// MessageStreamID is inherited from prev if available.
func (h *ChunkHeader) parseFMT1(r io.Reader, prev *ChunkHeader) error {
	var mh [7]byte
	if _, err := io.ReadFull(r, mh[:]); err != nil {
		return fmt.Errorf("message header FMT1: %w", err)
	}
	h.headerBytes += 7
	delta := readUint24(mh[0:3])
	h.Timestamp = delta
	h.IsDelta = true
	h.MessageLength = readUint24(mh[3:6])
	h.MessageTypeID = mh[6]
	return h.readExtendedTimestamp(r, delta)
}

// parseFMT2 reads a 3-byte message header (timestamp delta only).
// Length, type and stream ID are inherited from prev.
func (h *ChunkHeader) parseFMT2(r io.Reader, prev *ChunkHeader) error {
	var mh [3]byte
	if _, err := io.ReadFull(r, mh[:]); err != nil {
		return fmt.Errorf("message header FMT2: %w", err)
	}
	h.headerBytes += 3
	delta := readUint24(mh[0:3])
	h.Timestamp = delta
	h.IsDelta = true
	if err := h.readExtendedTimestamp(r, delta); err != nil {
		return err
	}
	// Inherit remaining fields from previous header
	if prev != nil && prev.CSID == h.CSID {
		h.MessageLength = prev.MessageLength
		h.MessageTypeID = prev.MessageTypeID
		h.MessageStreamID = prev.MessageStreamID
	}
	return nil
}

// parseFMT3 reads no new header fields; all values are inherited from prev.
// If prev used an extended timestamp, a 4-byte value is still read from the wire.
func (h *ChunkHeader) parseFMT3(r io.Reader, prev *ChunkHeader, basicBytes int) error {
	if prev == nil || prev.CSID != h.CSID {
		return fmt.Errorf("message header FMT3: missing previous header for CSID %d", h.CSID)
	}
	csid := h.CSID
	*h = *prev
	h.FMT = 3
	h.CSID = csid
	h.headerBytes = basicBytes
	if prev.HasExtendedTimestamp {
		var ext [4]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return fmt.Errorf("extended timestamp FMT3: %w", err)
		}
		h.headerBytes += 4
		val := binary.BigEndian.Uint32(ext[:])
		h.ExtendedTimestampValue = val
		h.Timestamp = val
	}
	return nil
}

package chunk

// Chunk header parsing (T017)
// Implements Basic Header + Message Header parsing for FMT 0-3 as per contracts/chunking.md
// Focus: wire-format fidelity, no allocation beyond small fixed-size scratch buffers.

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Constants for limits / markers
const (
	extendedTimestampMarker = 0xFFFFFF
)

// ChunkHeader represents the parsed header (not including chunk data) for a single RTMP chunk.
// For FMT 1/2 the Timestamp field holds the delta (delta semantics indicated by IsDelta=true).
// For FMT 3 no new fields are transmitted; the parser copies prior header if provided.
// HasExtendedTimestamp indicates that a 4-byte extended timestamp followed the (message) header.
// The ExtendedTimestampValue always contains the absolute timestamp value for FMT0, or the delta value
// for FMT1/2 when HasExtendedTimestamp is true. For FMT3 it mirrors the prior header when applicable.
type ChunkHeader struct {
	FMT                    uint8
	CSID                   uint32
	Timestamp              uint32 // Absolute (FMT0) or delta (FMT1/2) or reused (FMT3)
	MessageLength          uint32
	MessageTypeID          uint8
	MessageStreamID        uint32
	HasExtendedTimestamp   bool
	ExtendedTimestampValue uint32 // Absolute or delta depending on FMT
	IsDelta                bool   // True for FMT1/2 (and FMT3 continuation of prior delta-based series)
	headerBytes            int    // Number of header bytes consumed (incl. extended timestamp if present)
}

// HeaderBytes returns number of bytes consumed for this header (basic + message + extended timestamp if any).
func (h *ChunkHeader) HeaderBytes() int { return h.headerBytes }

// parseBasicHeader reads the Basic Header (1-3 bytes) returning fmt, csid and bytes consumed.
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

// readUint24 reads a 24-bit big-endian unsigned integer.
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
	case 0: // 11 bytes message header
		var mh [11]byte
		if _, err = io.ReadFull(r, mh[:]); err != nil {
			return nil, fmt.Errorf("message header FMT0: %w", err)
		}
		h.headerBytes += 11
		ts := readUint24(mh[0:3])
		h.Timestamp = ts
		h.MessageLength = readUint24(mh[3:6])
		h.MessageTypeID = mh[6]
		h.MessageStreamID = binary.LittleEndian.Uint32(mh[7:11])
		if ts == extendedTimestampMarker {
			var ext [4]byte
			if _, err = io.ReadFull(r, ext[:]); err != nil {
				return nil, fmt.Errorf("extended timestamp FMT0: %w", err)
			}
			h.headerBytes += 4
			h.HasExtendedTimestamp = true
			val := binary.BigEndian.Uint32(ext[:])
			h.ExtendedTimestampValue = val
			h.Timestamp = val // Replace marker with real absolute timestamp
		}
	case 1: // 7 bytes: timestamp delta + length + type
		var mh [7]byte
		if _, err = io.ReadFull(r, mh[:]); err != nil {
			return nil, fmt.Errorf("message header FMT1: %w", err)
		}
		h.headerBytes += 7
		delta := readUint24(mh[0:3])
		h.Timestamp = delta
		h.IsDelta = true
		h.MessageLength = readUint24(mh[3:6])
		h.MessageTypeID = mh[6]
		if delta == extendedTimestampMarker {
			var ext [4]byte
			if _, err = io.ReadFull(r, ext[:]); err != nil {
				return nil, fmt.Errorf("extended timestamp FMT1: %w", err)
			}
			h.headerBytes += 4
			h.HasExtendedTimestamp = true
			val := binary.BigEndian.Uint32(ext[:])
			h.ExtendedTimestampValue = val
			h.Timestamp = val // store delta value (full) for caller to apply
		}
	case 2: // 3 bytes: timestamp delta only
		var mh [3]byte
		if _, err = io.ReadFull(r, mh[:]); err != nil {
			return nil, fmt.Errorf("message header FMT2: %w", err)
		}
		h.headerBytes += 3
		delta := readUint24(mh[0:3])
		h.Timestamp = delta
		h.IsDelta = true
		if delta == extendedTimestampMarker {
			var ext [4]byte
			if _, err = io.ReadFull(r, ext[:]); err != nil {
				return nil, fmt.Errorf("extended timestamp FMT2: %w", err)
			}
			h.headerBytes += 4
			h.HasExtendedTimestamp = true
			val := binary.BigEndian.Uint32(ext[:])
			h.ExtendedTimestampValue = val
			h.Timestamp = val
		}
		// Inherit remaining fields from previous header (required for correctness downstream)
		if prev == nil || prev.CSID != csid {
			// We tolerate nil here; higher layer can decide to error when applying state.
		} else {
			h.MessageLength = prev.MessageLength
			h.MessageTypeID = prev.MessageTypeID
			h.MessageStreamID = prev.MessageStreamID
		}
	case 3: // No header; inherit all from previous header
		if prev == nil || prev.CSID != csid {
			return nil, fmt.Errorf("message header FMT3: missing previous header for CSID %d", csid)
		}
		// Copy all fields (shallow)
		*h = *prev
		h.FMT = 3
		h.headerBytes = basicBytes // override consumed bytes (only basic header + maybe extended)
		// If previous used extended timestamp for this message sequence, we must read it again.
		if prev.HasExtendedTimestamp {
			var ext [4]byte
			if _, err = io.ReadFull(r, ext[:]); err != nil {
				return nil, fmt.Errorf("extended timestamp FMT3: %w", err)
			}
			h.headerBytes += 4
			// Value should match previous absolute / delta; we ignore mismatch but could log later.
			// Overwrite with what we read.
			val := binary.BigEndian.Uint32(ext[:])
			h.ExtendedTimestampValue = val
			if prev.IsDelta {
				h.Timestamp = val
			} else {
				h.Timestamp = val
			}
		}
	default:
		return nil, fmt.Errorf("unsupported FMT value %d", fmtVal)
	}
	return h, nil
}

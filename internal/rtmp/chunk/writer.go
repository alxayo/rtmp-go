package chunk

// Header serialization (T018)
// Implements Basic Header + Message Header + Extended Timestamp encoding for FMT 0-3.
// Focus: zero allocations (caller gets []byte), protocol fidelity, mirrors parser logic in header.go.

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	fmt0 = 0
	fmt1 = 1
	fmt2 = 2
	fmt3 = 3
)

// encodeBasicHeader encodes the Basic Header (1-3 bytes) into dst and returns resulting slice.
// Follows CSID encoding rules (spec / contracts/chunking.md).
func encodeBasicHeader(dst []byte, fmtVal uint8, csid uint32) ([]byte, error) {
	if fmtVal > 3 {
		return nil, fmt.Errorf("invalid fmt %d", fmtVal)
	}
	if csid < 2 { // 0 & 1 reserved in RTMP spec
		return nil, fmt.Errorf("invalid csid %d (must be >=2)", csid)
	}
	switch {
	case csid >= 2 && csid <= 63:
		b := byte(fmtVal<<6) | byte(csid)
		dst = append(dst, b)
	case csid >= 64 && csid <= 319:
		b0 := byte(fmtVal<<6) | 0 // marker for 2-byte form
		b1 := byte(csid - 64)
		dst = append(dst, b0, b1)
	case csid >= 320 && csid <= 65599:
		val := csid - 64
		b0 := byte(fmtVal<<6) | 1 // marker for 3-byte form
		b1 := byte(val & 0xFF)
		b2 := byte(val >> 8)
		dst = append(dst, b0, b1, b2)
	default:
		return nil, fmt.Errorf("csid %d out of range", csid)
	}
	return dst, nil
}

// writeUint24 writes a 24-bit big-endian integer into the 3-byte slice.
func writeUint24(b []byte, v uint32) {
	b[0] = byte(v >> 16)
	b[1] = byte(v >> 8)
	b[2] = byte(v)
}

// EncodeChunkHeader serializes a ChunkHeader (only header bytes, no payload) and returns the header slice.
// prev provides context for FMT3 and extended timestamp reuse semantics.
func EncodeChunkHeader(h *ChunkHeader, prev *ChunkHeader) ([]byte, error) {
	if h == nil {
		return nil, errors.New("nil header")
	}
	// Determine if we must emit extended timestamp.
	// For FMT0: absolute timestamp; FMT1/2: delta; FMT3: reuse previous timestamp value but still must repeat extended if previous used it.
	var (
		needExtended bool
		tsField      uint32 // value to emit (absolute or delta depending on FMT)
	)
	switch h.FMT {
	case fmt0:
		tsField = h.Timestamp
		needExtended = h.Timestamp >= extendedTimestampMarker
	case fmt1, fmt2:
		tsField = h.Timestamp // contains delta per parser contract
		needExtended = h.Timestamp >= extendedTimestampMarker
	case fmt3:
		if prev == nil || prev.CSID != h.CSID {
			return nil, fmt.Errorf("FMT3 requires previous header for CSID %d", h.CSID)
		}
		// FMT3 reuses everything; extended timestamp must be re-emitted iff previous header used extended.
		needExtended = prev.Timestamp >= extendedTimestampMarker || prev.HasExtendedTimestamp
		// Timestamp value for extended emission is previous absolute/delta value.
		tsField = prev.Timestamp
	default:
		return nil, fmt.Errorf("unsupported fmt %d", h.FMT)
	}

	buf := make([]byte, 0, 1+11+4) // worst-case
	var err error
	buf, err = encodeBasicHeader(buf, h.FMT, h.CSID)
	if err != nil {
		return nil, err
	}

	// Message header per FMT
	switch h.FMT {
	case fmt0:
		mh := make([]byte, 11)
		if needExtended {
			writeUint24(mh[0:3], extendedTimestampMarker)
		} else {
			writeUint24(mh[0:3], tsField)
		}
		writeUint24(mh[3:6], h.MessageLength)
		mh[6] = h.MessageTypeID
		binary.LittleEndian.PutUint32(mh[7:11], h.MessageStreamID)
		buf = append(buf, mh...)
	case fmt1:
		mh := make([]byte, 7)
		if needExtended {
			writeUint24(mh[0:3], extendedTimestampMarker)
		} else {
			writeUint24(mh[0:3], tsField) // delta
		}
		writeUint24(mh[3:6], h.MessageLength)
		mh[6] = h.MessageTypeID
		buf = append(buf, mh...)
	case fmt2:
		mh := make([]byte, 3)
		if needExtended {
			writeUint24(mh[0:3], extendedTimestampMarker)
		} else {
			writeUint24(mh[0:3], tsField)
		}
		buf = append(buf, mh...)
	case fmt3:
		// no message header bytes
	}

	if needExtended {
		var ext [4]byte
		binary.BigEndian.PutUint32(ext[:], tsField)
		buf = append(buf, ext[:]...)
	}
	return buf, nil
}

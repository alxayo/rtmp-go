package chunk

// Header serialization (T018)
// Implements Basic Header + Message Header + Extended Timestamp encoding for FMT 0-3.
// Focus: zero allocations (caller gets []byte), protocol fidelity, mirrors parser logic in header.go.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
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

// -----------------------------------------------------------------------------
// Writer (T021) – fragments Messages into chunks using FMT0 + FMT3 continuation.
// -----------------------------------------------------------------------------

// Writer emits RTMP chunks for outbound messages. Not concurrency-safe; expected
// usage is a single write goroutine per connection.
type Writer struct {
	w         io.Writer
	chunkSize uint32 // outbound chunk size (default 128 if zero)
}

// NewWriter creates a new chunk Writer.
func NewWriter(w io.Writer, chunkSize uint32) *Writer {
	if chunkSize == 0 {
		chunkSize = 128
	}
	return &Writer{w: w, chunkSize: chunkSize}
}

// SetChunkSize updates the outbound chunk size (validated to sane bounds).
func (w *Writer) SetChunkSize(size uint32) {
	if size >= 1 && size <= 65536 {
		w.chunkSize = size
	}
}

// EncodeHeaderOnly helper for header-focused tests (mirrors reader tests style).
func (w *Writer) EncodeHeaderOnly(h *ChunkHeader, prev *ChunkHeader) (int, error) {
	b, err := EncodeChunkHeader(h, prev)
	if err != nil {
		return 0, err
	}
	return w.w.Write(b)
}

// WriteMessage fragments and writes a full RTMP message as one or more chunks.
// Requirements:
//   - First chunk FMT0 (absolute timestamp + full header)
//   - Continuations FMT3 (header compression) with repeated extended timestamp if used
//   - Atomic per-chunk Write calls (header+payload together)
func (w *Writer) WriteMessage(msg *Message) error {
	if w == nil || w.w == nil {
		return errors.New("writer: nil underlying writer")
	}
	if msg == nil {
		return errors.New("writer: nil message")
	}
	if msg.MessageLength == 0 {
		msg.MessageLength = uint32(len(msg.Payload))
	}
	if int(msg.MessageLength) != len(msg.Payload) {
		return fmt.Errorf("writer: payload length %d != declared %d", len(msg.Payload), msg.MessageLength)
	}
	cs := w.chunkSize
	if cs == 0 {
		cs = 128
	}

	// First (FMT0) header
	first := &ChunkHeader{
		FMT:             fmt0,
		CSID:            msg.CSID,
		Timestamp:       msg.Timestamp,
		MessageLength:   msg.MessageLength,
		MessageTypeID:   msg.TypeID,
		MessageStreamID: msg.MessageStreamID,
	}
	if msg.Timestamp >= extendedTimestampMarker {
		first.HasExtendedTimestamp = true
	}
	hdr, err := EncodeChunkHeader(first, nil)
	if err != nil {
		return fmt.Errorf("writer: encode first header: %w", err)
	}
	toSend := msg.Payload
	if uint32(len(toSend)) > cs {
		toSend = toSend[:cs]
	}
	if err := writeChunk(w.w, hdr, toSend); err != nil {
		return err
	}
	written := uint32(len(toSend))
	prev := first

	// Continuation chunks (FMT3)
	for written < msg.MessageLength {
		remain := msg.MessageLength - written
		sz := remain
		if sz > cs {
			sz = cs
		}
		cont := &ChunkHeader{FMT: fmt3, CSID: msg.CSID}
		hdr3, err := EncodeChunkHeader(cont, prev)
		if err != nil {
			return fmt.Errorf("writer: encode continuation header: %w", err)
		}
		start := written
		end := written + sz
		if end > uint32(len(msg.Payload)) {
			return fmt.Errorf("writer: bounds (end=%d > len=%d)", end, len(msg.Payload))
		}
		if err := writeChunk(w.w, hdr3, msg.Payload[start:end]); err != nil {
			return err
		}
		written = end
		// carry forward extended timestamp semantics implicitly via first.HasExtendedTimestamp
		prev = cont
		if first.HasExtendedTimestamp {
			cont.HasExtendedTimestamp = true
			cont.Timestamp = first.Timestamp // for EncodeChunkHeader logic if reused later
		}
	}
	return nil
}

// writeChunk builds a single buffer header+payload and writes it once (atomic chunk emission).
func writeChunk(w io.Writer, header []byte, payload []byte) error {
	buf := make([]byte, 0, len(header)+len(payload))
	buf = append(buf, header...)
	buf = append(buf, payload...)
	_, err := w.Write(buf)
	return err
}

package chunk

// Dechunker implementation (T020)
// Reassembles RTMP messages from an interleaved stream of chunks, honoring
// per-CSID state, header compression, extended timestamps, and dynamic
// inbound chunk size changes (Set Chunk Size control message, type id 1).
//
// Design goals:
//  - Single pass streaming: no buffering beyond current chunk & in‑flight message buffers.
//  - Stateful per CSID using ChunkStreamState from state.go.
//  - Protocol fidelity: header parsing delegated to ParseChunkHeader (header.go).
//  - Minimal allocations: reuse scratch buffer for chunk payload reads.
//
// Public contract:
//  NewReader(r, initialChunkSize) *Reader
//  (*Reader).SetChunkSize(size)   -- external override (e.g. after control handler)
//  (*Reader).ReadMessage() (*Message, error) -- blocking read returning next complete message.
//
// Error model:
//  Returns *errors.ChunkError wrapping underlying IO/parse/state issues.
//  io.EOF is passed through only when encountered before starting a new header.

import (
	"encoding/binary"
	"fmt"
	"io"

	protoerr "github.com/alxayo/go-rtmp/internal/errors"
)

// Reader converts a byte stream of RTMP chunks into complete Messages.
// Not safe for concurrent use; expected usage is a single read loop goroutine.
type Reader struct {
	br         io.Reader
	chunkSize  uint32 // inbound chunk size (payload per chunk, sans headers)
	states     map[uint32]*ChunkStreamState
	prevHeader map[uint32]*ChunkHeader // last parsed header per CSID (for FMT3 continuity)
	scratch    []byte                  // reused payload buffer sized to current chunkSize
}

// NewReader creates a new dechunker with the provided initial inbound chunk size (spec default 128).
func NewReader(r io.Reader, chunkSize uint32) *Reader {
	if chunkSize == 0 {
		chunkSize = 128
	}
	return &Reader{
		br:         r,
		chunkSize:  chunkSize,
		states:     make(map[uint32]*ChunkStreamState),
		prevHeader: make(map[uint32]*ChunkHeader),
	}
}

// SetChunkSize overrides the inbound chunk size; safe to call between ReadMessage invocations.
func (r *Reader) SetChunkSize(size uint32) {
	if size >= 1 && size <= 65536 { // basic sanity; spec permits up to at least 65536 in typical impls
		r.chunkSize = size
		// Reset scratch so it can be reallocated lazily to new size when needed.
		r.scratch = nil
	}
}

// nextHeader parses the next chunk header, using prior header for CSID when needed (FMT3).
func (r *Reader) nextHeader() (*ChunkHeader, error) {
	// Provide prior header for this CSID only when needed; ParseChunkHeader decides usage.
	// However we don't know CSID until after basic header parse inside ParseChunkHeader.
	// Strategy: Attempt parse with nil; if FMT3 error complaining about missing previous, retry with stored.
	// Simpler: Provide stored prev for every call; parser only uses for FMT2/3; safe if nil.
	// We need CSID to index map; ParseChunkHeader needs prev to inherit for FMT3.
	// Therefore we must first parse basic header manually? To avoid duplicating logic we adopt two-step:
	//  1. Peek & parse basic header using a tee reader; easier is to re-implement minimal basic header parse here.
	// To keep code DRY we'll duplicate tiny basic header parse (few lines) instead of read+unread complexity.

	// Re-parse basic header logic (mirroring parseBasicHeader) so we can supply prev when needed.
	var first [1]byte
	if _, err := io.ReadFull(r.br, first[:]); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return nil, err
		}
		return nil, protoerr.NewChunkError("reader.basic_header", err)
	}
	fmtVal := first[0] >> 6
	raw := first[0] & 0x3F
	csid := uint32(0)
	consumed := 1
	switch raw {
	case 0:
		var b1 [1]byte
		if _, err := io.ReadFull(r.br, b1[:]); err != nil {
			return nil, protoerr.NewChunkError("reader.basic_header.2byte", err)
		}
		consumed++
		csid = uint32(b1[0]) + 64
	case 1:
		var b2 [2]byte
		if _, err := io.ReadFull(r.br, b2[:]); err != nil {
			return nil, protoerr.NewChunkError("reader.basic_header.3byte", err)
		}
		consumed += 2
		csid = uint32(b2[0]) + 64 + (uint32(b2[1]) << 8)
	default:
		csid = uint32(raw)
	}

	// We now need to read the remainder of the chunk header (message + optional extended timestamp).
	// Rather than reimplement, we'll manually complete message header parse for non-FMT3 based on spec,
	// replicating logic from ParseChunkHeader but tailored (to avoid double-reading basic header).
	// For maintainability we could refactor ParseChunkHeader into two pieces, but keeping T017 stable.
	// Duplicate small logic with care.

	h := &ChunkHeader{FMT: fmtVal, CSID: csid, headerBytes: consumed}
	var err error
	switch fmtVal {
	case 0:
		var mh [11]byte
		if _, err = io.ReadFull(r.br, mh[:]); err != nil {
			return nil, protoerr.NewChunkError("reader.message_header.fmt0", err)
		}
		h.headerBytes += 11
		abs := uint32(mh[0])<<16 | uint32(mh[1])<<8 | uint32(mh[2])
		h.Timestamp = abs
		h.MessageLength = uint32(mh[3])<<16 | uint32(mh[4])<<8 | uint32(mh[5])
		h.MessageTypeID = mh[6]
		h.MessageStreamID = binary.LittleEndian.Uint32(mh[7:11])
		if abs == extendedTimestampMarker {
			var ext [4]byte
			if _, err = io.ReadFull(r.br, ext[:]); err != nil {
				return nil, protoerr.NewChunkError("reader.extended_timestamp.fmt0", err)
			}
			h.headerBytes += 4
			h.HasExtendedTimestamp = true
			val := binary.BigEndian.Uint32(ext[:])
			h.ExtendedTimestampValue = val
			h.Timestamp = val
		}
	case 1:
		var mh [7]byte
		if _, err = io.ReadFull(r.br, mh[:]); err != nil {
			return nil, protoerr.NewChunkError("reader.message_header.fmt1", err)
		}
		h.headerBytes += 7
		delta := uint32(mh[0])<<16 | uint32(mh[1])<<8 | uint32(mh[2])
		h.Timestamp = delta
		h.IsDelta = true
		h.MessageLength = uint32(mh[3])<<16 | uint32(mh[4])<<8 | uint32(mh[5])
		h.MessageTypeID = mh[6]
		// FMT1 reuses MessageStreamID from previous header (per RTMP spec)
		if prev := r.prevHeader[csid]; prev != nil {
			h.MessageStreamID = prev.MessageStreamID
		}
		if delta == extendedTimestampMarker {
			var ext [4]byte
			if _, err = io.ReadFull(r.br, ext[:]); err != nil {
				return nil, protoerr.NewChunkError("reader.extended_timestamp.fmt1", err)
			}
			h.headerBytes += 4
			h.HasExtendedTimestamp = true
			val := binary.BigEndian.Uint32(ext[:])
			h.ExtendedTimestampValue = val
			h.Timestamp = val
		}
	case 2:
		var mh [3]byte
		if _, err = io.ReadFull(r.br, mh[:]); err != nil {
			return nil, protoerr.NewChunkError("reader.message_header.fmt2", err)
		}
		h.headerBytes += 3
		delta := uint32(mh[0])<<16 | uint32(mh[1])<<8 | uint32(mh[2])
		h.Timestamp = delta
		h.IsDelta = true
		if delta == extendedTimestampMarker {
			var ext [4]byte
			if _, err = io.ReadFull(r.br, ext[:]); err != nil {
				return nil, protoerr.NewChunkError("reader.extended_timestamp.fmt2", err)
			}
			h.headerBytes += 4
			h.HasExtendedTimestamp = true
			val := binary.BigEndian.Uint32(ext[:])
			h.ExtendedTimestampValue = val
			h.Timestamp = val
		}
		if prev := r.prevHeader[csid]; prev != nil {
			h.MessageLength = prev.MessageLength
			h.MessageTypeID = prev.MessageTypeID
			h.MessageStreamID = prev.MessageStreamID
		}
	case 3:
		prev := r.prevHeader[csid]
		if prev == nil {
			return nil, protoerr.NewChunkError("reader.message_header.fmt3", fmt.Errorf("missing previous header for csid %d", csid))
		}
		// Copy entire previous header (value semantics)
		*h = *prev
		h.FMT = 3
		// If previous used extended timestamp we must read it again.
		if prev.HasExtendedTimestamp {
			var ext [4]byte
			if _, err = io.ReadFull(r.br, ext[:]); err != nil {
				return nil, protoerr.NewChunkError("reader.extended_timestamp.fmt3", err)
			}
			h.headerBytes += 4
			val := binary.BigEndian.Uint32(ext[:])
			h.ExtendedTimestampValue = val
			// Timestamp semantics same (abs or delta) – just overwrite.
			h.Timestamp = val
		}
	default:
		return nil, protoerr.NewChunkError("reader.message_header", fmt.Errorf("unsupported fmt %d", fmtVal))
	}
	return h, nil
}

// ReadMessage blocks until the next complete RTMP message is reassembled or an error occurs.
// It transparently updates internal chunk size on receiving a Set Chunk Size (type id 1) control message.
func (r *Reader) ReadMessage() (*Message, error) {
	for {
		// Parse next chunk header
		h, err := r.nextHeader()
		if err != nil {
			if err == io.EOF { // propagate EOF cleanly
				return nil, err
			}
			return nil, err
		}
		csid := h.CSID
		// Fetch / init state
		st := r.states[csid]
		if st == nil {
			st = &ChunkStreamState{CSID: csid}
			r.states[csid] = st
		}
		if err = st.ApplyHeader(h); err != nil {
			return nil, err
		}
		// Store header as previous for this CSID (for FMT2 inheritance / FMT3 continuation)
		r.prevHeader[csid] = h

		// Determine bytes to read for this chunk
		remaining := st.BytesRemaining()
		if remaining == 0 { // possible zero-length message
			complete, msg, err := st.AppendChunkData(nil)
			if err != nil {
				return nil, err
			}
			if complete {
				r.maybeHandleControl(msg)
				return msg, nil
			}
			continue // need next header
		}
		readLen := remaining
		if readLen > r.chunkSize {
			readLen = r.chunkSize
		}
		// Ensure scratch buffer capacity
		if uint32(cap(r.scratch)) < readLen {
			r.scratch = make([]byte, readLen)
		}
		buf := r.scratch[:readLen]
		if _, err := io.ReadFull(r.br, buf); err != nil {
			return nil, protoerr.NewChunkError("reader.read_chunk", err)
		}
		complete, msg, err := st.AppendChunkData(buf)
		if err != nil {
			return nil, err
		}
		if complete {
			r.maybeHandleControl(msg)
			return msg, nil
		}
		// Otherwise loop for next chunk (interleaving naturally supported because we restart header parse)
	}
}

// maybeHandleControl inspects message for Set Chunk Size and applies size update immediately.
func (r *Reader) maybeHandleControl(msg *Message) {
	if msg == nil {
		return
	}
	// RTMP control messages (chunk type ID 1-6) travel typically on CSID 2, msid 0.
	if msg.TypeID == 1 && msg.MessageStreamID == 0 && len(msg.Payload) >= 4 {
		v := binary.BigEndian.Uint32(msg.Payload[:4])
		if v > 0 && v <= 65536 { // guard
			r.SetChunkSize(v)
		}
	}
}

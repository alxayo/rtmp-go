package chunk

// Chunk Reader (Dechunker)
// =======================
// RTMP transmits data as interleaved "chunks" — small fragments of larger messages.
// Multiple messages from different streams can be interleaved chunk by chunk.
// The Reader's job is to reassemble these chunks back into complete Messages.
//
// How it works:
//   1. Read the next chunk header (determines which stream and message it belongs to)
//   2. Read the chunk payload (up to chunkSize bytes)
//   3. Append the payload to the in-progress message for that stream
//   4. If the message is complete (all bytes received), return it
//   5. Otherwise, loop back to step 1 (next chunk may be from a different stream)
//
// The Reader also handles dynamic chunk size changes: when it receives a
// Set Chunk Size control message (TypeID 1), it updates its internal chunk
// size so subsequent chunks are read with the new size.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	protoerr "github.com/alxayo/go-rtmp/internal/errors"
)

// Reader converts a byte stream of interleaved RTMP chunks into complete Messages.
// It maintains per-stream state to handle header compression and multi-chunk reassembly.
// Not safe for concurrent use; designed for a single read-loop goroutine per connection.
type Reader struct {
	br         io.Reader                    // underlying byte stream (typically a TCP connection)
	chunkSize  uint32                       // maximum payload bytes per chunk (default 128, server may increase)
	states     map[uint32]*ChunkStreamState // per-CSID assembly state (tracks partial messages)
	prevHeader map[uint32]*ChunkHeader      // last header per CSID (for FMT 1/2/3 field inheritance)
	scratch    []byte                       // reusable buffer for reading chunk payloads
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

// nextHeader parses the next chunk header, using prior header for CSID when needed (FMT2/3).
func (r *Reader) nextHeader() (*ChunkHeader, error) {
	// Parse basic header to learn CSID, then supply the stored previous header
	// so the FMT-specific parsers can inherit fields for FMT1/2/3.
	fmtVal, csid, basicBytes, err := parseBasicHeader(r.br)
	if err != nil {
		// Propagate EOF cleanly (reader shutdown).
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, io.EOF
		}
		return nil, protoerr.NewChunkError("reader.basic_header", err)
	}

	prev := r.prevHeader[csid]
	h := &ChunkHeader{FMT: fmtVal, CSID: csid, headerBytes: basicBytes}

	switch fmtVal {
	case 0:
		if err := h.parseFMT0(r.br); err != nil {
			return nil, protoerr.NewChunkError("reader.message_header.fmt0", err)
		}
	case 1:
		if err := h.parseFMT1(r.br, prev); err != nil {
			return nil, protoerr.NewChunkError("reader.message_header.fmt1", err)
		}
		// FMT1 inherits MessageStreamID from previous header (per RTMP spec)
		if prev != nil {
			h.MessageStreamID = prev.MessageStreamID
		}
	case 2:
		if err := h.parseFMT2(r.br, prev); err != nil {
			return nil, protoerr.NewChunkError("reader.message_header.fmt2", err)
		}
	case 3:
		if err := h.parseFMT3(r.br, prev, basicBytes); err != nil {
			return nil, protoerr.NewChunkError("reader.message_header.fmt3", err)
		}
	default:
		return nil, protoerr.NewChunkError("reader.message_header", fmt.Errorf("unsupported fmt %d", fmtVal))
	}
	return h, nil
}

// ReadMessage blocks until the next complete RTMP message is reassembled or an error occurs.
// It transparently updates internal chunk size on receiving a Set Chunk Size (type id 1) control message.
//
// The reassembly loop handles chunk interleaving: chunks from different CSIDs can arrive
// interleaved, so we maintain per-CSID state and keep looping until one CSID's message
// is fully assembled (bytesReceived == messageLength).
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
		// Ensure scratch buffer capacity (exponential growth to reduce allocations)
		if uint32(cap(r.scratch)) < readLen {
			newCap := readLen
			if newCap < r.chunkSize*2 {
				newCap = r.chunkSize * 2
			}
			r.scratch = make([]byte, newCap)
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

// maybeHandleControl checks if a completed message is a Set Chunk Size control
// message (TypeID 1, MSID 0) and automatically updates the reader's chunk size.
// This allows the reader to adapt when the sender changes its chunk size mid-stream,
// which is normal during RTMP session setup (servers typically increase from 128 to 4096).
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

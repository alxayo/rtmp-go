package chunk

// ChunkStreamState Management (T019)
// Maintains per-CSID state required for RTMP chunk header compression and
// progressive message reassembly. The reader (T020) will keep a map[uint32]*ChunkStreamState.
//
// Semantics follow contracts/chunking.md and spec notes:
//  FMT0: absolute timestamp; new message (all header fields present)
//  FMT1: timestamp delta; new message (length + type present, stream id reused)
//  FMT2: timestamp delta only; new message (length, type, stream id reused)
//  FMT3: continuation chunk for the *current* (in‑flight) message – no header field changes
//
// Message completion is signalled when bytesReceived == lastMsgLength. At that point
// a *Message is returned (payload copied). Header field values remain so they can be
// reused for subsequent compressed headers (FMT1/2/3) per spec.

import (
	"fmt"

	protoerr "github.com/alxayo/go-rtmp/internal/errors"
)

// ChunkStreamState holds rolling state for a single chunk stream (CSID).
// Fields exported to aid white-box testing & potential observability.
type ChunkStreamState struct {
	CSID            uint32
	LastTimestamp   uint32
	LastMsgLength   uint32
	LastMsgTypeID   uint8
	LastMsgStreamID uint32

	buffer        []byte
	bytesReceived uint32
	inProgress    bool // true while assembling a multi-chunk message
}

// ResetBuffer clears the assembly buffer but keeps header context (used after message extraction).
func (s *ChunkStreamState) ResetBuffer() {
	if s == nil {
		return
	}
	s.buffer = s.buffer[:0]
	s.bytesReceived = 0
	s.inProgress = false
}

// ApplyHeader applies a parsed ChunkHeader to the state, updating header compression fields
// and (for FMT0/FMT1/FMT2) starting a new message assembly. For FMT3 it validates continuity.
func (s *ChunkStreamState) ApplyHeader(h *ChunkHeader) error {
	if h == nil {
		return protoerr.NewChunkError("state.apply_header", fmt.Errorf("nil header"))
	}
	if s.CSID == 0 { // first use – bind CSID
		s.CSID = h.CSID
	}
	if s.CSID != h.CSID {
		return protoerr.NewChunkError("state.apply_header", fmt.Errorf("csid mismatch: have %d want %d", s.CSID, h.CSID))
	}
	switch h.FMT {
	case 0: // full header – absolute timestamp
		s.LastTimestamp = h.Timestamp
		s.LastMsgLength = h.MessageLength
		s.LastMsgTypeID = h.MessageTypeID
		s.LastMsgStreamID = h.MessageStreamID
		s.ResetBuffer()
		s.inProgress = true
	case 1: // delta + length + type (reuse stream id)
		// FMT1 can be first chunk on a CSID if client assumes MessageStreamID=0
		// This is common for command/control messages. Accept and use MSID=0 as default.
		if s.LastMsgStreamID == 0 {
			s.LastMsgStreamID = 0         // Explicit: assume control stream (MSID=0)
			s.LastTimestamp = h.Timestamp // First use: treat as absolute
		} else {
			s.LastTimestamp += h.Timestamp // Subsequent: delta
		}
		s.LastMsgLength = h.MessageLength
		s.LastMsgTypeID = h.MessageTypeID
		s.ResetBuffer()
		s.inProgress = true
	case 2: // delta only (reuse length, type, stream id)
		if s.LastMsgStreamID == 0 || s.LastMsgLength == 0 {
			return protoerr.NewChunkError("state.apply_header", fmt.Errorf("FMT2 without prior state"))
		}
		s.LastTimestamp += h.Timestamp
		s.ResetBuffer()
		s.inProgress = true
	case 3: // continuation – MUST have active in-progress message
		if !s.inProgress || s.LastMsgLength == 0 {
			return protoerr.NewChunkError("state.apply_header", fmt.Errorf("FMT3 without active message"))
		}
		// no field changes
	default:
		return protoerr.NewChunkError("state.apply_header", fmt.Errorf("unsupported fmt %d", h.FMT))
	}
	return nil
}

// AppendChunkData appends payload bytes for the current (in-progress) message.
// Returns (complete, *Message, error). When complete==true msg is non-nil and
// the state's buffer is reset for the next message while header fields persist.
func (s *ChunkStreamState) AppendChunkData(data []byte) (bool, *Message, error) {
	if len(data) == 0 {
		return s.isComplete(), nil, nil
	}
	if !s.inProgress {
		return false, nil, protoerr.NewChunkError("state.append", fmt.Errorf("no active message"))
	}
	// Lazy allocate capacity for entire message to avoid repeated growth.
	if s.buffer == nil {
		capHint := s.LastMsgLength
		if capHint == 0 {
			capHint = uint32(len(data))
		}
		s.buffer = make([]byte, 0, capHint)
	}
	if s.bytesReceived+uint32(len(data)) > s.LastMsgLength {
		return false, nil, protoerr.NewChunkError("state.append", fmt.Errorf("overflow: have %d + %d > %d", s.bytesReceived, len(data), s.LastMsgLength))
	}
	s.buffer = append(s.buffer, data...)
	s.bytesReceived += uint32(len(data))
	if s.bytesReceived == s.LastMsgLength { // complete
		msg := &Message{
			CSID:            s.CSID,
			Timestamp:       s.LastTimestamp,
			MessageLength:   s.LastMsgLength,
			TypeID:          s.LastMsgTypeID,
			MessageStreamID: s.LastMsgStreamID,
			Payload:         append([]byte(nil), s.buffer...), // copy
		}
		// Keep header fields, clear assembly state.
		s.ResetBuffer()
		return true, msg, nil
	}
	return false, nil, nil
}

// isComplete returns true if current message assembly has reached declared length.
func (s *ChunkStreamState) isComplete() bool {
	return s.inProgress && s.bytesReceived == s.LastMsgLength && s.LastMsgLength > 0
}

// BytesRemaining returns number of bytes still needed for the in-progress message.
func (s *ChunkStreamState) BytesRemaining() uint32 {
	if !s.inProgress || s.LastMsgLength == 0 {
		return 0
	}
	if s.bytesReceived >= s.LastMsgLength {
		return 0
	}
	return s.LastMsgLength - s.bytesReceived
}

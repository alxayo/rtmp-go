package rpc

import (
	"fmt"
	"sync"

	"github.com/alxayo/go-rtmp/internal/errors"
	"github.com/alxayo/go-rtmp/internal/rtmp/amf"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// StreamIDAllocator provides a simple, concurrency-safe incremental allocator
// for RTMP message stream IDs. The RTMP spec allows the server to choose the
// stream ID returned by createStream; most simple implementations start at 1
// and increment by 1 for each new logical stream.
//
// This lightweight allocator keeps implementation local to the response
// builder. If broader session management later centralises stream tracking,
// this can be replaced transparently by passing a different Allocate func.
type StreamIDAllocator struct {
	mu   sync.Mutex
	next uint32
}

// NewStreamIDAllocator returns an allocator whose first Allocate() call
// returns 1 (the conventional first stream ID).
func NewStreamIDAllocator() *StreamIDAllocator { return &StreamIDAllocator{next: 1} }

// Allocate returns the next stream ID.
func (a *StreamIDAllocator) Allocate() uint32 {
	a.mu.Lock()
	id := a.next
	a.next++
	a.mu.Unlock()
	return id
}

// BuildCreateStreamResponse constructs the standard _result response to a
// createStream command. AMF0 sequence:
// ["_result", transactionID, null, streamID]
//
// The returned message is an AMF0 Command Message (TypeID=20) with
// MessageStreamID=0 (connection-level). CSID selection is deferred to the
// chunk writer layer.
//
// Errors are wrapped as protocol errors with a component key of
// "createstream.response.encode".
func BuildCreateStreamResponse(transactionID float64, allocator *StreamIDAllocator) (*chunk.Message, uint32, error) {
	if allocator == nil {
		// Defensive: enforce non-nil allocator to avoid hidden global state.
		return nil, 0, errors.NewProtocolError("createstream.response", fmt.Errorf("nil allocator"))
	}
	streamID := allocator.Allocate()

	payload, err := amf.EncodeAll(
		"_result",         // command name
		transactionID,     // original transaction id
		nil,               // null per spec
		float64(streamID), // stream id as AMF0 number
	)
	if err != nil {
		return nil, 0, errors.NewProtocolError("createstream.response.encode", fmt.Errorf("amf encode: %w", err))
	}

	msg := &chunk.Message{
		TypeID:          commandMessageAMF0TypeID,
		MessageStreamID: 0, // still connection-level
		Payload:         payload,
		MessageLength:   uint32(len(payload)),
	}
	return msg, streamID, nil
}

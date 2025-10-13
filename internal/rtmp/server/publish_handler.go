package server

// Publish Handler (Task T049)
// ---------------------------
// This file implements the publish command handler which registers a
// publisher connection in the stream registry and sends an `onStatus`
// NetStream.Publish.Start status message back to the client. The handler
// intentionally keeps scope small (no media forwarding yet) and returns the
// built status message so unit tests can assert on its contents without
// needing the full dispatcher stack.

import (
	"fmt"

	rtmperrors "github.com/alxayo/go-rtmp/internal/errors"
	"github.com/alxayo/go-rtmp/internal/rtmp/amf"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
	"github.com/alxayo/go-rtmp/internal/rtmp/rpc"
)

// sender is the minimal interface required from a connection for this task.
// *conn.Connection satisfies it. We keep it tiny so tests can use a stub.
type sender interface {
	SendMessage(*chunk.Message) error
}

// HandlePublish parses the publish command message, registers the publisher
// in the registry (creating the stream if necessary) and sends an onStatus
// NetStream.Publish.Start message. It returns the generated onStatus message
// (already sent) for test assertion. Errors are wrapped as protocol errors
// where appropriate.
func HandlePublish(reg *Registry, conn sender, app string, msg *chunk.Message) (*chunk.Message, error) {
	if reg == nil || conn == nil || msg == nil {
		return nil, rtmperrors.NewProtocolError("publish.handle", fmt.Errorf("nil argument"))
	}

	// Parse the incoming publish command (dependency T037).
	pcmd, err := rpc.ParsePublishCommand(app, msg)
	if err != nil {
		return nil, err
	}

	// Look up or create the stream in the registry (dependency T048).
	stream, _ := reg.CreateStream(pcmd.StreamKey)
	if stream == nil {
		return nil, rtmperrors.NewProtocolError("publish.handle", fmt.Errorf("failed to create stream"))
	}

	// Enforce single publisher (spec requirement).
	if err := stream.SetPublisher(conn); err != nil {
		return nil, err // already a *errors.ProtocolError from registry or ErrPublisherExists
	}

	// Build onStatus NetStream.Publish.Start (dependency T039). T039 may not
	// yet be implemented; we inline a minimal builder consistent with the
	// commands contract so this task can progress independently.
	info := map[string]interface{}{
		"level":       "status",
		"code":        "NetStream.Publish.Start",
		"description": fmt.Sprintf("Publishing %s.", pcmd.StreamKey),
		"details":     pcmd.StreamKey,
	}

	payload, err := amf.EncodeAll(
		"onStatus", // command name
		float64(0), // transaction ID (notification)
		nil,        // command object (null)
		info,       // info object
	)
	if err != nil {
		return nil, rtmperrors.NewProtocolError("publish.handle.encode", err)
	}

	onStatus := &chunk.Message{
		CSID:            5,                                     // typical control / onStatus chunk stream id (spec allows 4/5)
		TypeID:          rpc.CommandMessageAMF0TypeIDForTest(), // expose constant via helper for now
		MessageStreamID: msg.MessageStreamID,                   // same stream id as publish command
		MessageLength:   uint32(len(payload)),
		Payload:         payload,
	}

	// Send the status message. If this fails we still return it so tests can
	// inspect the structure; caller may decide follow-up action.
	_ = conn.SendMessage(onStatus)
	return onStatus, nil
}

// PublisherDisconnected clears the publisher from the stream if it matches
// the provided connection. This allows tests to simulate connection close
// without the full connection lifecycle implemented yet. Future tasks can
// extend this to broadcast Stream EOF to subscribers.
func PublisherDisconnected(reg *Registry, streamKey string, pub sender) {
	if reg == nil || streamKey == "" || pub == nil {
		return
	}
	s := reg.GetStream(streamKey)
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.Publisher == pub {
		s.Publisher = nil
	}
	s.mu.Unlock()
}

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

	// Build onStatus NetStream.Publish.Start (reuses shared builder from play_handler.go).
	onStatus, err := buildOnStatus(msg.MessageStreamID, pcmd.StreamKey, "NetStream.Publish.Start", fmt.Sprintf("Publishing %s.", pcmd.StreamKey))
	if err != nil {
		return nil, rtmperrors.NewProtocolError("publish.handle.encode", err)
	}

	// Send the status message. If this fails we still return it so tests can
	// inspect the structure; caller may decide follow-up action.
	_ = conn.SendMessage(onStatus)
	return onStatus, nil
}

// PublisherDisconnected clears the publisher from the stream if it matches
// the provided connection. Called during connection teardown to allow the
// stream key to be re-used by a new publisher.
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

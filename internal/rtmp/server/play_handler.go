package server

// Play Handler (Task T050)
// ------------------------
// Subscribes a client connection to an existing published stream. Mirrors the
// lightweight approach used in publish_handler.go: minimal parsing, registry
// lookups and onStatus/control message construction without depending on the
// yet-to-be-integrated full dispatcher/connection stack. Returns the final
// onStatus message (already sent) for test assertions.

import (
	"fmt"

	rtmperrors "github.com/alxayo/go-rtmp/internal/errors"
	"github.com/alxayo/go-rtmp/internal/rtmp/amf"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
	"github.com/alxayo/go-rtmp/internal/rtmp/control"
	"github.com/alxayo/go-rtmp/internal/rtmp/rpc"
)

// HandlePlay parses the incoming play command (msg) and attempts to subscribe
// the connection to the target stream. It sends (in order):
//  1. onStatus NetStream.Play.StreamNotFound  (if missing stream or publisher) OR
//  1. User Control Stream Begin (event 0)
//  2. onStatus NetStream.Play.Start
//
// Only the final onStatus (either StreamNotFound or Play.Start) is returned.
func HandlePlay(reg *Registry, conn sender, app string, msg *chunk.Message) (*chunk.Message, error) {
	if reg == nil || conn == nil || msg == nil {
		return nil, rtmperrors.NewProtocolError("play.handle", fmt.Errorf("nil argument"))
	}

	pcmd, err := rpc.ParsePlayCommand(msg, app) // dependency T038
	if err != nil {
		return nil, err
	}

	stream := reg.GetStream(pcmd.StreamKey)
	if stream == nil || stream.Publisher == nil { // not found or no active publisher
		// Build and send StreamNotFound onStatus (dependency T039 pattern - inline builder).
		notFound, _ := buildOnStatus(msg.MessageStreamID, pcmd.StreamKey, "NetStream.Play.StreamNotFound", fmt.Sprintf("Stream %s not found.", pcmd.StreamKey))
		_ = conn.SendMessage(notFound)
		return notFound, nil
	}

	// Add subscriber (connection implements sender -> minimal interface; tests use stub implementing SendMessage).
	stream.AddSubscriber(conn.(interface{ SendMessage(*chunk.Message) error }))

	// 1. User Control Stream Begin (event 0) with the play command's message stream id.
	uc := control.EncodeUserControlStreamBegin(msg.MessageStreamID)
	_ = conn.SendMessage(uc)

	// 2. onStatus NetStream.Play.Start
	started, err := buildOnStatus(msg.MessageStreamID, pcmd.StreamKey, "NetStream.Play.Start", fmt.Sprintf("Started playing %s.", pcmd.StreamKey))
	if err != nil {
		return nil, rtmperrors.NewProtocolError("play.handle.encode", err)
	}
	_ = conn.SendMessage(started)
	return started, nil
}

// buildOnStatus creates an AMF0 onStatus message consistent with the pattern used
// in publish_handler.go (we replicate instead of factoring early to keep task scope small).
func buildOnStatus(streamID uint32, streamKey, code, description string) (*chunk.Message, error) {
	info := map[string]interface{}{
		"level":       "status",
		"code":        code,
		"description": description,
		"details":     streamKey,
	}
	payload, err := amf.EncodeAll("onStatus", float64(0), nil, info)
	if err != nil {
		return nil, err
	}
	return &chunk.Message{
		CSID:            5,
		TypeID:          rpc.CommandMessageAMF0TypeIDForTest(),
		MessageStreamID: streamID,
		MessageLength:   uint32(len(payload)),
		Payload:         payload,
	}, nil
}

// SubscriberDisconnected removes the subscriber from the stream's list (if present).
// This mirrors PublisherDisconnected for symmetry and test support.
func SubscriberDisconnected(reg *Registry, streamKey string, sub sender) {
	if reg == nil || streamKey == "" || sub == nil {
		return
	}
	s := reg.GetStream(streamKey)
	if s == nil {
		return
	}
	// The registry Stream has a RemoveSubscriber helper added in this task.
	if rs, ok := any(sub).(interface{ SendMessage(*chunk.Message) error }); ok {
		_ = rs // unused – presence ensures type alignment
	}
	// Convert to media.Subscriber via duck typing: it only needs SendMessage(*chunk.Message) error.
	s.RemoveSubscriber(sub.(interface{ SendMessage(*chunk.Message) error }))
}

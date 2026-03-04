// helpers_test.go – shared test doubles and builders for the server package tests.
//
// These helpers are used across publish_handler_test.go, play_handler_test.go,
// and other server tests to avoid duplicating stub types and message builders.
package server

import (
	"github.com/alxayo/go-rtmp/internal/rtmp/amf"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
	"github.com/alxayo/go-rtmp/internal/rtmp/rpc"
)

// stubConn captures the last message sent via SendMessage for assertions.
type stubConn struct{ last *chunk.Message }

func (s *stubConn) SendMessage(m *chunk.Message) error { s.last = m; return nil }

// capturingConn records every message sent via SendMessage.
type capturingConn struct{ sent []*chunk.Message }

func (c *capturingConn) SendMessage(m *chunk.Message) error { c.sent = append(c.sent, m); return nil }

// stubPublisher is a minimal placeholder to mark a stream as published.
type stubPublisher struct{}

// buildPublishMessage builds a minimal AMF0 "publish" command message
// for the given stream name.
func buildPublishMessage(streamName string) *chunk.Message {
	payload, _ := amf.EncodeAll("publish", float64(0), nil, streamName, "live")
	return &chunk.Message{TypeID: rpc.CommandMessageAMF0TypeIDForTest(), Payload: payload, MessageLength: uint32(len(payload)), MessageStreamID: 1}
}

// buildPlayMessage constructs a minimal AMF0 "play" command message.
func buildPlayMessage(streamName string) *chunk.Message {
	payload, _ := amf.EncodeAll("play", float64(0), nil, streamName)
	return &chunk.Message{TypeID: rpc.CommandMessageAMF0TypeIDForTest(), Payload: payload, MessageLength: uint32(len(payload)), MessageStreamID: 1}
}

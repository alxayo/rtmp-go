// handler_test.go – tests for RTMP control message handling logic.
//
// Handle() receives decoded control messages and updates the corresponding
// connection-level state pointers (read chunk size, window ack size, etc.)
// and may send outbound responses (e.g., PingResponse to a PingRequest).
//
// Testing strategy:
//   - captureSender: a fake sender that records outbound messages in a slice,
//     allowing tests to inspect what Handle() sent without a real network.
//   - State mutation: each test verifies the correct pointer was updated.
//   - Error paths: nil context, nil fields → must return an error.
package control

import (
	"testing"

	"log/slog"

	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// captureSender is a test double that collects outbound messages.
// If err is non-nil, all sends fail with that error.
type captureSender struct {
	msgs []*chunk.Message
	err  error
}

// send appends the message to the captured list (or returns err if set).
func (c *captureSender) send(m *chunk.Message) error {
	if c.err != nil {
		return c.err
	}
	c.msgs = append(c.msgs, m)
	return nil
}

// TestHandle_ControlMessages_StateUpdates feeds four control messages through
// Handle() in sequence and verifies each one mutates the right Context field:
//  1. SetChunkSize → ReadChunkSize = 4096
//  2. WindowAcknowledgementSize → WindowAckSize = 2,500,000
//  3. SetPeerBandwidth → PeerBandwidth = 2,500,000, LimitType = 2
//  4. Acknowledgement → LastPeerAck = 1,000,000
func TestHandle_ControlMessages_StateUpdates(t *testing.T) {
	readChunkSize := uint32(128)
	windowAckSize := uint32(0)
	peerBandwidth := uint32(0)
	limitType := uint8(0)
	lastAck := uint32(0)

	cs := &captureSender{}
	ctx := &Context{
		ReadChunkSize: &readChunkSize,
		WindowAckSize: &windowAckSize,
		PeerBandwidth: &peerBandwidth,
		LimitType:     &limitType,
		LastPeerAck:   &lastAck,
		Log:           slog.Default(),
		Send:          cs.send,
	}

	// 1. Set Chunk Size
	m1 := EncodeSetChunkSize(4096)
	if err := Handle(ctx, m1); err != nil {
		(t).Fatalf("handle set chunk size: %v", err)
	}
	if readChunkSize != 4096 {
		(t).Fatalf("readChunkSize not updated got=%d", readChunkSize)
	}

	// 2. Window Ack Size
	m2 := EncodeWindowAcknowledgementSize(2_500_000)
	if err := Handle(ctx, m2); err != nil {
		(t).Fatalf("handle window ack size: %v", err)
	}
	if windowAckSize != 2_500_000 {
		(t).Fatalf("windowAckSize not updated got=%d", windowAckSize)
	}

	// 3. Set Peer Bandwidth
	m3 := EncodeSetPeerBandwidth(2_500_000, 2)
	if err := Handle(ctx, m3); err != nil {
		(t).Fatalf("handle set peer bandwidth: %v", err)
	}
	if peerBandwidth != 2_500_000 || limitType != 2 {
		(t).Fatalf("peer bandwidth fields mismatch bw=%d lt=%d", peerBandwidth, limitType)
	}

	// 4. Acknowledgement
	m4 := EncodeAcknowledgement(1_000_000)
	if err := Handle(ctx, m4); err != nil {
		(t).Fatalf("handle acknowledgement: %v", err)
	}
	if lastAck != 1_000_000 {
		(t).Fatalf("lastAck mismatch got=%d", lastAck)
	}
}

// TestHandle_UserControl_PingRequestResponse verifies the RTMP ping
// round-trip: Handle() receives a PingRequest and automatically sends
// back a PingResponse with the same timestamp echoed.
func TestHandle_UserControl_PingRequestResponse(t *testing.T) {
	readChunkSize := uint32(128)
	windowAckSize := uint32(0)
	peerBandwidth := uint32(0)
	limitType := uint8(0)
	lastAck := uint32(0)
	cs := &captureSender{}
	ctx := &Context{&readChunkSize, &windowAckSize, &peerBandwidth, &limitType, &lastAck, slog.Default(), cs.send}

	const ts = 123456
	pingReq := EncodeUserControlPingRequest(ts)
	if err := Handle(ctx, pingReq); err != nil {
		(t).Fatalf("handle ping request: %v", err)
	}
	if len(cs.msgs) != 1 {
		(t).Fatalf("expected 1 outbound message got=%d", len(cs.msgs))
	}
	resp := cs.msgs[0]
	if resp.TypeID != TypeUserControl || len(resp.Payload) != 6 || resp.Payload[1] != byte(UCPingResponse) {
		(t).Fatalf("unexpected ping response payload: % X", resp.Payload)
	}
	// verify timestamp echo
	if ts != uint32(resp.Payload[2])<<24|uint32(resp.Payload[3])<<16|uint32(resp.Payload[4])<<8|uint32(resp.Payload[5]) {
		(t).Fatalf("timestamp not echoed in ping response: % X", resp.Payload[2:])
	}
}

// TestHandle_Errors exercises defensive checks: nil Context and a Context
// with nil pointer fields must both return errors rather than panicking.
func TestHandle_Errors(t *testing.T) {
	// Nil context
	if err := Handle(nil, &chunk.Message{}); err == nil {
		(t).Fatalf("expected error for nil context")
	}
	// Bad context (nil field)
	ctx := &Context{}
	if err := Handle(ctx, &chunk.Message{}); err == nil {
		(t).Fatalf("expected error for invalid context")
	}
}

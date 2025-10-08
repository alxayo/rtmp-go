package control

// T024: Control Message Handlers
// --------------------------------
// Provides handler logic that consumes already reassembled RTMP control
// messages (types 1-6) and mutates caller supplied state. We keep this
// package decoupled from the higher level \"conn\" package to avoid an
// import cycle (conn will call into control handlers). Instead we expose a
// generic Context object composed of pointers to the mutable state fields
// and a Send function for emitting response control messages (e.g. Ping
// Response).
//
// Design goals:
//   * Pure functions over explicit state (easy to test)
//   * No hidden global vars
//   * Wire format parsing delegated to decoder (T023)
//   * Emission of outbound control messages delegated to encoder (T022)
//
// Integration note (future tasks): The connection read loop should build a
// Context per connection populating fields backed by the real connection
// struct (readChunkSize, windowAckSize, peerBandwidth, etc.).

import (
	"fmt"
	"log/slog"

	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// Context carries mutable control-related state for a single RTMP
// connection. All pointer fields are required (nil panics are considered
// programmer errors – caught early in tests). Send MUST be non-nil.
// The naming mirrors the contract fields (readChunkSize, windowAckSize,
// peerBandwidth, limitType) so higher layers can wire them directly.
type Context struct {
	ReadChunkSize *uint32
	WindowAckSize *uint32
	PeerBandwidth *uint32
	LimitType     *uint8
	LastPeerAck   *uint32 // optional tracking of most recent peer ACK (sequence number)
	Log           *slog.Logger
	Send          func(*chunk.Message) error // used to emit Ping Response (and future control msgs)
}

// Handle processes a single control *chunk.Message* (types 1-6). It decodes
// the payload, mutates context state, and may emit response control
// messages (e.g. Ping Response). Non-control messages return an error to
// allow the caller to route/ignore appropriately.
func Handle(ctx *Context, msg *chunk.Message) error {
	if ctx == nil || ctx.ReadChunkSize == nil || ctx.WindowAckSize == nil || ctx.PeerBandwidth == nil || ctx.LimitType == nil || ctx.Send == nil {
		return fmt.Errorf("control handler: invalid context (nil field)")
	}
	if msg == nil {
		return fmt.Errorf("control handler: nil message")
	}
	decoded, err := Decode(msg.TypeID, msg.Payload)
	if err != nil {
		return fmt.Errorf("control handler decode: %w", err)
	}

	switch v := decoded.(type) {
	case *SetChunkSize:
		old := *ctx.ReadChunkSize
		*ctx.ReadChunkSize = v.Size
		if ctx.Log != nil {
			ctx.Log.Debug("Set Chunk Size received", "old", old, "new", v.Size)
		}
	case *Acknowledgement:
		if ctx.LastPeerAck != nil {
			*ctx.LastPeerAck = v.SequenceNumber
		}
		if ctx.Log != nil {
			ctx.Log.Debug("Acknowledgement received", "seq", v.SequenceNumber)
		}
	case *UserControl:
		// Only subset of events required for current phase.
		switch v.EventType {
		case UCStreamBegin:
			if ctx.Log != nil {
				ctx.Log.Info("User Control: Stream Begin", "stream_id", v.StreamID)
			}
		case UCPingRequest:
			// Respond with Ping Response echoing timestamp
			if ctx.Log != nil {
				ctx.Log.Debug("Ping Request received", "ts", v.Timestamp)
			}
			resp := EncodeUserControlPingResponse(v.Timestamp)
			if err := ctx.Send(resp); err != nil {
				return fmt.Errorf("control handler: send ping response: %w", err)
			}
		case UCPingResponse:
			// Latency measurement hook (not implemented – just log at debug level)
			if ctx.Log != nil {
				ctx.Log.Debug("Ping Response received", "ts", v.Timestamp)
			}
		default:
			if ctx.Log != nil {
				ctx.Log.Debug("User Control: unhandled event", "event_type", v.EventType)
			}
		}
	case *WindowAcknowledgementSize:
		old := *ctx.WindowAckSize
		*ctx.WindowAckSize = v.Size
		if ctx.Log != nil {
			ctx.Log.Debug("Window Ack Size received", "old", old, "new", v.Size)
		}
	case *SetPeerBandwidth:
		oldBW, oldLT := *ctx.PeerBandwidth, *ctx.LimitType
		*ctx.PeerBandwidth = v.Bandwidth
		*ctx.LimitType = v.LimitType
		if ctx.Log != nil {
			ctx.Log.Debug("Set Peer Bandwidth received", "old_bw", oldBW, "new_bw", v.Bandwidth, "old_lt", oldLT, "new_lt", v.LimitType)
		}
	case *AbortMessage:
		// Currently we don't buffer partial messages (chunk layer handles abort semantics).
		if ctx.Log != nil {
			ctx.Log.Debug("Abort Message received (ignored in this phase)", "csid", v.CSID)
		}
	default:
		return fmt.Errorf("control handler: unexpected decoded type %T", v)
	}
	return nil
}

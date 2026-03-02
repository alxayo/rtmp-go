package conn

// Control Burst Sequence
// =====================
// After a successful RTMP handshake, the server immediately sends three
// control messages to configure the connection parameters:
//   1. Window Acknowledgement Size — flow control (client must ack after N bytes)
//   2. Set Peer Bandwidth — suggests the client's max output rate
//   3. Set Chunk Size — increases chunk payload from default 128 to 4096 bytes

import (
	"encoding/binary"
	"fmt"
	"sync/atomic"

	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
	"github.com/alxayo/go-rtmp/internal/rtmp/control"
)

const (
	windowAckSizeValue     uint32 = 2_500_000 // bytes before client must send acknowledgement
	peerBandwidthValue     uint32 = 2_500_000 // suggested maximum output rate for client
	peerBandwidthLimitType        = 2         // Dynamic: client may adjust this value
	serverChunkSize        uint32 = 4096      // negotiated chunk size (up from protocol default of 128)
)

// sendInitialControlBurst performs the control burst by enqueuing messages
// to the connection's outbound queue. It is invoked asynchronously by Accept().
// A best-effort approach is used: the first encountered error aborts the
// remaining sends (subsequent tasks may choose to retry / degrade gracefully).
func sendInitialControlBurst(c *Connection) error {
	if c == nil {
		return fmt.Errorf("control burst: nil connection")
	}

	// Build messages in required order.
	msgs := []*chunk.Message{
		control.EncodeWindowAcknowledgementSize(windowAckSizeValue),
		control.EncodeSetPeerBandwidth(peerBandwidthValue, peerBandwidthLimitType),
		control.EncodeSetChunkSize(serverChunkSize),
	}

	for _, m := range msgs {
		// Debug log the message being sent
		c.log.Debug("Control burst sending", "type_id", m.TypeID, "csid", m.CSID, "msid", m.MessageStreamID, "payload_len", len(m.Payload))

		// Use SendMessage to properly enqueue messages through the writeLoop
		if err := c.SendMessage(m); err != nil {
			return fmt.Errorf("control burst enqueue type=%d: %w", m.TypeID, err)
		}
		// Per message logging with concise metadata.
		switch m.TypeID {
		case control.TypeWindowAcknowledgement:
			if len(m.Payload) == 4 {
				c.log.Info("Control sent: Window Acknowledgement Size", "size", binary.BigEndian.Uint32(m.Payload))
			} else {
				c.log.Info("Control sent: Window Acknowledgement Size")
			}
		case control.TypeSetPeerBandwidth:
			if len(m.Payload) == 5 {
				bw := binary.BigEndian.Uint32(m.Payload[:4])
				c.log.Info("Control sent: Set Peer Bandwidth", "bandwidth", bw, "limit_type", m.Payload[4])
			} else {
				c.log.Info("Control sent: Set Peer Bandwidth")
			}
		case control.TypeSetChunkSize:
			if len(m.Payload) == 4 {
				newSize := binary.BigEndian.Uint32(m.Payload)
				c.log.Info("Control sent: Set Chunk Size", "size", newSize)
				// CRITICAL: Update the connection's write chunk size to match what we told the peer
				atomic.StoreUint32(&c.writeChunkSize, newSize)
			} else {
				c.log.Info("Control sent: Set Chunk Size")
			}
		default:
			c.log.Info("Control sent", "type_id", m.TypeID)
		}
	}
	return nil
}

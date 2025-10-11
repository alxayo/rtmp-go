package conn

// T025: Implement Control Burst Sequence
// --------------------------------------
// After a successful RTMP handshake the server must immediately send (in this
// strict order) three protocol control messages on CSID=2 / MSID=0:
//   1. Window Acknowledgement Size (2,500,000 bytes)
//   2. Set Peer Bandwidth        (2,500,000 bytes, limit type 2 = Dynamic)
//   3. Set Chunk Size            (4,096 bytes)
//
// Requirements:
//   * Messages are sent sequentially but the burst itself runs in a goroutine
//     (Accept remains non-blocking after handshake completion).
//   * Each message is logged.
//   * Future tasks will integrate connection state mutation; for now we only
//     emit wire-format messages.

import (
	"encoding/binary"
	"fmt"

	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
	"github.com/alxayo/go-rtmp/internal/rtmp/control"
)

const (
	windowAckSizeValue     uint32 = 2_500_000
	peerBandwidthValue     uint32 = 2_500_000
	peerBandwidthLimitType        = 2 // Dynamic
	serverChunkSize        uint32 = 4096
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
				c.writeChunkSize = newSize
			} else {
				c.log.Info("Control sent: Set Chunk Size")
			}
		default:
			c.log.Info("Control sent", "type_id", m.TypeID)
		}
	}
	return nil
}

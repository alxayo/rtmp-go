// control_burst_test.go – verifies the RTMP "control burst" sent by the
// server immediately after the handshake completes.
//
// Per the RTMP spec, the server must send three control messages before any
// application-level commands:
//  1. Window Acknowledgement Size (type 5) – tells the client how often
//     to send ACKs.
//  2. Set Peer Bandwidth (type 6) – tells the client to limit its send rate.
//  3. Set Chunk Size (type 1) – negotiates a larger chunk size (4096).
//
// All three must be on CSID 2 with MSID 0 (the protocol control stream).
package conn

import (
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
	"github.com/alxayo/go-rtmp/internal/rtmp/control"
	"github.com/alxayo/go-rtmp/internal/rtmp/handshake"
)

// dialAndHandshake is a local copy of the handshake helper (same package,
// separate file – Go allows this within the same test package).
func dialAndHandshake(t *testing.T, addr string) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if err := handshake.ClientHandshake(c); err != nil {
		t.Fatalf("client handshake: %v", err)
	}
	return c
}

// TestControlBurstSequence performs a full handshake then reads 3 control
// messages from the server, verifying type, CSID, MSID, and payload values:
//   - Window Ack Size = windowAckSizeValue (from conn constants)
//   - Peer Bandwidth = peerBandwidthValue with peerBandwidthLimitType
//   - Chunk Size = serverChunkSize (typically 4096)
func TestControlBurstSequence(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	// Start accept in background.
	acceptCh := make(chan *Connection, 1)
	errCh := make(chan error, 1)
	go func() {
		c, err := Accept(ln)
		if err != nil {
			errCh <- err
			return
		}
		acceptCh <- c
	}()

	client := dialAndHandshake(t, ln.Addr().String())
	defer client.Close()

	// Wait for server connection (handshake done).
	var serverConn *Connection
	select {
	case serverConn = <-acceptCh:
	case err := <-errCh:
		t.Fatalf("accept error: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for accept")
	}
	defer serverConn.Close()

	// Reader to parse three control messages emitted by burst.
	r := chunk.NewReader(client, 128)
	wantTypes := []uint8{control.TypeWindowAcknowledgement, control.TypeSetPeerBandwidth, control.TypeSetChunkSize}
	for i, want := range wantTypes {
		// Guard against hang if burst failed.
		_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
		msg, err := r.ReadMessage()
		if err != nil {
			t.Fatalf("read message %d: %v", i, err)
		}
		if msg.TypeID != want {
			t.Fatalf("message %d wrong type got=%d want=%d", i, msg.TypeID, want)
		}
		if msg.CSID != 2 || msg.MessageStreamID != 0 {
			t.Fatalf("message %d control channel invariants violated csid=%d msid=%d", i, msg.CSID, msg.MessageStreamID)
		}
		switch want {
		case control.TypeWindowAcknowledgement:
			if len(msg.Payload) != 4 || binary.BigEndian.Uint32(msg.Payload) != windowAckSizeValue {
				t.Fatalf("WAS payload mismatch: % X", msg.Payload)
			}
		case control.TypeSetPeerBandwidth:
			if len(msg.Payload) != 5 || binary.BigEndian.Uint32(msg.Payload[:4]) != peerBandwidthValue || msg.Payload[4] != peerBandwidthLimitType {
				t.Fatalf("SPB payload mismatch: % X", msg.Payload)
			}
		case control.TypeSetChunkSize:
			if len(msg.Payload) != 4 || binary.BigEndian.Uint32(msg.Payload) != serverChunkSize {
				t.Fatalf("SCS payload mismatch: % X", msg.Payload)
			}
		}
	}
}

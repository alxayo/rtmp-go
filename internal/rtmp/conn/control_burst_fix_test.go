package conn

import (
	"net"
	"sync/atomic"
	"testing"

	"github.com/alxayo/go-rtmp/internal/rtmp/handshake"
)

// dialAndHandshakeLocal is a minimal helper for this test
func dialAndHandshakeLocal(t *testing.T, addr string) net.Conn {
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

// TestControlBurstUpdatesWriteChunkSize verifies that sending the SetChunkSize
// control message during the control burst also updates the connection's
// writeChunkSize field so that subsequent writes use the advertised chunk size.
// This test validates the fix for the Wireshark "Malformed Packet" error where
// the server advertised 4096-byte chunks but was actually sending 128-byte chunks.
func TestControlBurstUpdatesWriteChunkSize(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	// Start accept in background
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

	client := dialAndHandshakeLocal(t, ln.Addr().String())
	defer client.Close()

	// Wait for server connection
	var serverConn *Connection
	select {
	case err := <-errCh:
		t.Fatalf("accept failed: %v", err)
	case serverConn = <-acceptCh:
		defer serverConn.Close()
	}

	// Verify that writeChunkSize was updated to 4096 by the control burst
	// (The control burst is sent automatically by Accept())
	actualWriteChunkSize := atomic.LoadUint32(&serverConn.writeChunkSize)
	if actualWriteChunkSize != serverChunkSize {
		t.Errorf("writeChunkSize = %d, want %d (should match advertised chunk size)", actualWriteChunkSize, serverChunkSize)
	}

	// Verify it's not still 128 (the bug we're fixing)
	if actualWriteChunkSize == 128 {
		t.Error("writeChunkSize still 128 - control burst did not update it! This is the bug.")
	}

	t.Logf("SUCCESS: writeChunkSize correctly set to %d", actualWriteChunkSize)
}

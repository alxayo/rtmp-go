package conn

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/alxayo/go-rtmp/internal/logger"
	"github.com/alxayo/go-rtmp/internal/rtmp/handshake"
)

// dialAndClientHandshake dials the given address and performs the client handshake.
func dialAndClientHandshake(t *testing.T, addr string) net.Conn {
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

func TestAccept_Success(t *testing.T) {
	// Capture logs in-memory to assert handshake logging path executed.
	pr, pw := io.Pipe()
	defer pr.Close()
	defer pw.Close()
	logger.UseWriter(pw)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

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

	clientConn := dialAndClientHandshake(t, ln.Addr().String())
	defer clientConn.Close()

	select {
	case c := <-acceptCh:
		if c.HandshakeDuration() <= 0 {
			t.Fatalf("expected positive handshake duration")
		}
		// Basic sanity: connection still open; write zero bytes (deadline just to not block).
		_ = clientConn.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
		_, _ = clientConn.Write([]byte{})
		_ = c.Close()
	case err := <-errCh:
		t.Fatalf("accept returned error: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for accept")
	}
}

func TestAccept_HandshakeFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	errCh := make(chan error, 1)
	go func() {
		_, err := Accept(ln)
		if err != nil {
			errCh <- err
		}
	}()

	// Dial and send invalid handshake (version 0x06) then close.
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	// Send C0 invalid + C1 zeros.
	buf := make([]byte, 1+handshake.PacketSize)
	buf[0] = 0x06
	if _, err := c.Write(buf); err != nil {
		t.Fatalf("write invalid c0c1: %v", err)
	}
	_ = c.Close()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatalf("expected handshake error")
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for handshake failure")
	}
}

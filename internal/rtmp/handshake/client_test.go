package handshake

import (
	"io"
	"net"
	"testing"
	"time"

	rerrors "github.com/alxayo/go-rtmp/internal/errors"
)

// TestClientHandshake_Valid performs a full round-trip with the real server handshake.
func TestClientHandshake_Valid(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	errCh := make(chan error, 1)
	go func() { errCh <- ServerHandshake(serverConn) }()

	if err := ClientHandshake(clientConn); err != nil {
		t.Fatalf("client handshake failed: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("server failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for server completion")
	}
}

// Simulated server that sends invalid version in S0.
func TestClientHandshake_InvalidVersion(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	go func() {
		// Read C0+C1
		buf := make([]byte, 1+PacketSize)
		if _, err := io.ReadFull(serverConn, buf); err != nil {
			return
		}
		// Write invalid S0 + S1
		out := make([]byte, 1+PacketSize)
		out[0] = 0x06 // invalid
		copy(out[1:], make([]byte, PacketSize))
		_, _ = serverConn.Write(out)
		// Do not send S2
	}()

	err := ClientHandshake(clientConn)
	if err == nil || !rerrors.IsProtocolError(err) {
		t.Fatalf("expected protocol error, got %v", err)
	}
}

// Server sends partial S0 then stalls inducing timeout.
func TestClientHandshake_TruncatedS1(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	go func() {
		buf := make([]byte, 1+PacketSize)
		if _, err := io.ReadFull(serverConn, buf); err != nil {
			return
		}
		// Write only S0 (1 byte) then close after delay to allow timeout path
		_, _ = serverConn.Write([]byte{Version})
		// leave connection open until client times out
	}()

	err := ClientHandshake(clientConn)
	if err == nil {
		t.Fatalf("expected timeout/protocol error")
	}
	if !rerrors.IsTimeout(err) && !rerrors.IsProtocolError(err) {
		t.Fatalf("unexpected error type: %v", err)
	}
}

// Force write failure from client side.
type failingWriteConn struct{ net.Conn }

func (f *failingWriteConn) Write(p []byte) (int, error) { return 0, io.ErrClosedPipe }

func TestClientHandshake_WriteFailure(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	fw := &failingWriteConn{clientConn}
	if err := ClientHandshake(fw); err == nil {
		t.Fatalf("expected write failure error")
	}
}

func TestClientHandshake_NilConn(t *testing.T) {
	if err := ClientHandshake(nil); err == nil {
		t.Fatalf("expected error for nil conn")
	}
}

// Provide mismatched S2 to exercise warning path but still succeed.
func TestClientHandshake_MismatchedS2(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	// Custom server implementing simple handshake minimally.
	errCh := make(chan error, 1)
	go func() {
		defer close(errCh)
		// Read C0+C1
		buf := make([]byte, 1+PacketSize)
		if _, err := io.ReadFull(serverConn, buf); err != nil {
			errCh <- err
			return
		}
		// Build S0+S1+S2 with WRONG S2 (zeros)
		out := make([]byte, 1+PacketSize+PacketSize)
		out[0] = Version
		// S1 random unused (zeros OK for test)
		copy(out[1:1+PacketSize], make([]byte, PacketSize))
		// S2 wrong (zeros already there)
		if _, err := serverConn.Write(out); err != nil {
			errCh <- err
			return
		}
		// Read C2 then done
		c2 := make([]byte, PacketSize)
		_, _ = io.ReadFull(serverConn, c2)
	}()

	if err := ClientHandshake(clientConn); err != nil {
		t.Fatalf("client handshake failed: %v", err)
	}
}

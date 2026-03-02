// client_test.go – tests for the client-side RTMP handshake.
//
// ClientHandshake() runs:
//
//	Send C0+C1 → Read S0+S1+S2 → Verify S0 version → Send C2=S1 → Done
//
// These tests cover:
//   - Happy path: paired with a real ServerHandshake on the other end.
//   - Invalid S0 version: fake server responds with 0x06.
//   - Truncated S1: fake server sends only S0 then stalls → timeout.
//   - Write failure: failingWriteConn returns io.ErrClosedPipe.
//   - Nil conn: should return error, not panic.
//   - Mismatched S2: fake server sends wrong S2 – client still succeeds.
//
// Key Go pattern: each test runs client + server in separate goroutines
// connected via net.Pipe(), with error channels for synchronization.
package handshake

import (
	"io"
	"net"
	"testing"
	"time"

	rerrors "github.com/alxayo/go-rtmp/internal/errors"
)

// TestClientHandshake_Valid pairs a real client and server handshake
// over net.Pipe and verifies both sides complete without error.
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

// TestClientHandshake_InvalidVersion starts a fake server that replies
// with S0 = 0x06 (invalid). ClientHandshake must return a protocol error.
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

// TestClientHandshake_TruncatedS1 starts a fake server that sends only
// S0 (1 byte) but never the full S1. ClientHandshake should timeout.
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

// failingWriteConn forces every Write call to fail – tests the error path
// when the client can't even send C0+C1.
type failingWriteConn struct{ net.Conn }

func (f *failingWriteConn) Write(p []byte) (int, error) { return 0, io.ErrClosedPipe }

// TestClientHandshake_WriteFailure wraps the conn so C0+C1 write fails.
func TestClientHandshake_WriteFailure(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	fw := &failingWriteConn{clientConn}
	if err := ClientHandshake(fw); err == nil {
		t.Fatalf("expected write failure error")
	}
}

// TestClientHandshake_NilConn ensures nil input returns an error cleanly.
func TestClientHandshake_NilConn(t *testing.T) {
	if err := ClientHandshake(nil); err == nil {
		t.Fatalf("expected error for nil conn")
	}
}

// TestClientHandshake_MismatchedS2 runs a custom fake server that sends
// an all-zero S2 (doesn't echo C1). The client should warn but still
// complete – matching the lenient behavior of real RTMP implementations.
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

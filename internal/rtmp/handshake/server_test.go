// server_test.go – integration-style tests for the server-side RTMP handshake.
//
// ServerHandshake() runs the full handshake on a net.Conn:
//
//	Read C0+C1 → Send S0+S1+S2 → Read C2 → Complete
//
// These tests use net.Pipe() for in-process TCP simulation and exercise:
//   - Happy path (valid C0+C1 + correct C2 echo).
//   - Invalid version (0x06 instead of 0x03).
//   - Truncated C1 (induces timeout).
//   - Mismatched C2 (should warn but still succeed – real clients diverge).
//   - Write failures (failingConn returning io.ErrClosedPipe).
//   - Read deadline / write deadline errors.
//   - Nil conn.
//
// Key Go concepts:
//   - net.Pipe: creates a synchronous in-memory conn pair for testing.
//   - io.ReadFull: reads exactly N bytes (used for 1536-byte packets).
//   - Goroutines + error channels for concurrent client/server testing.
//   - Custom conn wrappers (failingConn, deadlineFailConn, readBufConn)
//     to inject specific failures.
package handshake

import (
	"errors"
	"io"
	"net"
	"os"
	"testing"
	"time"

	rerrors "github.com/alxayo/go-rtmp/internal/errors"
)

// loadGolden loads a golden binary file from tests/golden/.
// If the file doesn't exist and it's the handshake C0+C1 vector, it
// auto-generates a minimal deterministic one to avoid flakiness.
func loadGolden(t *testing.T, name string) []byte {
	t.Helper()
	path := "../../../tests/golden/" + name
	b, err := os.ReadFile(path)
	if err == nil {
		return b
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read golden %s: %v", name, err)
	}
	// Auto-generate minimal deterministic golden if missing to avoid test flakiness in dev.
	if name == "handshake_valid_c0c1.bin" {
		buf := make([]byte, 1+PacketSize)
		buf[0] = Version
		// remaining bytes zero (valid simple handshake C1 shape)
		// Persist so subsequent runs use the file.
		_ = os.WriteFile(path, buf, 0o644)
		return buf
	}
	t.Fatalf("golden %s missing", name)
	return nil
}

// TestServerHandshake_Valid runs a complete valid handshake over net.Pipe:
// client sends C0+C1 from the golden file, reads back S0+S1+S2, verifies
// S2 echoes C1, then sends C2 = S1. Server goroutine must complete without error.
func TestServerHandshake_Valid(t *testing.T) {
	// Golden file contains C0+C1 (1+1536 bytes)
	c0c1 := loadGolden(t, "handshake_valid_c0c1.bin")
	if len(c0c1) != 1+PacketSize {
		t.Fatalf("unexpected golden length %d", len(c0c1))
	}

	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	errCh := make(chan error, 1)
	go func() { errCh <- ServerHandshake(serverConn) }()

	// Client sends C0+C1
	if _, err := clientConn.Write(c0c1); err != nil {
		t.Fatalf("write C0+C1: %v", err)
	}

	// Read S0+S1+S2
	sBuf := make([]byte, 1+PacketSize+PacketSize)
	if _, err := io.ReadFull(clientConn, sBuf); err != nil {
		t.Fatalf("read S0+S1+S2: %v", err)
	}
	if sBuf[0] != Version {
		t.Fatalf("expected S0 version 0x03 got 0x%02x", sBuf[0])
	}

	s1 := sBuf[1 : 1+PacketSize]
	s2 := sBuf[1+PacketSize:]
	c1 := c0c1[1:]
	if !bytesEqual(s2, c1) {
		t.Fatalf("S2 did not echo C1")
	}

	// Echo S1 back as C2
	if _, err := clientConn.Write(s1); err != nil {
		t.Fatalf("write C2: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("handshake failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for server handshake")
	}
}

// TestServerHandshake_InvalidVersion sends version byte 0x06 (not the
// required 0x03) and expects a protocol error from the server.
func TestServerHandshake_InvalidVersion(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	errCh := make(chan error, 1)
	go func() { errCh <- ServerHandshake(serverConn) }()

	// Send invalid version 0x06 + C1
	buf := make([]byte, 1+PacketSize)
	buf[0] = 0x06
	if _, err := clientConn.Write(buf); err != nil {
		t.Fatalf("write invalid C0+C1: %v", err)
	}

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatalf("expected error for invalid version")
		}
		if !rerrors.IsProtocolError(err) {
			t.Fatalf("expected protocol error got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("server did not respond with error in time")
	}
}

// TestServerHandshake_TruncatedC1 sends only C0 + 500 bytes of C1 (instead
// of the full 1536) and then stalls. The server's 5-second read deadline
// should fire and return a timeout or protocol error.
func TestServerHandshake_TruncatedC1(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	errCh := make(chan error, 1)
	go func() { errCh <- ServerHandshake(serverConn) }()

	// Send C0 + partial C1 then no more data (forces timeout)
	if _, err := clientConn.Write([]byte{Version}); err != nil {
		t.Fatalf("write C0: %v", err)
	}
	if _, err := clientConn.Write(make([]byte, 500)); err != nil {
		t.Fatalf("write partial C1: %v", err)
	}

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatalf("expected timeout/protocol error for truncated C1")
		}
		if !rerrors.IsTimeout(err) && !rerrors.IsProtocolError(err) {
			t.Fatalf("unexpected error type: %v", err)
		}
	case <-time.After(7 * time.Second):
		t.Fatalf("handshake did not timeout as expected")
	}
}

// TestServerHandshake_MismatchedC2Warn sends an all-zero C2 instead of
// echoing S1. The server should log a warning but still complete –
// real-world clients sometimes don't echo S1 exactly.
func TestServerHandshake_MismatchedC2Warn(t *testing.T) {
	// Valid path but send random (non-echo) C2 to exercise warning + success.
	c0c1 := make([]byte, 1+PacketSize)
	c0c1[0] = Version
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	errCh := make(chan error, 1)
	go func() { errCh <- ServerHandshake(serverConn) }()

	if _, err := clientConn.Write(c0c1); err != nil {
		t.Fatalf("write C0+C1: %v", err)
	}

	sBuf := make([]byte, 1+PacketSize+PacketSize)
	if _, err := io.ReadFull(clientConn, sBuf); err != nil {
		t.Fatalf("read S0+S1+S2: %v", err)
	}

	// Provide wrong C2 (all zero)
	if _, err := clientConn.Write(make([]byte, PacketSize)); err != nil {
		t.Fatalf("write mismatched C2: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("handshake should still succeed despite C2 mismatch: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for server")
	}
}

// failingConn wraps a net.Conn and forces Write to fail immediately with
// io.ErrClosedPipe – used to test the error path when sending S0+S1+S2.
type failingConn struct{ net.Conn }

func (f *failingConn) Write(p []byte) (int, error) { return 0, io.ErrClosedPipe }

// TestServerHandshake_WriteFailure provides a valid C0+C1 but uses
// failingConn so the server's Write(S0+S1+S2) fails.
func TestServerHandshake_WriteFailure(t *testing.T) {
	c0c1 := make([]byte, 1+PacketSize)
	c0c1[0] = Version
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	fc := &failingConn{serverConn}

	errCh := make(chan error, 1)
	go func() { errCh <- ServerHandshake(fc) }()

	if _, err := clientConn.Write(c0c1); err != nil {
		t.Fatalf("write C0+C1: %v", err)
	}

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatalf("expected error due to failing write")
		}
		if !rerrors.IsProtocolError(err) {
			t.Fatalf("expected protocol error got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for handshake failure")
	}
}

// TestServerHandshake_C2ReadError completes the handshake up through
// S0+S1+S2 but closes the client conn before sending C2. The server's
// read of C2 should fail with a protocol error.
func TestServerHandshake_C2ReadError(t *testing.T) {
	c0c1 := make([]byte, 1+PacketSize)
	c0c1[0] = Version
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	errCh := make(chan error, 1)
	go func() { errCh <- ServerHandshake(serverConn) }()

	if _, err := clientConn.Write(c0c1); err != nil {
		t.Fatalf("write C0+C1: %v", err)
	}

	// Read S0+S1+S2 then close before sending C2
	buf := make([]byte, 1+PacketSize+PacketSize)
	if _, err := io.ReadFull(clientConn, buf); err != nil {
		t.Fatalf("read S0+S1+S2: %v", err)
	}
	_ = clientConn.Close()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatalf("expected error for missing C2")
		}
		if !rerrors.IsProtocolError(err) {
			t.Fatalf("expected protocol error got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for handshake C2 read error")
	}
}

// TestServerHandshake_NilConn ensures that passing nil triggers a clean
// error rather than a nil-pointer panic.
func TestServerHandshake_NilConn(t *testing.T) {
	if err := ServerHandshake(nil); err == nil {
		t.Fatalf("expected error for nil conn")
	}
}

// deadlineFailConn wraps net.Conn and can selectively fail SetReadDeadline
// or SetWriteDeadline – used to test timeout-setup error branches.
type deadlineFailConn struct {
	net.Conn
	failRead  bool
	failWrite bool
}

func (d *deadlineFailConn) SetReadDeadline(t time.Time) error {
	if d.failRead {
		return io.ErrClosedPipe
	}
	return nil
}
func (d *deadlineFailConn) SetWriteDeadline(t time.Time) error {
	if d.failWrite {
		return io.ErrClosedPipe
	}
	return nil
}

// readBufConn provides predetermined bytes for Read, bypassing the real
// network. Useful for feeding valid C0+C1 data to trigger later failures
// (e.g. write deadline error).
type readBufConn struct {
	*deadlineFailConn
	buf []byte
}

func (r *readBufConn) Read(p []byte) (int, error) {
	if len(r.buf) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

// TestServerHandshake_SetReadDeadlineError verifies that a failed
// SetReadDeadline at the very start of the handshake returns an error.
func TestServerHandshake_SetReadDeadlineError(t *testing.T) {
	// Only triggers first setReadDeadline failure; no further ops.
	serverConn, clientConn := net.Pipe()
	_ = clientConn.Close()
	_ = serverConn.Close()
	df := &deadlineFailConn{Conn: serverConn, failRead: true}
	if err := ServerHandshake(df); err == nil {
		t.Fatalf("expected error from set read deadline")
	}
}

// TestServerHandshake_SetWriteDeadlineError feeds valid C0+C1 via
// readBufConn but makes SetWriteDeadline fail to cover that error branch.
func TestServerHandshake_SetWriteDeadlineError(t *testing.T) {
	// Provide valid C0+C1 bytes via custom reader, then fail on write deadline.
	c0c1 := make([]byte, 1+PacketSize)
	c0c1[0] = Version
	rb := &readBufConn{deadlineFailConn: &deadlineFailConn{failWrite: true}, buf: c0c1}
	if err := ServerHandshake(rb); err == nil {
		t.Fatalf("expected error from set write deadline")
	}
}

package handshake

import (
	"io"
	"net"
	"os"
	"testing"
	"time"

	rerrors "github.com/alxayo/go-rtmp/internal/errors"
)

// loadGolden loads a golden binary file (helper for readability)
func loadGolden(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("../../../tests/golden/" + name) // relative from handshake package
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return b
}

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

// failingConn wraps a net.Conn and forces Write to fail to exercise error path.
type failingConn struct{ net.Conn }

func (f *failingConn) Write(p []byte) (int, error) { return 0, io.ErrClosedPipe }

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

func TestServerHandshake_NilConn(t *testing.T) {
	if err := ServerHandshake(nil); err == nil {
		t.Fatalf("expected error for nil conn")
	}
}

// deadlineFailConn simulates failures for SetRead/SetWriteDeadline to cover error branches.
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

// readBufConn provides predetermined bytes for Read.
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

func TestServerHandshake_SetWriteDeadlineError(t *testing.T) {
	// Provide valid C0+C1 bytes via custom reader, then fail on write deadline.
	c0c1 := make([]byte, 1+PacketSize)
	c0c1[0] = Version
	rb := &readBufConn{deadlineFailConn: &deadlineFailConn{failWrite: true}, buf: c0c1}
	if err := ServerHandshake(rb); err == nil {
		t.Fatalf("expected error from set write deadline")
	}
}

package server

import (
	"net"
	"testing"
	"time"

	"github.com/alxayo/go-rtmp/internal/rtmp/handshake"
)

// TestServerStartStop verifies basic lifecycle: Start on :0, Addr non-nil, Stop idempotent.
func TestServerStartStop(t *testing.T) {
	s := New(Config{ListenAddr: ":0"})
	if err := s.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if s.Addr() == nil {
		t.Fatalf("expected non-nil addr")
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
	// Second stop should be no-op
	if err := s.Stop(); err != nil {
		t.Fatalf("second stop failed: %v", err)
	}
}

// TestServerAcceptConnection dials the server and ensures the connection is tracked.
func TestServerAcceptConnection(t *testing.T) {
	s := New(Config{ListenAddr: ":0"})
	if err := s.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer s.Stop()
	addr := s.Addr().String()
	// Dial after small delay to ensure accept loop active.
	time.Sleep(50 * time.Millisecond)
	c, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer c.Close()
	// Perform RTMP client handshake so the server can register the connection.
	if err := handshake.ClientHandshake(c); err != nil {
		t.Fatalf("client handshake failed: %v", err)
	}
	// Allow server to register the connection after the handshake.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.ConnectionCount() == 1 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if s.ConnectionCount() != 1 {
		t.Fatalf("expected 1 connection, got %d", s.ConnectionCount())
	}
}

// TestServerGracefulShutdown ensures active connections are closed on Stop.
func TestServerGracefulShutdown(t *testing.T) {
	s := New(Config{ListenAddr: ":0"})
	if err := s.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	addr := s.Addr().String()
	c, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	// Perform RTMP client handshake so the server can register the connection.
	if err := handshake.ClientHandshake(c); err != nil {
		t.Fatalf("client handshake failed: %v", err)
	}
	// Wait until tracked.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.ConnectionCount() == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if s.ConnectionCount() != 1 {
		t.Fatalf("expected 1 connection, got %d", s.ConnectionCount())
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
	// Subsequent read/write should fail after server close. TCP close propagation
	// may need a brief moment, so retry until an error is observed.
	var writeErr error
	writeDeadline := time.Now().Add(time.Second)
	for time.Now().Before(writeDeadline) {
		_, writeErr = c.Write([]byte{0})
		if writeErr != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if writeErr == nil {
		t.Fatalf("expected write error after stop")
	}
}

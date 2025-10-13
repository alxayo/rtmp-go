package server

import (
	"net"
	"testing"
	"time"
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
	// Allow handshake + control burst to complete.
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
	// Subsequent read/write should fail quickly due to close (handshake already occurred so we write nothing).
	if _, err := c.Write([]byte{0}); err == nil {
		t.Fatalf("expected write error after stop")
	}
}

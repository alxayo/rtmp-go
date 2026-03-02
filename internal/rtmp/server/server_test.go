// server_test.go – tests for the RTMP server lifecycle.
//
// The Server manages: listener, accept loop, connection tracking, and
// graceful shutdown. These tests verify:
//   - Start/Stop idempotency (Stop can be called twice safely).
//   - Accept loop: TCP dial + handshake → connection tracked.
//   - Graceful shutdown: Stop closes all active connections.
//
// Key Go concepts:
//   - ListenAddr ":0" lets the OS pick a free port (avoids conflicts).
//   - Polling loop with deadline for async connection tracking.
//   - handshake.ClientHandshake to complete the 3-way RTMP handshake.
package server

import (
	"net"
	"testing"
	"time"

	"github.com/alxayo/go-rtmp/internal/rtmp/handshake"
)

// TestServerStartStop verifies the basic lifecycle: Start on :0 picks a
// free port, Addr returns the bound address, Stop closes the listener,
// and calling Stop again is a no-op.
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

// TestServerAcceptConnection dials the server, completes the RTMP
// handshake, and polls ConnectionCount until it reaches 1 (or times out).
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
	if err := handshake.ClientHandshake(c); err != nil {
		t.Fatalf("client handshake failed: %v", err)
	}
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

// TestServerGracefulShutdown connects a client, waits for tracking, then
// calls Stop. After Stop, ConnectionCount must be 0 and the client's
// Read must eventually fail (EOF or connection reset).
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
	// After stop, all connections should be cleaned up.
	if s.ConnectionCount() != 0 {
		t.Fatalf("expected 0 connections after stop, got %d", s.ConnectionCount())
	}
	// Drain any buffered data (control burst), then confirm connection is closed.
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 4096)
	for {
		_, err := c.Read(buf)
		if err != nil {
			break // Expected: EOF or connection reset
		}
	}
}

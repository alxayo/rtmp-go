package integration

import (
	"io"
	"net"
	"testing"
	"time"

	rtmperr "github.com/alxayo/go-rtmp/internal/errors"
	"github.com/alxayo/go-rtmp/internal/rtmp/handshake"
)

// Integration test for RTMP simple handshake (T009).
// This is a high-level test exercising both server and client handshake logic over an in-memory pipe.
// Current status: handshake package only has stubs; test will fail (TDD) until implementation.
func TestHandshakeIntegration(t *testing.T) {
	t.Run("valid handshake", func(t *testing.T) {
		serverConn, clientConn := net.Pipe()
		defer serverConn.Close()
		defer clientConn.Close()

		serverErrCh := make(chan error, 1)
		go func() {
			serverErrCh <- handshake.ServerHandshake(serverConn)
		}()

		clientErr := handshake.ClientHandshake(clientConn)
		srvErr := <-serverErrCh

		if clientErr != nil || srvErr != nil {
			t.Fatalf("expected successful handshake, got clientErr=%v serverErr=%v", clientErr, srvErr)
		}
	})

	t.Run("invalid version", func(t *testing.T) {
		serverConn, clientConn := net.Pipe()
		defer serverConn.Close()
		defer clientConn.Close()

		serverErrCh := make(chan error, 1)
		go func() { serverErrCh <- handshake.ServerHandshake(serverConn) }()

		// Client sends invalid C0 (0x06) + C1 (1536 zero bytes) then closes.
		if _, err := clientConn.Write([]byte{0x06}); err != nil {
			t.Fatalf("write C0: %v", err)
		}
		if _, err := clientConn.Write(make([]byte, 1536)); err != nil {
			t.Fatalf("write C1: %v", err)
		}
		_ = clientConn.Close()

		select {
		case err := <-serverErrCh:
			if err == nil {
				t.Fatalf("expected error for invalid version, got nil")
			}
			if !rtmperr.IsProtocolError(err) {
				t.Fatalf("expected protocol error type, got %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("server handshake did not return within timeout")
		}
	})

	t.Run("truncated C1 timeout", func(t *testing.T) {
		serverConn, clientConn := net.Pipe()
		defer serverConn.Close()
		defer clientConn.Close()

		serverErrCh := make(chan error, 1)
		go func() { serverErrCh <- handshake.ServerHandshake(serverConn) }()

		// Write C0 + partial C1 (only 500 bytes instead of 1536) then remain idle.
		if _, err := clientConn.Write([]byte{0x03}); err != nil {
			t.Fatalf("write C0: %v", err)
		}
		if _, err := clientConn.Write(make([]byte, 500)); err != nil {
			t.Fatalf("write partial C1: %v", err)
		}

		// Wait for server to time out (contract: 5s). Use 7s cap to avoid hanging test suite.
		select {
		case err := <-serverErrCh:
			if err == nil {
				t.Fatalf("expected timeout/protocol error for truncated C1, got nil")
			}
			if !rtmperr.IsTimeout(err) && !rtmperr.IsProtocolError(err) {
				t.Fatalf("expected timeout or protocol error, got %v", err)
			}
		case <-time.After(7 * time.Second):
			// Attempt to unblock and gather any pending bytes.
			_ = clientConn.SetDeadline(time.Now())
			buf := make([]byte, 1)
			_, _ = io.ReadFull(clientConn, buf)
			t.Fatalf("server handshake did not time out within expected window")
		}
	})
}

// conn_test.go – tests for the RTMP Connection lifecycle.
//
// A Connection wraps a TCP socket and manages the full RTMP lifecycle:
//  1. Accept: TCP accept → server-side handshake → control burst
//  2. ReadLoop: goroutine reads chunks and dispatches Messages via handler
//  3. SendMessage: queues outbound messages for the write loop
//  4. Close: graceful shutdown with context cancellation
//
// Key Go concepts demonstrated:
//   - net.Listen + net.Dial for in-process TCP testing.
//   - handshake.ClientHandshake for completing the 3-way RTMP handshake.
//   - sync/atomic for thread-safe boolean flags in goroutine communication.
//   - Channels (acceptCh, errCh) for goroutine result passing.
//   - time.After for test timeouts (avoids hanging on failure).
package conn

import (
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alxayo/go-rtmp/internal/logger"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
	"github.com/alxayo/go-rtmp/internal/rtmp/handshake"
)

// dialAndClientHandshake is a test helper that dials a TCP address and
// performs the RTMP client-side handshake (C0+C1 → read S0+S1+S2 → send C2).
// It fails the test immediately if anything goes wrong.
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

// TestAccept_Success verifies the happy path: TCP accept → server handshake
// completes → Connection has a positive handshake duration. Logger is
// redirected to io.Discard so the control burst doesn't block on output.
func TestAccept_Success(t *testing.T) {
	// Redirect logs to discard to avoid blocking on output.
	logger.UseWriter(io.Discard)

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

// TestAccept_HandshakeFailure sends an invalid RTMP version byte (0x06
// instead of 0x03) and verifies that Accept returns an error rather than
// accepting the connection.
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

// --- T046 Additional Tests ---

// TestReadLoopMessageDispatch verifies that when a client sends a chunk
// message, the server Connection's readLoop goroutine delivers it to the
// registered message handler callback.
func TestReadLoopMessageDispatch(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	connCh := make(chan *Connection, 1)
	go func() { c, _ := Accept(ln); connCh <- c }()

	client := dialAndClientHandshake(t, ln.Addr().String())
	defer client.Close()

	serverConn := <-connCh
	if serverConn == nil {
		t.Fatalf("server conn nil")
	}
	var dispatched atomic.Bool
	serverConn.SetMessageHandler(func(m *chunk.Message) {
		if string(m.Payload) == "hi" {
			dispatched.Store(true)
		}
	})
	serverConn.Start() // Start readLoop after handler is set

	// Send a simple command message from client to server.
	w := chunk.NewWriter(client, 128)
	msg := &chunk.Message{CSID: 3, Timestamp: 0, MessageLength: 2, TypeID: 20, MessageStreamID: 0, Payload: []byte("hi")}
	if err := w.WriteMessage(msg); err != nil {
		t.Fatalf("client write: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if dispatched.Load() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !dispatched.Load() {
		t.Fatalf("message not dispatched")
	}
	_ = serverConn.Close()
}

// TestWriteLoopChunkingAndSend forces a tiny write chunk size (5 bytes) on
// the server connection, then sends a 10-byte message. The client must
// receive the full payload despite the message being fragmented into 2 chunks.
//
// The test also demonstrates reading past the 3 control-burst messages that
// the server sends automatically upon accepting a connection.
func TestWriteLoopChunkingAndSend(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	connCh := make(chan *Connection, 1)
	go func() { c, _ := Accept(ln); connCh <- c }()
	client := dialAndClientHandshake(t, ln.Addr().String())
	defer client.Close()
	serverConn := <-connCh
	if serverConn == nil {
		t.Fatalf("nil server conn")
	}
	atomic.StoreUint32(&serverConn.writeChunkSize, 5) // force fragmentation

	payload := []byte("abcdefghij") // 10 bytes -> 2 chunks of 5
	msg := &chunk.Message{CSID: 3, Timestamp: 0, MessageLength: uint32(len(payload)), TypeID: 20, MessageStreamID: 0, Payload: payload}
	if err := serverConn.SendMessage(msg); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	r := chunk.NewReader(client, 128)
	// Skip initial 3 control burst messages if they arrive first.
	deadline := time.Now().Add(3 * time.Second)
	burstRead := 0
	for burstRead < 3 && time.Now().Before(deadline) {
		_ = client.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, err := r.ReadMessage()
		if err != nil {
			t.Fatalf("read burst message %d: %v", burstRead, err)
		}
		burstRead++
	}
	// The reader auto-updated to 4096 from the SetChunkSize control message.
	// Override to match the server's forced writeChunkSize of 5.
	r.SetChunkSize(5)

	var received *chunk.Message
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	m, err := r.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(m.Payload) == string(payload) {
		received = m
	}
	if received == nil {
		t.Fatalf("did not receive message")
	}
	if string(received.Payload) != string(payload) {
		t.Fatalf("payload mismatch got=%s", string(received.Payload))
	}
	_ = serverConn.Close()
}

// TestCloseGraceful verifies that calling Close() on a connection makes
// subsequent SendMessage calls fail immediately with an error (context
// canceled) rather than blocking forever.
func TestCloseGraceful(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	connCh := make(chan *Connection, 1)
	go func() { c, _ := Accept(ln); connCh <- c }()
	client := dialAndClientHandshake(t, ln.Addr().String())
	defer client.Close()
	serverConn := <-connCh
	if serverConn == nil {
		t.Fatalf("nil server conn")
	}
	if err := serverConn.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// Sending after close should fail quickly.
	err = serverConn.SendMessage(&chunk.Message{CSID: 3, TypeID: 20, MessageStreamID: 0, Payload: []byte("x")})
	if err == nil {
		t.Fatalf("expected error sending after close")
	}
}

// --- Disconnect Handler Tests ---

// TestDisconnectHandler_FiresOnEOF verifies the disconnect handler fires
// when the client closes the connection (EOF in readLoop).
func TestDisconnectHandler_FiresOnEOF(t *testing.T) {
	logger.UseWriter(io.Discard)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	connCh := make(chan *Connection, 1)
	go func() { c, _ := Accept(ln); connCh <- c }()
	client := dialAndClientHandshake(t, ln.Addr().String())
	serverConn := <-connCh
	if serverConn == nil {
		t.Fatalf("server conn nil")
	}

	var fired atomic.Bool
	serverConn.SetDisconnectHandler(func() { fired.Store(true) })
	serverConn.SetMessageHandler(func(m *chunk.Message) {})
	serverConn.Start()

	// Client close triggers EOF in readLoop
	client.Close()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !fired.Load() {
		time.Sleep(10 * time.Millisecond)
	}
	if !fired.Load() {
		t.Fatal("disconnect handler did not fire on EOF")
	}
	_ = serverConn.Close()
}

// TestDisconnectHandler_FiresOnContextCancel verifies the disconnect handler
// fires when Close() is called (context cancellation).
func TestDisconnectHandler_FiresOnContextCancel(t *testing.T) {
	logger.UseWriter(io.Discard)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	connCh := make(chan *Connection, 1)
	go func() { c, _ := Accept(ln); connCh <- c }()
	client := dialAndClientHandshake(t, ln.Addr().String())
	defer client.Close()
	serverConn := <-connCh
	if serverConn == nil {
		t.Fatalf("server conn nil")
	}

	var fired atomic.Bool
	serverConn.SetDisconnectHandler(func() { fired.Store(true) })
	serverConn.SetMessageHandler(func(m *chunk.Message) {})
	serverConn.Start()

	// Close triggers context cancel → readLoop exits → handler fires
	_ = serverConn.Close()

	if !fired.Load() {
		t.Fatal("disconnect handler did not fire on context cancel")
	}
}

// TestDisconnectHandler_NilSafe verifies readLoop exits cleanly when no
// disconnect handler is set (nil handler must not panic).
func TestDisconnectHandler_NilSafe(t *testing.T) {
	logger.UseWriter(io.Discard)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	connCh := make(chan *Connection, 1)
	go func() { c, _ := Accept(ln); connCh <- c }()
	client := dialAndClientHandshake(t, ln.Addr().String())
	serverConn := <-connCh
	if serverConn == nil {
		t.Fatalf("server conn nil")
	}

	// No disconnect handler set — just message handler
	serverConn.SetMessageHandler(func(m *chunk.Message) {})
	serverConn.Start()

	// Client close triggers EOF → readLoop exits → no panic
	client.Close()

	// Close should complete without hanging or panicking
	_ = serverConn.Close()
}

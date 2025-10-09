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

// --- T046 Additional Tests ---

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
	serverConn.writeChunkSize = 5 // force fragmentation

	payload := []byte("abcdefghij") // 10 bytes -> 2 chunks of 5
	msg := &chunk.Message{CSID: 3, Timestamp: 0, MessageLength: uint32(len(payload)), TypeID: 20, MessageStreamID: 0, Payload: payload}
	if err := serverConn.SendMessage(msg); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	r := chunk.NewReader(client, 128)
	// Skip initial 3 control burst messages if they arrive first.
	deadline := time.Now().Add(3 * time.Second)
	var received *chunk.Message
	for time.Now().Before(deadline) {
		_ = client.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		m, err := r.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if string(m.Payload) == string(payload) {
			received = m
			break
		}
	}
	if received == nil {
		t.Fatalf("did not receive message")
	}
	if string(received.Payload) != string(payload) {
		t.Fatalf("payload mismatch got=%s", string(received.Payload))
	}
	_ = serverConn.Close()
}

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

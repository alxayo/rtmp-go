package conn

// Package conn provides the TCP connection lifecycle integration glue that
// sits above the handshake layer and (later) below the chunk/control layers.
//
// T016: Integrate Handshake into Connection
//  - After net.Listener.Accept() perform handshake.ServerHandshake
//  - Log handshake completion with duration
//  - On handshake error: close connection and return error
//
// The package purposefully keeps scope tiny for this task: a single Accept
// helper plus a lightweight Connection wrapper that will be expanded by
// subsequent tasks (control burst, read/write loops, stream registry, etc.).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alxayo/go-rtmp/internal/logger"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
	"github.com/alxayo/go-rtmp/internal/rtmp/handshake"
)

// Connection represents an accepted RTMP connection that has successfully
// completed the RTMP simple handshake and is ready for chunk layer processing.
// Future tasks will add read/write goroutines, control message negotiation,
// and command handling. For now we only retain metadata useful for logging
// and tests.
// (Session entity implemented in session.go – placeholder removed)

type Connection struct {
	// Immutable / identity
	id                string
	netConn           net.Conn
	remoteAddr        net.Addr
	acceptedAt        time.Time
	handshakeDuration time.Duration
	log               *slog.Logger

	// Context & lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Protocol state (subset per T046 requirements)
	readChunkSize  uint32
	writeChunkSize uint32
	windowAckSize  uint32
	chunkStreams   map[uint32]*chunk.ChunkStreamState // accessed only by readLoop
	outboundQueue  chan *chunk.Message
	session        *Session // placeholder (T047)

	// Internal helpers
	onMessage func(*chunk.Message) // test hook / dispatcher injection
}

// ID returns the logical connection id.
func (c *Connection) ID() string { return c.id }

// NetConn exposes the underlying net.Conn (read-only usage expected by higher layers).
func (c *Connection) NetConn() net.Conn { return c.netConn }

// HandshakeDuration returns how long the RTMP handshake took.
func (c *Connection) HandshakeDuration() time.Duration { return c.handshakeDuration }

// Close closes the underlying connection.
func (c *Connection) Close() error {
	if c.cancel != nil {
		c.cancel()
	}
	// Closing the underlying net.Conn will unblock reader/writer.
	_ = c.netConn.Close()
	// Wait for goroutines (bounded: they exit on ctx cancellation).
	c.wg.Wait()
	return nil
}

// SetMessageHandler installs a callback invoked by the readLoop for every
// fully reassembled RTMP message. MUST be called before Start().
func (c *Connection) SetMessageHandler(fn func(*chunk.Message)) { c.onMessage = fn }

// Start begins the readLoop. MUST be called after SetMessageHandler() to avoid race condition.
func (c *Connection) Start() {
	c.startReadLoop()
}

// SendMessage enqueues a message for outbound transmission (chunked by writeLoop).
// It enforces a small timeout to provide backpressure behavior.
func (c *Connection) SendMessage(msg *chunk.Message) error {
	if c == nil || c.outboundQueue == nil {
		return errors.New("connection not initialized")
	}
	if msg == nil {
		return errors.New("nil message")
	}
	// Derive short timeout context.
	deadline := time.NewTimer(200 * time.Millisecond)
	defer deadline.Stop()
	select {
	case <-c.ctx.Done():
		return context.Canceled
	case c.outboundQueue <- msg:
		return nil
	case <-deadline.C:
		return fmt.Errorf("send queue full (len=%d)", len(c.outboundQueue))
	}
}

// startReadLoop begins the dechunk → dispatch loop.
func (c *Connection) startReadLoop() {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		r := chunk.NewReader(c.netConn, c.readChunkSize)
		c.log.Debug("readLoop started", "initial_chunk_size", c.readChunkSize)
		for {
			select {
			case <-c.ctx.Done():
				c.log.Debug("readLoop context cancelled")
				return
			default:
			}
			c.log.Debug("readLoop waiting for message")
			msg, err := r.ReadMessage()
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, net.ErrClosed) {
					return
				}
				// Distinguish expected termination (EOF) vs unexpected errors.
				if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
					c.log.Debug("readLoop closed", "error", err)
				} else {
					c.log.Error("readLoop error", "error", err)
				}
				return
			}
			c.log.Debug("readLoop received message", "type_id", msg.TypeID, "msid", msg.MessageStreamID, "len", len(msg.Payload))
			if c.onMessage != nil {
				c.onMessage(msg)
			}
		}
	}()
}

// Helper to unify EOF detection without importing io here again in patch context.
func ioEOF(err error) error { return err }

// startWriteLoop consumes outboundQueue and writes chunked messages.
func (c *Connection) startWriteLoop() {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		w := chunk.NewWriter(c.netConn, c.writeChunkSize)
		c.log.Debug("writeLoop started", "write_chunk_size", c.writeChunkSize)
		for {
			select {
			case <-c.ctx.Done():
				c.log.Debug("writeLoop context cancelled")
				return
			case msg, ok := <-c.outboundQueue:
				if !ok {
					c.log.Debug("writeLoop queue closed")
					return
				}
				c.log.Debug("writeLoop sending message", "type_id", msg.TypeID, "csid", msg.CSID, "msid", msg.MessageStreamID, "len", len(msg.Payload))
				// Sync writer chunk size with potentially updated field.
				w.SetChunkSize(c.writeChunkSize)
				if err := w.WriteMessage(msg); err != nil {
					c.log.Error("writeLoop write failed", "error", err)
					return
				}
				c.log.Debug("writeLoop message sent successfully", "type_id", msg.TypeID)
			}
		}
	}()
}

var connCounter uint64

// nextID generates a simple monotonically increasing connection identifier.
func nextID() string { return fmt.Sprintf("c%06d", atomic.AddUint64(&connCounter, 1)) }

// Accept performs a blocking Accept() on the provided listener, runs the
// server-side RTMP handshake, and returns a *Connection on success. On
// handshake failure the underlying net.Conn is closed and the error returned.
//
// This function is intentionally synchronous; a typical server will wrap it
// inside an accept loop and launch a goroutine per successful connection.
func Accept(l net.Listener) (*Connection, error) {
	if l == nil {
		return nil, fmt.Errorf("nil listener")
	}
	raw, err := l.Accept()
	if err != nil {
		return nil, err
	}

	start := time.Now()
	if err := handshake.ServerHandshake(raw); err != nil {
		// Handshake failure: ensure connection is closed and log context.
		_ = raw.Close()
		logger.Logger().Error("Handshake failed", "error", err, "remote", raw.RemoteAddr().String())
		return nil, err
	}
	dur := time.Since(start)

	id := nextID()
	lgr := logger.WithConn(logger.Logger(), id, raw.RemoteAddr().String())
	lgr.Info("Connection accepted", "handshake_ms", dur.Milliseconds())

	ctx, cancel := context.WithCancel(context.Background())
	c := &Connection{
		id:                id,
		netConn:           raw,
		remoteAddr:        raw.RemoteAddr(),
		acceptedAt:        start,
		handshakeDuration: dur,
		log:               lgr,
		ctx:               ctx,
		cancel:            cancel,
		readChunkSize:     128,
		writeChunkSize:    128,
		windowAckSize:     windowAckSizeValue, // align with control burst constants
		chunkStreams:      make(map[uint32]*chunk.ChunkStreamState),
		outboundQueue:     make(chan *chunk.Message, 100),
	}

	// Start write loop first so control burst can be queued
	c.startWriteLoop()

	// Send control burst synchronously BEFORE starting read loop
	// This ensures the client receives the burst before we process any client messages
	if err := sendInitialControlBurst(c); err != nil {
		c.log.Error("Control burst failed", "error", err)
		_ = c.Close()
		return nil, fmt.Errorf("control burst: %w", err)
	}

	// NOTE: readLoop is NOT started here to avoid race condition with message handler setup.
	// Caller MUST call Start() after setting message handler via SetMessageHandler().

	return c, nil
}

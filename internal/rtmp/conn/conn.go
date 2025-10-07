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
	"fmt"
	"log/slog"
	"net"
	"sync/atomic"
	"time"

	"github.com/alxayo/go-rtmp/internal/logger"
	"github.com/alxayo/go-rtmp/internal/rtmp/handshake"
)

// Connection represents an accepted RTMP connection that has successfully
// completed the RTMP simple handshake and is ready for chunk layer processing.
// Future tasks will add read/write goroutines, control message negotiation,
// and command handling. For now we only retain metadata useful for logging
// and tests.
type Connection struct {
	id                string
	netConn           net.Conn
	acceptedAt        time.Time
	handshakeDuration time.Duration
	log               *slog.Logger
}

// ID returns the logical connection id.
func (c *Connection) ID() string { return c.id }

// NetConn exposes the underlying net.Conn (read-only usage expected by higher layers).
func (c *Connection) NetConn() net.Conn { return c.netConn }

// HandshakeDuration returns how long the RTMP handshake took.
func (c *Connection) HandshakeDuration() time.Duration { return c.handshakeDuration }

// Close closes the underlying connection.
func (c *Connection) Close() error { return c.netConn.Close() }

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

	return &Connection{
		id:                id,
		netConn:           raw,
		acceptedAt:        start,
		handshakeDuration: dur,
		log:               lgr,
	}, nil
}

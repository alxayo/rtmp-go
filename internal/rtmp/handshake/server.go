package handshake

// Server-side RTMP simple handshake finite state machine (T014).
// Implements the sequence: Read C0+C1 -> Send S0+S1+S2 -> Read C2 -> Complete.
// Wire format references: contracts/handshake.md and spec notes. Version 0x03 only.

import (
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"time"

	rerrors "github.com/alxayo/go-rtmp/internal/errors"
	"github.com/alxayo/go-rtmp/internal/logger"
)

const (
	serverReadTimeout  = 5 * time.Second // per spec: 5s deadline for each blocking read phase
	serverWriteTimeout = 5 * time.Second
)

// ServerHandshake performs the server side RTMP simple handshake on the provided
// connection. It is a blocking call; on success the connection is positioned
// immediately after the C2 read (ready for chunk stream processing). On failure
// a *HandshakeError or *TimeoutError is returned (which satisfy IsProtocolError / IsTimeout).
//
// The function purposefully does not return the Handshake struct instance to keep
// the public API minimal for now; later integration (T016) can be adjusted to retain
// timestamps if required.
func ServerHandshake(conn net.Conn) error {
	if conn == nil {
		return rerrors.NewHandshakeError("init", fmt.Errorf("nil conn"))
	}
	log := logger.Logger().With("phase", "handshake", "side", "server")

	h := New() // FSM state container

	// 1. Read C0 (version) + C1 (1536 bytes). We use a single buffer to ensure
	// contiguous read semantics for potential future digest schemes (even though
	// we implement simple handshake only).
	c0c1 := make([]byte, 1+PacketSize)
	if err := setReadDeadline(conn, serverReadTimeout); err != nil {
		return err
	}
	if _, err := io.ReadFull(conn, c0c1); err != nil {
		if isTimeoutErr(err) {
			return rerrors.NewTimeoutError("read C0+C1", serverReadTimeout, err)
		}
		return rerrors.NewHandshakeError("read C0+C1", err)
	}
	c0 := c0c1[0]
	c1 := c0c1[1:]
	if err := h.AcceptC0C1(c0, c1); err != nil {
		return err
	}
	if c0 != Version {
		return rerrors.NewHandshakeError("validate version", fmt.Errorf("unsupported version 0x%02x", c0))
	}

	// 2. Prepare S1 (timestamp + zero + random[1528])
	var s1 [PacketSize]byte
	// Timestamp: Use current Unix ms mod 2^32
	ts := uint32(time.Now().UnixMilli() & 0xFFFFFFFF)
	s1[0] = byte(ts >> 24)
	s1[1] = byte(ts >> 16)
	s1[2] = byte(ts >> 8)
	s1[3] = byte(ts)
	// 4 bytes zero already default
	if _, err := rand.Read(s1[randomFieldOffset:]); err != nil {
		return rerrors.NewHandshakeError("rand S1", err)
	}
	if err := h.SetS1(s1[:]); err != nil { // advances state to SentS0S1S2
		return err
	}

	// 3. Prepare S2 = echo of C1 (byte-for-byte)
	s2 := h.C1() // returns copy, safe to reuse

	// 4. Send S0+S1+S2 atomically. Allocate a single contiguous buffer of 1+1536+1536 bytes.
	out := make([]byte, 1+PacketSize+PacketSize)
	out[0] = Version // S0
	copy(out[1:1+PacketSize], s1[:])
	copy(out[1+PacketSize:], s2)
	if err := setWriteDeadline(conn, serverWriteTimeout); err != nil {
		return err
	}
	if err := writeFull(conn, out); err != nil {
		if isTimeoutErr(err) {
			return rerrors.NewTimeoutError("write S0+S1+S2", serverWriteTimeout, err)
		}
		return rerrors.NewHandshakeError("write S0+S1+S2", err)
	}

	// 5. Read C2 (1536 bytes)
	if err := setReadDeadline(conn, serverReadTimeout); err != nil {
		return err
	}
	c2 := make([]byte, PacketSize)
	if _, err := io.ReadFull(conn, c2); err != nil {
		if isTimeoutErr(err) {
			return rerrors.NewTimeoutError("read C2", serverReadTimeout, err)
		}
		return rerrors.NewHandshakeError("read C2", err)
	}
	if err := h.AcceptC2(c2); err != nil {
		return err
	}

	// Optional validation: C2 should echo S1. Non-fatal; warn if mismatch.
	if !bytesEqual(c2, s1[:]) {
		log.Warn("C2 echo mismatch", "expected_echo_len", len(s1), "got_len", len(c2))
	}

	if err := h.Complete(); err != nil {
		return err
	}

	// Clear deadlines after successful handshake so subsequent chunk reads
	// can operate without timeout constraints (T016 integration requirement).
	// This prevents spurious "i/o timeout" errors when client delays sending
	// the connect command after handshake (common with OBS Studio).
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		log.Warn("Failed to clear read deadline", "error", err)
	}
	if err := conn.SetWriteDeadline(time.Time{}); err != nil {
		log.Warn("Failed to clear write deadline", "error", err)
	}

	log.Info("Handshake completed", "c1_ts", h.C1Timestamp(), "s1_ts", h.S1Timestamp())
	return nil
}

// Helper: set deadlines with error wrapping.
func setReadDeadline(c net.Conn, d time.Duration) error {
	if err := c.SetReadDeadline(time.Now().Add(d)); err != nil {
		return rerrors.NewHandshakeError("set read deadline", err)
	}
	return nil
}
func setWriteDeadline(c net.Conn, d time.Duration) error {
	if err := c.SetWriteDeadline(time.Now().Add(d)); err != nil {
		return rerrors.NewHandshakeError("set write deadline", err)
	}
	return nil
}

// writeFull ensures entire buffer is written.
func writeFull(w io.Writer, b []byte) error {
	off := 0
	for off < len(b) {
		n, err := w.Write(b[off:])
		if err != nil {
			return err
		}
		off += n
	}
	return nil
}

// bytesEqual is a small inline version (avoids importing bytes just for Equal)
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// isTimeoutErr performs a lightweight timeout classification so we can convert
// into TimeoutError. We check for net.Error with Timeout() and io.ErrUnexpectedEOF
// (the latter combined with a deadline read indicates a premature close, still
// treat as protocol / timeout layered error).
func isTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	type to interface{ Timeout() bool }
	if ne, ok := err.(to); ok && ne.Timeout() {
		return true
	}
	return false
}

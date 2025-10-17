package handshake

// Client-side RTMP simple handshake finite state machine (T015).
// Implements: Send C0+C1 -> Read S0+S1 -> Send C2 -> (optional) Read S2 -> Complete.
// Mirrors server.go patterns for deadlines, logging, and error wrapping.

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
	clientReadTimeout  = 5 * time.Second
	clientWriteTimeout = 5 * time.Second
)

// ClientHandshake performs the RTMP simple handshake as a client. On success the
// connection is positioned immediately after (optional) S2 read and ready for
// chunk stream negotiation.
func ClientHandshake(conn net.Conn) error {
	if conn == nil {
		return rerrors.NewHandshakeError("init", fmt.Errorf("nil conn"))
	}
	log := logger.Logger().With("phase", "handshake", "side", "client")

	// Construct C1: timestamp(4) + zero(4) + random(1528)
	var c1 [PacketSize]byte
	ts := uint32(time.Now().UnixMilli() & 0xFFFFFFFF)
	c1[0] = byte(ts >> 24)
	c1[1] = byte(ts >> 16)
	c1[2] = byte(ts >> 8)
	c1[3] = byte(ts)
	if _, err := rand.Read(c1[randomFieldOffset:]); err != nil {
		return rerrors.NewHandshakeError("rand C1", err)
	}

	// Send C0+C1 atomically.
	c0c1 := make([]byte, 1+PacketSize)
	c0c1[0] = Version
	copy(c0c1[1:], c1[:])
	if err := setWriteDeadline(conn, clientWriteTimeout); err != nil {
		return err
	}
	if err := writeFull(conn, c0c1); err != nil {
		if isTimeoutErr(err) {
			return rerrors.NewTimeoutError("write C0+C1", clientWriteTimeout, err)
		}
		return rerrors.NewHandshakeError("write C0+C1", err)
	}

	// Read S0+S1 (1+1536). Server may have already sent S2 as well; spec flow says
	// to send C2 before reading S2, but net.Pipe has minimal buffering so we perform
	// an opportunistic non-blocking read of S2 right after S0+S1 to avoid a potential
	// deadlock while still treating S2 as optional for semantic purposes.
	if err := setReadDeadline(conn, clientReadTimeout); err != nil {
		return err
	}
	s0s1 := make([]byte, 1+PacketSize)
	if _, err := io.ReadFull(conn, s0s1); err != nil {
		if isTimeoutErr(err) {
			return rerrors.NewTimeoutError("read S0+S1", clientReadTimeout, err)
		}
		return rerrors.NewHandshakeError("read S0+S1", err)
	}
	if s0s1[0] != Version {
		return rerrors.NewHandshakeError("validate S0", fmt.Errorf("unsupported version 0x%02x", s0s1[0]))
	}
	s1 := s0s1[1:]

	// Opportunistically read S2 if already available (server sends S0+S1+S2 together).
	var haveS2 bool
	var s2buf [PacketSize]byte
	// Tiny deadline to avoid blocking if S2 not yet sent by a non-compliant server.
	_ = conn.SetReadDeadline(time.Now().Add(1 * time.Millisecond))
	if _, err := io.ReadFull(conn, s2buf[:]); err == nil {
		haveS2 = true
		// Validate S2 echoes our original C1; warn if mismatch but continue.
		if !bytesEqual(s2buf[:], c1[:]) {
			log.Warn("S2 early echo mismatch", "expected_echo_len", len(c1))
		}
	}

	// Prepare C2 = echo of S1 (byte-for-byte)
	c2 := make([]byte, PacketSize)
	copy(c2, s1)

	// Send C2
	if err := setWriteDeadline(conn, clientWriteTimeout); err != nil {
		return err
	}
	if err := writeFull(conn, c2); err != nil {
		if isTimeoutErr(err) {
			return rerrors.NewTimeoutError("write C2", clientWriteTimeout, err)
		}
		return rerrors.NewHandshakeError("write C2", err)
	}

	// If we did not already consume S2 above, attempt a best-effort read now (optional).
	if !haveS2 {
		if err := setReadDeadline(conn, clientReadTimeout); err == nil {
			s2 := make([]byte, PacketSize)
			if _, err := io.ReadFull(conn, s2); err == nil {
				if !bytesEqual(s2, c1[:]) {
					log.Warn("S2 echo mismatch", "expected_echo_len", len(c1))
				}
			}
		}
	}

	// Clear deadlines after successful handshake so subsequent chunk operations
	// can operate without timeout constraints. This prevents spurious "i/o timeout"
	// errors during media streaming when connection is used for extended periods.
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		log.Warn("Failed to clear read deadline", "error", err)
	}
	if err := conn.SetWriteDeadline(time.Time{}); err != nil {
		log.Warn("Failed to clear write deadline", "error", err)
	}

	log.Info("Handshake completed", "c1_ts", ts)
	return nil
}

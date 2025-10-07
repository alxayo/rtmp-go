package handshake

// NOTE: This file now only retains the client-side stub (T015) while the
// server FSM (T014) is implemented in server.go. Once T015 is executed this
// stub will be replaced with the real client implementation.

import (
	"errors"
	"net"
)

// ErrNotImplemented indicates a handshake side (currently client) is not yet implemented.
var ErrNotImplemented = errors.New("handshake not implemented (client side pending)")

// ClientHandshake performs the client side RTMP simple handshake (stub placeholder for T015).
func ClientHandshake(conn net.Conn) error { return ErrNotImplemented }

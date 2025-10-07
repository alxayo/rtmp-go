package handshake

// NOTE: Temporary stub to allow integration test (T009) to compile before
// the real handshake implementation tasks (T013-T015). These functions will
// be replaced with the full finite state machine per the handshake contract.

import (
	"errors"
	"net"
)

// ErrNotImplemented is returned by the stub functions until the handshake FSM
// is implemented.
var ErrNotImplemented = errors.New("handshake not implemented")

// ServerHandshake performs the server side RTMP simple handshake (stub).
func ServerHandshake(conn net.Conn) error { return ErrNotImplemented }

// ClientHandshake performs the client side RTMP simple handshake (stub).
func ClientHandshake(conn net.Conn) error { return ErrNotImplemented }

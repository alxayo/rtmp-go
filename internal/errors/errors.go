package errors

// This package defines domain-specific error types for the RTMP server.
// Each error type corresponds to a layer of the RTMP protocol stack:
//   - ProtocolError: generic RTMP protocol violations
//   - HandshakeError: failures during the initial handshake exchange
//   - ChunkError: problems parsing or serializing chunk-level framing
//   - AMFError: failures encoding/decoding AMF0 data format
//   - TimeoutError: operations that exceeded their deadline
//
// All protocol errors implement the protocolMarker interface, enabling
// callers to check if any error in a chain is protocol-related via
// IsProtocolError(). Errors support Go 1.13+ unwrapping via Unwrap().

import (
	"context"
	stdErrors "errors"
	"fmt"
	"time"
)

// protocolMarker is implemented by all protocol-layer error types so we can classify them.
type protocolMarker interface {
	error
	isProtocol()
}

// ProtocolError is a generic RTMP protocol layer error (validation, state, etc).
type ProtocolError struct {
	Op  string // high-level operation (e.g. "state.transition", "decode.message")
	Err error  // underlying cause (may be nil)
}

func (e *ProtocolError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("protocol error: %s", e.Op)
	}
	return fmt.Sprintf("protocol error: %s: %v", e.Op, e.Err)
}
func (e *ProtocolError) Unwrap() error { return e.Err }
func (e *ProtocolError) isProtocol()   {}

// HandshakeError indicates an RTMP handshake violation or failure.
type HandshakeError struct {
	Op  string
	Err error
}

func (e *HandshakeError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("handshake error: %s", e.Op)
	}
	return fmt.Sprintf("handshake error: %s: %v", e.Op, e.Err)
}
func (e *HandshakeError) Unwrap() error { return e.Err }
func (e *HandshakeError) isProtocol()   {}

// ChunkError indicates an RTMP chunk parsing / serialization violation.
type ChunkError struct {
	Op  string
	Err error
}

func (e *ChunkError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("chunk error: %s", e.Op)
	}
	return fmt.Sprintf("chunk error: %s: %v", e.Op, e.Err)
}
func (e *ChunkError) Unwrap() error { return e.Err }
func (e *ChunkError) isProtocol()   {}

// AMFError indicates a failure in AMF0 encoding/decoding.
type AMFError struct {
	Op  string
	Err error
}

func (e *AMFError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("amf error: %s", e.Op)
	}
	return fmt.Sprintf("amf error: %s: %v", e.Op, e.Err)
}
func (e *AMFError) Unwrap() error { return e.Err }
func (e *AMFError) isProtocol()   {}

// TimeoutError indicates an operation exceeded a deadline or idle timeout.
type TimeoutError struct {
	Op       string
	Duration time.Duration
	Err      error
}

func (e *TimeoutError) Error() string {
	base := fmt.Sprintf("timeout error: %s (after %s)", e.Op, e.Duration)
	if e.Err != nil {
		return base + ": " + e.Err.Error()
	}
	return base
}
func (e *TimeoutError) Unwrap() error { return e.Err }

// IsTimeout returns true if err is (or wraps) a TimeoutError, a context deadline exceeded,
// or any error type that exposes Timeout() bool and returns true.
func IsTimeout(err error) bool {
	if err == nil {
		return false
	}
	var te *TimeoutError
	if stdErrors.As(err, &te) {
		return true
	}
	if stdErrors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var toErr interface{ Timeout() bool }
	if stdErrors.As(err, &toErr) && toErr.Timeout() {
		return true
	}
	return false
}

// IsProtocolError returns true if the error chain contains any protocol-layer
// error (ProtocolError, HandshakeError, ChunkError, AMFError).
func IsProtocolError(err error) bool {
	if err == nil {
		return false
	}
	var pm protocolMarker
	return stdErrors.As(err, &pm)
}

// Constructors — these create new error instances wrapping an underlying cause.
// The 'op' parameter describes what operation failed (e.g. "read C0+C1").
// The 'cause' parameter is the original error that triggered this failure.
func NewProtocolError(op string, cause error) error  { return &ProtocolError{Op: op, Err: cause} }
func NewHandshakeError(op string, cause error) error { return &HandshakeError{Op: op, Err: cause} }
func NewChunkError(op string, cause error) error     { return &ChunkError{Op: op, Err: cause} }
func NewAMFError(op string, cause error) error       { return &AMFError{Op: op, Err: cause} }
func NewTimeoutError(op string, d time.Duration, cause error) error {
	return &TimeoutError{Op: op, Duration: d, Err: cause}
}

// AuthError indicates an authentication or authorization failure.
type AuthError struct {
	Op  string // operation (e.g. "publish.auth", "play.auth")
	Err error  // underlying cause
}

func (e *AuthError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("auth error: %s", e.Op)
	}
	return fmt.Sprintf("auth error: %s: %v", e.Op, e.Err)
}
func (e *AuthError) Unwrap() error { return e.Err }
func (e *AuthError) isProtocol()   {} // classified as protocol-layer

// NewAuthError creates an AuthError wrapping the given cause.
func NewAuthError(op string, cause error) error { return &AuthError{Op: op, Err: cause} }

// Usage pattern example:
//  if _, err := io.ReadFull(r, buf); err != nil {
//      return NewHandshakeError("read C0+C1", fmt.Errorf("io: %w", err))
//  }
// Keep layering context with fmt.Errorf("...: %w", err).

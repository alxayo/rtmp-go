// Package errors defines domain-specific error types for the RTMP and SRT
// protocol stacks, enabling callers to classify and respond to different
// failure modes.
//
// # Error Hierarchy
//
// All protocol-layer errors implement a common protocolMarker interface,
// allowing callers to use errors.As() to distinguish protocol failures
// from other errors (network I/O, EOF, context cancellation, etc.).
//
// Error types (in order of protocol layer, lowest to highest):
//
//   - HandshakeError: Failures during initial protocol setup
//   - ChunkError: Problems parsing or serializing chunk-level framing
//   - ControlError: Issues with control messages (Set Chunk Size, Ack, etc.)
//   - AMFError: Failures encoding/decoding AMF0 data (object serialization)
//   - ProtocolError: Generic violations (state machine, validation)
//   - TimeoutError: Operations that exceeded deadlines
//   - SRTError: SRT protocol layer failures (packet parsing, congestion control)
//   - TSError: MPEG-TS demux failures (container format issues)
//
// Each error type wraps an underlying cause (the original error from
// io.Read, JSON parsing, etc.) via the Unwrap() method, enabling
// error chain inspection in Go 1.13+.
//
// # Error Wrapping Pattern
//
// All errors are created via constructor functions that take an operation
// name (high-level context) and an underlying cause:
//
//	if _, err := io.ReadFull(r, buf); err != nil {
//	    return errors.NewHandshakeError("read C0+C1", fmt.Errorf("io: %w", err))
//	}
//
// Always wrap the cause with fmt.Errorf("...: %w", err) so the chain
// is preserved. This allows callers to inspect the full error chain
// when debugging.
//
// # Usage: Classification
//
// Use errors.As() to check for specific error types:
//
//	var he *errors.HandshakeError
//	if errors.As(err, &he) {
//	    log.Debug("handshake failed", "op", he.Op, "cause", he.Err)
//	    // Maybe retry or give up
//	}
//
// Use errors.Is() to check for sentinel values:
//
//	if errors.Is(err, errors.ErrAuthFailed) {
//	    conn.Reject("Invalid token")
//	}
//
// Use IsProtocolError() to distinguish protocol failures from I/O:
//
//	if errors.IsProtocolError(err) {
//	    // Malformed data, close connection
//	    conn.Close()
//	} else if errors.Is(err, io.EOF) {
//	    // Client disconnected gracefully
//	    conn.Close()
//	} else {
//	    // Network error, might retry
//	    log.Error("read", "err", err)
//	}
//
// Use IsTimeout() to detect deadline failures:
//
//	if errors.IsTimeout(err) {
//	    // Zombie connection, kill it
//	    conn.Destroy()
//	}
//
// # Error Propagation
//
// When wrapping errors, always preserve the cause. If multiple layers
// fail, the error chain will include the context from each layer:
//
//	// Layer 1: Chunk reader fails
//	return errors.NewChunkError("read payload", io.EOF)
//
//	// Layer 2: Command parser fails
//	if err := r.ReadMessage(); err != nil {
//	    return errors.NewProtocolError("parse command", err)
//	}
//	// Result: ProtocolError(parse command) wraps ChunkError(read payload) wraps io.EOF
//
// Callers can then inspect the full chain to understand what went wrong.
//
// # Sentinel Errors
//
// Some errors are returned as sentinel values (module-level variables):
//
//	var (
//	    ErrAuthFailed = errors.New("authentication failed")
//	    ErrNotFound   = errors.New("stream not found")
//	)
//
// These are used for errors that don't require cause wrapping, as the
// error is the final determination (not a parsing failure to debug).
//
// # Logging Errors
//
// When logging errors, include the operation context:
//
//	log.Info("read message failed",
//	    "op", he.Op,
//	    "err", he.Err,
//	)
//
// This gives the log aggregator (CloudWatch, ELK) enough information
// to aggregate similar errors and find root causes.
//
// # Integration Points
//
// Errors are returned from:
//   - handshake package: Handshake negotiation failures
//   - chunk package: RTMP chunk parsing
//   - amf package: Object encoding/decoding
//   - rpc package: Command processing
//   - rtmp/conn package: Connection lifecycle
//   - srt package: SRT packet handling
package errors

// Package errors – tests for the RTMP-specific error types.
//
// This project defines domain-specific error types (HandshakeError, ChunkError,
// AMFError, ProtocolError, TimeoutError) so callers can classify failures and
// decide how to react (e.g. close the connection on protocol errors but retry
// on timeouts).
//
// Key Go concepts demonstrated here:
//   - errors.Is / errors.As – Go's idiomatic way to inspect wrapped errors.
//   - Interface-based classification – IsProtocolError walks the error chain
//     looking for the protocolMarker interface.
//   - Timeout duck-typing – IsTimeout checks for Go's standard Timeout()
//     interface as well as context.DeadlineExceeded.
package errors

import (
	"context"
	stdErrors "errors"
	"fmt"
	"testing"
	"time"
)

// fakeTimeoutErr is a test double that satisfies Go's implicit "timeout"
// interface (Error() string + Timeout() bool). Many stdlib types like
// net.OpError implement this. We use a minimal struct to keep the test
// focused on classification logic without importing the net package.
type fakeTimeoutErr struct{}

func (fakeTimeoutErr) Error() string { return "fake timeout" }
func (fakeTimeoutErr) Timeout() bool { return true }

// TestIsProtocolErrorClassification verifies that every domain error type
// (Handshake, Chunk, AMF, Protocol) is correctly recognized by the
// IsProtocolError classifier.
//
// It also demonstrates Go's error wrapping chain:
//
//	root → wrapped (via fmt.Errorf %w) → HandshakeError
//
// and proves that errors.Is / errors.As can "see through" the chain to find
// the original root cause or a concrete type anywhere in the chain.
func TestIsProtocolErrorClassification(t *testing.T) {
	// Build a 3-level error chain: root → wrapped → HandshakeError.
	root := stdErrors.New("root")
	wrapped := fmt.Errorf("adding context: %w", root)
	hs := NewHandshakeError("server.read", wrapped)

	// HandshakeError should satisfy the protocolMarker interface.
	if !IsProtocolError(hs) {
		t.Fatalf("expected IsProtocolError=true for handshake error")
	}
	// errors.Is walks the Unwrap() chain to find the original root.
	if !stdErrors.Is(hs, root) {
		t.Fatalf("expected errors.Is to find root cause")
	}
	// errors.As extracts the concrete *HandshakeError from the chain.
	var he *HandshakeError
	if !stdErrors.As(hs, &he) {
		t.Fatalf("expected errors.As to *HandshakeError")
	}
	if he.Op != "server.read" {
		t.Fatalf("unexpected op: %s", he.Op)
	}

	// Every domain error type must pass the protocol-error check.
	ck := NewChunkError("parse.basicHeader", nil)
	if !IsProtocolError(ck) {
		t.Fatalf("expected chunk error classified as protocol")
	}
	amf := NewAMFError("decode.number", nil)
	if !IsProtocolError(amf) {
		t.Fatalf("expected amf error classified as protocol")
	}
	p := NewProtocolError("state.transition", stdErrors.New("invalid state"))
	if !IsProtocolError(p) {
		t.Fatalf("expected protocol error classified")
	}
}

// TestIsTimeout checks the three kinds of errors that IsTimeout must
// recognise:
//  1. Our own TimeoutError type.
//  2. context.DeadlineExceeded (standard library sentinel).
//  3. Any error implementing Timeout() bool (e.g. net.OpError).
//
// It also confirms that timeouts are NOT classified as protocol errors –
// the two categories are intentionally disjoint.
func TestIsTimeout(t *testing.T) {
	root := fakeTimeoutErr{}
	to := NewTimeoutError("handshake.read", 5*time.Second, root)
	if !IsTimeout(to) {
		t.Fatalf("expected TimeoutError recognized")
	}
	// Timeouts and protocol errors are disjoint categories.
	if IsProtocolError(to) {
		t.Fatalf("timeout should NOT be protocol error")
	}
	// context.DeadlineExceeded is a standard Go sentinel for timeouts.
	if !IsTimeout(context.DeadlineExceeded) {
		t.Fatalf("expected context deadline recognized")
	}
	// Any error with a Timeout() bool method (duck-typing) counts.
	var ne error = root
	if !IsTimeout(ne) {
		t.Fatalf("expected net-like timeout recognized")
	}
}

// TestUnwrapChains builds a multi-level wrapping chain:
//
//	base → l1 (fmt.Errorf %w) → l2 (HandshakeError)
//
// and proves that errors.Is can still reach the deepest base cause, while
// errors.As can match the protocolMarker interface at any level.
func TestUnwrapChains(t *testing.T) {
	base := stdErrors.New("io EOF")
	l1 := fmt.Errorf("read: %w", base)
	l2 := NewHandshakeError("handshake.read", l1)
	if !stdErrors.Is(l2, base) {
		t.Fatalf("errors.Is should reach base cause")
	}
	var pm protocolMarker
	if !stdErrors.As(l2, &pm) {
		t.Fatalf("expected to match protocolMarker via As")
	}
}

// TestNilSafety ensures classifiers don't panic on nil – a common edge case.
func TestNilSafety(t *testing.T) {
	if IsProtocolError(nil) {
		t.Fatalf("nil should not be protocol error")
	}
	if IsTimeout(nil) {
		t.Fatalf("nil should not be timeout")
	}
}

// TestConstructorWithoutCause verifies that error constructors work even
// when there is no underlying cause (nil). The Error() string should
// still produce a meaningful message.
func TestConstructorWithoutCause(t *testing.T) {
	ck := NewChunkError("parse.msgHeader", nil)
	if ck == nil {
		t.Fatalf("constructor returned nil")
	}
	if errStr := ck.Error(); errStr == "" {
		t.Fatalf("expected non-empty error string")
	}
}

// TestNilErrBranchesAndStrings exercises every error constructor with a nil
// cause, verifying classification and non-empty Error() strings. This is a
// coverage test that ensures nil-cause code paths don't panic or produce
// degenerate output.
func TestNilErrBranchesAndStrings(t *testing.T) {
	// ProtocolError with nil cause
	p := NewProtocolError("op1", nil)
	if p == nil {
		t.Fatalf("nil protocol error")
	}
	if !IsProtocolError(p) {
		t.Fatalf("expected protocol classification")
	}
	if s := p.Error(); s == "" || s == "protocol error:" {
		t.Fatalf("unexpected protocol error string: %q", s)
	}

	h := NewHandshakeError("op2", nil)
	if s := h.Error(); s == "" || s == "handshake error:" {
		t.Fatalf("bad handshake error string: %q", s)
	}

	c := NewChunkError("op3", nil)
	if s := c.Error(); s == "" {
		t.Fatalf("empty chunk error string")
	}

	a := NewAMFError("op4", nil)
	if s := a.Error(); s == "" {
		t.Fatalf("empty amf error string")
	}

	to := NewTimeoutError("op5", 100*time.Millisecond, nil)
	if !IsTimeout(to) {
		t.Fatalf("timeout classification failed")
	}
	if IsProtocolError(to) {
		t.Fatalf("timeout misclassified as protocol")
	}
	if s := to.Error(); s == "" {
		t.Fatalf("empty timeout error string")
	}
}

// TestNegativePredicates confirms that plain stdlib errors are NOT mistakenly
// classified as protocol or timeout errors.
func TestNegativePredicates(t *testing.T) {
	if IsProtocolError(stdErrors.New("plain")) {
		t.Fatalf("plain error shouldn't be protocol")
	}
	if IsTimeout(stdErrors.New("plain")) {
		t.Fatalf("plain error shouldn't be timeout")
	}
}

// TestAuthErrorClassification verifies that AuthError is correctly
// classified as a protocol-layer error and supports wrapping/unwrapping.
func TestAuthErrorClassification(t *testing.T) {
	cause := stdErrors.New("invalid token")
	ae := NewAuthError("publish.auth", cause)

	// Should be classified as protocol error
	if !IsProtocolError(ae) {
		t.Fatal("AuthError should be classified as protocol error")
	}

	// Should support unwrapping
	if !stdErrors.Is(ae, cause) {
		t.Fatal("errors.Is should find root cause")
	}

	// Should have meaningful message
	if s := ae.Error(); s == "" {
		t.Fatal("empty error string")
	}

	// Nil cause should work
	ae2 := NewAuthError("play.auth", nil)
	if ae2 == nil {
		t.Fatal("constructor returned nil")
	}
	if s := ae2.Error(); s == "" {
		t.Fatal("empty error string for nil cause")
	}
}

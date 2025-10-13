package errors

import (
	"context"
	stdErrors "errors"
	"fmt"
	"testing"
	"time"
)

// fakeTimeoutErr simulates a net.Error with Timeout semantics (we don't need full net.Error here).
type fakeTimeoutErr struct{}

func (fakeTimeoutErr) Error() string { return "fake timeout" }
func (fakeTimeoutErr) Timeout() bool { return true }

func TestIsProtocolErrorClassification(t *testing.T) {
	root := stdErrors.New("root")
	wrapped := fmt.Errorf("adding context: %w", root)
	hs := NewHandshakeError("server.read", wrapped)
	if !IsProtocolError(hs) {
		t.Fatalf("expected IsProtocolError=true for handshake error")
	}
	if !stdErrors.Is(hs, root) {
		t.Fatalf("expected errors.Is to find root cause")
	}
	var he *HandshakeError
	if !stdErrors.As(hs, &he) {
		t.Fatalf("expected errors.As to *HandshakeError")
	}
	if he.Op != "server.read" {
		t.Fatalf("unexpected op: %s", he.Op)
	}

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

func TestIsTimeout(t *testing.T) {
	root := fakeTimeoutErr{}
	to := NewTimeoutError("handshake.read", 5*time.Second, root)
	if !IsTimeout(to) {
		t.Fatalf("expected TimeoutError recognized")
	}
	if IsProtocolError(to) {
		t.Fatalf("timeout should NOT be protocol error")
	}
	if !IsTimeout(context.DeadlineExceeded) {
		t.Fatalf("expected context deadline recognized")
	}
	var ne error = root
	if !IsTimeout(ne) {
		t.Fatalf("expected net-like timeout recognized")
	}
}

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

func TestNilSafety(t *testing.T) {
	if IsProtocolError(nil) {
		t.Fatalf("nil should not be protocol error")
	}
	if IsTimeout(nil) {
		t.Fatalf("nil should not be timeout")
	}
}

func TestConstructorWithoutCause(t *testing.T) {
	ck := NewChunkError("parse.msgHeader", nil)
	if ck == nil {
		t.Fatalf("constructor returned nil")
	}
	if errStr := ck.Error(); errStr == "" {
		t.Fatalf("expected non-empty error string")
	}
}

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

func TestNegativePredicates(t *testing.T) {
	if IsProtocolError(stdErrors.New("plain")) {
		t.Fatalf("plain error shouldn't be protocol")
	}
	if IsTimeout(stdErrors.New("plain")) {
		t.Fatalf("plain error shouldn't be timeout")
	}
}

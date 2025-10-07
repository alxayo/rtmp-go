package handshake

import (
	stdErrors "errors"
	"testing"

	rerrors "github.com/alxayo/go-rtmp/internal/errors"
)

// helper to assert protocol / handshake error
func isHandshakeErr(err error) bool {
	if err == nil {
		return false
	}
	var he *rerrors.HandshakeError
	return stdErrors.As(err, &he)
}

func TestStateString(t *testing.T) {
	cases := []struct {
		s    State
		want string
	}{
		{StateInitial, "Initial"},
		{StateRecvC0C1, "RecvC0C1"},
		{StateSentS0S1S2, "SentS0S1S2"},
		{StateRecvC2, "RecvC2"},
		{StateCompleted, "Completed"},
		{State(999), "Unknown"},
	}
	for _, c := range cases {
		if got := c.s.String(); got != c.want {
			t.Fatalf("state %v got %q want %q", int(c.s), got, c.want)
		}
	}
}

func TestHandshakeTransitions(t *testing.T) {
	h := New()
	if h.State() != StateInitial {
		t.Fatalf("expected Initial")
	}

	// Prepare minimal valid C1/S1/C2 buffers.
	c1 := make([]byte, PacketSize)
	c1[0], c1[1], c1[2], c1[3] = 0x00, 0x00, 0x00, 0x01 // timestamp 1
	if err := h.AcceptC0C1(Version, c1); err != nil {
		t.Fatalf("AcceptC0C1 failed: %v", err)
	}
	if h.State() != StateRecvC0C1 {
		t.Fatalf("expected RecvC0C1, got %s", h.State())
	}
	if h.C1Timestamp() != 1 {
		t.Fatalf("expected C1 ts=1 got %d", h.C1Timestamp())
	}

	s1 := make([]byte, PacketSize)
	s1[0], s1[1], s1[2], s1[3] = 0x00, 0x00, 0x00, 0x02 // timestamp 2
	if err := h.SetS1(s1); err != nil {
		t.Fatalf("SetS1 failed: %v", err)
	}
	if h.State() != StateSentS0S1S2 {
		t.Fatalf("expected SentS0S1S2 got %s", h.State())
	}
	if h.S1Timestamp() != 2 {
		t.Fatalf("expected S1 ts=2 got %d", h.S1Timestamp())
	}

	c2 := make([]byte, PacketSize)
	if err := h.AcceptC2(c2); err != nil {
		t.Fatalf("AcceptC2 failed: %v", err)
	}
	if h.State() != StateRecvC2 {
		t.Fatalf("expected RecvC2 got %s", h.State())
	}

	if err := h.Complete(); err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	if !h.HasCompleted() || h.State() != StateCompleted {
		t.Fatalf("expected Completed")
	}
}

func TestHandshakeInvalidTransitions(t *testing.T) {
	h := New()
	bad := make([]byte, PacketSize-1)

	// wrong version
	if err := h.AcceptC0C1(0x05, make([]byte, PacketSize)); err == nil || !isHandshakeErr(err) {
		t.Fatalf("expected version error")
	}
	// wrong size
	if err := h.AcceptC0C1(Version, bad); err == nil || !isHandshakeErr(err) {
		t.Fatalf("expected size error")
	}

	// complete valid first step
	if err := h.AcceptC0C1(Version, make([]byte, PacketSize)); err != nil {
		t.Fatalf("unexpected: %v", err)
	}

	// invalid SetS1 size
	if err := h.SetS1(bad); err == nil || !isHandshakeErr(err) {
		t.Fatalf("expected SetS1 size error")
	}

	// valid SetS1
	if err := h.SetS1(make([]byte, PacketSize)); err != nil {
		t.Fatalf("SetS1 valid failed: %v", err)
	}

	// AcceptC2 wrong size
	if err := h.AcceptC2(bad); err == nil || !isHandshakeErr(err) {
		t.Fatalf("expected C2 size error")
	}

	// valid AcceptC2
	if err := h.AcceptC2(make([]byte, PacketSize)); err != nil {
		t.Fatalf("AcceptC2 valid failed: %v", err)
	}

	// premature Complete (we already moved to RecvC2 so this should succeed AFTER we call Complete once)
	// Call Complete twice: second should error.
	if err := h.Complete(); err != nil {
		t.Fatalf("Complete first failed: %v", err)
	}
	if err := h.Complete(); err == nil || !isHandshakeErr(err) {
		t.Fatalf("expected Complete state error")
	}
}

func TestInvalidOrder(t *testing.T) {
	h := New()
	// Calling SetS1 before AcceptC0C1
	if err := h.SetS1(make([]byte, PacketSize)); err == nil || !isHandshakeErr(err) {
		t.Fatalf("expected state error for SetS1 early")
	}
	// Calling AcceptC2 before proper states
	if err := h.AcceptC2(make([]byte, PacketSize)); err == nil || !isHandshakeErr(err) {
		t.Fatalf("expected state error for AcceptC2 early")
	}
	if err := h.Complete(); err == nil || !isHandshakeErr(err) {
		t.Fatalf("expected state error for Complete early")
	}
}

func TestHandshakeAdditionalCoverage(t *testing.T) {
	h := New()
	// Accessors before data present.
	if h.C1() != nil {
		t.Fatalf("expected nil C1 before accept")
	}
	if h.S1() != nil {
		t.Fatalf("expected nil S1 before set")
	}

	c1 := make([]byte, PacketSize)
	if err := h.AcceptC0C1(Version, c1); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if h.C1() == nil {
		t.Fatalf("expected C1 data after accept")
	}
	// Second AcceptC0C1 should error (invalid state)
	if err := h.AcceptC0C1(Version, c1); err == nil || !isHandshakeErr(err) {
		t.Fatalf("expected second AcceptC0C1 state error")
	}

	s1 := make([]byte, PacketSize)
	if err := h.SetS1(s1); err != nil {
		t.Fatalf("unexpected SetS1: %v", err)
	}
	if h.S1() == nil {
		t.Fatalf("expected S1 data after set")
	}
	if err := h.SetS1(s1); err == nil || !isHandshakeErr(err) {
		t.Fatalf("expected second SetS1 state error")
	}

	c2 := make([]byte, PacketSize)
	if err := h.AcceptC2(c2); err != nil {
		t.Fatalf("unexpected AcceptC2: %v", err)
	}
	if err := h.Complete(); err != nil {
		t.Fatalf("unexpected Complete: %v", err)
	}
	// AcceptC2 after completion should error.
	if err := h.AcceptC2(c2); err == nil || !isHandshakeErr(err) {
		t.Fatalf("expected AcceptC2 after completion error")
	}
}

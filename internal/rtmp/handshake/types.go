package handshake

import (
	"fmt"
	errors "github.com/alxayo/go-rtmp/internal/errors"
)

// Handshake constants based on RTMP simple (version 3) handshake.
// C0/S0 is a single version byte (0x03). Each of C1, S1, C2, S2 are 1536 bytes.
const (
	Version           = 0x03
	PacketSize        = 1536 // size of C1/S1/C2/S2 blocks
	timeFieldOffset   = 0    // first 4 bytes are timestamp
	zeroFieldOffset   = 4    // next 4 bytes are zero / reserved
	randomFieldOffset = 8    // remaining 1528 bytes random data
)

// State represents the server-side simple handshake progression.
// (Client differs slightly; this FSM is focused on the server path required
// for subsequent tasks. Client can reuse the enum for symmetry.)
type State int

const (
	StateInitial State = iota
	StateRecvC0C1
	StateSentS0S1S2
	StateRecvC2
	StateCompleted
)

func (s State) String() string {
	switch s {
	case StateInitial:
		return "Initial"
	case StateRecvC0C1:
		return "RecvC0C1"
	case StateSentS0S1S2:
		return "SentS0S1S2"
	case StateRecvC2:
		return "RecvC2"
	case StateCompleted:
		return "Completed"
	default:
		return "Unknown"
	}
}

// Handshake holds in-memory state required to validate and complete the
// RTMP simple handshake. It deliberately stores full C1 and S1 blocks so
// later phases (e.g. echo validation, timestamps) can reference them.
//
// The byte arrays are fixed-size to avoid extra allocations and to enforce
// compile-time size guarantees.
type Handshake struct {
	state       State
	c1          [PacketSize]byte
	s1          [PacketSize]byte
	haveC1      bool
	haveS1      bool
	haveC2      bool
	c1Timestamp uint32
	s1Timestamp uint32
}

// New creates a new handshake state container in Initial state.
func New() *Handshake { return &Handshake{state: StateInitial} }

// State returns the current FSM state.
func (h *Handshake) State() State { return h.state }

// AcceptC0C1 records the client's C0 version byte and C1 1536-byte block.
// Expects to be called in StateInitial. On success moves to StateRecvC0C1.
func (h *Handshake) AcceptC0C1(c0 byte, c1 []byte) error {
	if h.state != StateInitial {
		return errors.NewHandshakeError("accept C0+C1", fmt.Errorf("invalid state %s", h.state))
	}
	if c0 != Version {
		return errors.NewHandshakeError("accept C0+C1", fmt.Errorf("unsupported version 0x%02x", c0))
	}
	if len(c1) != PacketSize {
		return errors.NewHandshakeError("accept C0+C1", fmt.Errorf("invalid C1 size %d", len(c1)))
	}
	copy(h.c1[:], c1)
	h.haveC1 = true
	h.c1Timestamp = uint32(c1[0])<<24 | uint32(c1[1])<<16 | uint32(c1[2])<<8 | uint32(c1[3])
	h.state = StateRecvC0C1
	return nil
}

// SetS1 sets the server's S1 block (must be 1536 bytes) after C0+C1 is
// accepted. Transition: RecvC0C1 -> SentS0S1S2.
func (h *Handshake) SetS1(s1 []byte) error {
	if h.state != StateRecvC0C1 {
		return errors.NewHandshakeError("set S1", fmt.Errorf("invalid state %s", h.state))
	}
	if len(s1) != PacketSize {
		return errors.NewHandshakeError("set S1", fmt.Errorf("invalid S1 size %d", len(s1)))
	}
	copy(h.s1[:], s1)
	h.haveS1 = true
	h.s1Timestamp = uint32(s1[0])<<24 | uint32(s1[1])<<16 | uint32(s1[2])<<8 | uint32(s1[3])
	h.state = StateSentS0S1S2
	return nil
}

// AcceptC2 registers receipt of the client's C2 block (length validation only
// here — full echo validation of S1 can be added in the FSM task). Transition:
// SentS0S1S2 -> RecvC2.
func (h *Handshake) AcceptC2(c2 []byte) error {
	if h.state != StateSentS0S1S2 {
		return errors.NewHandshakeError("accept C2", fmt.Errorf("invalid state %s", h.state))
	}
	if len(c2) != PacketSize {
		return errors.NewHandshakeError("accept C2", fmt.Errorf("invalid C2 size %d", len(c2)))
	}
	h.haveC2 = true
	h.state = StateRecvC2
	return nil
}

// Complete marks the handshake as fully completed. Transition: RecvC2 -> Completed.
func (h *Handshake) Complete() error {
	if h.state != StateRecvC2 {
		return errors.NewHandshakeError("complete", fmt.Errorf("invalid state %s", h.state))
	}
	h.state = StateCompleted
	return nil
}

// Accessors for timestamps (useful in tests and later logic).
func (h *Handshake) C1Timestamp() uint32 { return h.c1Timestamp }
func (h *Handshake) S1Timestamp() uint32 { return h.s1Timestamp }

// C1 returns a copy of the C1 payload if present, else nil.
func (h *Handshake) C1() []byte {
	if !h.haveC1 {
		return nil
	}
	b := make([]byte, PacketSize)
	copy(b, h.c1[:])
	return b
}

// S1 returns a copy of the S1 payload if present, else nil.
func (h *Handshake) S1() []byte {
	if !h.haveS1 {
		return nil
	}
	b := make([]byte, PacketSize)
	copy(b, h.s1[:])
	return b
}

// HasCompleted returns true if the FSM reached Completed.
func (h *Handshake) HasCompleted() bool { return h.state == StateCompleted }

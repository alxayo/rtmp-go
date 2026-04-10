package circular

import (
	"fmt"
	"testing"
)

// TestNew verifies that New masks raw uint32 values to 31 bits,
// stripping the top bit so the result is always in [0, MaxVal].
func TestNew(t *testing.T) {
	tests := []struct {
		name string
		in   uint32
		want Number
	}{
		{"zero", 0, 0},
		{"small value", 42, 42},
		{"max value", uint32(MaxVal), MaxVal},
		{"above max masks top bit", 0x80000000, 0},                     // only top bit set → masked to 0
		{"full 32-bit masks to MaxVal", 0xFFFFFFFF, MaxVal},             // all bits set → top bit cleared
		{"just above max", uint32(MaxVal) + 1, 0},                       // 2^31 → 0 after mask
		{"high bit plus value", 0x80000005, 5},                          // top bit + 5 → 5
		{"large value with top bit", 0xDEADBEEF, Number(0x5EADBEEF)},    // mask clears bit 31
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := New(tt.in)
			if got != tt.want {
				t.Errorf("New(%#x) = %#x, want %#x", tt.in, got, tt.want)
			}
		})
	}
}

// TestVal checks that Val returns the raw underlying uint32.
func TestVal(t *testing.T) {
	tests := []struct {
		n    Number
		want uint32
	}{
		{0, 0},
		{42, 42},
		{MaxVal, uint32(MaxVal)},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d", tt.n), func(t *testing.T) {
			if got := tt.n.Val(); got != tt.want {
				t.Errorf("Number(%d).Val() = %d, want %d", tt.n, got, tt.want)
			}
		})
	}
}

// TestInc verifies incrementing by 1 with wraparound.
func TestInc(t *testing.T) {
	tests := []struct {
		name string
		n    Number
		want Number
	}{
		{"zero increments to one", 0, 1},
		{"normal increment", 99, 100},
		{"max wraps to zero", MaxVal, 0},         // critical wraparound edge case
		{"max-1 to max", MaxVal - 1, MaxVal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.n.Inc(); got != tt.want {
				t.Errorf("Number(%d).Inc() = %d, want %d", tt.n, got, tt.want)
			}
		})
	}
}

// TestAdd verifies addition of an arbitrary delta with wraparound.
func TestAdd(t *testing.T) {
	tests := []struct {
		name  string
		n     Number
		delta uint32
		want  Number
	}{
		{"add zero", 5, 0, 5},
		{"add small", 5, 10, 15},
		{"add wraps around", MaxVal, 1, 0},                   // MaxVal + 1 wraps to 0
		{"add wraps past zero", MaxVal - 2, 5, 2},            // (MaxVal-2)+5 = MaxVal+3 → 2
		{"add large delta", 0, uint32(MaxVal), MaxVal},        // 0 + MaxVal = MaxVal
		{"add large wraps", MaxVal, uint32(MaxVal), MaxVal - 1}, // MaxVal + MaxVal = 2*MaxVal → masked
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.n.Add(tt.delta); got != tt.want {
				t.Errorf("Number(%d).Add(%d) = %d, want %d", tt.n, tt.delta, got, tt.want)
			}
		})
	}
}

// TestDistance checks forward (clockwise) distance in circular space.
func TestDistance(t *testing.T) {
	tests := []struct {
		name string
		from Number
		to   Number
		want uint32
	}{
		{"same value", 5, 5, 0},                                    // distance to self is 0
		{"zero to zero", 0, 0, 0},
		{"forward normal", 5, 10, 5},                               // simple subtraction
		{"forward from zero", 0, 100, 100},
		{"forward to max", 0, MaxVal, uint32(MaxVal)},
		{"wraps around", MaxVal - 5, 3, 9},                         // 6 steps to MaxVal + 1 step wrap + 3 = 9 (actually: MaxVal - (MaxVal-5) = 5... let me recalc)
		{"wraps from max", MaxVal, 0, 1},                           // one step: MaxVal → 0
		{"wraps from max to 5", MaxVal, 5, 6},                      // MaxVal→0→1→2→3→4→5 = 6 steps
		{"near-max to near-zero", MaxVal - 1, 2, 4},                // (MaxVal-1)→MaxVal→0→1→2 = 4 steps
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.from.Distance(tt.to); got != tt.want {
				t.Errorf("Number(%d).Distance(%d) = %d, want %d", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

// TestBefore checks the half-space ordering rule.
// n.Before(other) is true when n is "earlier" than other in the
// circular sequence, i.e. the forward distance is in (0, HalfMax).
func TestBefore(t *testing.T) {
	tests := []struct {
		name string
		n    Number
		other Number
		want bool
	}{
		{"normal order", 5, 10, true},
		{"reverse order", 10, 5, false},
		{"equal values", 7, 7, false},                                // equal → not before
		{"zero before one", 0, 1, true},
		{"wraps: near-max before small", MaxVal - 1, 2, true},       // forward distance is 4, well within HalfMax
		{"wraps: small NOT before near-max", 2, MaxVal - 1, false},  // forward distance > HalfMax → false
		{"zero and max", 0, MaxVal, false},                           // distance = MaxVal ≥ HalfMax → false
		{"max before zero", MaxVal, 0, true},                         // distance = 1 → true
		{"at half boundary", 0, HalfMax, false},                      // distance == HalfMax → not strictly less → false
		{"just under half", 0, HalfMax - 1, true},                    // distance = HalfMax-1 < HalfMax → true
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.n.Before(tt.other); got != tt.want {
				t.Errorf("Number(%d).Before(%d) = %v, want %v", tt.n, tt.other, got, tt.want)
			}
		})
	}
}

// TestAfter mirrors TestBefore: n.After(other) == other.Before(n).
func TestAfter(t *testing.T) {
	tests := []struct {
		name  string
		n     Number
		other Number
		want  bool
	}{
		{"normal", 10, 5, true},
		{"reverse", 5, 10, false},
		{"equal", 7, 7, false},
		{"wraps: small after near-max", 2, MaxVal - 1, true},            // MaxVal-1 is before 2 (wrapping) → 2 is after
		{"wraps: near-max NOT after small", MaxVal - 1, 2, false},    // near-max is BEFORE small (wrapping) → not after
		{"zero after max", 0, MaxVal, true},                          // MaxVal is before 0 (distance=1) → 0 is after
		{"max NOT after zero", MaxVal, 0, false},                     // MaxVal is before 0 → not after
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.n.After(tt.other); got != tt.want {
				t.Errorf("Number(%d).After(%d) = %v, want %v", tt.n, tt.other, got, tt.want)
			}
		})
	}
}

// TestBeforeOrEqual and TestAfterOrEqual add the equality case.
func TestBeforeOrEqual(t *testing.T) {
	tests := []struct {
		name  string
		n     Number
		other Number
		want  bool
	}{
		{"equal", 5, 5, true},
		{"before", 5, 10, true},
		{"after", 10, 5, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.n.BeforeOrEqual(tt.other); got != tt.want {
				t.Errorf("Number(%d).BeforeOrEqual(%d) = %v, want %v", tt.n, tt.other, got, tt.want)
			}
		})
	}
}

func TestAfterOrEqual(t *testing.T) {
	tests := []struct {
		name  string
		n     Number
		other Number
		want  bool
	}{
		{"equal", 5, 5, true},
		{"after", 10, 5, true},
		{"before", 5, 10, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.n.AfterOrEqual(tt.other); got != tt.want {
				t.Errorf("Number(%d).AfterOrEqual(%d) = %v, want %v", tt.n, tt.other, got, tt.want)
			}
		})
	}
}

// TestInRange checks inclusive circular range membership for both
// normal (non-wrapping) and wrapping ranges.
func TestInRange(t *testing.T) {
	tests := []struct {
		name string
		n    Number
		lo   Number
		hi   Number
		want bool
	}{
		// --- Normal (non-wrapping) ranges ---
		{"inside normal range", 5, 3, 7, true},
		{"at low bound", 3, 3, 7, true},
		{"at high bound", 7, 3, 7, true},
		{"below range", 2, 3, 7, false},
		{"above range", 8, 3, 7, false},

		// --- Wrapping ranges: [lo..MaxVal] ∪ [0..hi] ---
		{"wrap: inside low segment", MaxVal - 3, MaxVal - 5, 3, true},
		{"wrap: at lo bound", MaxVal - 5, MaxVal - 5, 3, true},
		{"wrap: at hi bound", 3, MaxVal - 5, 3, true},
		{"wrap: at zero", 0, MaxVal - 5, 3, true},
		{"wrap: outside in middle", 100, MaxVal - 5, 3, false},

		// --- Single-element range ---
		{"single element in", 5, 5, 5, true},
		{"single element out", 6, 5, 5, false},

		// --- Full-ish ranges ---
		{"zero in [0, MaxVal-1]", 0, 0, MaxVal - 1, true},
		{"MaxVal in [0, MaxVal]", MaxVal, 0, MaxVal, true},

		// --- Edge: range spanning zero ---
		{"wrap: MaxVal in [MaxVal, 0]", MaxVal, MaxVal, 0, true},
		{"wrap: 0 in [MaxVal, 0]", 0, MaxVal, 0, true},
		{"wrap: 1 NOT in [MaxVal, 0]", 1, MaxVal, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.n.InRange(tt.lo, tt.hi); got != tt.want {
				t.Errorf("Number(%d).InRange(%d, %d) = %v, want %v",
					tt.n, tt.lo, tt.hi, got, tt.want)
			}
		})
	}
}

// TestEdgeCases exercises the extreme boundaries of the 31-bit space.
func TestEdgeCases(t *testing.T) {
	// Incrementing MaxVal must wrap to 0.
	if got := MaxVal.Inc(); got != 0 {
		t.Errorf("MaxVal.Inc() = %d, want 0", got)
	}

	// Distance from 0 to 0 is 0.
	if got := Number(0).Distance(0); got != 0 {
		t.Errorf("Distance(0, 0) = %d, want 0", got)
	}

	// 0 is not before 0.
	if Number(0).Before(0) {
		t.Error("0.Before(0) should be false")
	}

	// HalfMax boundary: distance exactly equals HalfMax → Before is false.
	if Number(0).Before(HalfMax) {
		t.Error("0.Before(HalfMax) should be false (distance == HalfMax, not <)")
	}

	// One less than HalfMax → Before is true.
	if !Number(0).Before(HalfMax - 1) {
		t.Error("0.Before(HalfMax-1) should be true")
	}

	// Symmetry check: if A.Before(B) then B.After(A).
	a, b := Number(100), Number(200)
	if a.Before(b) != b.After(a) {
		t.Error("Before/After symmetry broken for 100, 200")
	}

	// Wraparound symmetry check.
	c, d := MaxVal-10, Number(10)
	if c.Before(d) != d.After(c) {
		t.Error("Before/After symmetry broken across wraparound")
	}
}

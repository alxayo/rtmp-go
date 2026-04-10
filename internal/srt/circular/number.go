package circular

// Number represents a 31-bit SRT sequence number with wraparound arithmetic.
// SRT uses 31-bit sequence numbers [0, 2^31-1] that wrap around.
// This is like a clock: after reaching the maximum, it goes back to 0.
// Comparisons must handle this wraparound correctly.
type Number uint32

const (
	// MaxVal is 2^31 - 1, the largest value a 31-bit sequence number can hold.
	// Any value above this gets masked (the top bit is stripped off).
	MaxVal Number = 0x7FFFFFFF

	// HalfMax is 2^30, exactly half the sequence-number space.
	// It is the dividing line for the "half-space" comparison rule:
	// if the forward distance between two numbers is less than HalfMax,
	// the first number is considered "before" the second.
	HalfMax Number = 0x40000000
)

// New creates a circular Number from a raw uint32.
// It masks the value to 31 bits by clearing the top bit,
// ensuring the result is always in [0, MaxVal].
func New(v uint32) Number { return Number(v & uint32(MaxVal)) }

// Val returns the underlying uint32 value of the sequence number.
func (n Number) Val() uint32 { return uint32(n) }

// Inc returns the next sequence number (n + 1) with wraparound.
// After MaxVal the sequence wraps back to 0.
func (n Number) Inc() Number { return New(uint32(n) + 1) }

// Add returns n + delta with wraparound.
// The result is always masked to 31 bits, so adding past MaxVal wraps.
func (n Number) Add(delta uint32) Number { return New(uint32(n) + delta) }

// Distance returns the forward (clockwise) distance from n to other
// in the circular 31-bit space. The result is always non-negative.
// If other == n the distance is 0.
//
// Example on a tiny 4-value circle [0,1,2,3]:
//
//	Distance(1, 3) = 2   (normal: 3-1)
//	Distance(3, 1) = 2   (wrapping: 4-3+1 = 2, going 3→0→1)
func (n Number) Distance(other Number) uint32 {
	// When other is ahead of (or equal to) n, simple subtraction works.
	if other >= n {
		return uint32(other - n)
	}
	// When other has wrapped past 0, we count the remaining distance
	// from n to MaxVal, then add the distance from 0 to other (+1 for
	// the step from MaxVal to 0).
	return uint32(MaxVal) - uint32(n) + uint32(other) + 1
}

// Before returns true if n comes before other in the circular space.
//
// It uses the "half-space" rule: compute the forward distance from n
// to other (mod 2^31). If that distance is greater than 0 and less
// than HalfMax, n is considered earlier in the sequence.
//
// This means each number has roughly 2^30 numbers "ahead" of it and
// 2^30 numbers "behind" it, which is sufficient for any realistic
// window of in-flight SRT packets.
func (n Number) Before(other Number) bool {
	// Compute forward distance as a circular number so it wraps correctly.
	diff := New(uint32(other) - uint32(n))
	// diff == 0 means the numbers are equal → not "before".
	// diff >= HalfMax means other is actually behind n (more than half
	// the space away in the forward direction).
	return diff > 0 && diff < HalfMax
}

// After returns true if n comes after other in the circular space.
// It is simply the reverse of Before: n is after other when other is
// before n.
func (n Number) After(other Number) bool { return other.Before(n) }

// BeforeOrEqual returns true if n comes before or is equal to other.
func (n Number) BeforeOrEqual(other Number) bool { return n == other || n.Before(other) }

// AfterOrEqual returns true if n comes after or is equal to other.
func (n Number) AfterOrEqual(other Number) bool { return n == other || n.After(other) }

// InRange returns true if n falls within the inclusive circular range
// [lo, hi].
//
// Two cases arise depending on whether the range wraps around MaxVal:
//
//  1. Normal (no wrap): lo <= hi  →  lo <= n <= hi
//  2. Wrapping:         lo > hi   →  n >= lo  OR  n <= hi
//     e.g. [MaxVal-5 .. 3] covers {MaxVal-5, …, MaxVal, 0, 1, 2, 3}
func (n Number) InRange(lo, hi Number) bool {
	if lo.BeforeOrEqual(hi) {
		// Normal contiguous range: n must be between lo and hi.
		return lo.BeforeOrEqual(n) && n.BeforeOrEqual(hi)
	}
	// Wrapping range: the valid region is [lo..MaxVal] ∪ [0..hi].
	// n qualifies if it is at or after lo, OR at or before hi.
	return lo.BeforeOrEqual(n) || n.BeforeOrEqual(hi)
}

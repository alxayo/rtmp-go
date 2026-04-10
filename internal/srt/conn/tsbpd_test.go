package conn

import "testing"

// TestTSBPDManagerDeliveryTime tests that the delivery time is correctly
// calculated as: baseTime + (pktTimestamp - firstTimestamp) + delay.
func TestTSBPDManagerDeliveryTime(t *testing.T) {
	mgr := NewTSBPDManager(120) // 120ms delay

	// Override baseTime to a known value for predictable testing
	mgr.baseTime = 1_000_000 // 1 second in microseconds

	// First packet at timestamp 5000μs → offset = 0, delivery = base + 0 + delay
	dt := mgr.DeliveryTime(5000)
	// delay = 120ms = 120,000μs
	want := uint64(1_000_000 + 0 + 120_000)
	if dt != want {
		t.Errorf("first packet delivery time: got %d, want %d", dt, want)
	}

	// Second packet at timestamp 15000μs → offset = 10000, delivery = base + 10000 + delay
	dt = mgr.DeliveryTime(15000)
	want = uint64(1_000_000 + 10_000 + 120_000)
	if dt != want {
		t.Errorf("second packet delivery time: got %d, want %d", dt, want)
	}
}

// TestTSBPDManagerTooLate tests the too-late packet drop detection.
func TestTSBPDManagerTooLate(t *testing.T) {
	mgr := NewTSBPDManager(100) // 100ms delay = 100,000μs
	mgr.baseTime = 1_000_000

	// First packet at timestamp 5000μs
	_ = mgr.DeliveryTime(5000) // delivery = 1,000,000 + 0 + 100,000 = 1,100,000

	// Before delivery time: not too late
	if mgr.TooLate(5000, 1_050_000) {
		t.Error("packet should not be too late before delivery time")
	}

	// At delivery time: not too late (equal means ready, not dropped)
	if mgr.TooLate(5000, 1_100_000) {
		t.Error("packet should not be too late at exactly delivery time")
	}

	// After delivery time: too late
	if !mgr.TooLate(5000, 1_100_001) {
		t.Error("packet should be too late after delivery time")
	}
}

// TestTSBPDManagerIsReady tests the delivery readiness check.
func TestTSBPDManagerIsReady(t *testing.T) {
	mgr := NewTSBPDManager(50) // 50ms delay
	mgr.baseTime = 0

	_ = mgr.DeliveryTime(1000)

	// Before delivery time: not ready
	if mgr.IsReady(1000, 49_999) {
		t.Error("packet should not be ready before delivery time")
	}

	// At delivery time: ready
	if !mgr.IsReady(1000, 50_000) {
		t.Error("packet should be ready at delivery time")
	}

	// After delivery time: still ready
	if !mgr.IsReady(1000, 100_000) {
		t.Error("packet should be ready after delivery time")
	}
}

// TestTSBPDManagerWraparound tests handling of 32-bit timestamp wraparound.
// SRT timestamps wrap every ~71.6 minutes (2^32 microseconds).
func TestTSBPDManagerWraparound(t *testing.T) {
	mgr := NewTSBPDManager(0) // 0 delay for simplicity
	mgr.baseTime = 0

	// Start near the end of the 32-bit range
	firstTS := uint32(0xFFFFF000) // Very close to max
	_ = mgr.DeliveryTime(firstTS) // Sets the base

	// Next packet is before the wrap — small increment
	ts2 := firstTS + 1000
	dt2 := mgr.DeliveryTime(ts2)
	if dt2 != 1000 {
		t.Errorf("pre-wrap delivery time: got %d, want 1000", dt2)
	}

	// Now a packet after the wrap — timestamp is small but actually later
	ts3 := uint32(500) // Wrapped around (0xFFFFFFFF → 0x00000000 → 0x000001F4)
	dt3 := mgr.DeliveryTime(ts3)

	// Expected: the offset should be (0x100000000 + 500 - firstTS)
	expectedOffset := tsWrapPeriod + uint64(ts3) - uint64(firstTS)
	if dt3 != expectedOffset {
		t.Errorf("post-wrap delivery time: got %d, want %d", dt3, expectedOffset)
	}

	// Verify monotonicity: post-wrap delivery should be after pre-wrap
	if dt3 <= dt2 {
		t.Errorf("delivery times should be monotonically increasing across wrap: %d <= %d", dt3, dt2)
	}
}

// TestTSBPDManagerZeroDelay tests behavior with zero delay (useful for testing).
func TestTSBPDManagerZeroDelay(t *testing.T) {
	mgr := NewTSBPDManager(0)
	mgr.baseTime = 0

	// First packet
	dt := mgr.DeliveryTime(1000)
	if dt != 0 { // offset=0, delay=0
		t.Errorf("zero delay first packet: got %d, want 0", dt)
	}

	// Second packet, 5000μs later
	dt = mgr.DeliveryTime(6000)
	if dt != 5000 { // offset=5000, delay=0
		t.Errorf("zero delay second packet: got %d, want 5000", dt)
	}
}

// TestACKStateShouldSendACK tests ACK generation timing logic.
func TestACKStateShouldSendACK(t *testing.T) {
	state := NewACKState()

	// Should send ACK immediately (lastACKTimeUs == 0)
	if !state.ShouldSendACK(1000) {
		t.Error("should send first ACK immediately")
	}

	// Record that we sent an ACK at time 1000
	state.RecordACKSent(1000)

	// Too soon — should not send another ACK
	if state.ShouldSendACK(5000) {
		t.Error("should not send ACK before 10ms interval")
	}

	// After 10ms (10000μs) — should send
	if !state.ShouldSendACK(11001) {
		t.Error("should send ACK after 10ms interval")
	}
}

// TestACKStatePacketCountTrigger tests that ACKs are triggered after
// receiving 64 packets, regardless of time.
func TestACKStatePacketCountTrigger(t *testing.T) {
	state := NewACKState()

	// Record initial ACK at time 1000μs (non-zero to avoid "never sent" trigger)
	state.RecordACKSent(1000)

	// Receive 63 packets — not enough to trigger
	for i := 0; i < 63; i++ {
		state.OnDataReceived()
	}
	if state.ShouldSendACK(1001) { // Very soon after last ACK
		t.Error("should not trigger ACK after 63 packets")
	}

	// One more packet — now we hit 64
	state.OnDataReceived()
	if !state.ShouldSendACK(1001) {
		t.Error("should trigger ACK after 64 packets")
	}
}

// TestACKStateRTTMeasurement tests the ACK→ACKACK RTT measurement flow.
func TestACKStateRTTMeasurement(t *testing.T) {
	state := NewACKState()

	// Send an ACK at time 100000μs
	ackSeq := state.RecordACKSent(100_000)
	if ackSeq != 1 {
		t.Errorf("first ACK seq: got %d, want 1", ackSeq)
	}

	// Look up the send time (simulating ACKACK arrival)
	sendTime := state.GetACKSendTime(ackSeq)
	if sendTime != 100_000 {
		t.Errorf("ACK send time: got %d, want 100000", sendTime)
	}

	// Second lookup should return 0 (already consumed)
	sendTime = state.GetACKSendTime(ackSeq)
	if sendTime != 0 {
		t.Error("duplicate ACK lookup should return 0")
	}
}

// TestACKStateSequenceIncrement tests that ACK sequence numbers increment.
func TestACKStateSequenceIncrement(t *testing.T) {
	state := NewACKState()

	seq1 := state.RecordACKSent(1000)
	seq2 := state.RecordACKSent(2000)
	seq3 := state.RecordACKSent(3000)

	if seq1 != 1 || seq2 != 2 || seq3 != 3 {
		t.Errorf("ACK sequences should increment: got %d, %d, %d", seq1, seq2, seq3)
	}
}

// TestACKStateOldPendingCleanup tests that old pending ACKs are cleaned up
// to prevent memory leaks in long-running connections.
func TestACKStateOldPendingCleanup(t *testing.T) {
	state := NewACKState()

	// Send ACK at time 0
	seq1 := state.RecordACKSent(0)

	// Send another ACK 11 seconds later — should clean up the old one
	_ = state.RecordACKSent(11_000_000)

	// The old ACK should have been cleaned up
	sendTime := state.GetACKSendTime(seq1)
	if sendTime != 0 {
		t.Error("old pending ACK should have been cleaned up")
	}
}

package conn

// TSBPD — Timestamp-Based Packet Delivery
//
// TSBPD is SRT's key mechanism for smooth live streaming over the internet.
// The idea is simple: every packet is delivered to the application at a fixed
// delay after it was originally sent, regardless of network jitter.
//
// Here's how it works:
//   1. The sender stamps each packet with a microsecond timestamp
//   2. The receiver holds each packet until:
//        deliveryTime = packetTimestamp + tsbpdDelay
//   3. This creates a "jitter buffer" — packets that arrive early wait,
//      and packets that arrive slightly late still get delivered on time
//   4. Packets that arrive WAY too late are dropped (TLPKTDROP)
//
// Timestamp wraparound:
// SRT timestamps are 32-bit microsecond counters. 2^32 microseconds is about
// 71.6 minutes, so the timestamp wraps back to 0 during long streams.
// We track a base offset to convert to 64-bit and handle these wraps.

import "time"

// tsWrapPeriod is the number of microseconds before a 32-bit timestamp wraps
// around. 2^32 = 4,294,967,296 microseconds ≈ 71.6 minutes.
const tsWrapPeriod = uint64(0x100000000)

// tsWrapThreshold is used to detect when a timestamp has wrapped around.
// If the difference between the current timestamp and the previous one is
// larger than this value (in the negative direction), we assume a wrap occurred.
// We use 75% of the wrap period as the detection threshold.
const tsWrapThreshold = tsWrapPeriod * 3 / 4

// TSBPDManager handles timestamp-based packet delivery scheduling.
// It converts SRT's 32-bit microsecond timestamps into absolute 64-bit
// delivery times, accounting for wraparound and the negotiated delay.
type TSBPDManager struct {
	// baseTime is the wall-clock time (in microseconds since Unix epoch)
	// when the connection was established. All delivery times are relative to this.
	baseTime uint64

	// tsBase is the SRT timestamp of the first packet we received.
	// All subsequent timestamps are measured relative to this.
	tsBase uint32

	// tsBaseSet tracks whether we've seen the first packet yet.
	// We need this because we can't set tsBase until we see the first timestamp.
	tsBaseSet bool

	// delay is the negotiated TSBPD delay in microseconds.
	// This is the "buffer depth" — how long we hold packets before delivery.
	delay uint64

	// wrapCount tracks how many times the 32-bit timestamp has wrapped around.
	// Each wrap adds tsWrapPeriod to our 64-bit timestamp calculation.
	wrapCount int

	// lastTS is the last timestamp we saw, used to detect wraps.
	// If the next timestamp is much smaller, we know it wrapped.
	lastTS uint32
}

// NewTSBPDManager creates a TSBPD manager with the given delay.
// The delay is specified in milliseconds (converted internally to microseconds).
func NewTSBPDManager(delayMs uint32) *TSBPDManager {
	return &TSBPDManager{
		baseTime: uint64(time.Now().UnixMicro()),
		delay:    uint64(delayMs) * 1000, // Convert ms → μs
	}
}

// DeliveryTime returns the wall-clock time (in microseconds) at which a packet
// with the given SRT timestamp should be delivered to the application.
//
// The formula is:
//
//	deliveryTime = baseTime + (pktTimestamp - firstTimestamp) + delay
//
// This ensures that all packets experience the same end-to-end delay,
// regardless of when they actually arrived at the receiver.
func (t *TSBPDManager) DeliveryTime(pktTimestamp uint32) uint64 {
	// Convert the 32-bit SRT timestamp to a 64-bit absolute offset
	absTS := t.toAbsoluteTS(pktTimestamp)

	// Delivery time = connection start + time offset + jitter buffer delay
	return t.baseTime + absTS + t.delay
}

// TooLate returns true if a packet's delivery deadline has already passed.
// These packets are no longer useful for live streaming and should be dropped.
// This implements SRT's TLPKTDROP (Too-Late Packet Drop) mechanism.
func (t *TSBPDManager) TooLate(pktTimestamp uint32, nowUs uint64) bool {
	return nowUs > t.DeliveryTime(pktTimestamp)
}

// IsReady returns true if a packet with the given timestamp is ready for
// delivery (its delivery time has arrived or passed).
func (t *TSBPDManager) IsReady(pktTimestamp uint32, nowUs uint64) bool {
	return nowUs >= t.DeliveryTime(pktTimestamp)
}

// toAbsoluteTS converts a 32-bit SRT timestamp to a 64-bit absolute
// timestamp (microseconds since the first packet), handling wraparound.
//
// The first packet's timestamp becomes the base (offset 0). Subsequent
// packets are relative to that. When the 32-bit counter wraps around
// (after ~71 minutes), we detect it and add the wrap period.
func (t *TSBPDManager) toAbsoluteTS(ts uint32) uint64 {
	// First packet: record its timestamp as the base
	if !t.tsBaseSet {
		t.tsBase = ts
		t.lastTS = ts
		t.tsBaseSet = true
		return 0 // First packet is at offset 0
	}

	// Detect timestamp wraparound:
	// If the new timestamp is much smaller than the last one,
	// the 32-bit counter probably wrapped around (e.g., from 0xFFFFFFF0 to 0x00000010)
	if ts < t.lastTS && uint64(t.lastTS)-uint64(ts) > tsWrapThreshold {
		t.wrapCount++
	}
	t.lastTS = ts

	// Calculate the absolute offset from the base timestamp,
	// accounting for any wraparounds that have occurred.
	// relative = (ts - tsBase) + wrapCount * 2^32
	relative := uint64(ts) - uint64(t.tsBase) + uint64(t.wrapCount)*tsWrapPeriod

	return relative
}

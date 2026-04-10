// Package conn manages the lifecycle of an established SRT connection,
// including packet queues, send/receive buffers, timer management, and
// the read/write APIs that higher-level code uses to exchange data.
package conn

import "time"

// Timer intervals for SRT connection management.
// These are defined by the SRT protocol specification to balance
// responsiveness (low latency) with efficiency (not too many control packets).
const (
	// ACKIntervalMs is how often the receiver sends an ACK packet to tell the
	// sender which data packets it has received. 10ms keeps latency low.
	ACKIntervalMs = 10

	// NAKIntervalMs is how often the receiver sends a NAK (negative ACK) to
	// report gaps in the received sequence. This triggers retransmission.
	NAKIntervalMs = 20

	// KeepaliveIntervalMs is how often an idle connection sends a keepalive
	// packet. Without this, firewalls/NATs would close the UDP "connection"
	// after a period of inactivity.
	KeepaliveIntervalMs = 1000
)

// TimerManager handles periodic timer events for an SRT connection.
// SRT uses three repeating timers:
//   - ACK timer: triggers sending acknowledgements to the sender
//   - NAK timer: triggers sending loss reports so the sender can retransmit
//   - Keepalive timer: prevents connection timeout during idle periods
type TimerManager struct {
	ackTicker       *time.Ticker // Fires every ACKIntervalMs
	nakTicker       *time.Ticker // Fires every NAKIntervalMs
	keepaliveTicker *time.Ticker // Fires every KeepaliveIntervalMs
}

// NewTimerManager creates timers with the standard SRT intervals.
// The caller must call Stop() when the connection is closed to free resources.
func NewTimerManager() *TimerManager {
	return &TimerManager{
		ackTicker:       time.NewTicker(ACKIntervalMs * time.Millisecond),
		nakTicker:       time.NewTicker(NAKIntervalMs * time.Millisecond),
		keepaliveTicker: time.NewTicker(KeepaliveIntervalMs * time.Millisecond),
	}
}

// ACKChan returns the channel that fires when it's time to send an ACK.
func (t *TimerManager) ACKChan() <-chan time.Time {
	return t.ackTicker.C
}

// NAKChan returns the channel that fires when it's time to send a NAK.
func (t *TimerManager) NAKChan() <-chan time.Time {
	return t.nakTicker.C
}

// KeepaliveChan returns the channel that fires when it's time to send a keepalive.
func (t *TimerManager) KeepaliveChan() <-chan time.Time {
	return t.keepaliveTicker.C
}

// Stop stops all timers and releases their resources.
// After calling Stop, none of the timer channels will fire again.
func (t *TimerManager) Stop() {
	t.ackTicker.Stop()
	t.nakTicker.Stop()
	t.keepaliveTicker.Stop()
}

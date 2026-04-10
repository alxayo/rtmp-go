package conn

// Reliability Loop — The Heart of SRT's Reliability Mechanism
//
// This file implements the main reliability loop that runs as a background
// goroutine for the entire lifetime of an SRT connection. It coordinates
// all the periodic tasks that keep an SRT connection reliable and smooth:
//
//   1. ACK generation (every 10ms) — Tell the sender what we've received
//   2. NAK generation (every 20ms) — Report detected packet losses
//   3. TSBPD delivery (every 1ms) — Deliver buffered packets on schedule
//   4. Keepalive (every 1s) — Prevent idle connection timeout
//   5. Retransmission — Resend packets the receiver reported as lost
//   6. Too-late packet drop — Discard packets past their delivery deadline
//
// All of these tasks use Go's time.Ticker for periodic scheduling and
// a select statement to handle whichever event fires next. The loop
// exits when the connection's context is cancelled (i.e., the connection
// is being closed).

import (
	"time"

	"github.com/alxayo/go-rtmp/internal/srt/packet"
)

// microsecondNow returns the current time in microseconds since the Unix epoch.
// This is the time unit used throughout SRT for timestamps and scheduling.
func microsecondNow() uint64 {
	return uint64(time.Now().UnixMicro())
}

// StartReliability starts the reliability loop as a background goroutine.
// This should be called once after the connection is established.
// The goroutine runs until the connection is closed.
func (c *Conn) StartReliability() {
	go c.reliabilityLoop()
}

// reliabilityLoop runs for the lifetime of the connection, managing all
// periodic reliability tasks. It uses Go's select statement to efficiently
// wait for the next timer event while remaining responsive to shutdown signals.
func (c *Conn) reliabilityLoop() {
	// Create periodic timers for each task.
	// time.NewTicker creates a channel that sends a value at regular intervals.
	ackTicker := time.NewTicker(time.Duration(ACKIntervalMs) * time.Millisecond)
	nakTicker := time.NewTicker(time.Duration(NAKIntervalMs) * time.Millisecond)
	keepaliveTicker := time.NewTicker(time.Duration(KeepaliveIntervalMs) * time.Millisecond)

	// Stop all tickers when the loop exits to free resources.
	// "defer" means these run when the function returns, even if it returns due to error.
	defer ackTicker.Stop()
	defer nakTicker.Stop()
	defer keepaliveTicker.Stop()

	for {
		// select waits for whichever event happens first.
		// It's like a switch statement but for channels/timers.
		select {

		case <-c.done:
			// The connection is being closed — exit the reliability loop.
			// c.done is a channel that gets closed when Close() is called.
			c.log.Debug("reliability loop exiting")
			return

		case <-ackTicker.C:
			// Time to send an ACK (every 10ms).
			// The ACK tells the sender what we've received.
			c.handleACKTick()

		case <-nakTicker.C:
			// Time to check for losses (every 20ms).
			// Send NAK for any missing packets and process retransmissions.
			c.handleNAKTick()

		case <-keepaliveTicker.C:
			// Time to send a keepalive (every 1s).
			// This prevents firewalls and NATs from closing the UDP mapping,
			// and tells the peer we're still alive.
			c.sendKeepalive()
		}
	}
}

// handleACKTick is called every ACK interval (10ms).
// It generates an ACK packet and sends it to the sender.
func (c *Conn) handleACKTick() {
	// Generate an ACK packet with current receiver state
	ack := c.GenerateACK()
	if ack == nil {
		return
	}

	// Send the ACK to the peer
	if err := c.sendControl(ack); err != nil {
		c.log.Warn("failed to send ACK", "error", err)
	}
}

// handleNAKTick is called every NAK interval (20ms).
// It checks for packet losses and sends NAK, then processes retransmissions.
func (c *Conn) handleNAKTick() {
	// Generate a NAK if there are detected losses
	nak := c.GenerateNAK()
	if nak != nil {
		if err := c.sendControl(nak); err != nil {
			c.log.Warn("failed to send NAK", "error", err)
		}
	}

	// Process any pending retransmissions from previous NAKs
	c.ProcessRetransmissions()
}

// sendKeepalive sends a keepalive control packet to the peer.
// Keepalive packets have no CIF (empty body) — they just say "I'm still here."
//
// Why keepalives matter:
//   - NAT/firewall mappings expire after a period of inactivity
//   - The peer needs to know we haven't crashed
//   - Without keepalives, idle connections would be silently dropped
func (c *Conn) sendKeepalive() {
	keepalive := &packet.ControlPacket{
		Header: packet.Header{
			IsControl:    true,
			Timestamp:    uint32(microsecondNow() & 0xFFFFFFFF),
			DestSocketID: c.peerSocketID,
		},
		Type: packet.CtrlKeepAlive,
	}

	if err := c.sendControl(keepalive); err != nil {
		c.log.Warn("failed to send keepalive", "error", err)
	}
}

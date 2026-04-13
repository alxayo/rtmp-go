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

	"github.com/alxayo/go-rtmp/internal/rtmp/metrics"
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
	deliveryTicker := time.NewTicker(1 * time.Millisecond)
	keepaliveTicker := time.NewTicker(time.Duration(KeepaliveIntervalMs) * time.Millisecond)

	// Stop all tickers when the loop exits to free resources.
	// "defer" means these run when the function returns, even if it returns due to error.
	defer ackTicker.Stop()
	defer nakTicker.Stop()
	defer deliveryTicker.Stop()
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

		case <-deliveryTicker.C:
			// TSBPD delivery check (every 1ms).
			// First, drop packets that are past their delivery deadline
			// (TLPKTDROP). This resolves gaps caused by permanently lost
			// packets, allowing delivery to continue.
			c.dropTooLate()

			// Then deliver packets whose TSBPD delivery time has arrived.
			// This handles packets released after TLPKTDROP clears gaps.
			c.deliverTSBPD()

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

// deliverTSBPD delivers packets whose TSBPD delivery time has arrived.
// It checks the next expected packet in sequence and delivers it if:
//  1. It exists in the receive buffer (no gap), AND
//  2. Its scheduled delivery time has passed
//
// This complements the receiver's contiguous delivery (which delivers in-order
// packets immediately on arrival). deliverTSBPD handles packets that became
// deliverable after TLPKTDROP resolved a gap by skipping lost packets.
func (c *Conn) deliverTSBPD() {
	now := microsecondNow()

	// Lock the receiver to safely access its buffer and delivery state.
	// We hold the lock for the entire delivery pass because we're modifying
	// lastDelivered and the receiveBuffer.
	c.receiver.mu.Lock()
	defer c.receiver.mu.Unlock()

	// Try to deliver as many contiguous ready packets as possible
	for {
		// The next packet we need to deliver (in sequence order)
		nextSeq := c.receiver.lastDelivered.Inc()

		// Check if we have this packet in the buffer
		pkt, exists := c.receiver.receiveBuffer[nextSeq]
		if !exists {
			// Gap — can't deliver until this packet arrives or is dropped
			break
		}

		// Check if the packet's delivery time has arrived.
		// TSBPD ensures we don't deliver packets too early — they wait
		// in the buffer until their scheduled playout time.
		if !c.tsbpd.IsReady(pkt.Timestamp, now) {
			// Packet arrived early — hold it until its delivery time
			break
		}

		// Delivery time has passed — send the payload to the application.
		// Non-blocking send avoids blocking the reliability loop if the
		// application is slow to consume data.
		select {
		case c.receiver.deliveryChan <- pkt.Payload:
			// Successfully delivered — remove from buffer and advance
			delete(c.receiver.receiveBuffer, nextSeq)
			c.receiver.lastDelivered = nextSeq
		default:
			// Delivery channel is full — back-pressure from application.
			// Stop delivering and try again on the next tick.
			return
		}
	}
}

// dropTooLate removes packets from the receive buffer that are past their
// delivery deadline. This implements TLPKTDROP (Too Late Packet Drop).
//
// TLPKTDROP handles two cases:
//  1. A buffered packet whose delivery time has passed → drop it
//  2. A missing packet (gap) where later packets are already too late →
//     skip the gap so delivery can continue
//
// Without TLPKTDROP, a single permanently lost packet would block delivery
// of ALL subsequent packets. By dropping late packets, we keep the live
// stream flowing with minimal interruption (the application sees a small
// glitch instead of a complete stall).
func (c *Conn) dropTooLate() {
	now := microsecondNow()

	c.receiver.mu.Lock()
	defer c.receiver.mu.Unlock()

	dropped := 0

	// Check the next expected packet and its successors.
	// Limit iterations to prevent infinite loops in edge cases.
	for i := 0; i < 100; i++ {
		nextSeq := c.receiver.lastDelivered.Inc()

		pkt, exists := c.receiver.receiveBuffer[nextSeq]
		if exists {
			// Case 1: The packet IS in the buffer — check if it's too late
			if c.tsbpd.TooLate(pkt.Timestamp, now) {
				// Past its deadline — drop it instead of delivering stale data
				delete(c.receiver.receiveBuffer, nextSeq)
				delete(c.receiver.lossDetected, nextSeq)
				c.receiver.lastDelivered = nextSeq
				dropped++
				continue
			}
			// Packet is still within its delivery window — stop checking
			break
		}

		// Case 2: The next packet is MISSING (gap in sequence).
		// Check if packets AFTER the gap are already too late. If so,
		// the missing packet would also be too late even if it arrived now,
		// so we should skip it and move on.
		skipGap := false
		checkSeq := nextSeq.Inc()
		for j := 0; j < 64; j++ {
			if laterPkt, ok := c.receiver.receiveBuffer[checkSeq]; ok {
				// Found a buffered packet after the gap.
				// If it's too late, the missing one is definitely too late too.
				if c.tsbpd.TooLate(laterPkt.Timestamp, now) {
					skipGap = true
				}
				break
			}
			checkSeq = checkSeq.Inc()
		}

		if skipGap {
			// Skip the missing packet — it's too late to matter
			delete(c.receiver.lossDetected, nextSeq)
			c.receiver.lastDelivered = nextSeq
			dropped++
			continue
		}

		// Gap exists but subsequent packets aren't too late yet — wait
		break
	}

	if dropped > 0 {
		metrics.SRTPacketsDropped.Add(int64(dropped))
		c.log.Debug("TLPKTDROP: dropped too-late packets", "count", dropped)
	}
}

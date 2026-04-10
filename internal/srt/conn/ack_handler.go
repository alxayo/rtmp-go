package conn

// ACK Handler — Acknowledgement Generation and ACKACK Processing
//
// In SRT, the receiver periodically sends ACK packets to the sender saying:
// "I have received all packets up to sequence number X."
//
// This serves two purposes:
//   1. The sender can free acknowledged packets from its send buffer
//   2. The ACK/ACKACK exchange measures round-trip time (RTT)
//
// ACK triggering rules (from the SRT specification):
//   - Every 10ms (time-based interval), OR
//   - Every 64 received data packets (count-based interval)
//   - Whichever comes first
//
// The ACKACK is the sender's response to an ACK. By comparing the timestamp
// when we sent the ACK with when we receive the ACKACK, we measure RTT.

import (
	"sync"

	"github.com/alxayo/go-rtmp/internal/srt/packet"
)

// ackPacketInterval is the number of data packets between ACK generation.
// If we receive this many packets before the time interval, we send an ACK early.
const ackPacketInterval = 64

// ACKState tracks the state needed for ACK generation and RTT measurement.
type ACKState struct {
	mu sync.Mutex

	// nextACKSeqNum is the sequence number for the next ACK packet.
	// This is NOT the data sequence number — it's a separate counter
	// that identifies each ACK so we can match it with its ACKACK response.
	nextACKSeqNum uint32

	// packetsRecvd counts data packets received since the last ACK.
	// When this reaches ackPacketInterval, we send an ACK immediately.
	packetsRecvd uint32

	// lastACKTimeUs is the timestamp (microseconds) when we last sent an ACK.
	// Used to enforce the 10ms time-based ACK interval.
	lastACKTimeUs uint64

	// pendingACKs maps ACK sequence numbers to the time they were sent.
	// When we receive an ACKACK with this sequence number, we can calculate
	// RTT = now - pendingACKs[ackSeqNum].
	pendingACKs map[uint32]uint64
}

// NewACKState creates a new ACK tracking state.
func NewACKState() *ACKState {
	return &ACKState{
		nextACKSeqNum: 1,           // ACK sequence numbers start at 1
		pendingACKs:   make(map[uint32]uint64),
	}
}

// ShouldSendACK returns true if it's time to send an ACK based on
// either the packet count threshold or the time interval.
func (a *ACKState) ShouldSendACK(nowUs uint64) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Check packet-count trigger: every 64 data packets
	if a.packetsRecvd >= ackPacketInterval {
		return true
	}

	// Check time-based trigger: every 10ms (10,000 microseconds)
	if a.lastACKTimeUs == 0 || nowUs-a.lastACKTimeUs >= uint64(ACKIntervalMs)*1000 {
		return true
	}

	return false
}

// OnDataReceived increments the received packet counter.
// Called each time a data packet arrives from the sender.
func (a *ACKState) OnDataReceived() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.packetsRecvd++
}

// RecordACKSent records that an ACK was sent at the given time.
// Returns the ACK sequence number that was assigned to this ACK.
func (a *ACKState) RecordACKSent(nowUs uint64) uint32 {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Assign and increment the ACK sequence number
	ackSeq := a.nextACKSeqNum
	a.nextACKSeqNum++

	// Record when we sent this ACK (for RTT calculation when ACKACK arrives)
	a.pendingACKs[ackSeq] = nowUs

	// Reset the packet counter and update the last ACK time
	a.packetsRecvd = 0
	a.lastACKTimeUs = nowUs

	// Clean up old pending ACKs (older than 10 seconds) to prevent memory leak
	for seq, ts := range a.pendingACKs {
		if nowUs-ts > 10_000_000 { // 10 seconds in microseconds
			delete(a.pendingACKs, seq)
		}
	}

	return ackSeq
}

// GetACKSendTime returns the send time for a given ACK sequence number.
// Used when an ACKACK arrives to calculate RTT.
// Returns 0 if the ACK sequence number is not found.
func (a *ACKState) GetACKSendTime(ackSeqNum uint32) uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()

	ts, exists := a.pendingACKs[ackSeqNum]
	if exists {
		// Remove it since we've used it for RTT measurement
		delete(a.pendingACKs, ackSeqNum)
	}
	return ts
}

// GenerateACK creates an ACK control packet from the current receiver state.
// The ACK tells the sender what we've received and provides network statistics.
// Returns nil if there's nothing new to acknowledge.
func (c *Conn) GenerateACK() *packet.ControlPacket {
	// Get the next expected sequence number from the receiver
	ackSeq := c.receiver.GetACKSequence()

	// Record that we're sending this ACK (for RTT measurement)
	nowUs := microsecondNow()
	ackNum := c.ackState.RecordACKSent(nowUs)

	// Build the ACK CIF (Control Information Field) with our statistics
	ackCIF := &packet.ACKCIF{
		LastACKPacketSeq: ackSeq.Val(),
		RTT:              c.sender.RTT(),
		RTTVariance:      c.sender.RTTVar(),
		AvailableBuffer:  c.receiver.AvailableBuffer(),
	}

	// Serialize the ACK CIF to bytes
	cifData, err := ackCIF.MarshalBinary()
	if err != nil {
		c.log.Warn("failed to marshal ACK CIF", "error", err)
		return nil
	}

	// Build the control packet envelope
	return &packet.ControlPacket{
		Header: packet.Header{
			IsControl:    true,
			Timestamp:    uint32(nowUs & 0xFFFFFFFF), // Truncate to 32 bits
			DestSocketID: c.peerSocketID,
		},
		Type:         packet.CtrlACK,
		TypeSpecific: ackNum, // ACK sequence number (for ACKACK matching)
		CIF:          cifData,
	}
}

// HandleACKACK processes an ACKACK control packet from the sender.
// The ACKACK echoes back the ACK sequence number, allowing us to
// measure the round-trip time (RTT) by comparing timestamps.
func (c *Conn) HandleACKACK(ackSeqNum uint32) {
	// Look up when we sent the original ACK
	sendTimeUs := c.ackState.GetACKSendTime(ackSeqNum)
	if sendTimeUs == 0 {
		// We don't have a record of this ACK — ignore
		return
	}

	// Calculate RTT = now - when we sent the ACK
	nowUs := microsecondNow()
	rttSample := uint32(nowUs - sendTimeUs)

	// Update the sender's smoothed RTT estimate
	c.sender.UpdateRTT(rttSample)

	c.log.Debug("RTT measured via ACKACK",
		"ack_seq", ackSeqNum,
		"rtt_sample_us", rttSample,
	)
}

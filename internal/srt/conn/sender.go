package conn

import (
	"log/slog"
	"sync"

	"github.com/alxayo/go-rtmp/internal/srt/circular"
	"github.com/alxayo/go-rtmp/internal/srt/packet"
)

// Sender manages the send-side of an SRT connection.
//
// When we send a data packet, we keep a copy in sendBuffer indexed by
// its sequence number. The copy stays there until the receiver ACKs it.
// If the receiver detects a gap and sends a NAK, we add those sequence
// numbers to lossQueue so they can be retransmitted.
//
// The sender also maintains a smoothed RTT (round-trip time) estimate
// using an Exponential Weighted Moving Average (EWMA). RTT is measured
// by embedding a timestamp in each ACK; when the ACKACK comes back,
// the difference is a new RTT sample.
type Sender struct {
	mu sync.Mutex

	// nextSeqNum is the sequence number that will be assigned to the
	// next outgoing data packet. It wraps around at 2^31 - 1 (31-bit space).
	nextSeqNum circular.Number

	// sendBuffer holds copies of sent packets keyed by sequence number.
	// Packets stay here until the receiver acknowledges them with an ACK.
	sendBuffer map[circular.Number]*packet.DataPacket

	// lossQueue holds sequence numbers that the receiver reported as lost
	// via NAK packets. These need to be retransmitted as soon as possible.
	lossQueue []circular.Number

	// lastACK is the highest sequence number acknowledged by the receiver.
	// All packets with sequence numbers before this have been received.
	lastACK circular.Number

	// config holds negotiated connection parameters (MTU, flow window, etc.)
	config ConnConfig

	// log is the structured logger for sender-related events.
	log *slog.Logger

	// --- RTT estimation (Exponential Weighted Moving Average) ---
	// rtt is the smoothed round-trip time in microseconds.
	// Updated as: rtt_new = 7/8 * rtt_old + 1/8 * sample
	rtt uint32

	// rttVar is the RTT variance in microseconds.
	// Updated as: rttVar_new = 3/4 * rttVar_old + 1/4 * |rtt_old - sample|
	rttVar uint32
}

// NewSender creates a new sender starting at the given initial sequence number.
// The initial sequence number is negotiated during the SRT handshake.
func NewSender(initialSeq circular.Number, cfg ConnConfig, log *slog.Logger) *Sender {
	return &Sender{
		nextSeqNum: initialSeq,
		sendBuffer: make(map[circular.Number]*packet.DataPacket),
		lossQueue:  nil,
		lastACK:    initialSeq,
		config:     cfg,
		log:        log.With("component", "sender"),
		rtt:        100000, // Start with 100ms default RTT (in microseconds)
		rttVar:     50000,  // Start with 50ms default variance
	}
}

// NextSequenceNumber returns the next sequence number to use for an outgoing
// packet, and advances the counter by one. The sequence number space is 31
// bits wide (0 to 0x7FFFFFFF) and wraps around automatically.
func (s *Sender) NextSequenceNumber() circular.Number {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Capture the current value to return
	seq := s.nextSeqNum

	// Advance to the next sequence number (handles 31-bit wraparound)
	s.nextSeqNum = s.nextSeqNum.Inc()

	return seq
}

// StoreSent records a sent packet in the send buffer for potential retransmission.
// The packet is stored by its sequence number so we can find it later if the
// receiver reports it as lost (NAK) or when we can remove it (ACK).
func (s *Sender) StoreSent(pkt *packet.DataPacket) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Store the packet using its sequence number as the key
	seq := circular.New(pkt.SequenceNumber)
	s.sendBuffer[seq] = pkt
}

// OnACK processes an ACK from the receiver. The ACK tells us that all packets
// with sequence numbers BEFORE ackSeq have been received. We can safely remove
// those packets from our send buffer since they don't need retransmission.
func (s *Sender) OnACK(ackSeq circular.Number) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Only process if this ACK is newer than the last one we saw.
	// Due to network reordering, we might receive old ACKs out of order.
	if ackSeq.BeforeOrEqual(s.lastACK) {
		return
	}

	// Remove all packets from the send buffer that have been acknowledged.
	// These are packets with sequence numbers before the ACK sequence number.
	for seq := range s.sendBuffer {
		if seq.Before(ackSeq) {
			delete(s.sendBuffer, seq)
		}
	}

	// Also remove any loss queue entries that have been acknowledged,
	// since the receiver already has them.
	filtered := s.lossQueue[:0]
	for _, seq := range s.lossQueue {
		if seq.AfterOrEqual(ackSeq) {
			// This loss entry is still relevant (not yet acknowledged)
			filtered = append(filtered, seq)
		}
	}
	s.lossQueue = filtered

	// Update the last acknowledged sequence number
	s.lastACK = ackSeq

	s.log.Debug("processed ACK",
		"ack_seq", ackSeq.Val(),
		"buffered", len(s.sendBuffer),
		"loss_queue", len(s.lossQueue),
	)
}

// OnNAK processes a NAK (negative acknowledgement) from the receiver.
// The NAK contains ranges of sequence numbers that the receiver is missing.
// Each range is a [start, end] pair (inclusive). We add all missing sequence
// numbers to the loss queue for retransmission.
func (s *Sender) OnNAK(ranges [][2]uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, r := range ranges {
		start := circular.New(r[0])
		end := circular.New(r[1])

		// Walk through each sequence number in the range [start, end]
		for seq := start; seq.BeforeOrEqual(end); seq = seq.Inc() {
			// Only add to loss queue if we still have this packet in the buffer
			if _, exists := s.sendBuffer[seq]; exists {
				s.lossQueue = append(s.lossQueue, seq)
			}
		}
	}

	s.log.Debug("processed NAK",
		"ranges", len(ranges),
		"loss_queue", len(s.lossQueue),
	)
}

// GetRetransmit returns the next packet to retransmit from the loss queue,
// or nil if there are no pending retransmissions. The returned packet is
// removed from the loss queue.
func (s *Sender) GetRetransmit() *packet.DataPacket {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Keep trying entries until we find one that's still in the buffer,
	// or run out of entries.
	for len(s.lossQueue) > 0 {
		// Pop the first entry from the loss queue
		seq := s.lossQueue[0]
		s.lossQueue = s.lossQueue[1:]

		// Look up the packet in the send buffer
		if pkt, exists := s.sendBuffer[seq]; exists {
			return pkt
		}
		// If it's not in the buffer, it was already ACKed — skip it
	}

	return nil
}

// UpdateRTT updates the smoothed RTT estimate using an Exponential Weighted
// Moving Average (EWMA). This is similar to how TCP estimates RTT.
//
// The formulas are:
//
//	rtt_new     = 7/8 * rtt_old + 1/8 * sample
//	rttVar_new  = 3/4 * rttVar_old + 1/4 * |rtt_old - sample|
//
// The smoothing reduces the impact of individual noisy samples while
// still tracking the actual RTT over time.
func (s *Sender) UpdateRTT(sampleUs uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Calculate the absolute difference between current estimate and sample
	var diff uint32
	if s.rtt > sampleUs {
		diff = s.rtt - sampleUs
	} else {
		diff = sampleUs - s.rtt
	}

	// Apply EWMA formulas:
	// rtt = 7/8 * rtt + 1/8 * sample
	s.rtt = (7*s.rtt + sampleUs) / 8

	// rttVar = 3/4 * rttVar + 1/4 * |diff|
	s.rttVar = (3*s.rttVar + diff) / 4

	s.log.Debug("RTT updated",
		"rtt_us", s.rtt,
		"rtt_var_us", s.rttVar,
		"sample_us", sampleUs,
	)
}

// RTT returns the current smoothed RTT in microseconds.
func (s *Sender) RTT() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rtt
}

// RTTVar returns the current RTT variance in microseconds.
func (s *Sender) RTTVar() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rttVar
}

// BufferedPackets returns the number of packets currently in the send buffer
// waiting for acknowledgement. This is useful for flow control and monitoring.
func (s *Sender) BufferedPackets() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sendBuffer)
}

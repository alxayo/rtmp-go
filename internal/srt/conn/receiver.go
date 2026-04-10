package conn

import (
	"log/slog"
	"sync"

	"github.com/alxayo/go-rtmp/internal/srt/circular"
	"github.com/alxayo/go-rtmp/internal/srt/packet"
)

// Receiver manages the receive-side of an SRT connection.
//
// When data packets arrive from the network, they may come out of order
// because UDP doesn't guarantee ordering. The Receiver buffers packets
// and delivers them to the application in the correct sequence.
//
// It also tracks which packets are missing (gaps in the sequence) so the
// connection can send NAK packets telling the sender to retransmit them.
//
// For live streaming, SRT uses TSBPD (Timestamp-Based Packet Delivery)
// to hold packets until their playout time, smoothing out network jitter.
// This initial implementation delivers packets as soon as contiguous
// sequences are available.
type Receiver struct {
	mu sync.Mutex

	// lastDelivered is the sequence number of the most recent packet
	// delivered to the application. The next packet we expect to deliver
	// is lastDelivered + 1.
	lastDelivered circular.Number

	// lastACKed is the sequence number we last reported in an ACK.
	// We only send a new ACK when we have new contiguous data to report.
	lastACKed circular.Number

	// receiveBuffer holds packets that have arrived but haven't been
	// delivered yet. Out-of-order packets wait here until the gap is filled.
	receiveBuffer map[circular.Number]*packet.DataPacket

	// deliveryChan is a buffered channel that carries delivered payload
	// data to the Read() method. Using a channel lets Read() block
	// efficiently until data is available.
	deliveryChan chan []byte

	// lossDetected tracks sequence numbers where we've detected a gap.
	// When packet N+2 arrives but N+1 hasn't, N+1 is marked as lost.
	// These are reported to the sender via NAK packets.
	lossDetected map[circular.Number]bool

	// config holds negotiated connection parameters.
	config ConnConfig

	// log is the structured logger for receiver events.
	log *slog.Logger
}

// NewReceiver creates a new receiver starting at the given initial sequence number.
// The lastDelivered is set to one-before the initial sequence number so that
// the first expected delivery is exactly initialSeq.
func NewReceiver(initialSeq circular.Number, cfg ConnConfig, log *slog.Logger) *Receiver {
	return &Receiver{
		// Set lastDelivered to initialSeq - 1 (with 31-bit wraparound).
		// This way, the next expected packet is initialSeq itself.
		lastDelivered: initialSeq.Add(circular.MaxVal.Val()),
		lastACKed:     initialSeq,
		receiveBuffer: make(map[circular.Number]*packet.DataPacket),
		deliveryChan:  make(chan []byte, 256),
		lossDetected:  make(map[circular.Number]bool),
		config:        cfg,
		log:           log.With("component", "receiver"),
	}
}

// OnData processes an incoming data packet from the network.
// It stores the packet in the receive buffer and then tries to deliver
// any contiguous run of packets starting from where we left off.
//
// If the packet's sequence number reveals a gap (some packets were skipped),
// those missing sequence numbers are recorded for NAK reporting.
func (r *Receiver) OnData(pkt *packet.DataPacket) {
	r.mu.Lock()
	defer r.mu.Unlock()

	seq := circular.New(pkt.SequenceNumber)

	// Ignore duplicate packets — we already have this one
	if _, exists := r.receiveBuffer[seq]; exists {
		return
	}

	// Ignore packets that we've already delivered to the application
	if seq.BeforeOrEqual(r.lastDelivered) {
		return
	}

	// Store the packet in the receive buffer
	r.receiveBuffer[seq] = pkt

	// Detect gaps: if this packet is more than 1 ahead of the last delivered,
	// everything in between is potentially lost.
	nextExpected := r.lastDelivered.Inc()
	if seq.After(nextExpected) {
		// Mark each missing sequence number between nextExpected and seq
		for gap := nextExpected; gap.Before(seq); gap = gap.Inc() {
			if _, buffered := r.receiveBuffer[gap]; !buffered {
				r.lossDetected[gap] = true
			}
		}
	}

	// If this packet fills a previously detected gap, remove it from lossDetected
	delete(r.lossDetected, seq)

	// Try to deliver contiguous packets starting from lastDelivered + 1
	r.deliverContiguous()
}

// deliverContiguous delivers all packets in sequence starting from
// lastDelivered + 1. It stops as soon as it hits a gap (missing packet).
//
// Each delivered packet's payload is sent to deliveryChan so the Read()
// method can pick it up. The packet is then removed from the buffer.
//
// IMPORTANT: This must be called while holding r.mu.
func (r *Receiver) deliverContiguous() {
	for {
		// The next packet we need to deliver
		nextSeq := r.lastDelivered.Inc()

		// Check if we have this packet in the buffer
		pkt, exists := r.receiveBuffer[nextSeq]
		if !exists {
			// Gap found — can't deliver further until this packet arrives
			break
		}

		// Deliver the payload to the application via the channel.
		// Use a non-blocking send: if the channel is full, we stop
		// delivering to avoid blocking the network read path.
		select {
		case r.deliveryChan <- pkt.Payload:
			// Successfully delivered
			delete(r.receiveBuffer, nextSeq)
			r.lastDelivered = nextSeq
		default:
			// Channel full — back-pressure from the application.
			// Stop delivering and try again later.
			r.log.Warn("delivery channel full, applying back-pressure",
				"seq", nextSeq.Val(),
			)
			return
		}
	}
}

// GetACKSequence returns the next expected sequence number for use in ACK packets.
// This tells the sender: "I've received everything before this number."
// Specifically, it returns lastDelivered + 1 (the first packet we haven't delivered).
func (r *Receiver) GetACKSequence() circular.Number {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.lastDelivered.Inc()
}

// GetLossReport returns sequence number ranges of detected gaps for NAK packets.
// Each range is a [start, end] pair (inclusive) of missing sequence numbers.
// After calling this, the loss list is cleared because the NAK has been sent.
func (r *Receiver) GetLossReport() [][2]uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()

	// If there are no losses, return nil (no NAK needed)
	if len(r.lossDetected) == 0 {
		return nil
	}

	// Collect all lost sequence numbers into a sorted list.
	// For simplicity, each lost packet is reported as its own single-element range.
	// A more optimized implementation would merge consecutive numbers into ranges.
	var ranges [][2]uint32
	for seq := range r.lossDetected {
		ranges = append(ranges, [2]uint32{seq.Val(), seq.Val()})
	}

	// Clear the loss list since we've reported these gaps
	r.lossDetected = make(map[circular.Number]bool)

	return ranges
}

// AvailableBuffer returns how many more packets can be buffered before we
// hit the flow window limit. The sender uses this (via ACK) to know how
// fast it can send without overwhelming the receiver.
func (r *Receiver) AvailableBuffer() uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()

	buffered := uint32(len(r.receiveBuffer))
	if buffered >= r.config.FlowWindow {
		// Buffer is full — tell sender to slow down
		return 0
	}
	return r.config.FlowWindow - buffered
}

// DeliveryChan returns the read-only channel that delivers payload data
// to the application's Read() method. Each item in the channel is the
// payload bytes from one data packet, delivered in sequence order.
func (r *Receiver) DeliveryChan() <-chan []byte {
	return r.deliveryChan
}

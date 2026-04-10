package conn

// NAK Handler — Negative Acknowledgement and Retransmission
//
// When the receiver detects a gap in the sequence numbers (e.g., it received
// packets 1, 2, 4, 5 but not 3), it sends a NAK (Negative Acknowledgement)
// listing the missing packets. The sender then retransmits those packets.
//
// SRT supports two NAK modes:
//   1. Immediate NAK: sent as soon as a gap is detected
//   2. Periodic NAK: sent at regular intervals (if PERIODICNAK flag is negotiated)
//
// Periodic NAK is important because the NAK packet itself might get lost
// (remember, this is all over UDP). By periodically re-sending NAKs, we
// ensure the sender eventually learns about every loss.

import (
	"github.com/alxayo/go-rtmp/internal/srt/packet"
)

// GenerateNAK creates a NAK control packet listing all detected packet losses.
// The receiver calls this periodically to report gaps in the received sequence.
// Returns nil if no losses have been detected since the last NAK.
func (c *Conn) GenerateNAK() *packet.ControlPacket {
	// Ask the receiver for its list of detected losses
	// Each range is [start, end] inclusive — a contiguous block of missing packets
	ranges := c.receiver.GetLossReport()

	// No losses to report
	if len(ranges) == 0 {
		return nil
	}

	// Encode the loss ranges into the SRT NAK CIF (Control Information Field) format.
	// Single losses are encoded as one 32-bit number.
	// Ranges are encoded as [start | 0x80000000, end].
	cifData := packet.EncodeLossRanges(ranges)

	c.log.Debug("generating NAK",
		"loss_ranges", len(ranges),
		"cif_bytes", len(cifData),
	)

	// Build the NAK control packet
	return &packet.ControlPacket{
		Header: packet.Header{
			IsControl:    true,
			Timestamp:    uint32(microsecondNow() & 0xFFFFFFFF),
			DestSocketID: c.peerSocketID,
		},
		Type: packet.CtrlNAK,
		CIF:  cifData,
	}
}

// HandleIncomingNAK processes a NAK control packet received from the peer.
// The NAK lists sequence numbers that the peer hasn't received, telling us
// to retransmit those packets.
func (c *Conn) HandleIncomingNAK(ctrl *packet.ControlPacket) {
	// Decode the loss ranges from the NAK's CIF
	ranges := packet.DecodeLossRanges(ctrl.CIF)
	if len(ranges) == 0 {
		return
	}

	// Tell the sender to queue these packets for retransmission
	c.sender.OnNAK(ranges)

	c.log.Debug("received NAK from peer",
		"loss_ranges", len(ranges),
	)
}

// ProcessRetransmissions sends queued retransmission packets.
// The sender maintains a loss queue of packets that need to be re-sent.
// This method pops packets from that queue and sends them, marking them
// as retransmissions (the R bit in the SRT data packet header).
func (c *Conn) ProcessRetransmissions() {
	// Process up to 10 retransmissions per call to avoid hogging the CPU
	for i := 0; i < 10; i++ {
		// Get the next packet to retransmit from the sender's loss queue
		pkt := c.sender.GetRetransmit()
		if pkt == nil {
			// No more packets to retransmit
			return
		}

		// Mark the packet as a retransmission so the receiver knows
		pkt.Retransmitted = true

		// Update the destination socket ID and timestamp for the resend
		pkt.Header.DestSocketID = c.peerSocketID
		pkt.Header.Timestamp = uint32(microsecondNow() & 0xFFFFFFFF)

		// Serialize and send the retransmission packet
		data, err := pkt.MarshalBinary()
		if err != nil {
			c.log.Warn("failed to marshal retransmit packet", "error", err)
			continue
		}

		if err := c.sendPacket(data); err != nil {
			c.log.Warn("failed to send retransmit packet", "error", err)
		}
	}
}

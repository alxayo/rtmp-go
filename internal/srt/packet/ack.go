package packet

// This file implements the ACK (Acknowledgment) Control Information Field.
// ACK packets are sent by the receiver to tell the sender which data
// packets have been successfully received. The ACK CIF also carries
// network performance metrics like RTT and bandwidth estimates, which
// the sender uses for congestion control.
//
// Wire layout of the ACK CIF (28 bytes):
//
//	 0                   1                   2                   3
//	 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|            Last Acknowledged Packet Sequence Number           |  Bytes 0-3
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                          RTT (µs)                            |  Bytes 4-7
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                     RTT Variance (µs)                        |  Bytes 8-11
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                Available Buffer Size (pkts)                  |  Bytes 12-15
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|               Packets Receiving Rate (pkts/s)                |  Bytes 16-19
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|               Estimated Bandwidth (pkts/s)                   |  Bytes 20-23
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                Receiving Rate (bytes/s)                       |  Bytes 24-27
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+

import (
	"encoding/binary"
	"fmt"
)

// ACKCIFSize is the fixed size of the ACK Control Information Field.
const ACKCIFSize = 28

// ACKCIF represents the acknowledgment data sent from receiver to sender.
// It confirms receipt of packets and provides network performance metrics
// that the sender uses to adjust its sending rate.
type ACKCIF struct {
	// LastACKPacketSeq is the sequence number of the last data packet
	// that has been received in order. All packets with sequence numbers
	// less than this value have been received. The sender can free them
	// from its send buffer.
	LastACKPacketSeq uint32

	// RTT is the round-trip time in microseconds, measured by the
	// ACK → ACKACK exchange. This is how long it takes a packet to
	// travel from sender to receiver and back.
	RTT uint32

	// RTTVariance is the variance (jitter) of the RTT measurement in
	// microseconds. High variance indicates an unstable network path.
	RTTVariance uint32

	// AvailableBuffer is the number of packets the receiver can still
	// accept in its receive buffer. If this drops to zero, the sender
	// must stop sending (flow control).
	AvailableBuffer uint32

	// PacketsReceiving is the rate at which packets are arriving at the
	// receiver, measured in packets per second.
	PacketsReceiving uint32

	// EstBandwidth is the estimated link bandwidth in packets per second,
	// based on the receiver's packet arrival measurements.
	EstBandwidth uint32

	// ReceivingRate is the rate at which data is arriving at the receiver,
	// measured in bytes per second. This accounts for packet sizes.
	ReceivingRate uint32
}

// MarshalBinary serializes the ACKCIF into its 28-byte wire format (big-endian).
func (a *ACKCIF) MarshalBinary() ([]byte, error) {
	buf := make([]byte, ACKCIFSize)

	// Write each 32-bit field sequentially in big-endian order.
	binary.BigEndian.PutUint32(buf[0:4], a.LastACKPacketSeq)
	binary.BigEndian.PutUint32(buf[4:8], a.RTT)
	binary.BigEndian.PutUint32(buf[8:12], a.RTTVariance)
	binary.BigEndian.PutUint32(buf[12:16], a.AvailableBuffer)
	binary.BigEndian.PutUint32(buf[16:20], a.PacketsReceiving)
	binary.BigEndian.PutUint32(buf[20:24], a.EstBandwidth)
	binary.BigEndian.PutUint32(buf[24:28], a.ReceivingRate)

	return buf, nil
}

// UnmarshalACKCIF parses a raw buffer into an ACKCIF.
// The buffer must be at least ACKCIFSize (28) bytes long.
func UnmarshalACKCIF(buf []byte) (*ACKCIF, error) {
	// Verify we have enough bytes for the full ACK CIF.
	if len(buf) < ACKCIFSize {
		return nil, fmt.Errorf("ACK CIF too short: need %d bytes, got %d", ACKCIFSize, len(buf))
	}

	a := &ACKCIF{}

	// Read each 32-bit field from its fixed position.
	a.LastACKPacketSeq = binary.BigEndian.Uint32(buf[0:4])
	a.RTT = binary.BigEndian.Uint32(buf[4:8])
	a.RTTVariance = binary.BigEndian.Uint32(buf[8:12])
	a.AvailableBuffer = binary.BigEndian.Uint32(buf[12:16])
	a.PacketsReceiving = binary.BigEndian.Uint32(buf[16:20])
	a.EstBandwidth = binary.BigEndian.Uint32(buf[20:24])
	a.ReceivingRate = binary.BigEndian.Uint32(buf[24:28])

	return a, nil
}

package conn

import (
	"testing"
	"time"

	"github.com/alxayo/go-rtmp/internal/srt/circular"
	"github.com/alxayo/go-rtmp/internal/srt/packet"
)

// --- ACK generation tests ---

// TestGenerateACKProducesValidPacket verifies that GenerateACK creates a
// properly formatted ACK control packet with the correct sequence number,
// RTT stats, and buffer information.
func TestGenerateACKProducesValidPacket(t *testing.T) {
	udpConn, peerAddr := testUDPConn(t)
	cfg := testConfig()
	log := testLogger()

	c := New(1, 2, peerAddr, udpConn, "test", cfg, log)
	defer c.Close()

	// Feed a data packet so there's something to acknowledge
	pkt := &packet.DataPacket{
		SequenceNumber: cfg.InitialSeqNum,
		Payload:        []byte("test data"),
	}
	c.receiver.OnData(pkt)
	<-c.receiver.DeliveryChan() // Drain the delivery channel

	// Generate an ACK
	ack := c.GenerateACK()
	if ack == nil {
		t.Fatal("GenerateACK returned nil, expected a valid ACK packet")
	}

	// Verify the packet structure
	if ack.Type != packet.CtrlACK {
		t.Errorf("ACK type: want %d, got %d", packet.CtrlACK, ack.Type)
	}
	if ack.Header.DestSocketID != c.peerSocketID {
		t.Errorf("dest socket ID: want %d, got %d", c.peerSocketID, ack.Header.DestSocketID)
	}
	if ack.TypeSpecific == 0 {
		t.Error("ACK sequence number (TypeSpecific) should not be 0")
	}

	// Parse the CIF to verify the acknowledged sequence number
	if len(ack.CIF) < packet.ACKCIFSize {
		t.Fatalf("ACK CIF too small: got %d bytes, want at least %d", len(ack.CIF), packet.ACKCIFSize)
	}
	ackCIF, err := packet.UnmarshalACKCIF(ack.CIF)
	if err != nil {
		t.Fatalf("unmarshal ACK CIF: %v", err)
	}

	// After receiving packet at InitialSeqNum, the ACK should report InitialSeqNum+1
	// (meaning "I've received everything before this")
	expectedACKSeq := cfg.InitialSeqNum + 1
	if ackCIF.LastACKPacketSeq != expectedACKSeq {
		t.Errorf("ACK seq: want %d, got %d", expectedACKSeq, ackCIF.LastACKPacketSeq)
	}

	// RTT should be non-zero (initialized to 100ms default)
	if ackCIF.RTT == 0 {
		t.Error("RTT in ACK CIF should be non-zero")
	}
}

// TestGenerateACKReturnsNilWhenNothingNew verifies that GenerateACK returns
// nil when no new data has been received since the last ACK.
func TestGenerateACKReturnsNilWhenNothingNew(t *testing.T) {
	udpConn, peerAddr := testUDPConn(t)
	cfg := testConfig()
	log := testLogger()

	c := New(1, 2, peerAddr, udpConn, "test", cfg, log)
	defer c.Close()

	// Generate an ACK with no data received — receiver's lastDelivered hasn't
	// changed so the ACK seq matches lastACKed. No new data to report.
	// Actually, the first GenerateACK will work because lastACKed starts at
	// InitialSeqNum and GetACKSequence returns the same. So it returns nil.
	ack := c.GenerateACK()
	if ack != nil {
		t.Error("GenerateACK should return nil when nothing new to ACK")
	}
}

// --- NAK generation tests ---

// TestGenerateNAKDetectsLossRanges verifies that GenerateNAK correctly
// reports gaps in the received sequence as loss ranges.
func TestGenerateNAKDetectsLossRanges(t *testing.T) {
	udpConn, peerAddr := testUDPConn(t)
	cfg := testConfig()
	log := testLogger()

	c := New(1, 2, peerAddr, udpConn, "test", cfg, log)
	defer c.Close()

	// Deliver packet 1000 (the initial), then skip 1001 and deliver 1002.
	// This creates a gap at sequence number 1001.
	c.receiver.OnData(&packet.DataPacket{
		SequenceNumber: cfg.InitialSeqNum,
		Payload:        []byte("first"),
	})
	<-c.receiver.DeliveryChan()

	c.receiver.OnData(&packet.DataPacket{
		SequenceNumber: cfg.InitialSeqNum + 2,
		Payload:        []byte("third"),
	})

	// Generate NAK — should report the missing packet
	nak := c.GenerateNAK()
	if nak == nil {
		t.Fatal("GenerateNAK returned nil, expected a NAK for missing packet")
	}

	if nak.Type != packet.CtrlNAK {
		t.Errorf("NAK type: want %d, got %d", packet.CtrlNAK, nak.Type)
	}

	// Decode the loss report from the NAK CIF
	ranges := packet.DecodeLossRanges(nak.CIF)
	if len(ranges) == 0 {
		t.Fatal("NAK should contain loss ranges")
	}

	// The missing packet is initialSeqNum + 1
	missingSeq := cfg.InitialSeqNum + 1
	found := false
	for _, r := range ranges {
		if r[0] <= missingSeq && r[1] >= missingSeq {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("NAK should report seq %d as lost, got ranges %v", missingSeq, ranges)
	}
}

// TestGenerateNAKReturnsNilWhenNoLosses verifies that GenerateNAK returns
// nil when all packets have arrived without gaps.
func TestGenerateNAKReturnsNilWhenNoLosses(t *testing.T) {
	udpConn, peerAddr := testUDPConn(t)
	cfg := testConfig()
	log := testLogger()

	c := New(1, 2, peerAddr, udpConn, "test", cfg, log)
	defer c.Close()

	// Deliver packets in order — no gaps
	c.receiver.OnData(&packet.DataPacket{
		SequenceNumber: cfg.InitialSeqNum,
		Payload:        []byte("a"),
	})
	<-c.receiver.DeliveryChan()

	c.receiver.OnData(&packet.DataPacket{
		SequenceNumber: cfg.InitialSeqNum + 1,
		Payload:        []byte("b"),
	})
	<-c.receiver.DeliveryChan()

	// No losses — NAK should be nil
	nak := c.GenerateNAK()
	if nak != nil {
		t.Error("GenerateNAK should return nil when no losses detected")
	}
}

// --- ACKACK RTT measurement tests ---

// TestHandleACKACKUpdatesRTT verifies the end-to-end RTT measurement flow:
// GenerateACK (records send time) → HandleACKACK (computes RTT) → RTT updated.
func TestHandleACKACKUpdatesRTT(t *testing.T) {
	udpConn, peerAddr := testUDPConn(t)
	cfg := testConfig()
	log := testLogger()

	c := New(1, 2, peerAddr, udpConn, "test", cfg, log)
	defer c.Close()

	// Record the initial RTT (default 100ms = 100,000μs)
	initialRTT := c.sender.RTT()

	// Feed a packet so GenerateACK has something to acknowledge
	c.receiver.OnData(&packet.DataPacket{
		SequenceNumber: cfg.InitialSeqNum,
		Payload:        []byte("data"),
	})
	<-c.receiver.DeliveryChan()

	// Generate an ACK — this records the send time in pendingACKs
	ack := c.GenerateACK()
	if ack == nil {
		t.Fatal("GenerateACK returned nil")
	}

	// The ACK's TypeSpecific field is the ACK sequence number
	ackSeqNum := ack.TypeSpecific

	// Simulate a small delay (ACKACK arriving later)
	time.Sleep(1 * time.Millisecond)

	// Process the ACKACK — this computes RTT and updates the sender
	c.HandleACKACK(ackSeqNum)

	// RTT should have changed from the initial value
	newRTT := c.sender.RTT()
	if newRTT == initialRTT {
		t.Error("RTT should have been updated after ACKACK processing")
	}
}

// TestHandleACKACKIgnoresUnknownSequence verifies that ACKACK packets with
// unknown sequence numbers are safely ignored.
func TestHandleACKACKIgnoresUnknownSequence(t *testing.T) {
	udpConn, peerAddr := testUDPConn(t)
	cfg := testConfig()
	log := testLogger()

	c := New(1, 2, peerAddr, udpConn, "test", cfg, log)
	defer c.Close()

	initialRTT := c.sender.RTT()

	// Process ACKACK for a sequence number we never sent
	c.HandleACKACK(99999)

	// RTT should not change
	if c.sender.RTT() != initialRTT {
		t.Error("RTT should not change for unknown ACKACK")
	}
}

// --- Keepalive tests ---

// TestSendKeepaliveSendsValidPacket verifies that sendKeepalive sends a
// properly formatted keepalive control packet to the peer.
func TestSendKeepaliveSendsValidPacket(t *testing.T) {
	udpConn, peerAddr := testUDPConn(t)
	cfg := testConfig()
	log := testLogger()

	c := New(1, 2, peerAddr, udpConn, "test", cfg, log)
	defer c.Close()

	// Send a keepalive
	c.sendKeepalive()

	// Read the packet that was sent to the peer (which is our own socket
	// in the test setup — loopback)
	buf := make([]byte, 1500)
	udpConn.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err := udpConn.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("failed to read sent keepalive: %v", err)
	}

	// Parse and verify the keepalive packet
	ctrl, err := packet.UnmarshalControlPacket(buf[:n])
	if err != nil {
		t.Fatalf("unmarshal keepalive: %v", err)
	}

	if ctrl.Type != packet.CtrlKeepAlive {
		t.Errorf("want keepalive type %d, got %d", packet.CtrlKeepAlive, ctrl.Type)
	}
	if ctrl.Header.DestSocketID != c.peerSocketID {
		t.Errorf("dest socket ID: want %d, got %d", c.peerSocketID, ctrl.Header.DestSocketID)
	}
}

// --- TSBPD delivery and TLPKTDROP tests ---

// TestDeliverTSBPDDeliversReadyPackets verifies that deliverTSBPD delivers
// packets whose TSBPD delivery time has arrived.
func TestDeliverTSBPDDeliversReadyPackets(t *testing.T) {
	udpConn, peerAddr := testUDPConn(t)
	cfg := testConfig()
	cfg.TSBPDDelay = 0 // Zero delay — packets are immediately ready
	log := testLogger()

	c := New(1, 2, peerAddr, udpConn, "test", cfg, log)
	defer c.Close()

	// Insert a packet directly into the receive buffer (bypassing OnData
	// to avoid the automatic contiguous delivery)
	seq := circular.New(cfg.InitialSeqNum)
	c.receiver.mu.Lock()
	c.receiver.receiveBuffer[seq] = &packet.DataPacket{
		Header: packet.Header{
			Timestamp: 1000, // Some timestamp
		},
		SequenceNumber: cfg.InitialSeqNum,
		Payload:        []byte("tsbpd delivery"),
	}
	c.receiver.mu.Unlock()

	// Call deliverTSBPD — with zero delay, the packet should be delivered
	c.deliverTSBPD()

	// Check the delivery channel
	select {
	case data := <-c.receiver.DeliveryChan():
		if string(data) != "tsbpd delivery" {
			t.Errorf("want 'tsbpd delivery', got '%s'", string(data))
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for TSBPD delivery")
	}
}

// TestDropTooLateDropsExpiredPackets verifies that dropTooLate removes
// buffered packets whose delivery deadline has passed.
func TestDropTooLateDropsExpiredPackets(t *testing.T) {
	udpConn, peerAddr := testUDPConn(t)
	cfg := testConfig()
	cfg.TSBPDDelay = 0 // Zero delay — delivery time is immediately
	log := testLogger()

	c := New(1, 2, peerAddr, udpConn, "test", cfg, log)
	defer c.Close()

	// Override TSBPD to have baseTime far in the past so packets are "too late"
	c.tsbpd = &TSBPDManager{
		baseTime: 1000, // Very far in the past (1000μs since epoch)
		delay:    0,
	}

	// Insert a packet with a timestamp that makes it very old
	seq := circular.New(cfg.InitialSeqNum)
	c.receiver.mu.Lock()
	c.receiver.receiveBuffer[seq] = &packet.DataPacket{
		Header: packet.Header{
			Timestamp: 100,
		},
		SequenceNumber: cfg.InitialSeqNum,
		Payload:        []byte("expired"),
	}
	c.receiver.mu.Unlock()

	// Call dropTooLate — should remove the expired packet
	c.dropTooLate()

	// The packet should have been removed from the buffer
	c.receiver.mu.Lock()
	_, exists := c.receiver.receiveBuffer[seq]
	lastDelivered := c.receiver.lastDelivered
	c.receiver.mu.Unlock()

	if exists {
		t.Error("too-late packet should have been removed from buffer")
	}

	// lastDelivered should have advanced past the dropped packet
	if lastDelivered != seq {
		t.Errorf("lastDelivered should be %d after drop, got %d", seq.Val(), lastDelivered.Val())
	}
}

// TestDropTooLateSkipsMissingPackets verifies that dropTooLate skips
// gaps (missing packets) when later buffered packets are past deadline.
func TestDropTooLateSkipsMissingPackets(t *testing.T) {
	udpConn, peerAddr := testUDPConn(t)
	cfg := testConfig()
	log := testLogger()

	c := New(1, 2, peerAddr, udpConn, "test", cfg, log)
	defer c.Close()

	// Set TSBPD so all packets appear too late
	c.tsbpd = &TSBPDManager{
		baseTime:  1000,
		delay:     0,
		tsBaseSet: true,
		tsBase:    0,
	}

	// Simulate a gap: packet at InitialSeqNum is missing,
	// but InitialSeqNum+1 is in the buffer and too late
	seq1 := circular.New(cfg.InitialSeqNum + 1)
	c.receiver.mu.Lock()
	c.receiver.receiveBuffer[seq1] = &packet.DataPacket{
		Header: packet.Header{
			Timestamp: 200,
		},
		SequenceNumber: cfg.InitialSeqNum + 1,
		Payload:        []byte("after gap"),
	}
	// Mark the missing packet as a detected loss
	c.receiver.lossDetected[circular.New(cfg.InitialSeqNum)] = true
	c.receiver.mu.Unlock()

	// dropTooLate should skip the missing packet AND drop the too-late one
	c.dropTooLate()

	c.receiver.mu.Lock()
	lastDelivered := c.receiver.lastDelivered
	_, gapStillLost := c.receiver.lossDetected[circular.New(cfg.InitialSeqNum)]
	_, pktExists := c.receiver.receiveBuffer[seq1]
	c.receiver.mu.Unlock()

	// The gap should have been skipped (removed from lossDetected)
	if gapStillLost {
		t.Error("missing packet should have been removed from lossDetected")
	}

	// The too-late buffered packet should also be dropped
	if pktExists {
		t.Error("too-late buffered packet should have been dropped")
	}

	// lastDelivered should have advanced past both
	expectedDelivered := circular.New(cfg.InitialSeqNum + 1)
	if lastDelivered != expectedDelivered {
		t.Errorf("lastDelivered: want %d, got %d", expectedDelivered.Val(), lastDelivered.Val())
	}
}

// --- Process retransmissions tests ---

// TestProcessRetransmissionsSendsPackets verifies that ProcessRetransmissions
// sends queued retransmit packets to the peer.
func TestProcessRetransmissionsSendsPackets(t *testing.T) {
	udpConn, peerAddr := testUDPConn(t)
	cfg := testConfig()
	log := testLogger()

	c := New(1, 2, peerAddr, udpConn, "test", cfg, log)
	defer c.Close()

	// Store a packet in the send buffer
	dataPkt := &packet.DataPacket{
		Header: packet.Header{
			Timestamp:    5000,
			DestSocketID: 2,
		},
		SequenceNumber: cfg.InitialSeqNum,
		Position:       packet.PositionSolo,
		Payload:        []byte("retransmit me"),
	}
	c.sender.StoreSent(dataPkt)

	// Report it as lost via NAK
	c.sender.OnNAK([][2]uint32{{cfg.InitialSeqNum, cfg.InitialSeqNum}})

	// Process retransmissions — should send the packet
	c.ProcessRetransmissions()

	// Read the retransmitted packet from the UDP socket
	buf := make([]byte, 1500)
	udpConn.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err := udpConn.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("failed to read retransmitted packet: %v", err)
	}

	// Parse and verify the retransmitted data packet
	retransmitted, err := packet.UnmarshalDataPacket(buf[:n])
	if err != nil {
		t.Fatalf("unmarshal retransmitted packet: %v", err)
	}

	if retransmitted.SequenceNumber != cfg.InitialSeqNum {
		t.Errorf("retransmit seq: want %d, got %d", cfg.InitialSeqNum, retransmitted.SequenceNumber)
	}
	if !retransmitted.Retransmitted {
		t.Error("retransmitted packet should have R flag set")
	}
	if string(retransmitted.Payload) != "retransmit me" {
		t.Errorf("payload: want 'retransmit me', got '%s'", string(retransmitted.Payload))
	}
}

// --- Reliability loop integration test ---

// TestReliabilityLoopExitsOnClose verifies that the reliability loop
// goroutine exits cleanly when the connection is closed.
func TestReliabilityLoopExitsOnClose(t *testing.T) {
	udpConn, peerAddr := testUDPConn(t)
	cfg := testConfig()
	log := testLogger()

	c := New(1, 2, peerAddr, udpConn, "test", cfg, log)

	// Start the reliability loop
	c.StartReliability()

	// Give it a moment to start running
	time.Sleep(20 * time.Millisecond)

	// Close the connection — should cause the reliability loop to exit
	c.Close()

	// Give it time to actually exit (the select on c.done should fire quickly)
	time.Sleep(20 * time.Millisecond)

	// If we get here without hanging, the loop exited correctly.
	// The test timeout will catch any hang.
	if c.State() != StateClosed {
		t.Errorf("connection should be closed, got %v", c.State())
	}
}

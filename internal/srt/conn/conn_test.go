package conn

import (
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"github.com/alxayo/go-rtmp/internal/srt/circular"
	"github.com/alxayo/go-rtmp/internal/srt/packet"
)

// testLogger returns a logger that discards output during tests,
// keeping test output clean. Use slog.LevelDebug if you need to
// see log output while debugging a failing test.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError, // Only show errors during tests
	}))
}

// testConfig returns a ConnConfig with sensible defaults for testing.
func testConfig() ConnConfig {
	return ConnConfig{
		MTU:            1500,
		FlowWindow:     8192,
		TSBPDDelay:     120,
		PeerTSBPDDelay: 120,
		InitialSeqNum:  1000,
		PayloadSize:    1484, // 1500 - 16 (header size)
	}
}

// testUDPConn creates a UDP connection suitable for testing.
// It binds to a random local port and returns the conn and its address.
func testUDPConn(t *testing.T) (*net.UDPConn, *net.UDPAddr) {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("resolve UDP addr: %v", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("listen UDP: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn, conn.LocalAddr().(*net.UDPAddr)
}

// --- Conn tests ---

// TestNewCreatesConnectionWithCorrectState verifies that a newly created
// connection starts in the Connected state with all fields properly set.
func TestNewCreatesConnectionWithCorrectState(t *testing.T) {
	udpConn, peerAddr := testUDPConn(t)
	cfg := testConfig()
	log := testLogger()

	c := New(42, 99, peerAddr, udpConn, "live/test", cfg, log)

	// Verify the connection is in the Connected state
	if c.State() != StateConnected {
		t.Errorf("want state Connected, got %v", c.State())
	}

	// Verify identity fields are set correctly
	if c.LocalSocketID() != 42 {
		t.Errorf("want localSocketID 42, got %d", c.LocalSocketID())
	}
	if c.PeerSocketID() != 99 {
		t.Errorf("want peerSocketID 99, got %d", c.PeerSocketID())
	}
	if c.StreamID() != "live/test" {
		t.Errorf("want streamID live/test, got %s", c.StreamID())
	}
	if c.PeerAddr() != peerAddr {
		t.Errorf("want peerAddr %v, got %v", peerAddr, c.PeerAddr())
	}

	// Verify subsystems were created
	if c.sender == nil {
		t.Error("sender should not be nil")
	}
	if c.receiver == nil {
		t.Error("receiver should not be nil")
	}
}

// TestStateTransitions verifies that connections transition through states
// correctly: Connected → Closing → Closed.
func TestStateTransitions(t *testing.T) {
	udpConn, peerAddr := testUDPConn(t)
	cfg := testConfig()
	log := testLogger()

	c := New(1, 2, peerAddr, udpConn, "test", cfg, log)

	// Should start as Connected
	if c.State() != StateConnected {
		t.Fatalf("initial state should be Connected, got %v", c.State())
	}

	// Close should transition through Closing to Closed
	err := c.Close()
	if err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	if c.State() != StateClosed {
		t.Errorf("state after Close should be Closed, got %v", c.State())
	}
}

// TestCloseFiresDisconnectHandler verifies that the disconnect callback
// is invoked when the connection is closed.
func TestCloseFiresDisconnectHandler(t *testing.T) {
	udpConn, peerAddr := testUDPConn(t)
	cfg := testConfig()
	log := testLogger()

	c := New(1, 2, peerAddr, udpConn, "test", cfg, log)

	// Track whether the disconnect handler was called
	called := false
	c.SetDisconnectHandler(func() {
		called = true
	})

	c.Close()

	if !called {
		t.Error("disconnect handler should have been called on Close")
	}
}

// TestCloseIsIdempotent verifies that calling Close multiple times is safe
// and the disconnect handler is only fired once.
func TestCloseIsIdempotent(t *testing.T) {
	udpConn, peerAddr := testUDPConn(t)
	cfg := testConfig()
	log := testLogger()

	c := New(1, 2, peerAddr, udpConn, "test", cfg, log)

	callCount := 0
	c.SetDisconnectHandler(func() {
		callCount++
	})

	// Close twice — should not panic or fire handler twice
	c.Close()
	c.Close()

	if callCount != 1 {
		t.Errorf("disconnect handler called %d times, want 1", callCount)
	}
}

// TestReadReturnsEOFAfterClose verifies that Read returns io.EOF when
// the connection has been closed.
func TestReadReturnsEOFAfterClose(t *testing.T) {
	udpConn, peerAddr := testUDPConn(t)
	cfg := testConfig()
	log := testLogger()

	c := New(1, 2, peerAddr, udpConn, "test", cfg, log)
	c.Close()

	buf := make([]byte, 100)
	_, err := c.Read(buf)
	if err == nil {
		t.Error("Read after Close should return an error")
	}
}

// TestRecvPacketRoutesDataToReceiver verifies that incoming data packets
// are correctly parsed and delivered to the receiver.
func TestRecvPacketRoutesDataToReceiver(t *testing.T) {
	udpConn, peerAddr := testUDPConn(t)
	cfg := testConfig()
	log := testLogger()

	c := New(1, 2, peerAddr, udpConn, "test", cfg, log)
	defer c.Close()

	// Create a data packet with the initial sequence number
	dataPkt := &packet.DataPacket{
		Header: packet.Header{
			IsControl:    false,
			Timestamp:    12345,
			DestSocketID: 1,
		},
		SequenceNumber: cfg.InitialSeqNum,
		Position:       packet.PositionSolo,
		Payload:        []byte("hello SRT"),
	}

	// Marshal it to raw bytes as if it came from the network
	raw, err := dataPkt.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal data packet: %v", err)
	}

	// Feed it to the connection
	c.RecvPacket(raw)

	// The data should be available via the receiver's delivery channel
	select {
	case data := <-c.receiver.DeliveryChan():
		if string(data) != "hello SRT" {
			t.Errorf("want payload 'hello SRT', got '%s'", string(data))
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for data delivery")
	}
}

// TestRecvPacketRoutesControlPackets verifies that incoming control packets
// are dispatched to the correct handler based on their type.
func TestRecvPacketRoutesControlPackets(t *testing.T) {
	udpConn, peerAddr := testUDPConn(t)
	cfg := testConfig()
	log := testLogger()

	c := New(1, 2, peerAddr, udpConn, "test", cfg, log)
	defer c.Close()

	// Create a keepalive control packet
	keepalive := &packet.ControlPacket{
		Header: packet.Header{
			IsControl:    true,
			Timestamp:    5000,
			DestSocketID: 1,
		},
		Type: packet.CtrlKeepAlive,
	}

	raw, err := keepalive.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal keepalive: %v", err)
	}

	// Should not panic — keepalive is handled silently
	c.RecvPacket(raw)
}

// TestRecvPacketHandlesShutdown verifies that receiving a Shutdown control
// packet causes the connection to close.
func TestRecvPacketHandlesShutdown(t *testing.T) {
	udpConn, peerAddr := testUDPConn(t)
	cfg := testConfig()
	log := testLogger()

	c := New(1, 2, peerAddr, udpConn, "test", cfg, log)

	// Create a shutdown control packet
	shutdown := &packet.ControlPacket{
		Header: packet.Header{
			IsControl:    true,
			DestSocketID: 1,
		},
		Type: packet.CtrlShutdown,
	}

	raw, err := shutdown.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal shutdown: %v", err)
	}

	// Feed the shutdown packet
	c.RecvPacket(raw)

	// Connection should now be closed
	if c.State() != StateClosed {
		t.Errorf("state after shutdown should be Closed, got %v", c.State())
	}
}

// --- Sender tests ---

// TestSenderNextSequenceNumber verifies that NextSequenceNumber returns
// sequential numbers and correctly handles 31-bit wraparound.
func TestSenderNextSequenceNumber(t *testing.T) {
	log := testLogger()
	cfg := testConfig()

	// Start at sequence number 100
	s := NewSender(circular.New(100), cfg, log)

	// First call should return 100
	seq1 := s.NextSequenceNumber()
	if seq1.Val() != 100 {
		t.Errorf("first seq: want 100, got %d", seq1.Val())
	}

	// Second call should return 101
	seq2 := s.NextSequenceNumber()
	if seq2.Val() != 101 {
		t.Errorf("second seq: want 101, got %d", seq2.Val())
	}

	// Third call should return 102
	seq3 := s.NextSequenceNumber()
	if seq3.Val() != 102 {
		t.Errorf("third seq: want 102, got %d", seq3.Val())
	}
}

// TestSenderNextSequenceNumberWraparound verifies that sequence numbers
// wrap around correctly at the 31-bit boundary (0x7FFFFFFF → 0).
func TestSenderNextSequenceNumberWraparound(t *testing.T) {
	log := testLogger()
	cfg := testConfig()

	// Start just before the maximum 31-bit value
	maxSeq := circular.MaxVal.Val() // 0x7FFFFFFF
	s := NewSender(circular.New(maxSeq), cfg, log)

	// Should return the max value
	seq1 := s.NextSequenceNumber()
	if seq1.Val() != maxSeq {
		t.Errorf("want max seq %d, got %d", maxSeq, seq1.Val())
	}

	// Next should wrap around to 0
	seq2 := s.NextSequenceNumber()
	if seq2.Val() != 0 {
		t.Errorf("want wrapped seq 0, got %d", seq2.Val())
	}
}

// TestSenderOnACKRemovesAcknowledgedPackets verifies that OnACK correctly
// removes all acknowledged packets from the send buffer.
func TestSenderOnACKRemovesAcknowledgedPackets(t *testing.T) {
	log := testLogger()
	cfg := testConfig()
	s := NewSender(circular.New(100), cfg, log)

	// Store 5 packets (seq 100-104)
	for i := uint32(0); i < 5; i++ {
		pkt := &packet.DataPacket{SequenceNumber: 100 + i}
		s.StoreSent(pkt)
	}

	if s.BufferedPackets() != 5 {
		t.Fatalf("want 5 buffered packets, got %d", s.BufferedPackets())
	}

	// ACK up to 103 — packets 100, 101, 102 should be removed
	// (ACK means "I've received everything before this number")
	s.OnACK(circular.New(103))

	if s.BufferedPackets() != 2 {
		t.Errorf("want 2 buffered packets after ACK, got %d", s.BufferedPackets())
	}
}

// TestSenderOnACKIgnoresOldACK verifies that an ACK for a sequence number
// older than the last ACK is ignored (protects against reordering).
func TestSenderOnACKIgnoresOldACK(t *testing.T) {
	log := testLogger()
	cfg := testConfig()
	s := NewSender(circular.New(100), cfg, log)

	for i := uint32(0); i < 5; i++ {
		pkt := &packet.DataPacket{SequenceNumber: 100 + i}
		s.StoreSent(pkt)
	}

	// ACK up to 103
	s.OnACK(circular.New(103))
	remaining := s.BufferedPackets()

	// Send an older ACK (101) — should have no effect
	s.OnACK(circular.New(101))

	if s.BufferedPackets() != remaining {
		t.Errorf("old ACK should not change buffer, was %d now %d", remaining, s.BufferedPackets())
	}
}

// TestSenderOnNAKQueuesRetransmissions verifies that OnNAK adds lost packets
// to the retransmission queue.
func TestSenderOnNAKQueuesRetransmissions(t *testing.T) {
	log := testLogger()
	cfg := testConfig()
	s := NewSender(circular.New(100), cfg, log)

	// Store packets 100-104
	for i := uint32(0); i < 5; i++ {
		pkt := &packet.DataPacket{
			SequenceNumber: 100 + i,
			Payload:        []byte{byte(i)},
		}
		s.StoreSent(pkt)
	}

	// NAK for packets 101 and 103 (two individual losses)
	s.OnNAK([][2]uint32{
		{101, 101},
		{103, 103},
	})

	// Should be able to get 2 retransmissions
	pkt1 := s.GetRetransmit()
	if pkt1 == nil {
		t.Fatal("expected retransmit packet, got nil")
	}
	if pkt1.SequenceNumber != 101 {
		t.Errorf("first retransmit: want seq 101, got %d", pkt1.SequenceNumber)
	}

	pkt2 := s.GetRetransmit()
	if pkt2 == nil {
		t.Fatal("expected second retransmit packet, got nil")
	}
	if pkt2.SequenceNumber != 103 {
		t.Errorf("second retransmit: want seq 103, got %d", pkt2.SequenceNumber)
	}

	// No more retransmissions
	pkt3 := s.GetRetransmit()
	if pkt3 != nil {
		t.Errorf("expected nil, got packet with seq %d", pkt3.SequenceNumber)
	}
}

// TestSenderOnNAKRangeQueuesAll verifies that a NAK with a range (e.g., 101-103)
// queues all packets in that range for retransmission.
func TestSenderOnNAKRangeQueuesAll(t *testing.T) {
	log := testLogger()
	cfg := testConfig()
	s := NewSender(circular.New(100), cfg, log)

	// Store packets 100-105
	for i := uint32(0); i < 6; i++ {
		pkt := &packet.DataPacket{SequenceNumber: 100 + i}
		s.StoreSent(pkt)
	}

	// NAK for range 101-103 (inclusive)
	s.OnNAK([][2]uint32{{101, 103}})

	// Should get exactly 3 retransmissions
	count := 0
	for s.GetRetransmit() != nil {
		count++
	}
	if count != 3 {
		t.Errorf("want 3 retransmissions from range, got %d", count)
	}
}

// TestSenderRTTEstimation verifies that the EWMA RTT calculation works correctly.
func TestSenderRTTEstimation(t *testing.T) {
	log := testLogger()
	cfg := testConfig()
	s := NewSender(circular.New(0), cfg, log)

	// Record the initial RTT (default is 100000 µs = 100ms)
	initialRTT := s.RTT()

	// Feed a sample that's lower than the default
	s.UpdateRTT(50000) // 50ms sample

	// RTT should have moved toward the sample (decreased)
	newRTT := s.RTT()
	if newRTT >= initialRTT {
		t.Errorf("RTT should decrease toward sample: initial=%d, new=%d", initialRTT, newRTT)
	}

	// Feed many samples at 50ms to converge. EWMA is 7/8 weighted toward
	// the old value, so it takes many iterations to fully converge.
	for i := 0; i < 100; i++ {
		s.UpdateRTT(50000)
	}

	// Should be very close to 50000 after 100 samples
	finalRTT := s.RTT()
	if finalRTT < 49000 || finalRTT > 51000 {
		t.Errorf("RTT should converge near 50000, got %d", finalRTT)
	}
}

// TestSenderRTTVarUpdates verifies that RTT variance is tracked.
func TestSenderRTTVarUpdates(t *testing.T) {
	log := testLogger()
	cfg := testConfig()
	s := NewSender(circular.New(0), cfg, log)

	// Initial variance should be non-zero
	if s.RTTVar() == 0 {
		t.Error("initial RTT variance should be non-zero")
	}

	// Feed consistent samples to reduce variance
	for i := 0; i < 50; i++ {
		s.UpdateRTT(80000) // consistent 80ms
	}

	// Variance should be small after many consistent samples
	if s.RTTVar() > 10000 {
		t.Errorf("RTT variance should be small after consistent samples, got %d", s.RTTVar())
	}
}

// --- Receiver tests ---

// TestReceiverOnDataDeliversContiguous verifies that packets arriving in order
// are delivered immediately through the delivery channel.
func TestReceiverOnDataDeliversContiguous(t *testing.T) {
	log := testLogger()
	cfg := testConfig()
	r := NewReceiver(circular.New(cfg.InitialSeqNum), cfg, log)

	// Send first packet (the initial sequence number)
	pkt := &packet.DataPacket{
		SequenceNumber: cfg.InitialSeqNum,
		Payload:        []byte("packet-1"),
	}
	r.OnData(pkt)

	// Should be available in the delivery channel
	select {
	case data := <-r.DeliveryChan():
		if string(data) != "packet-1" {
			t.Errorf("want 'packet-1', got '%s'", string(data))
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for delivery")
	}
}

// TestReceiverOnDataBuffersOutOfOrder verifies that out-of-order packets
// are buffered and only delivered once the gap is filled.
func TestReceiverOnDataBuffersOutOfOrder(t *testing.T) {
	log := testLogger()
	cfg := testConfig()
	r := NewReceiver(circular.New(cfg.InitialSeqNum), cfg, log)

	// Send packet 2 first (skipping packet 1) — this creates a gap
	pkt2 := &packet.DataPacket{
		SequenceNumber: cfg.InitialSeqNum + 1,
		Payload:        []byte("packet-2"),
	}
	r.OnData(pkt2)

	// Nothing should be delivered yet because packet 1 is missing
	select {
	case <-r.DeliveryChan():
		t.Error("should not deliver out-of-order packet immediately")
	case <-time.After(50 * time.Millisecond):
		// Good — no delivery yet
	}

	// Now send packet 1 — this fills the gap
	pkt1 := &packet.DataPacket{
		SequenceNumber: cfg.InitialSeqNum,
		Payload:        []byte("packet-1"),
	}
	r.OnData(pkt1)

	// Both packets should now be delivered in order
	select {
	case data := <-r.DeliveryChan():
		if string(data) != "packet-1" {
			t.Errorf("first delivery: want 'packet-1', got '%s'", string(data))
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first delivery")
	}

	select {
	case data := <-r.DeliveryChan():
		if string(data) != "packet-2" {
			t.Errorf("second delivery: want 'packet-2', got '%s'", string(data))
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for second delivery")
	}
}

// TestReceiverGetACKSequence verifies that the ACK sequence number reflects
// the next expected packet after all contiguous deliveries.
func TestReceiverGetACKSequence(t *testing.T) {
	log := testLogger()
	cfg := testConfig()
	r := NewReceiver(circular.New(cfg.InitialSeqNum), cfg, log)

	// Before any data, ACK sequence should be the initial seq num
	ackSeq := r.GetACKSequence()
	if ackSeq.Val() != cfg.InitialSeqNum {
		t.Errorf("initial ACK seq: want %d, got %d", cfg.InitialSeqNum, ackSeq.Val())
	}

	// Deliver two packets in order
	r.OnData(&packet.DataPacket{
		SequenceNumber: cfg.InitialSeqNum,
		Payload:        []byte("a"),
	})
	// Drain the delivery channel
	<-r.DeliveryChan()

	r.OnData(&packet.DataPacket{
		SequenceNumber: cfg.InitialSeqNum + 1,
		Payload:        []byte("b"),
	})
	<-r.DeliveryChan()

	// ACK sequence should now be initialSeqNum + 2
	ackSeq = r.GetACKSequence()
	if ackSeq.Val() != cfg.InitialSeqNum+2 {
		t.Errorf("ACK seq after 2 packets: want %d, got %d", cfg.InitialSeqNum+2, ackSeq.Val())
	}
}

// TestReceiverGetLossReportDetectsGaps verifies that the receiver correctly
// detects and reports missing packets when there are gaps in the sequence.
func TestReceiverGetLossReportDetectsGaps(t *testing.T) {
	log := testLogger()
	cfg := testConfig()
	r := NewReceiver(circular.New(cfg.InitialSeqNum), cfg, log)

	// Send packet 1000 (initial) then skip 1001, send 1002
	r.OnData(&packet.DataPacket{
		SequenceNumber: cfg.InitialSeqNum,
		Payload:        []byte("first"),
	})
	<-r.DeliveryChan()

	// Skip 1001 and send 1002 — creates a gap at 1001
	r.OnData(&packet.DataPacket{
		SequenceNumber: cfg.InitialSeqNum + 2,
		Payload:        []byte("third"),
	})

	// Loss report should include the missing sequence number
	report := r.GetLossReport()
	if len(report) == 0 {
		t.Fatal("expected loss report for missing packet, got none")
	}

	// The missing packet is initialSeqNum + 1
	found := false
	for _, r := range report {
		if r[0] == cfg.InitialSeqNum+1 {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("loss report should include seq %d, got %v", cfg.InitialSeqNum+1, report)
	}

	// After calling GetLossReport, the loss list should be cleared
	report2 := r.GetLossReport()
	if len(report2) != 0 {
		t.Errorf("loss report should be empty after retrieval, got %v", report2)
	}
}

// TestReceiverAvailableBuffer verifies that the available buffer count
// decreases as packets are buffered.
func TestReceiverAvailableBuffer(t *testing.T) {
	log := testLogger()
	cfg := testConfig()
	cfg.FlowWindow = 100 // Small window for testing
	r := NewReceiver(circular.New(cfg.InitialSeqNum), cfg, log)

	// Initially, the full flow window should be available
	avail := r.AvailableBuffer()
	if avail != 100 {
		t.Errorf("initial available buffer: want 100, got %d", avail)
	}

	// Buffer an out-of-order packet (it stays in the buffer, not delivered)
	r.OnData(&packet.DataPacket{
		SequenceNumber: cfg.InitialSeqNum + 5, // gap, won't be delivered
		Payload:        []byte("x"),
	})

	avail2 := r.AvailableBuffer()
	if avail2 != 99 {
		t.Errorf("available buffer after 1 buffered packet: want 99, got %d", avail2)
	}
}

// TestReceiverDuplicatePacketsIgnored verifies that receiving the same
// packet twice doesn't cause double delivery or errors.
func TestReceiverDuplicatePacketsIgnored(t *testing.T) {
	log := testLogger()
	cfg := testConfig()
	r := NewReceiver(circular.New(cfg.InitialSeqNum), cfg, log)

	pkt := &packet.DataPacket{
		SequenceNumber: cfg.InitialSeqNum,
		Payload:        []byte("data"),
	}

	// Send the same packet twice
	r.OnData(pkt)
	r.OnData(pkt) // duplicate — should be ignored

	// Should only get one delivery
	<-r.DeliveryChan()

	select {
	case <-r.DeliveryChan():
		t.Error("duplicate packet should not cause double delivery")
	case <-time.After(50 * time.Millisecond):
		// Good — no duplicate delivery
	}
}

// --- TimerManager tests ---

// TestTimerManagerCreatesAndStops verifies that timers are created with
// the correct intervals and can be stopped cleanly.
func TestTimerManagerCreatesAndStops(t *testing.T) {
	tm := NewTimerManager()

	// Verify all timer channels are non-nil
	if tm.ACKChan() == nil {
		t.Error("ACK timer channel should not be nil")
	}
	if tm.NAKChan() == nil {
		t.Error("NAK timer channel should not be nil")
	}
	if tm.KeepaliveChan() == nil {
		t.Error("keepalive timer channel should not be nil")
	}

	// Stop should not panic
	tm.Stop()
}

// TestTimerManagerACKFires verifies that the ACK timer fires within the
// expected interval.
func TestTimerManagerACKFires(t *testing.T) {
	tm := NewTimerManager()
	defer tm.Stop()

	// ACK timer should fire within 2x the interval (allow for scheduling jitter)
	select {
	case <-tm.ACKChan():
		// Good — timer fired
	case <-time.After(time.Duration(ACKIntervalMs*3) * time.Millisecond):
		t.Error("ACK timer did not fire within expected interval")
	}
}

// --- State String tests ---

// TestStateString verifies that State values have correct string representations.
func TestStateString(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{StateConnected, "Connected"},
		{StateClosing, "Closing"},
		{StateClosed, "Closed"},
		{State(99), "Unknown"},
	}

	for _, tt := range tests {
		got := tt.state.String()
		if got != tt.want {
			t.Errorf("State(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

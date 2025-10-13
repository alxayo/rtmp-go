package integration

// Integration tests for RTMP relay feature (Feature 002)
// These tests validate end-to-end publish → relay → play flow

import (
	"bytes"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/alxayo/go-rtmp/internal/rtmp/amf"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
	"github.com/alxayo/go-rtmp/internal/rtmp/server"
)

// TestPublishToPlayRelay validates basic relay functionality:
// 1. Publisher connects and publishes to "live/test"
// 2. Subscriber connects and plays "live/test"
// 3. Publisher sends audio/video messages
// 4. Subscriber receives the same messages
func TestPublishToPlayRelay(t *testing.T) {
	// Start server
	cfg := server.Config{
		ListenAddr: "127.0.0.1:0", // Random port
		RecordDir:  "",            // No recording for this test
		RecordAll:  false,
	}

	srv := server.New(cfg)
	if err := srv.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer srv.Stop()

	serverAddr := srv.Addr().String()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Connect publisher
	pubConn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		t.Fatalf("Publisher failed to connect: %v", err)
	}
	defer pubConn.Close()

	// Perform publisher handshake
	if err := performHandshake(pubConn); err != nil {
		t.Fatalf("Publisher handshake failed: %v", err)
	}

	// Publisher: connect command
	if err := sendConnectCommand(pubConn, "live"); err != nil {
		t.Fatalf("Publisher connect failed: %v", err)
	}

	// Read connect response
	if err := readAndDiscardMessages(pubConn, 2, 5*time.Second); err != nil {
		t.Fatalf("Publisher connect response failed: %v", err)
	}

	// Publisher: createStream command
	if err := sendCreateStreamCommand(pubConn); err != nil {
		t.Fatalf("Publisher createStream failed: %v", err)
	}

	// Read createStream response
	if err := readAndDiscardMessages(pubConn, 2, 5*time.Second); err != nil {
		t.Fatalf("Publisher createStream response failed: %v", err)
	}

	// Publisher: publish command
	if err := sendPublishCommand(pubConn, "live", "test"); err != nil {
		t.Fatalf("Publisher publish failed: %v", err)
	}

	// Read publish response
	if err := readAndDiscardMessages(pubConn, 1, 5*time.Second); err != nil {
		t.Fatalf("Publisher publish response failed: %v", err)
	}

	// Give server time to register publisher
	time.Sleep(100 * time.Millisecond)

	// Connect subscriber
	subConn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		t.Fatalf("Subscriber failed to connect: %v", err)
	}
	defer subConn.Close()

	// Perform subscriber handshake
	if err := performHandshake(subConn); err != nil {
		t.Fatalf("Subscriber handshake failed: %v", err)
	}

	// Subscriber: connect command
	if err := sendConnectCommand(subConn, "live"); err != nil {
		t.Fatalf("Subscriber connect failed: %v", err)
	}

	// Read connect response
	if err := readAndDiscardMessages(subConn, 2, 5*time.Second); err != nil {
		t.Fatalf("Subscriber connect response failed: %v", err)
	}

	// Subscriber: createStream command
	if err := sendCreateStreamCommand(subConn); err != nil {
		t.Fatalf("Subscriber createStream failed: %v", err)
	}

	// Read createStream response
	if err := readAndDiscardMessages(subConn, 2, 5*time.Second); err != nil {
		t.Fatalf("Subscriber createStream response failed: %v", err)
	}

	// Subscriber: play command
	if err := sendPlayCommand(subConn, "live", "test"); err != nil {
		t.Fatalf("Subscriber play failed: %v", err)
	}

	// Read play response
	if err := readAndDiscardMessages(subConn, 2, 5*time.Second); err != nil {
		t.Fatalf("Subscriber play response failed: %v", err)
	}

	// Publisher: Send audio message
	audioPayload := []byte{0xAF, 0x00, 0x01, 0x02, 0x03, 0x04} // AAC sequence header
	audioMsg := &chunk.Message{
		CSID:            4, // Audio messages use CSID 4
		TypeID:          8, // Audio
		MessageStreamID: 1,
		Timestamp:       1000,
		Payload:         audioPayload,
	}

	if err := sendMessage(pubConn, audioMsg); err != nil {
		t.Fatalf("Failed to send audio message: %v", err)
	}

	// Publisher: Send video message
	videoPayload := []byte{0x17, 0x00, 0x00, 0x00, 0x00, 0x01, 0x64} // AVC sequence header
	videoMsg := &chunk.Message{
		CSID:            6, // Video messages use CSID 6
		TypeID:          9, // Video
		MessageStreamID: 1,
		Timestamp:       2000,
		Payload:         videoPayload,
	}

	if err := sendMessage(pubConn, videoMsg); err != nil {
		t.Fatalf("Failed to send video message: %v", err)
	}

	// Subscriber: Receive messages (should get audio and video)
	receivedAudio := false
	receivedVideo := false

	for i := 0; i < 10; i++ { // Try reading up to 10 messages
		msg, err := readMessage(subConn, 2*time.Second)
		if err != nil {
			if i > 5 { // Give some attempts before failing
				break
			}
			continue
		}

		if msg.TypeID == 8 {
			receivedAudio = true
			if !bytes.Equal(msg.Payload, audioPayload) {
				t.Errorf("Audio payload mismatch: expected %v, got %v", audioPayload, msg.Payload)
			}
		}

		if msg.TypeID == 9 {
			receivedVideo = true
			if !bytes.Equal(msg.Payload, videoPayload) {
				t.Errorf("Video payload mismatch: expected %v, got %v", videoPayload, msg.Payload)
			}
		}

		if receivedAudio && receivedVideo {
			break
		}
	}

	if !receivedAudio {
		t.Error("Subscriber did not receive audio message")
	}

	if !receivedVideo {
		t.Error("Subscriber did not receive video message")
	}

	t.Logf("✅ Relay test passed: subscriber received both audio and video messages")
}

// TestRelayMultipleSubscribers validates that multiple subscribers receive the same media
func TestRelayMultipleSubscribers(t *testing.T) {
	// Start server
	cfg := server.Config{
		ListenAddr: "127.0.0.1:0",
		RecordDir:  "",
		RecordAll:  false,
	}

	srv := server.New(cfg)
	if err := srv.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer srv.Stop()

	serverAddr := srv.Addr().String()
	time.Sleep(100 * time.Millisecond)

	// Setup publisher (abbreviated version)
	pubConn := mustSetupPublisher(t, serverAddr, "live", "multitest")
	defer pubConn.Close()

	time.Sleep(100 * time.Millisecond)

	// Setup 3 subscribers
	sub1 := mustSetupSubscriber(t, serverAddr, "live", "multitest")
	defer sub1.Close()

	sub2 := mustSetupSubscriber(t, serverAddr, "live", "multitest")
	defer sub2.Close()

	sub3 := mustSetupSubscriber(t, serverAddr, "live", "multitest")
	defer sub3.Close()

	// Publisher sends audio message
	audioPayload := []byte{0xAF, 0x01, 0xAA, 0xBB}
	audioMsg := &chunk.Message{
		CSID:            4, // Audio messages use CSID 4
		TypeID:          8,
		MessageStreamID: 1,
		Timestamp:       3000,
		Payload:         audioPayload,
	}

	if err := sendMessage(pubConn, audioMsg); err != nil {
		t.Fatalf("Failed to send audio: %v", err)
	}

	// All subscribers should receive the message
	subscribers := []net.Conn{sub1, sub2, sub3}
	for i, sub := range subscribers {
		received := false
		for j := 0; j < 10; j++ {
			msg, err := readMessage(sub, 2*time.Second)
			if err != nil {
				continue
			}
			if msg.TypeID == 8 && bytes.Equal(msg.Payload, audioPayload) {
				received = true
				break
			}
		}

		if !received {
			t.Errorf("Subscriber %d did not receive audio message", i+1)
		}
	}

	t.Logf("✅ Multiple subscribers test passed: all 3 subscribers received the message")
}

// Helper functions

func performHandshake(conn net.Conn) error {
	// Send C0+C1
	c0c1 := make([]byte, 1+1536)
	c0c1[0] = 0x03 // RTMP version 3

	if _, err := conn.Write(c0c1); err != nil {
		return fmt.Errorf("write C0+C1: %w", err)
	}

	// Read S0+S1+S2
	s0s1s2 := make([]byte, 1+1536+1536)
	if _, err := conn.Read(s0s1s2); err != nil {
		return fmt.Errorf("read S0+S1+S2: %w", err)
	}

	// Send C2 (echo S1)
	c2 := s0s1s2[1:1537]
	if _, err := conn.Write(c2); err != nil {
		return fmt.Errorf("write C2: %w", err)
	}

	return nil
}

func sendConnectCommand(conn net.Conn, app string) error {
	// Build connect command manually using amf.EncodeAll
	payload, err := amf.EncodeAll(
		"connect",
		float64(1), // Transaction ID
		map[string]interface{}{
			"app":            app,
			"tcUrl":          fmt.Sprintf("rtmp://localhost/%s", app),
			"flashVer":       "FMLE/3.0",
			"objectEncoding": float64(0),
		},
	)
	if err != nil {
		return fmt.Errorf("encode connect: %w", err)
	}

	msg := &chunk.Message{
		CSID:            3,  // Command messages use CSID 3
		TypeID:          20, // AMF0 command
		MessageStreamID: 0,
		Timestamp:       0,
		Payload:         payload,
	}

	return sendMessage(conn, msg)
}

func sendCreateStreamCommand(conn net.Conn) error {
	// Build createStream command manually using amf.EncodeAll
	payload, err := amf.EncodeAll(
		"createStream",
		float64(2), // Transaction ID
		nil,        // Null
	)
	if err != nil {
		return fmt.Errorf("encode createStream: %w", err)
	}

	msg := &chunk.Message{
		CSID:            3,  // Command messages use CSID 3
		TypeID:          20, // AMF0 command
		MessageStreamID: 0,
		Timestamp:       0,
		Payload:         payload,
	}

	return sendMessage(conn, msg)
}

func sendPublishCommand(conn net.Conn, app, streamName string) error {
	// Build publish command manually using amf.EncodeAll
	payload, err := amf.EncodeAll(
		"publish",
		float64(0), // Transaction ID (publish always uses 0)
		nil,        // Null
		streamName, // Publishing name
		"live",     // Publishing type
	)
	if err != nil {
		return fmt.Errorf("encode publish: %w", err)
	}

	msg := &chunk.Message{
		CSID:            3,  // Command messages use CSID 3
		TypeID:          20, // AMF0 command
		MessageStreamID: 1,
		Timestamp:       0,
		Payload:         payload,
	}

	return sendMessage(conn, msg)
}

func sendPlayCommand(conn net.Conn, app, streamName string) error {
	// Build play command manually using amf.EncodeAll
	payload, err := amf.EncodeAll(
		"play",
		float64(0),  // Transaction ID
		nil,         // Null
		streamName,  // Stream name
		float64(-2), // Start: -2 (live)
	)
	if err != nil {
		return fmt.Errorf("encode play: %w", err)
	}

	msg := &chunk.Message{
		CSID:            3,  // Command messages use CSID 3
		TypeID:          20, // AMF0 command
		MessageStreamID: 1,
		Timestamp:       0,
		Payload:         payload,
	}

	return sendMessage(conn, msg)
}

func sendMessage(conn net.Conn, msg *chunk.Message) error {
	writer := chunk.NewWriter(conn, 128)
	return writer.WriteMessage(msg)
}

func readMessage(conn net.Conn, timeout time.Duration) (*chunk.Message, error) {
	conn.SetReadDeadline(time.Now().Add(timeout))
	reader := chunk.NewReader(conn, 128)
	return reader.ReadMessage()
}

func readAndDiscardMessages(conn net.Conn, count int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	reader := chunk.NewReader(conn, 128)

	for i := 0; i < count; i++ {
		conn.SetReadDeadline(deadline)
		if _, err := reader.ReadMessage(); err != nil {
			return fmt.Errorf("failed to read message %d: %w", i+1, err)
		}
	}

	return nil
}

func mustSetupPublisher(t *testing.T, addr, app, streamName string) net.Conn {
	t.Helper()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Publisher dial failed: %v", err)
	}

	if err := performHandshake(conn); err != nil {
		conn.Close()
		t.Fatalf("Publisher handshake failed: %v", err)
	}

	if err := sendConnectCommand(conn, app); err != nil {
		conn.Close()
		t.Fatalf("Publisher connect failed: %v", err)
	}
	readAndDiscardMessages(conn, 2, 5*time.Second)

	if err := sendCreateStreamCommand(conn); err != nil {
		conn.Close()
		t.Fatalf("Publisher createStream failed: %v", err)
	}
	readAndDiscardMessages(conn, 2, 5*time.Second)

	if err := sendPublishCommand(conn, app, streamName); err != nil {
		conn.Close()
		t.Fatalf("Publisher publish failed: %v", err)
	}
	readAndDiscardMessages(conn, 1, 5*time.Second)

	return conn
}

func mustSetupSubscriber(t *testing.T, addr, app, streamName string) net.Conn {
	t.Helper()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Subscriber dial failed: %v", err)
	}

	if err := performHandshake(conn); err != nil {
		conn.Close()
		t.Fatalf("Subscriber handshake failed: %v", err)
	}

	if err := sendConnectCommand(conn, app); err != nil {
		conn.Close()
		t.Fatalf("Subscriber connect failed: %v", err)
	}
	readAndDiscardMessages(conn, 2, 5*time.Second)

	if err := sendCreateStreamCommand(conn); err != nil {
		conn.Close()
		t.Fatalf("Subscriber createStream failed: %v", err)
	}
	readAndDiscardMessages(conn, 2, 5*time.Second)

	if err := sendPlayCommand(conn, app, streamName); err != nil {
		conn.Close()
		t.Fatalf("Subscriber play failed: %v", err)
	}
	readAndDiscardMessages(conn, 2, 5*time.Second)

	return conn
}

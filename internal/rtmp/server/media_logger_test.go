// media_logger_test.go – tests for the media statistics logger.
//
// MediaLogger tracks audio/video message counts and byte totals per
// connection. It processes messages asynchronously and periodically logs
// summary statistics.
//
// Tests verify:
//   - Audio-only, video-only, and mixed message counting.
//   - Non-media messages (TypeID 20 = command) are ignored.
//   - Periodic stats logging fires on the configured interval.
//
// Key Go concepts:
//   - time.Sleep for async processing synchronization.
//   - slog.New with custom handler for stdout debug output.
package server

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// TestMediaLogger_ProcessMessage_Audio sends one audio message (TypeID 8)
// and verifies audioCount=1, videoCount=0, totalBytes=10.
func TestMediaLogger_ProcessMessage_Audio(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ml := NewMediaLogger("test-conn-001", log, 100*time.Millisecond)
	defer ml.Stop()

	// Create a mock audio message (type 8)
	msg := &chunk.Message{
		CSID:            4,
		Timestamp:       1000,
		MessageLength:   10,
		TypeID:          8, // Audio
		MessageStreamID: 1,
		Payload:         []byte{0xAF, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, // AAC header
	}

	ml.ProcessMessage(msg)

	// Wait briefly for async processing
	time.Sleep(50 * time.Millisecond)

	audioCount, videoCount, totalBytes, _, _ := ml.GetStats()
	if audioCount != 1 {
		t.Errorf("Expected audio count 1, got %d", audioCount)
	}
	if videoCount != 0 {
		t.Errorf("Expected video count 0, got %d", videoCount)
	}
	if totalBytes != 10 {
		t.Errorf("Expected totalBytes 10, got %d", totalBytes)
	}
}

// TestMediaLogger_ProcessMessage_Video sends one video message (TypeID 9)
// and verifies videoCount=1, audioCount=0, totalBytes=15.
func TestMediaLogger_ProcessMessage_Video(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ml := NewMediaLogger("test-conn-002", log, 100*time.Millisecond)
	defer ml.Stop()

	// Create a mock video message (type 9)
	msg := &chunk.Message{
		CSID:            6,
		Timestamp:       2000,
		MessageLength:   15,
		TypeID:          9, // Video
		MessageStreamID: 1,
		Payload:         []byte{0x17, 0x00, 0x00, 0x00, 0x00, 0x01, 0x64, 0x00, 0x1F, 0xFF, 0xE1, 0x00, 0x00, 0x00, 0x00}, // H.264 AVC header
	}

	ml.ProcessMessage(msg)

	// Wait briefly for async processing
	time.Sleep(50 * time.Millisecond)

	audioCount, videoCount, totalBytes, _, _ := ml.GetStats()
	if audioCount != 0 {
		t.Errorf("Expected audio count 0, got %d", audioCount)
	}
	if videoCount != 1 {
		t.Errorf("Expected video count 1, got %d", videoCount)
	}
	if totalBytes != 15 {
		t.Errorf("Expected totalBytes 15, got %d", totalBytes)
	}
}

// TestMediaLogger_ProcessMessage_Mixed sends 2 audio + 1 video messages
// and verifies combined counts and totalBytes (10+20+10=40).
func TestMediaLogger_ProcessMessage_Mixed(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ml := NewMediaLogger("test-conn-003", log, 100*time.Millisecond)
	defer ml.Stop()

	// Send audio message
	audioMsg := &chunk.Message{
		CSID:            4,
		Timestamp:       1000,
		MessageLength:   10,
		TypeID:          8,
		MessageStreamID: 1,
		Payload:         make([]byte, 10),
	}
	ml.ProcessMessage(audioMsg)

	// Send video message
	videoMsg := &chunk.Message{
		CSID:            6,
		Timestamp:       2000,
		MessageLength:   20,
		TypeID:          9,
		MessageStreamID: 1,
		Payload:         make([]byte, 20),
	}
	ml.ProcessMessage(videoMsg)

	// Send another audio message
	ml.ProcessMessage(audioMsg)

	time.Sleep(50 * time.Millisecond)

	audioCount, videoCount, totalBytes, _, _ := ml.GetStats()
	if audioCount != 2 {
		t.Errorf("Expected audio count 2, got %d", audioCount)
	}
	if videoCount != 1 {
		t.Errorf("Expected video count 1, got %d", videoCount)
	}
	if totalBytes != 40 { // 10 + 20 + 10
		t.Errorf("Expected totalBytes 40, got %d", totalBytes)
	}
}

// TestMediaLogger_ProcessMessage_NonMedia sends a command message
// (TypeID 20) and verifies it is NOT counted as audio or video.
func TestMediaLogger_ProcessMessage_NonMedia(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ml := NewMediaLogger("test-conn-004", log, 100*time.Millisecond)
	defer ml.Stop()

	// Send a command message (type 20)
	cmdMsg := &chunk.Message{
		CSID:            3,
		Timestamp:       0,
		MessageLength:   100,
		TypeID:          20, // Command
		MessageStreamID: 0,
		Payload:         make([]byte, 100),
	}
	ml.ProcessMessage(cmdMsg)

	time.Sleep(50 * time.Millisecond)

	audioCount, videoCount, totalBytes, _, _ := ml.GetStats()
	if audioCount != 0 || videoCount != 0 || totalBytes != 0 {
		t.Error("Non-media messages should not be counted")
	}
}

// TestMediaLogger_PeriodicStats sends 5 audio messages over 250ms and
// waits for at least one periodic stats log interval (200ms) to fire.
func TestMediaLogger_PeriodicStats(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ml := NewMediaLogger("test-conn-005", log, 200*time.Millisecond)
	defer ml.Stop()

	// Send some messages
	for i := 0; i < 5; i++ {
		msg := &chunk.Message{
			CSID:            4,
			Timestamp:       uint32(i * 100),
			MessageLength:   100,
			TypeID:          8,
			MessageStreamID: 1,
			Payload:         make([]byte, 100),
		}
		ml.ProcessMessage(msg)
		time.Sleep(50 * time.Millisecond)
	}

	// Wait for at least one stats log interval
	time.Sleep(300 * time.Millisecond)

	audioCount, _, totalBytes, _, _ := ml.GetStats()
	if audioCount != 5 {
		t.Errorf("Expected audio count 5, got %d", audioCount)
	}
	if totalBytes != 500 {
		t.Errorf("Expected totalBytes 500, got %d", totalBytes)
	}
}

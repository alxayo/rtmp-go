package server

// Media Packet Logger
// -------------------
// Provides observability for incoming media packets (audio/video) with:
//   * Per-connection packet counters (audio/video separate)
//   * Codec detection on first audio/video packets
//   * Periodic stats logging (configurable interval)
//   * Debug-level per-packet logging
//   * Info-level summary logging

import (
	"log/slog"
	"sync"
	"time"

	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
	"github.com/alxayo/go-rtmp/internal/rtmp/media"
)

// MediaLogger tracks and logs media packet statistics for a connection.
type MediaLogger struct {
	connID string
	log    *slog.Logger
	mu     sync.RWMutex

	// Counters
	audioCount uint64
	videoCount uint64
	totalBytes uint64

	// Codec info
	audioCodec string
	videoCodec string

	// Timing
	firstPacketTime time.Time
	lastPacketTime  time.Time

	// Control
	statsInterval time.Duration
	statsTicker   *time.Ticker
	stopChan      chan struct{}
}

// NewMediaLogger creates a new media logger for a connection.
func NewMediaLogger(connID string, logger *slog.Logger, statsInterval time.Duration) *MediaLogger {
	if statsInterval == 0 {
		statsInterval = 30 * time.Second // default: log stats every 30 seconds
	}

	ml := &MediaLogger{
		connID:        connID,
		log:           logger.With("component", "media_logger", "conn_id", connID),
		statsInterval: statsInterval,
		stopChan:      make(chan struct{}),
	}

	// Start periodic stats logging
	ml.statsTicker = time.NewTicker(statsInterval)
	go ml.statsLoop()

	return ml
}

// ProcessMessage analyzes an RTMP message and logs relevant media information.
func (ml *MediaLogger) ProcessMessage(msg *chunk.Message) {
	if msg == nil {
		return
	}

	// Only process audio (8) and video (9) messages
	if msg.TypeID != 8 && msg.TypeID != 9 {
		return
	}

	ml.mu.Lock()
	defer ml.mu.Unlock()

	now := time.Now()
	if ml.firstPacketTime.IsZero() {
		ml.firstPacketTime = now
		ml.log.Info("First media packet received",
			"type", mediaTypeString(msg.TypeID),
			"timestamp", msg.Timestamp)
	}
	ml.lastPacketTime = now

	// Update counters
	ml.totalBytes += uint64(len(msg.Payload))

	if msg.TypeID == 8 {
		ml.audioCount++
		// Detect audio codec on first packet
		if ml.audioCodec == "" && len(msg.Payload) > 0 {
			if am, err := media.ParseAudioMessage(msg.Payload); err == nil {
				ml.audioCodec = am.Codec
				ml.log.Info("Audio codec detected",
					"codec", ml.audioCodec,
					"packet_type", am.PacketType)
			}
		}
	} else if msg.TypeID == 9 {
		ml.videoCount++
		// Detect video codec on first packet
		if ml.videoCodec == "" && len(msg.Payload) > 0 {
			if vm, err := media.ParseVideoMessage(msg.Payload); err == nil {
				ml.videoCodec = vm.Codec
				ml.log.Info("Video codec detected",
					"codec", ml.videoCodec,
					"frame_type", vm.FrameType,
					"packet_type", vm.PacketType)
			}
		}
	}

	// Debug-level per-packet logging
	ml.log.Debug("Media packet",
		"type", mediaTypeString(msg.TypeID),
		"csid", msg.CSID,
		"msid", msg.MessageStreamID,
		"timestamp", msg.Timestamp,
		"length", msg.MessageLength,
		"payload_size", len(msg.Payload))
}

// statsLoop periodically logs aggregated statistics.
func (ml *MediaLogger) statsLoop() {
	for {
		select {
		case <-ml.stopChan:
			return
		case <-ml.statsTicker.C:
			ml.logStats()
		}
	}
}

// logStats logs current statistics at INFO level.
func (ml *MediaLogger) logStats() {
	ml.mu.RLock()
	defer ml.mu.RUnlock()

	// Don't log if no packets received yet
	if ml.audioCount == 0 && ml.videoCount == 0 {
		return
	}

	duration := time.Since(ml.firstPacketTime)
	bitrate := float64(ml.totalBytes*8) / duration.Seconds() / 1000.0 // kbps

	ml.log.Info("Media statistics",
		"audio_packets", ml.audioCount,
		"video_packets", ml.videoCount,
		"total_bytes", ml.totalBytes,
		"bitrate_kbps", int(bitrate),
		"audio_codec", ml.audioCodec,
		"video_codec", ml.videoCodec,
		"duration_sec", int(duration.Seconds()))
}

// Stop halts the periodic stats logging and logs final statistics.
func (ml *MediaLogger) Stop() {
	close(ml.stopChan)
	if ml.statsTicker != nil {
		ml.statsTicker.Stop()
	}

	// Log final stats
	ml.logStats()
}

// GetStats returns current statistics (for testing or external consumers).
func (ml *MediaLogger) GetStats() (audioCount, videoCount, totalBytes uint64, audioCodec, videoCodec string) {
	ml.mu.RLock()
	defer ml.mu.RUnlock()
	return ml.audioCount, ml.videoCount, ml.totalBytes, ml.audioCodec, ml.videoCodec
}

// mediaTypeString converts message type ID to human-readable string.
func mediaTypeString(typeID uint8) string {
	switch typeID {
	case 8:
		return "audio"
	case 9:
		return "video"
	default:
		return "unknown"
	}
}

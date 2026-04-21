package main

// StdinListener reads RTMP hook events from stdin (piped from rtmp-go's stdio
// hook output on stderr). It filters for segment_complete events and submits
// upload jobs directly — no filesystem watching or stabilization delay needed.
//
// Expected input format (one per line):
//   RTMP_EVENT: {"type":"segment_complete","timestamp":1714168200,"conn_id":"abc","stream_key":"live/stream1","data":{...}}

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
)

// HookEvent represents a parsed RTMP hook event from the stdio stream.
type HookEvent struct {
	Type      string                 `json:"type"`
	Timestamp int64                  `json:"timestamp"`
	ConnID    string                 `json:"conn_id"`
	StreamKey string                 `json:"stream_key"`
	Data      map[string]interface{} `json:"data"`
}

// StdinListener reads events from an io.Reader (typically os.Stdin) and
// dispatches segment_complete events to the provided callback.
type StdinListener struct {
	reader   io.Reader
	logger   *slog.Logger
	onSegment func(event HookEvent)
}

// NewStdinListener creates a listener that reads hook events from the given
// reader. The onSegment callback is called for each segment_complete event.
func NewStdinListener(reader io.Reader, logger *slog.Logger, onSegment func(HookEvent)) *StdinListener {
	return &StdinListener{
		reader:    reader,
		logger:    logger,
		onSegment: onSegment,
	}
}

const rtmpEventPrefix = "RTMP_EVENT: "

// Run reads events until ctx is cancelled or the reader is closed.
// It blocks until complete.
func (l *StdinListener) Run(ctx context.Context) error {
	scanner := bufio.NewScanner(l.reader)
	// Allow up to 1MB lines (segment events with long paths)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, rtmpEventPrefix) {
			continue
		}

		jsonStr := strings.TrimPrefix(line, rtmpEventPrefix)
		var event HookEvent
		if err := json.Unmarshal([]byte(jsonStr), &event); err != nil {
			l.logger.Warn("failed to parse hook event", "error", err, "line", truncate(line, 200))
			continue
		}

		switch event.Type {
		case "segment_complete":
			l.logger.Debug("segment_complete event received",
				"stream_key", event.StreamKey,
				"path", event.Data["path"],
			)
			l.onSegment(event)
		case "recording_start":
			l.logger.Info("recording started", "stream_key", event.StreamKey)
		case "recording_stop":
			l.logger.Info("recording stopped", "stream_key", event.StreamKey)
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

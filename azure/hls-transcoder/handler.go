package main

// Handler processes incoming webhook events from rtmp-server's hook system.
// It dispatches publish_start events to start FFmpeg transcoding and
// publish_stop events to stop transcoding for the given stream.

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
)

// HookEvent represents a parsed RTMP hook event from the webhook payload.
// This matches the hooks.Event struct from rtmp-go's hook system.
type HookEvent struct {
	Type      string                 `json:"type"`
	Timestamp int64                  `json:"timestamp"`
	ConnID    string                 `json:"conn_id"`
	StreamKey string                 `json:"stream_key"`
	Data      map[string]interface{} `json:"data"`
}

// Handler routes incoming webhook events to the appropriate transcoder actions.
type Handler struct {
	transcoder *Transcoder
	logger     *slog.Logger
}

// NewHandler creates a handler that dispatches webhook events to the transcoder.
func NewHandler(transcoder *Transcoder, logger *slog.Logger) *Handler {
	return &Handler{
		transcoder: transcoder,
		logger:     logger,
	}
}

// HandleEvent processes a single webhook event HTTP request.
// POST /events — accepts JSON webhook payloads from rtmp-server.
func (h *Handler) HandleEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024)) // 1MB limit
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var event HookEvent
	if err := json.Unmarshal(body, &event); err != nil {
		h.logger.Warn("failed to parse webhook event", "error", err)
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	switch event.Type {
	case "publish_start":
		h.logger.Info("publish_start event received", "stream_key", event.StreamKey)
		if event.StreamKey == "" {
			h.logger.Warn("publish_start event missing stream_key")
			http.Error(w, "missing stream_key", http.StatusBadRequest)
			return
		}
		h.transcoder.Start(event.StreamKey)

	case "publish_stop":
		h.logger.Info("publish_stop event received", "stream_key", event.StreamKey)
		if event.StreamKey == "" {
			h.logger.Warn("publish_stop event missing stream_key")
			http.Error(w, "missing stream_key", http.StatusBadRequest)
			return
		}
		h.transcoder.Stop(event.StreamKey)

	default:
		h.logger.Debug("ignoring event", "type", event.Type, "stream_key", event.StreamKey)
	}

	w.WriteHeader(http.StatusOK)
}

// HandleHealth returns 200 OK for Container Apps liveness/readiness probes.
func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

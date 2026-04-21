package main

// WebhookListener receives RTMP hook events via HTTP POST requests from
// rtmp-go's webhook hook system. This enables push-based segment notifications
// when rtmp-server and blob-sidecar run as separate Container Apps (where
// stdio piping is not possible).
//
// The webhook JSON payload matches the hooks.Event struct from rtmp-go:
//
//	POST /events
//	Content-Type: application/json
//	{"type":"segment_complete","timestamp":1714168200,"conn_id":"abc","stream_key":"live/stream1","data":{"path":"/recordings/seg001.flv",...}}

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

// WebhookListener starts an HTTP server that receives hook events from
// rtmp-go's webhook hook and dispatches segment_complete events to the
// provided callback.
type WebhookListener struct {
	addr      string
	logger    *slog.Logger
	onSegment func(event HookEvent)
	server    *http.Server
	mu        sync.Mutex
}

// NewWebhookListener creates a listener that receives hook events via HTTP.
func NewWebhookListener(addr string, logger *slog.Logger, onSegment func(HookEvent)) *WebhookListener {
	return &WebhookListener{
		addr:      addr,
		logger:    logger,
		onSegment: onSegment,
	}
}

// Run starts the HTTP server and blocks until ctx is cancelled.
func (l *WebhookListener) Run(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/events", l.handleEvent)
	mux.HandleFunc("/health", l.handleHealth)

	l.mu.Lock()
	l.server = &http.Server{
		Addr:         l.addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		BaseContext:  func(_ net.Listener) context.Context { return ctx },
	}
	l.mu.Unlock()

	// Shutdown on context cancellation
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		l.server.Shutdown(shutdownCtx)
	}()

	l.logger.Info("webhook listener starting", "addr", l.addr)

	if err := l.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("webhook listener: %w", err)
	}
	return nil
}

// handleEvent processes incoming hook events.
func (l *WebhookListener) handleEvent(w http.ResponseWriter, r *http.Request) {
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
		l.logger.Warn("webhook: failed to parse event", "error", err)
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	switch event.Type {
	case "segment_complete":
		l.logger.Debug("segment_complete event received via webhook",
			"stream_key", event.StreamKey,
			"path", event.Data["path"],
		)
		l.onSegment(event)
	case "recording_start":
		l.logger.Info("recording started", "stream_key", event.StreamKey)
	case "recording_stop":
		l.logger.Info("recording stopped", "stream_key", event.StreamKey)
	default:
		l.logger.Debug("webhook: ignoring event", "type", event.Type)
	}

	w.WriteHeader(http.StatusOK)
}

// handleHealth returns 200 OK for liveness/readiness probes.
func (l *WebhookListener) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

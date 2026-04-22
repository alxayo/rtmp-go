package main

// WebhookListener starts an HTTP server that receives hook events from
// rtmp-server's webhook hook system. It delegates request handling to the
// Handler and manages the HTTP server lifecycle with graceful shutdown.

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// WebhookListener wraps an HTTP server for receiving webhook events.
type WebhookListener struct {
	addr    string
	handler *Handler
	logger  *slog.Logger
	server  *http.Server
}

// NewWebhookListener creates a listener that serves webhook events on the
// given address. The handler processes /events and /health routes.
func NewWebhookListener(addr string, handler *Handler, logger *slog.Logger) *WebhookListener {
	return &WebhookListener{
		addr:    addr,
		handler: handler,
		logger:  logger,
	}
}

// Run starts the HTTP server and blocks until ctx is cancelled.
// On context cancellation, the server shuts down gracefully with a 5-second
// timeout for in-flight requests.
func (l *WebhookListener) Run(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/events", l.handler.HandleEvent)
	mux.HandleFunc("/health", l.handler.HandleHealth)

	l.server = &http.Server{
		Addr:         l.addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		BaseContext:  func(_ net.Listener) context.Context { return ctx },
	}

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

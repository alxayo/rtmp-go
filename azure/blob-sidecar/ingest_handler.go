package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"
)

// IngestHandler handles HTTP PUT requests for direct segment/playlist uploads.
type IngestHandler struct {
	backend   StorageBackend
	maxBody   int64
	authToken string // optional bearer token
	logger    *slog.Logger
}

// NewIngestHandler creates a new ingest handler.
func NewIngestHandler(backend StorageBackend, maxBody int64, authToken string, logger *slog.Logger) *IngestHandler {
	if maxBody <= 0 {
		maxBody = 50 * 1024 * 1024 // 50MB default
	}
	return &IngestHandler{
		backend:   backend,
		maxBody:   maxBody,
		authToken: authToken,
		logger:    logger,
	}
}

// ServeHTTP handles PUT and GET requests for ingest endpoint.
func (h *IngestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPut:
		h.handlePut(w, r)
	case http.MethodGet:
		// Health check only on /health or /
		if r.URL.Path == "/health" || r.URL.Path == "/" {
			h.handleHealth(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		h.logger.Warn("unsupported method", "method", r.Method, "path", r.URL.Path)
	}
}

// handleHealth responds to GET requests with 200 OK for liveness probes.
func (h *IngestHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/health" && r.URL.Path != "/" {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "OK")
	h.logger.Debug("health check", "remote_addr", r.RemoteAddr)
}

// handlePut processes PUT requests for segment/playlist uploads.
func (h *IngestHandler) handlePut(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Verify auth token if configured
	if h.authToken != "" {
		if !h.verifyToken(r) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			h.logger.Warn("unauthorized upload attempt", "remote_addr", r.RemoteAddr, "path", r.URL.Path)
			return
		}
	}

	// Extract blob path from URL
	blobPath := strings.TrimPrefix(r.URL.Path, "/ingest/")
	if blobPath == "" || blobPath == "/" {
		http.Error(w, "Bad request: missing blob path", http.StatusBadRequest)
		h.logger.Warn("empty blob path", "remote_addr", r.RemoteAddr)
		return
	}

	// Validate path (no traversal, no absolute paths)
	if err := h.validatePath(blobPath); err != nil {
		http.Error(w, fmt.Sprintf("Bad request: %v", err), http.StatusBadRequest)
		h.logger.Warn("invalid path", "remote_addr", r.RemoteAddr, "path", blobPath, "error", err)
		return
	}

	// Check Content-Length header: allow known sizes (>0) and chunked transfer (-1)
	// FFmpeg's HLS HTTP muxer uses chunked transfer encoding (no Content-Length)
	if r.ContentLength == 0 {
		http.Error(w, "Bad request: empty body", http.StatusBadRequest)
		h.logger.Warn("empty content-length", "remote_addr", r.RemoteAddr, "path", blobPath, "content_length", r.ContentLength)
		return
	}

	// Enforce max body size when Content-Length is known
	if r.ContentLength > 0 && r.ContentLength > h.maxBody {
		http.Error(w, fmt.Sprintf("Payload too large: %d > %d", r.ContentLength, h.maxBody), http.StatusRequestEntityTooLarge)
		h.logger.Warn("oversized upload attempt",
			"remote_addr", r.RemoteAddr,
			"path", blobPath,
			"size_bytes", r.ContentLength,
			"max_bytes", h.maxBody)
		return
	}

	// Wrap body reader with size limiter for defense-in-depth
	// This caps both known-size and chunked transfers
	limitedReader := io.LimitReader(r.Body, h.maxBody+1)

	// Call storage backend (synchronous upload)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	err := h.backend.Store(ctx, blobPath, limitedReader, r.ContentLength)
	if err != nil {
		http.Error(w, fmt.Sprintf("Internal server error"), http.StatusInternalServerError)
		h.logger.Error("upload failed",
			"remote_addr", r.RemoteAddr,
			"path", blobPath,
			"size_bytes", r.ContentLength,
			"error", err)
		return
	}

	// Success
	duration := time.Since(start)
	w.WriteHeader(http.StatusCreated)
	h.logger.Info("segment uploaded via HTTP",
		"remote_addr", r.RemoteAddr,
		"path", blobPath,
		"size_bytes", r.ContentLength,
		"duration_ms", duration.Milliseconds())
}

// verifyToken checks Authorization header against configured token.
func (h *IngestHandler) verifyToken(r *http.Request) bool {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return false
	}

	// Expect "Bearer <token>"
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return false
	}

	token := parts[1]
	return token == h.authToken
}

// validatePath checks for path traversal and other invalid patterns.
func (h *IngestHandler) validatePath(blobPath string) error {
	// Normalize path
	blobPath = path.Clean(blobPath)

	// Reject absolute paths
	if strings.HasPrefix(blobPath, "/") {
		return errors.New("absolute paths not allowed")
	}

	// Reject parent directory traversal
	if strings.Contains(blobPath, "..") {
		return errors.New("parent directory traversal (..) not allowed")
	}

	// Reject null bytes
	if strings.Contains(blobPath, "\x00") {
		return errors.New("null bytes not allowed")
	}

	// Must have at least one character
	if len(blobPath) == 0 {
		return errors.New("path cannot be empty")
	}

	return nil
}

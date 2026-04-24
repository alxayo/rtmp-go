package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// MockStorageBackend is a mock for testing.
type MockStorageBackend struct {
	stored  []storedData
	lastErr error
}

type storedData struct {
	blobPath string
	size     int64
	data     []byte
}

func (m *MockStorageBackend) Store(ctx context.Context, blobPath string, reader io.Reader, size int64) error {
	if m.lastErr != nil {
		return m.lastErr
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	m.stored = append(m.stored, storedData{blobPath, size, data})
	return nil
}

// TestIngestHandler_PutSegment tests a successful segment upload.
func TestIngestHandler_PutSegment(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	backend := &MockStorageBackend{}
	handler := NewIngestHandler(backend, 50*1024*1024, "", logger)

	data := []byte("test segment data")
	req := httptest.NewRequest("PUT", "/ingest/hls/test/seg_00001.ts", bytes.NewReader(data))
	req.ContentLength = int64(len(data))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected %d, got %d", http.StatusCreated, w.Code)
	}

	if len(backend.stored) != 1 {
		t.Fatalf("expected 1 stored item, got %d", len(backend.stored))
	}

	if backend.stored[0].blobPath != "hls/test/seg_00001.ts" {
		t.Errorf("expected path 'hls/test/seg_00001.ts', got %q", backend.stored[0].blobPath)
	}

	if !bytes.Equal(backend.stored[0].data, data) {
		t.Errorf("expected data %q, got %q", string(data), string(backend.stored[0].data))
	}
}

// TestIngestHandler_PutPlaylist tests playlist upload.
func TestIngestHandler_PutPlaylist(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	backend := &MockStorageBackend{}
	handler := NewIngestHandler(backend, 50*1024*1024, "", logger)

	playlist := `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:10
#EXTINF:9.9,
seg_00001.ts
#EXT-X-ENDLIST`

	req := httptest.NewRequest("PUT", "/ingest/hls/test/index.m3u8", bytes.NewReader([]byte(playlist)))
	req.ContentLength = int64(len(playlist))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected %d, got %d", http.StatusCreated, w.Code)
	}

	if len(backend.stored) != 1 || backend.stored[0].blobPath != "hls/test/index.m3u8" {
		t.Fatal("playlist not stored correctly")
	}
}

// TestIngestHandler_PathTraversal rejects .. in path.
func TestIngestHandler_PathTraversal(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	backend := &MockStorageBackend{}
	handler := NewIngestHandler(backend, 50*1024*1024, "", logger)

	req := httptest.NewRequest("PUT", "/ingest/../../../etc/passwd", bytes.NewReader([]byte("bad")))
	req.ContentLength = 3

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected %d, got %d", http.StatusBadRequest, w.Code)
	}

	if len(backend.stored) != 0 {
		t.Fatal("path traversal was not blocked")
	}
}

// TestIngestHandler_AbsolutePath rejects absolute paths.
func TestIngestHandler_AbsolutePath(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	backend := &MockStorageBackend{}
	handler := NewIngestHandler(backend, 50*1024*1024, "", logger)

	req := httptest.NewRequest("PUT", "/ingest//etc/passwd", bytes.NewReader([]byte("bad")))
	req.ContentLength = 3

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected %d, got %d", http.StatusBadRequest, w.Code)
	}
}

// TestIngestHandler_OversizedBody rejects bodies larger than maxBody.
func TestIngestHandler_OversizedBody(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	backend := &MockStorageBackend{}
	handler := NewIngestHandler(backend, 1000, "", logger) // 1KB max

	data := make([]byte, 2000)
	req := httptest.NewRequest("PUT", "/ingest/hls/test/huge.ts", bytes.NewReader(data))
	req.ContentLength = 2000

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected %d, got %d", http.StatusRequestEntityTooLarge, w.Code)
	}

	if len(backend.stored) != 0 {
		t.Fatal("oversized upload was not rejected")
	}
}

// TestIngestHandler_MissingContentLength rejects missing header.
func TestIngestHandler_MissingContentLength(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	backend := &MockStorageBackend{}
	handler := NewIngestHandler(backend, 50*1024*1024, "", logger)

	req := httptest.NewRequest("PUT", "/ingest/hls/test/seg.ts", nil)
	// Don't set ContentLength (defaults to -1)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected %d, got %d", http.StatusBadRequest, w.Code)
	}
}

// TestIngestHandler_TokenAuth_Valid accepts valid token.
func TestIngestHandler_TokenAuth_Valid(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	backend := &MockStorageBackend{}
	handler := NewIngestHandler(backend, 50*1024*1024, "secret123", logger)

	data := []byte("test")
	req := httptest.NewRequest("PUT", "/ingest/hls/test/seg.ts", bytes.NewReader(data))
	req.ContentLength = int64(len(data))
	req.Header.Set("Authorization", "Bearer secret123")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected %d, got %d", http.StatusCreated, w.Code)
	}
}

// TestIngestHandler_TokenAuth_Invalid rejects invalid token.
func TestIngestHandler_TokenAuth_Invalid(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	backend := &MockStorageBackend{}
	handler := NewIngestHandler(backend, 50*1024*1024, "secret123", logger)

	req := httptest.NewRequest("PUT", "/ingest/hls/test/seg.ts", bytes.NewReader([]byte("test")))
	req.ContentLength = 4
	req.Header.Set("Authorization", "Bearer wrongtoken")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

// TestIngestHandler_TokenAuth_Missing rejects missing token.
func TestIngestHandler_TokenAuth_Missing(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	backend := &MockStorageBackend{}
	handler := NewIngestHandler(backend, 50*1024*1024, "secret123", logger)

	req := httptest.NewRequest("PUT", "/ingest/hls/test/seg.ts", bytes.NewReader([]byte("test")))
	req.ContentLength = 4
	// No Authorization header

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

// TestIngestHandler_MethodNotAllowed rejects GET.
func TestIngestHandler_MethodNotAllowed(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	backend := &MockStorageBackend{}
	handler := NewIngestHandler(backend, 50*1024*1024, "", logger)

	req := httptest.NewRequest("GET", "/ingest/hls/test/seg.ts", nil)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected %d, got %d", http.StatusMethodNotAllowed, w.Code)
	}
}

// TestIngestHandler_UndersizedSegment rejects .ts < 1KB.
func TestIngestHandler_UndersizedSegment(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	backend := &MockStorageBackend{
		lastErr: fmt.Errorf("segment too small: 512 bytes (minimum 1KB for .ts)"),
	}
	handler := NewIngestHandler(backend, 50*1024*1024, "", logger)

	data := make([]byte, 512)
	req := httptest.NewRequest("PUT", "/ingest/hls/test/seg.ts", bytes.NewReader(data))
	req.ContentLength = 512

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected %d for undersized segment, got %d", http.StatusInternalServerError, w.Code)
	}
}

// TestIngestHandler_Health tests GET /health endpoint.
func TestIngestHandler_Health(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	backend := &MockStorageBackend{}
	handler := NewIngestHandler(backend, 50*1024*1024, "", logger)

	req := httptest.NewRequest("GET", "/health", nil)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected %d, got %d", http.StatusOK, w.Code)
	}

	if w.Body.String() != "OK" {
		t.Errorf("expected 'OK', got %q", w.Body.String())
	}
}

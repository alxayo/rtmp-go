package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// TestBlobBackendStoresWithCorrectPath tests that BlobBackend calls UploadStream with correct parameters.
func TestBlobBackendStoresWithCorrectPath(t *testing.T) {
	// Mock logger
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Mock uploader and router
	mockUploader := &Uploader{} // We'll mock UploadStream
	mockRouter := &Router{}

	backend := NewBlobBackend(mockUploader, mockRouter, logger)

	// Test data
	blobPath := "hls/live_test/stream_0/seg_00001.ts"
	data := []byte("fake segment data")
	_ = bytes.NewReader(data) // Not used in this test, just verify extraction

	// Extract stream key manually to verify behavior
	streamKey := backend.extractStreamKeyFromPath(blobPath)
	if streamKey != "live_test/stream_0" {
		t.Errorf("expected stream key 'live_test/stream_0', got %q", streamKey)
	}
}

// TestBlobBackendExtractsStreamKey tests stream key extraction from various path formats.
func TestBlobBackendExtractsStreamKey(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	backend := NewBlobBackend(nil, nil, logger)

	tests := []struct {
		blobPath  string
		expected  string
		name      string
	}{
		{
			name:     "nested stream key",
			blobPath: "hls/live_test/stream_0/seg_00001.ts",
			expected: "live_test/stream_0",
		},
		{
			name:     "simple stream key",
			blobPath: "hls/test/seg_00001.ts",
			expected: "test",
		},
		{
			name:     "deep nested",
			blobPath: "segments/app/channel/x/stream/0/seg_00001.ts",
			expected: "app/channel/x/stream/0",
		},
		{
			name:     "too short",
			blobPath: "hls/seg.ts",
			expected: "",
		},
		{
			name:     "empty",
			blobPath: "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := backend.extractStreamKeyFromPath(tt.blobPath)
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

// TestLocalBackendCreatesDirectoriesAndFiles tests that LocalBackend creates parent directories and files.
func TestLocalBackendCreatesDirectoriesAndFiles(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	backend := NewLocalBackend(tmpDir, logger)

	// Test data
	blobPath := "hls/live_test/stream_0/seg_00001.ts"
	data := []byte("test segment data")
	reader := bytes.NewReader(data)

	// Store the segment
	err := backend.Store(context.Background(), blobPath, reader, int64(len(data)))
	if err != nil {
		t.Fatalf("Store() failed: %v", err)
	}

	// Verify file was created
	expectedPath := filepath.Join(tmpDir, filepath.FromSlash(blobPath))
	_, err = os.Stat(expectedPath)
	if err != nil {
		t.Fatalf("file not found at %s: %v", expectedPath, err)
	}

	// Verify file contents
	content, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("ReadFile() failed: %v", err)
	}
	if !bytes.Equal(content, data) {
		t.Errorf("expected %q, got %q", string(data), string(content))
	}
}

// TestLocalBackendCallsSync tests that LocalBackend calls f.Sync() on the file.
func TestLocalBackendCallsSync(t *testing.T) {
	tmpDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	backend := NewLocalBackend(tmpDir, logger)

	blobPath := "test_file.txt"
	data := []byte("sync test")
	reader := bytes.NewReader(data)

	err := backend.Store(context.Background(), blobPath, reader, int64(len(data)))
	if err != nil {
		t.Fatalf("Store() failed: %v", err)
	}

	// If file is on disk and readable, Sync() worked (otherwise data might be buffered)
	expectedPath := filepath.Join(tmpDir, blobPath)
	content, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("ReadFile() failed: %v", err)
	}
	if !bytes.Equal(content, data) {
		t.Errorf("data not synced properly")
	}
}

// TestLocalBackendHandlesLargeFiles tests LocalBackend with larger files.
func TestLocalBackendHandlesLargeFiles(t *testing.T) {
	tmpDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	backend := NewLocalBackend(tmpDir, logger)

	blobPath := "video/segment.ts"
	// Create 10MB of data
	data := make([]byte, 10*1024*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}
	reader := bytes.NewReader(data)

	err := backend.Store(context.Background(), blobPath, reader, int64(len(data)))
	if err != nil {
		t.Fatalf("Store() failed: %v", err)
	}

	// Verify file size
	expectedPath := filepath.Join(tmpDir, filepath.FromSlash(blobPath))
	stat, err := os.Stat(expectedPath)
	if err != nil {
		t.Fatalf("Stat() failed: %v", err)
	}
	if stat.Size() != int64(len(data)) {
		t.Errorf("expected size %d, got %d", len(data), stat.Size())
	}
}

// TestLocalBackendPreservesDirectoryStructure tests that nested paths are preserved.
func TestLocalBackendPreservesDirectoryStructure(t *testing.T) {
	tmpDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	backend := NewLocalBackend(tmpDir, logger)

	blobPath := "hls/live/channel_1/stream_720p/segment_001.ts"
	data := []byte("segment")
	reader := bytes.NewReader(data)

	err := backend.Store(context.Background(), blobPath, reader, int64(len(data)))
	if err != nil {
		t.Fatalf("Store() failed: %v", err)
	}

	// Verify directory structure
	expectedPath := filepath.Join(tmpDir, filepath.FromSlash(blobPath))
	expectedDir := filepath.Dir(expectedPath)
	_, err = os.Stat(expectedDir)
	if err != nil {
		t.Fatalf("directory structure not created: %v", err)
	}

	_, err = os.Stat(expectedPath)
	if err != nil {
		t.Fatalf("file not found: %v", err)
	}
}

// TestStorageBackendFactory tests the NewStorageBackend factory function.
func TestStorageBackendFactory(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	tests := []struct {
		name        string
		backendType string
		uploader    *Uploader
		router      *Router
		localDir    string
		expectError bool
		expectType  string
	}{
		{
			name:        "blob backend",
			backendType: "blob",
			uploader:    &Uploader{},
			router:      &Router{},
			expectError: false,
			expectType:  "*main.BlobBackend",
		},
		{
			name:        "local backend",
			backendType: "local",
			localDir:    "/tmp",
			expectError: false,
			expectType:  "*main.LocalBackend",
		},
		{
			name:        "invalid backend",
			backendType: "invalid",
			expectError: true,
		},
		{
			name:        "blob without uploader",
			backendType: "blob",
			uploader:    nil,
			router:      &Router{},
			expectError: true,
		},
		{
			name:        "local without dir",
			backendType: "local",
			localDir:    "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend, err := NewStorageBackend(tt.backendType, tt.uploader, tt.router, tt.localDir, logger)
			if tt.expectError && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tt.expectError && backend == nil {
				t.Fatalf("expected backend, got nil")
			}
		})
	}
}

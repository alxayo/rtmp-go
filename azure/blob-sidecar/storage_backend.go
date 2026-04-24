package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// StorageBackend defines the interface for storing segments (blob or local filesystem).
type StorageBackend interface {
	// Store writes data to storage at the given blobPath.
	// ctx: context for cancellation and timeouts
	// blobPath: destination path (e.g., "hls/live_test/stream_0/seg_00001.ts")
	// reader: data source
	// size: content length in bytes
	// Returns error on failure.
	Store(ctx context.Context, blobPath string, reader io.Reader, size int64) error
}

// NewStorageBackend creates a StorageBackend based on the backend type and configuration.
func NewStorageBackend(backendType string, uploader *Uploader, router *Router, localDir string, logger *slog.Logger) (StorageBackend, error) {
	switch backendType {
	case "blob":
		if uploader == nil || router == nil {
			return nil, fmt.Errorf("blob backend requires uploader and router")
		}
		return NewBlobBackend(uploader, router, logger), nil
	case "local":
		if localDir == "" {
			return nil, fmt.Errorf("local backend requires -ingest-local-dir to be set")
		}
		return NewLocalBackend(localDir, logger), nil
	default:
		return nil, fmt.Errorf("unknown storage backend: %s", backendType)
	}
}

// BlobBackend uploads segments to Azure Blob Storage.
type BlobBackend struct {
	uploader *Uploader
	router   *Router
	logger   *slog.Logger
}

// NewBlobBackend creates a new BlobBackend.
func NewBlobBackend(uploader *Uploader, router *Router, logger *slog.Logger) *BlobBackend {
	return &BlobBackend{
		uploader: uploader,
		router:   router,
		logger:   logger,
	}
}

// Store uploads a segment to Azure Blob Storage.
// blobPath is expected to be in format: "{prefix}/{streamKey}/{filename}"
// The router extracts streamKey and resolves to a tenant's storage account.
func (b *BlobBackend) Store(ctx context.Context, blobPath string, reader io.Reader, size int64) error {
	// Extract stream key from blob path (e.g., "hls/live_test/stream_0/seg_00001.ts" -> "live_test/stream_0")
	streamKey := b.extractStreamKeyFromPath(blobPath)
	if streamKey == "" {
		return fmt.Errorf("blob backend: could not extract stream key from blob path: %s", blobPath)
	}

	// Resolve tenant using router
	tenant, err := b.router.ResolveByStreamKey(streamKey)
	if err != nil {
		return fmt.Errorf("blob backend: failed to resolve tenant for stream key %s: %w", streamKey, err)
	}

	// Extract filename from blob path
	filename := filepath.Base(blobPath)

	b.logger.Debug("uploading to blob storage",
		"blob_path", blobPath,
		"stream_key", streamKey,
		"filename", filename,
		"storage_account", tenant.StorageAccount,
		"size_bytes", size,
	)

	// Call uploader's UploadStream method
	if err := b.uploader.UploadStream(ctx, tenant, streamKey, filename, reader, size); err != nil {
		return fmt.Errorf("blob backend: upload failed: %w", err)
	}

	return nil
}

// extractStreamKeyFromPath extracts the stream key portion from a blob path.
// Example: "hls/live_test/stream_0/seg_00001.ts" -> "live_test/stream_0"
// This is a heuristic that works for common directory structures.
func (b *BlobBackend) extractStreamKeyFromPath(blobPath string) string {
	// Normalize path
	blobPath = filepath.ToSlash(blobPath)

	// Split path into components
	parts := strings.Split(strings.Trim(blobPath, "/"), "/")
	if len(parts) < 3 {
		// Too short to extract meaningful stream key
		// Pattern: prefix/streamkey/filename requires at least 3 parts
		return ""
	}

	// Typical pattern: "prefix/streamkey_part1/streamkey_part2/filename.ts"
	// Return all but the first component (prefix) and last component (filename)
	// This works for nested stream keys like "live_test/stream_0"
	if len(parts) > 2 {
		return strings.Join(parts[1:len(parts)-1], "/")
	}

	return ""
}

// LocalBackend writes segments to the local filesystem.
type LocalBackend struct {
	rootDir string
	logger  *slog.Logger
}

// NewLocalBackend creates a new LocalBackend.
func NewLocalBackend(rootDir string, logger *slog.Logger) *LocalBackend {
	return &LocalBackend{
		rootDir: rootDir,
		logger:  logger,
	}
}

// Store writes a segment to the local filesystem.
// blobPath is written relative to rootDir, preserving directory hierarchy.
func (l *LocalBackend) Store(ctx context.Context, blobPath string, reader io.Reader, size int64) error {
	// Construct full path
	fullPath := filepath.Join(l.rootDir, filepath.FromSlash(blobPath))

	// Create parent directories
	dirPath := filepath.Dir(fullPath)
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return fmt.Errorf("local backend: failed to create directory %s: %w", dirPath, err)
	}

	l.logger.Debug("writing to local filesystem",
		"blob_path", blobPath,
		"full_path", fullPath,
		"size_bytes", size,
	)

	// Open file for writing
	f, err := os.Create(fullPath)
	if err != nil {
		return fmt.Errorf("local backend: failed to create file %s: %w", fullPath, err)
	}
	defer f.Close()

	// Copy data — use CopyN when size is known, Copy for chunked transfers
	var written int64
	if size > 0 {
		written, err = io.CopyN(f, reader, size)
		if err != nil && err != io.EOF {
			return fmt.Errorf("local backend: failed to write to file %s: %w", fullPath, err)
		}
		if written != size {
			return fmt.Errorf("local backend: wrote %d bytes but expected %d for %s", written, size, fullPath)
		}
	} else {
		// Chunked transfer: Content-Length unknown (-1)
		written, err = io.Copy(f, reader)
		if err != nil {
			return fmt.Errorf("local backend: failed to write to file %s: %w", fullPath, err)
		}
	}

	// Explicit sync to disk
	if err := f.Sync(); err != nil {
		return fmt.Errorf("local backend: failed to sync file %s: %w", fullPath, err)
	}

	l.logger.Debug("successfully wrote local file",
		"blob_path", blobPath,
		"full_path", fullPath,
		"bytes_written", written,
	)

	return nil
}

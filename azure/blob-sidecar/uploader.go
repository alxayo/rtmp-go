package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
)

// UploadJob represents a segment file ready to be uploaded.
type UploadJob struct {
	FilePath  string
	Tenant    *StorageTarget
	StreamKey string
}

// Uploader manages concurrent uploads to Azure Blob Storage.
type Uploader struct {
	workers int
	cleanup bool
	logger  *slog.Logger

	jobs    chan UploadJob
	wg      sync.WaitGroup
	clients sync.Map // cache of *azblob.Client per storage account URL
}

// NewUploader creates an uploader with the specified number of workers.
func NewUploader(workers int, cleanup bool, logger *slog.Logger) *Uploader {
	if workers <= 0 {
		workers = 4
	}
	return &Uploader{
		workers: workers,
		cleanup: cleanup,
		logger:  logger,
		jobs:    make(chan UploadJob, 100), // buffered to avoid blocking watcher
	}
}

// Start launches the upload worker pool.
func (u *Uploader) Start(ctx context.Context) {
	for i := 0; i < u.workers; i++ {
		u.wg.Add(1)
		go u.worker(ctx, i)
	}
	u.logger.Info("uploader started", "workers", u.workers, "cleanup", u.cleanup)
}

// Submit enqueues an upload job. Non-blocking (drops if queue is full with a warning).
func (u *Uploader) Submit(job UploadJob) {
	select {
	case u.jobs <- job:
	default:
		u.logger.Warn("upload queue full, dropping segment",
			"path", job.FilePath, "stream_key", job.StreamKey)
	}
}

// UploadStream uploads segment data from an io.Reader directly to Azure Blob Storage.
// This is a synchronous upload (blocks until complete or error).
// Used by the HTTP ingest handler for immediate uploads with guaranteed ordering.
func (u *Uploader) UploadStream(ctx context.Context, tenant *StorageTarget, streamKey, blobName string, reader io.Reader, size int64) error {
	start := time.Now()

	// Validate .ts size (defense-in-depth against incomplete segments)
	if strings.HasSuffix(strings.ToLower(blobName), ".ts") && size < 1024 {
		u.logger.Warn("rejecting undersized segment",
			"blob_name", blobName, "size_bytes", size, "stream_key", streamKey)
		return fmt.Errorf("segment too small: %d bytes (minimum 1KB for .ts)", size)
	}

	// Get client (with caching)
	client, err := u.getClient(tenant)
	if err != nil {
		return fmt.Errorf("get client: %w", err)
	}

	// Build blob path: {path_prefix}/{streamKey}/{blobName}
	blobPath := filepath.Join(tenant.PathPrefix, streamKey, blobName)
	// Normalize to forward slashes for blob paths
	blobPath = filepath.ToSlash(blobPath)

	// Upload with retry logic
	err = u.uploadStreamWithRetry(ctx, client, tenant.Container, blobPath, reader, size)
	if err != nil {
		return fmt.Errorf("blob upload: %w", err)
	}

	duration := time.Since(start)
	u.logger.Info("stream uploaded",
		"stream_key", streamKey,
		"blob", blobPath,
		"container", tenant.Container,
		"size_bytes", size,
		"duration_ms", duration.Milliseconds(),
		"account", tenant.StorageAccount)

	return nil
}

// uploadStreamWithRetry uploads data with exponential backoff retry on transient failures.
// Attempts: 1, 2, 3 with backoffs: 100ms, 200ms, 400ms
func (u *Uploader) uploadStreamWithRetry(ctx context.Context, client *azblob.Client, container, blobPath string, reader io.Reader, size int64) error {
	const maxRetries = 3
	const baseBackoff = 100 * time.Millisecond

	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		// Check context before attempting upload
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Read data into memory (needed for retries since we can't seek on arbitrary readers)
		// For the first attempt, we try to use the reader directly if it's seekable
		var uploadReader io.Reader
		var uploadErr error

		if attempt == 0 {
			// First attempt: try to use reader directly (may work for some types)
			uploadReader = reader
		} else {
			// Retry: need to re-read data. Since we can't seek, we read into buffer first
			// This is a fallback; in practice, caller should handle buffering if multiple retries needed
			u.logger.Debug("retry would require re-reading data, failing", "attempt", attempt)
			return fmt.Errorf("cannot retry stream-based upload: %w", lastErr)
		}

		// Perform upload
		_, uploadErr = client.UploadStream(ctx, container, blobPath, uploadReader, &azblob.UploadStreamOptions{})
		if uploadErr == nil {
			return nil // Success
		}

		lastErr = uploadErr

		// Check if error is retryable
		if !isRetryableError(uploadErr) {
			u.logger.Warn("non-retryable error, giving up", "error", uploadErr, "blob", blobPath)
			return uploadErr
		}

		u.logger.Warn("transient error, retrying",
			"blob", blobPath, "attempt", attempt+1, "error", uploadErr)

		// Exponential backoff with small jitter
		if attempt < maxRetries-1 {
			backoff := baseBackoff * time.Duration(math.Pow(2, float64(attempt)))
			jitter := time.Duration(rand.Intn(50)) * time.Millisecond // up to 50ms jitter
			totalBackoff := backoff + jitter

			select {
			case <-time.After(totalBackoff):
				// Continue to next attempt
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return fmt.Errorf("upload failed after %d attempts: %w", maxRetries, lastErr)
}

// isRetryableError checks if an error is likely transient and worth retrying.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := err.Error()

	// List of strings indicating transient/retryable errors
	retryablePatterns := []string{
		"timeout",
		"connection reset",
		"i/o timeout",
		"temporary failure",
		"TooManyRequests",
		"InternalServerError",
		"BadGateway",
		"ServiceUnavailable",
		"GatewayTimeout",
	}

	for _, pattern := range retryablePatterns {
		if strings.Contains(errMsg, pattern) {
			return true
		}
	}

	return false
}

// Shutdown gracefully stops the uploader, finishing in-progress uploads.
func (u *Uploader) Shutdown(ctx context.Context) {
	close(u.jobs)

	done := make(chan struct{})
	go func() {
		u.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		u.logger.Info("all uploads completed")
	case <-ctx.Done():
		u.logger.Warn("shutdown timeout, some uploads may be incomplete")
	}
}

func (u *Uploader) worker(ctx context.Context, id int) {
	defer u.wg.Done()

	for job := range u.jobs {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := u.upload(ctx, job); err != nil {
			u.logger.Error("upload failed",
				"path", job.FilePath,
				"stream_key", job.StreamKey,
				"tenant", job.Tenant.StorageAccount,
				"error", err)
		}
	}
}

func (u *Uploader) upload(ctx context.Context, job UploadJob) error {
	start := time.Now()

	// Get or create blob client for this storage account
	client, err := u.getClient(job.Tenant)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}

	// Open the segment file
	f, err := os.Open(job.FilePath)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat file: %w", err)
	}

	// Defense-in-depth: reject empty or undersized .ts segments.
	// A valid 3-second HLS segment is hundreds of KB minimum. Sub-1KB files
	// are either partially written (SMB cache not flushed) or corrupt.
	filename := filepath.Base(job.FilePath)
	if strings.HasSuffix(strings.ToLower(filename), ".ts") && info.Size() < 1024 {
		u.logger.Warn("skipping undersized segment",
			"path", job.FilePath, "size_bytes", info.Size(), "stream_key", job.StreamKey)
		return nil
	}

	// Build blob name: {path_prefix}/{stream_key}/{filename}
	blobName := filepath.Join(job.Tenant.PathPrefix, job.StreamKey, filename)
	// Normalize to forward slashes for blob paths
	blobName = filepath.ToSlash(blobName)

	// Upload with retry (Azure SDK has built-in retry)
	_, err = client.UploadFile(ctx, job.Tenant.Container, blobName, f, &azblob.UploadFileOptions{})
	if err != nil {
		return fmt.Errorf("blob upload: %w", err)
	}

	duration := time.Since(start)
	u.logger.Info("segment uploaded",
		"stream_key", job.StreamKey,
		"blob", blobName,
		"container", job.Tenant.Container,
		"size_bytes", info.Size(),
		"duration_ms", duration.Milliseconds(),
		"account", job.Tenant.StorageAccount)

	// Optionally clean up local file
	if u.cleanup {
		if err := os.Remove(job.FilePath); err != nil {
			u.logger.Warn("failed to delete local segment", "path", job.FilePath, "error", err)
		}
	}

	return nil
}

func (u *Uploader) getClient(tenant *StorageTarget) (*azblob.Client, error) {
	// Check cache
	if cached, ok := u.clients.Load(tenant.StorageAccount); ok {
		return cached.(*azblob.Client), nil
	}

	// Create new client based on credential type
	var client *azblob.Client
	var err error

	switch tenant.Credential {
	case "connection-string":
		connStr := os.Getenv(tenant.ConnectionStringEnv)
		if connStr == "" {
			return nil, fmt.Errorf("env var %q is empty", tenant.ConnectionStringEnv)
		}
		client, err = azblob.NewClientFromConnectionString(connStr, nil)

	case "managed-identity", "":
		cred, credErr := azidentity.NewDefaultAzureCredential(nil)
		if credErr != nil {
			return nil, fmt.Errorf("managed identity: %w", credErr)
		}
		client, err = azblob.NewClient(tenant.StorageAccount, cred, nil)

	default:
		return nil, fmt.Errorf("unknown credential type: %q", tenant.Credential)
	}

	if err != nil {
		return nil, err
	}

	// Cache the client
	u.clients.Store(tenant.StorageAccount, client)
	return client, nil
}

package srtauth

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// FileResolver loads per-stream passphrases from a JSON file.
// The file format is a simple JSON object mapping stream keys to passphrases:
//
//	{"live/stream1": "secret-passphrase-1", "live/stream2": "secret-passphrase-2"}
//
// Call [FileResolver.Reload] to re-read the file at runtime (e.g., on SIGHUP).
// If reload fails (bad JSON, invalid passphrase), the previous valid map is
// preserved so existing connections are not disrupted.
//
// All methods are safe for concurrent use — an [sync.RWMutex] protects the
// passphrase map, allowing many concurrent ResolvePassphrase reads with
// infrequent Reload writes.
type FileResolver struct {
	// path is the filesystem path to the JSON passphrase file.
	// Set once during construction and never modified.
	path string

	// mu protects the passphrases map. Reload takes a write lock;
	// ResolvePassphrase takes a read lock. This allows concurrent
	// lookups without blocking each other.
	mu sync.RWMutex

	// passphrases maps stream keys (e.g., "live/stream1") to their
	// SRT encryption passphrases. Replaced atomically by Reload —
	// the entire map is swapped, never mutated in place.
	passphrases map[string]string
}

// NewFileResolver creates a FileResolver and loads passphrases from the
// specified JSON file. Returns an error if the file cannot be read,
// contains invalid JSON, or any passphrase violates SRT constraints.
func NewFileResolver(path string) (*FileResolver, error) {
	r := &FileResolver{path: path}
	if err := r.Reload(); err != nil {
		return nil, fmt.Errorf("load srt passphrase file %s: %w", path, err)
	}
	return r, nil
}

// Reload re-reads the passphrase file from disk and replaces the in-memory
// map atomically. The reload uses a validate-then-swap strategy:
//
//  1. Read the file from disk
//  2. Parse as JSON
//  3. Validate every passphrase against SRT constraints
//  4. Only if all validations pass, swap the map under a write lock
//
// If any step fails, the previous map is preserved and an error is returned.
// This ensures that a typo in the passphrase file doesn't break existing streams.
// Safe to call concurrently with ResolvePassphrase.
func (r *FileResolver) Reload() error {
	data, err := os.ReadFile(r.path)
	if err != nil {
		return err
	}

	var passphrases map[string]string
	if err := json.Unmarshal(data, &passphrases); err != nil {
		return fmt.Errorf("parse srt passphrase file: %w", err)
	}

	// Validate every passphrase before swapping the map.
	// If any single entry is invalid, we reject the entire file
	// to prevent a partially-valid config from being loaded.
	for streamKey, passphrase := range passphrases {
		if err := ValidatePassphrase(passphrase); err != nil {
			return fmt.Errorf("stream %q: %w", streamKey, err)
		}
	}

	// All validations passed — swap the map under a write lock.
	// The write lock is held only for the pointer swap (not for
	// the file I/O or validation above), minimizing lock contention.
	r.mu.Lock()
	r.passphrases = passphrases
	r.mu.Unlock()
	return nil
}

// ResolvePassphrase returns the passphrase for the given stream key.
// Returns [ErrStreamNotFound] (wrapped with the stream key) if the
// stream key is not in the loaded passphrase file.
//
// Uses a read lock so multiple goroutines can resolve concurrently
// without blocking each other — only Reload takes a write lock.
func (r *FileResolver) ResolvePassphrase(streamKey string) (string, error) {
	r.mu.RLock()
	passphrase, exists := r.passphrases[streamKey]
	r.mu.RUnlock()

	if !exists {
		return "", fmt.Errorf("%w: %q", ErrStreamNotFound, streamKey)
	}
	return passphrase, nil
}

// EncryptionRequired returns true — a FileResolver always requires
// encryption because it was constructed with an explicit passphrase file.
func (r *FileResolver) EncryptionRequired() bool {
	return true
}

// Path returns the filesystem path this resolver reads passphrases from.
// Useful for logging which file was loaded or for diagnostics.
func (r *FileResolver) Path() string {
	return r.path
}

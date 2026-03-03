package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// FileValidator loads tokens from a JSON file and validates requests
// against them. The file format is a simple JSON object mapping stream
// keys to their expected tokens:
//
//	{"live/stream1": "secret123", "live/stream2": "abc456"}
//
// Call [FileValidator.Reload] to re-read the file at runtime (e.g. on
// SIGHUP). All methods are safe for concurrent use.
type FileValidator struct {
	path   string
	mu     sync.RWMutex
	tokens map[string]string
}

// NewFileValidator creates a FileValidator and loads tokens from the
// specified JSON file. Returns an error if the file cannot be read or
// contains invalid JSON.
func NewFileValidator(path string) (*FileValidator, error) {
	v := &FileValidator{path: path}
	if err := v.Reload(); err != nil {
		return nil, fmt.Errorf("load auth file %s: %w", path, err)
	}
	return v, nil
}

// Reload re-reads the token file from disk and replaces the in-memory
// token map. Safe to call concurrently with validation requests.
func (v *FileValidator) Reload() error {
	data, err := os.ReadFile(v.path)
	if err != nil {
		return err
	}
	var tokens map[string]string
	if err := json.Unmarshal(data, &tokens); err != nil {
		return fmt.Errorf("parse auth file: %w", err)
	}
	v.mu.Lock()
	v.tokens = tokens
	v.mu.Unlock()
	return nil
}

// ValidatePublish checks the token for a publish request.
func (v *FileValidator) ValidatePublish(_ context.Context, req *Request) error {
	return v.validate(req)
}

// ValidatePlay checks the token for a play (subscribe) request.
func (v *FileValidator) ValidatePlay(_ context.Context, req *Request) error {
	return v.validate(req)
}

// validate performs the actual token comparison under a read lock.
func (v *FileValidator) validate(req *Request) error {
	token := req.QueryParams["token"]
	if token == "" {
		return ErrTokenMissing
	}
	v.mu.RLock()
	expected, exists := v.tokens[req.StreamKey]
	v.mu.RUnlock()
	if !exists || token != expected {
		return ErrUnauthorized
	}
	return nil
}

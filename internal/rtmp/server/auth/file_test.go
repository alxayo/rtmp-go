package auth

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeTokenFile is a test helper that writes a JSON token map to a
// temporary file and returns its path.
func writeTokenFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.json")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	return path
}

// TestFileValidator_ValidToken loads a token file and validates a
// correct token.
func TestFileValidator_ValidToken(t *testing.T) {
	path := writeTokenFile(t, `{"live/stream1": "secret123"}`)
	v, err := NewFileValidator(path)
	if err != nil {
		t.Fatalf("NewFileValidator: %v", err)
	}

	err = v.ValidatePublish(context.Background(), &Request{
		StreamKey:   "live/stream1",
		QueryParams: map[string]string{"token": "secret123"},
	})
	if err != nil {
		t.Fatalf("expected valid token accepted, got %v", err)
	}
}

// TestFileValidator_WrongToken loads a token file and rejects a wrong token.
func TestFileValidator_WrongToken(t *testing.T) {
	path := writeTokenFile(t, `{"live/stream1": "secret123"}`)
	v, err := NewFileValidator(path)
	if err != nil {
		t.Fatalf("NewFileValidator: %v", err)
	}

	err = v.ValidatePublish(context.Background(), &Request{
		StreamKey:   "live/stream1",
		QueryParams: map[string]string{"token": "wrong"},
	})
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

// TestFileValidator_MissingToken expects ErrTokenMissing when no token
// is provided.
func TestFileValidator_MissingToken(t *testing.T) {
	path := writeTokenFile(t, `{"live/stream1": "secret123"}`)
	v, err := NewFileValidator(path)
	if err != nil {
		t.Fatalf("NewFileValidator: %v", err)
	}

	err = v.ValidatePublish(context.Background(), &Request{
		StreamKey:   "live/stream1",
		QueryParams: map[string]string{},
	})
	if !errors.Is(err, ErrTokenMissing) {
		t.Fatalf("expected ErrTokenMissing, got %v", err)
	}
}

// TestFileValidator_InvalidJSON verifies the constructor fails on
// malformed JSON.
func TestFileValidator_InvalidJSON(t *testing.T) {
	path := writeTokenFile(t, `not json`)
	_, err := NewFileValidator(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// TestFileValidator_MissingFile verifies the constructor fails when the
// file doesn't exist.
func TestFileValidator_MissingFile(t *testing.T) {
	_, err := NewFileValidator("/nonexistent/tokens.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// TestFileValidator_Reload verifies that Reload replaces the token map:
// old token is rejected, new token is accepted.
func TestFileValidator_Reload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.json")

	// Write initial tokens
	if err := os.WriteFile(path, []byte(`{"live/s1": "old_token"}`), 0644); err != nil {
		t.Fatal(err)
	}
	v, err := NewFileValidator(path)
	if err != nil {
		t.Fatalf("NewFileValidator: %v", err)
	}

	// Initial token works
	err = v.ValidatePublish(context.Background(), &Request{
		StreamKey:   "live/s1",
		QueryParams: map[string]string{"token": "old_token"},
	})
	if err != nil {
		t.Fatalf("old token should work: %v", err)
	}

	// Update the file with a new token
	if err := os.WriteFile(path, []byte(`{"live/s1": "new_token"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := v.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	// Old token rejected
	err = v.ValidatePublish(context.Background(), &Request{
		StreamKey:   "live/s1",
		QueryParams: map[string]string{"token": "old_token"},
	})
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("old token should be rejected after reload, got %v", err)
	}

	// New token accepted
	err = v.ValidatePublish(context.Background(), &Request{
		StreamKey:   "live/s1",
		QueryParams: map[string]string{"token": "new_token"},
	})
	if err != nil {
		t.Fatalf("new token should work after reload: %v", err)
	}
}

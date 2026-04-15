package srtauth

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// --- StaticResolver tests ---

// TestStaticResolver_ResolvePassphrase verifies that a StaticResolver returns
// the same passphrase for any stream key, including empty strings and arbitrary
// values. This confirms the "one passphrase for all streams" contract.
func TestStaticResolver_ResolvePassphrase(t *testing.T) {
	r, err := NewStaticResolver("my-secret-pass")
	if err != nil {
		t.Fatal(err)
	}

	// Returns the same passphrase for any stream key.
	for _, key := range []string{"live/stream1", "live/stream2", "", "anything"} {
		got, err := r.ResolvePassphrase(key)
		if err != nil {
			t.Errorf("ResolvePassphrase(%q) error: %v", key, err)
		}
		if got != "my-secret-pass" {
			t.Errorf("ResolvePassphrase(%q) = %q, want %q", key, got, "my-secret-pass")
		}
	}
}

// TestStaticResolver_EncryptionRequired verifies that a StaticResolver always
// reports encryption as required, since it was constructed with an explicit passphrase.
func TestStaticResolver_EncryptionRequired(t *testing.T) {
	r, _ := NewStaticResolver("my-secret-pass")
	if !r.EncryptionRequired() {
		t.Error("StaticResolver.EncryptionRequired() = false, want true")
	}
}

// TestNewStaticResolver_TooShort verifies that construction fails when the
// passphrase is shorter than the SRT minimum of 10 characters.
func TestNewStaticResolver_TooShort(t *testing.T) {
	_, err := NewStaticResolver("short")
	if err == nil {
		t.Fatal("expected error for short passphrase")
	}
}

// TestNewStaticResolver_TooLong verifies that construction fails when the
// passphrase exceeds the SRT maximum of 79 characters.
func TestNewStaticResolver_TooLong(t *testing.T) {
	long := string(make([]byte, 80))
	for i := range long {
		long = long[:i] + "a" + long[i+1:]
	}
	_, err := NewStaticResolver(long)
	if err == nil {
		t.Fatal("expected error for long passphrase")
	}
}

// --- FileResolver tests ---

// writePassphraseFile is a test helper that writes a JSON passphrase file
// to the given directory and returns its path. The content should be a
// valid (or intentionally invalid) JSON string for testing.
func writePassphraseFile(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "passphrases.json")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestFileResolver_LoadAndResolve verifies the happy path: load a valid JSON
// passphrase file with multiple streams and resolve each one correctly.
func TestFileResolver_LoadAndResolve(t *testing.T) {
	dir := t.TempDir()
	path := writePassphraseFile(t, dir, `{
		"live/stream1": "passphrase-for-stream-one",
		"live/stream2": "passphrase-for-stream-two"
	}`)

	r, err := NewFileResolver(path)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		key  string
		want string
	}{
		{"live/stream1", "passphrase-for-stream-one"},
		{"live/stream2", "passphrase-for-stream-two"},
	}

	for _, tt := range tests {
		got, err := r.ResolvePassphrase(tt.key)
		if err != nil {
			t.Errorf("ResolvePassphrase(%q) error: %v", tt.key, err)
		}
		if got != tt.want {
			t.Errorf("ResolvePassphrase(%q) = %q, want %q", tt.key, got, tt.want)
		}
	}
}

// TestFileResolver_StreamNotFound verifies that resolving an unknown stream key
// returns ErrStreamNotFound, which callers can check with errors.Is().
func TestFileResolver_StreamNotFound(t *testing.T) {
	dir := t.TempDir()
	path := writePassphraseFile(t, dir, `{"live/known": "valid-passphrase"}`)

	r, err := NewFileResolver(path)
	if err != nil {
		t.Fatal(err)
	}

	_, err = r.ResolvePassphrase("live/unknown")
	if !errors.Is(err, ErrStreamNotFound) {
		t.Errorf("expected ErrStreamNotFound, got: %v", err)
	}
}

// TestFileResolver_InvalidJSON verifies that construction fails gracefully
// when the passphrase file contains malformed JSON.
func TestFileResolver_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := writePassphraseFile(t, dir, `not valid json`)

	_, err := NewFileResolver(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// TestFileResolver_PassphraseTooShort verifies that construction fails when
// any passphrase in the file is shorter than the SRT minimum of 10 characters.
func TestFileResolver_PassphraseTooShort(t *testing.T) {
	dir := t.TempDir()
	path := writePassphraseFile(t, dir, `{"live/stream1": "short"}`)

	_, err := NewFileResolver(path)
	if err == nil {
		t.Fatal("expected error for short passphrase in file")
	}
}

// TestFileResolver_PassphraseTooLong verifies that construction fails when
// any passphrase in the file exceeds the SRT maximum of 79 characters.
func TestFileResolver_PassphraseTooLong(t *testing.T) {
	dir := t.TempDir()
	// 80 chars = too long
	long := "aaaaaaaaaabbbbbbbbbbccccccccccddddddddddeeeeeeeeeeffffffffff" +
		"gggggggggghhhhhhhhhhx"
	path := writePassphraseFile(t, dir, `{"live/stream1": "`+long+`"}`)

	_, err := NewFileResolver(path)
	if err == nil {
		t.Fatal("expected error for long passphrase in file")
	}
}

// TestFileResolver_FileNotFound verifies that construction fails when the
// specified passphrase file does not exist on disk.
func TestFileResolver_FileNotFound(t *testing.T) {
	_, err := NewFileResolver("/nonexistent/path/file.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// TestFileResolver_Reload verifies that Reload() picks up changes from disk:
// updated passphrases are reflected, new streams become available, and
// removed streams return ErrStreamNotFound.
func TestFileResolver_Reload(t *testing.T) {
	dir := t.TempDir()
	path := writePassphraseFile(t, dir, `{"live/stream1": "original-passphrase"}`)

	r, err := NewFileResolver(path)
	if err != nil {
		t.Fatal(err)
	}

	// Verify original passphrase.
	got, _ := r.ResolvePassphrase("live/stream1")
	if got != "original-passphrase" {
		t.Fatalf("before reload: got %q, want %q", got, "original-passphrase")
	}

	// Update the file and reload.
	writePassphraseFile(t, dir, `{
		"live/stream1": "updated-passphrase",
		"live/stream3": "brand-new-stream"
	}`)

	if err := r.Reload(); err != nil {
		t.Fatal(err)
	}

	// Verify updated passphrase.
	got, _ = r.ResolvePassphrase("live/stream1")
	if got != "updated-passphrase" {
		t.Errorf("after reload: got %q, want %q", got, "updated-passphrase")
	}

	// Verify new stream is available.
	got, _ = r.ResolvePassphrase("live/stream3")
	if got != "brand-new-stream" {
		t.Errorf("after reload: got %q, want %q", got, "brand-new-stream")
	}

	// Verify removed stream is gone.
	_, err = r.ResolvePassphrase("live/stream2")
	if !errors.Is(err, ErrStreamNotFound) {
		t.Errorf("expected ErrStreamNotFound for removed stream, got: %v", err)
	}
}

// TestFileResolver_ReloadPreservesOnError verifies the "preserve on failure"
// guarantee: if Reload() encounters invalid JSON, the previous valid
// passphrase map remains in effect and existing streams keep working.
func TestFileResolver_ReloadPreservesOnError(t *testing.T) {
	dir := t.TempDir()
	path := writePassphraseFile(t, dir, `{"live/stream1": "original-passphrase"}`)

	r, err := NewFileResolver(path)
	if err != nil {
		t.Fatal(err)
	}

	// Write invalid content and try to reload.
	writePassphraseFile(t, dir, `not valid json`)
	if err := r.Reload(); err == nil {
		t.Fatal("expected reload error for invalid JSON")
	}

	// Original passphrase should still work.
	got, err := r.ResolvePassphrase("live/stream1")
	if err != nil {
		t.Fatalf("after failed reload: unexpected error: %v", err)
	}
	if got != "original-passphrase" {
		t.Errorf("after failed reload: got %q, want %q", got, "original-passphrase")
	}
}

// TestFileResolver_ReloadPreservesOnValidationError verifies that a reload
// with structurally valid JSON but an invalid passphrase (too short) also
// preserves the previous map — validation errors are treated the same as
// parse errors for safety.
func TestFileResolver_ReloadPreservesOnValidationError(t *testing.T) {
	dir := t.TempDir()
	path := writePassphraseFile(t, dir, `{"live/stream1": "original-passphrase"}`)

	r, err := NewFileResolver(path)
	if err != nil {
		t.Fatal(err)
	}

	// Write a file with a passphrase that's too short.
	writePassphraseFile(t, dir, `{"live/stream1": "short"}`)
	if err := r.Reload(); err == nil {
		t.Fatal("expected reload error for short passphrase")
	}

	// Original passphrase should still work.
	got, _ := r.ResolvePassphrase("live/stream1")
	if got != "original-passphrase" {
		t.Errorf("after failed reload: got %q, want %q", got, "original-passphrase")
	}
}

// TestFileResolver_ConcurrentAccess stress-tests the RWMutex by running
// 50 concurrent ResolvePassphrase reads alongside 5 concurrent Reloads.
// This test is designed to catch data races (run with -race flag).
func TestFileResolver_ConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	path := writePassphraseFile(t, dir, `{"live/stream1": "concurrent-passphrase"}`)

	r, err := NewFileResolver(path)
	if err != nil {
		t.Fatal(err)
	}

	// Concurrent reads with intermittent reloads — the goroutine counts
	// are chosen to create realistic contention: many readers, few writers.
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = r.ResolvePassphrase("live/stream1")
		}()
	}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.Reload()
		}()
	}
	wg.Wait()
}

// TestFileResolver_EncryptionRequired verifies that a FileResolver always
// reports encryption as required, since it was constructed with a passphrase file.
func TestFileResolver_EncryptionRequired(t *testing.T) {
	dir := t.TempDir()
	path := writePassphraseFile(t, dir, `{"live/stream1": "valid-passphrase"}`)

	r, _ := NewFileResolver(path)
	if !r.EncryptionRequired() {
		t.Error("FileResolver.EncryptionRequired() = false, want true")
	}
}

// --- ValidatePassphrase tests ---

// TestValidatePassphrase uses table-driven tests to verify the boundary
// conditions of SRT passphrase validation: exactly 10 chars (minimum),
// exactly 79 chars (maximum), and the error cases just outside those bounds.
func TestValidatePassphrase(t *testing.T) {
	tests := []struct {
		name       string
		passphrase string
		wantErr    bool
	}{
		{"valid_10_chars", "1234567890", false},
		{"valid_79_chars", "aaaaaaaaa_bbbbbbbbb_ccccccccc_ddddddddd_eeeeeeeee_fffffffff_ggggggggg_hhhhhhhhh", false},
		{"too_short_9", "123456789", true},
		{"too_long_80", "aaaaaaaaa_bbbbbbbbb_ccccccccc_ddddddddd_eeeeeeeee_fffffffff_ggggggggg_hhhhhhhhhi", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePassphrase(tt.passphrase)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePassphrase(%q) error = %v, wantErr %v", tt.passphrase, err, tt.wantErr)
			}
		})
	}
}

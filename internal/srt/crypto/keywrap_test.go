package crypto

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"
)

// hexDecode is a test helper that decodes a hex string or panics.
// This keeps test vector definitions clean and readable.
func hexDecode(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("invalid hex string %q: %v", s, err)
	}
	return b
}

// TestRFC3394Vectors validates our Wrap implementation against the official
// test vectors from RFC 3394 Section 4. These are the authoritative
// known-answer tests for AES Key Wrap.
func TestRFC3394Vectors(t *testing.T) {
	// Each test case uses a different KEK size (128, 192, 256 bits) but
	// the same 128-bit plaintext, producing different ciphertexts.
	tests := []struct {
		name       string // human-readable test name
		kek        string // Key Encryption Key in hex
		plaintext  string // key data to wrap in hex
		ciphertext string // expected wrapped output in hex
	}{
		{
			name:       "128-bit KEK wrapping 128-bit key",
			kek:        "000102030405060708090A0B0C0D0E0F",
			plaintext:  "00112233445566778899AABBCCDDEEFF",
			ciphertext: "1FA68B0A8112B447AEF34BD8FB5A7B829D3E862371D2CFE5",
		},
		{
			name:       "192-bit KEK wrapping 128-bit key",
			kek:        "000102030405060708090A0B0C0D0E0F1011121314151617",
			plaintext:  "00112233445566778899AABBCCDDEEFF",
			ciphertext: "96778B25AE6CA435F92B5B97C050AED2468AB8A17AD84E5D",
		},
		{
			name:       "256-bit KEK wrapping 128-bit key",
			kek:        "000102030405060708090A0B0C0D0E0F101112131415161718191A1B1C1D1E1F",
			plaintext:  "00112233445566778899AABBCCDDEEFF",
			ciphertext: "64E8C3F9CE0F5BA263E9777905818A2A93C8191E7D6E8AE7",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kek := hexDecode(t, tc.kek)
			plaintext := hexDecode(t, tc.plaintext)
			wantCiphertext := hexDecode(t, tc.ciphertext)

			// Test wrapping: plaintext → ciphertext.
			got, err := Wrap(kek, plaintext)
			if err != nil {
				t.Fatalf("Wrap() unexpected error: %v", err)
			}
			if !bytes.Equal(got, wantCiphertext) {
				t.Errorf("Wrap() mismatch\n  got:  %X\n  want: %X", got, wantCiphertext)
			}

			// Test unwrapping: ciphertext → plaintext.
			unwrapped, err := Unwrap(kek, wantCiphertext)
			if err != nil {
				t.Fatalf("Unwrap() unexpected error: %v", err)
			}
			if !bytes.Equal(unwrapped, plaintext) {
				t.Errorf("Unwrap() mismatch\n  got:  %X\n  want: %X", unwrapped, plaintext)
			}
		})
	}
}

// TestWrapUnwrapRoundTrip verifies that wrapping then unwrapping a key
// produces the original plaintext. This tests with various key sizes
// and plaintext lengths.
func TestWrapUnwrapRoundTrip(t *testing.T) {
	tests := []struct {
		name      string
		kekLen    int // KEK length in bytes (16, 24, or 32)
		plainLen  int // plaintext length in bytes (must be ≥16, multiple of 8)
	}{
		{"AES-128 KEK, 16-byte plaintext", 16, 16},
		{"AES-128 KEK, 32-byte plaintext", 16, 32},
		{"AES-192 KEK, 24-byte plaintext", 24, 24},
		{"AES-256 KEK, 16-byte plaintext", 32, 16},
		{"AES-256 KEK, 40-byte plaintext", 32, 40},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create deterministic test data (not random, so tests are
			// reproducible).
			kek := make([]byte, tc.kekLen)
			for i := range kek {
				kek[i] = byte(i)
			}
			plaintext := make([]byte, tc.plainLen)
			for i := range plaintext {
				plaintext[i] = byte(i + 0x10)
			}

			// Wrap the plaintext.
			wrapped, err := Wrap(kek, plaintext)
			if err != nil {
				t.Fatalf("Wrap() error: %v", err)
			}

			// Wrapped output should be 8 bytes longer than plaintext.
			if len(wrapped) != len(plaintext)+8 {
				t.Fatalf("wrapped length = %d, want %d", len(wrapped), len(plaintext)+8)
			}

			// Unwrap should recover the original plaintext.
			recovered, err := Unwrap(kek, wrapped)
			if err != nil {
				t.Fatalf("Unwrap() error: %v", err)
			}
			if !bytes.Equal(recovered, plaintext) {
				t.Errorf("round-trip mismatch\n  got:  %X\n  want: %X", recovered, plaintext)
			}
		})
	}
}

// TestWrapInvalidKEKLength verifies that Wrap rejects KEK lengths that
// are not valid AES key sizes (16, 24, or 32 bytes).
func TestWrapInvalidKEKLength(t *testing.T) {
	plaintext := make([]byte, 16)

	// Try invalid KEK lengths — none of these are valid AES key sizes.
	badLengths := []int{0, 1, 8, 15, 17, 31, 33, 64}
	for _, kekLen := range badLengths {
		kek := make([]byte, kekLen)
		_, err := Wrap(kek, plaintext)
		if err == nil {
			t.Errorf("Wrap() with %d-byte KEK: expected error, got nil", kekLen)
		}
	}
}

// TestUnwrapInvalidKEKLength verifies that Unwrap rejects invalid KEK lengths.
func TestUnwrapInvalidKEKLength(t *testing.T) {
	ciphertext := make([]byte, 24) // minimum valid ciphertext size

	badLengths := []int{0, 1, 8, 15, 17, 31, 33, 64}
	for _, kekLen := range badLengths {
		kek := make([]byte, kekLen)
		_, err := Unwrap(kek, ciphertext)
		if err == nil {
			t.Errorf("Unwrap() with %d-byte KEK: expected error, got nil", kekLen)
		}
	}
}

// TestWrapPlaintextTooShort verifies that Wrap rejects plaintext shorter
// than 16 bytes. RFC 3394 requires at least 2 blocks (n ≥ 2).
func TestWrapPlaintextTooShort(t *testing.T) {
	kek := make([]byte, 16)

	// Try plaintext lengths that are too short (less than 16 bytes).
	shortLengths := []int{0, 8}
	for _, pLen := range shortLengths {
		plaintext := make([]byte, pLen)
		_, err := Wrap(kek, plaintext)
		if err == nil {
			t.Errorf("Wrap() with %d-byte plaintext: expected error, got nil", pLen)
		}
	}
}

// TestWrapPlaintextNotMultipleOf8 verifies that Wrap rejects plaintext
// whose length is not a multiple of 8 bytes.
func TestWrapPlaintextNotMultipleOf8(t *testing.T) {
	kek := make([]byte, 16)

	// These lengths are ≥16 but not multiples of 8.
	badLengths := []int{17, 18, 19, 20, 21, 22, 23}
	for _, pLen := range badLengths {
		plaintext := make([]byte, pLen)
		_, err := Wrap(kek, plaintext)
		if err == nil {
			t.Errorf("Wrap() with %d-byte plaintext: expected error, got nil", pLen)
		}
	}
}

// TestUnwrapTamperedCiphertext verifies that Unwrap detects corrupted data.
// When even a single byte of the ciphertext is changed, the integrity
// check (A != DefaultIV) must fail.
func TestUnwrapTamperedCiphertext(t *testing.T) {
	kek := make([]byte, 16)
	for i := range kek {
		kek[i] = byte(i)
	}
	plaintext := make([]byte, 16)
	for i := range plaintext {
		plaintext[i] = byte(i + 0x10)
	}

	// First, create a valid wrapped ciphertext.
	wrapped, err := Wrap(kek, plaintext)
	if err != nil {
		t.Fatalf("Wrap() error: %v", err)
	}

	// Flip one bit in the middle of the ciphertext to simulate tampering.
	tampered := make([]byte, len(wrapped))
	copy(tampered, wrapped)
	tampered[len(tampered)/2] ^= 0x01

	// Unwrap should detect the corruption and return ErrIntegrityCheck.
	_, err = Unwrap(kek, tampered)
	if !errors.Is(err, ErrIntegrityCheck) {
		t.Errorf("Unwrap(tampered) error = %v, want %v", err, ErrIntegrityCheck)
	}
}

// TestUnwrapWrongKEK verifies that unwrapping with a different KEK than
// the one used for wrapping fails the integrity check.
func TestUnwrapWrongKEK(t *testing.T) {
	kek := make([]byte, 16)
	for i := range kek {
		kek[i] = byte(i)
	}
	plaintext := make([]byte, 16)
	for i := range plaintext {
		plaintext[i] = byte(i + 0x10)
	}

	// Wrap with the correct KEK.
	wrapped, err := Wrap(kek, plaintext)
	if err != nil {
		t.Fatalf("Wrap() error: %v", err)
	}

	// Try to unwrap with a different KEK.
	wrongKEK := make([]byte, 16)
	for i := range wrongKEK {
		wrongKEK[i] = byte(i + 0xFF)
	}

	_, err = Unwrap(wrongKEK, wrapped)
	if !errors.Is(err, ErrIntegrityCheck) {
		t.Errorf("Unwrap(wrongKEK) error = %v, want %v", err, ErrIntegrityCheck)
	}
}

// TestUnwrapCiphertextTooShort verifies that Unwrap rejects ciphertext
// shorter than 24 bytes (8-byte A + minimum 16 bytes of wrapped data).
func TestUnwrapCiphertextTooShort(t *testing.T) {
	kek := make([]byte, 16)

	shortLengths := []int{0, 8, 16}
	for _, cLen := range shortLengths {
		ciphertext := make([]byte, cLen)
		_, err := Unwrap(kek, ciphertext)
		if err == nil {
			t.Errorf("Unwrap() with %d-byte ciphertext: expected error, got nil", cLen)
		}
	}
}

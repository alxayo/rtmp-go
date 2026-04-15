package crypto

import (
	"bytes"
	"testing"
)

// TestPacketCipherRoundTrip verifies that encrypting then decrypting a
// payload recovers the original plaintext. This is the most fundamental
// correctness test for AES-CTR.
func TestPacketCipherRoundTrip(t *testing.T) {
	sek := make([]byte, 16)
	salt := make([]byte, 16)
	for i := range sek {
		sek[i] = byte(i)
	}
	for i := range salt {
		salt[i] = byte(i + 0x80)
	}

	pc, err := NewPacketCipher(sek, salt)
	if err != nil {
		t.Fatalf("NewPacketCipher() error: %v", err)
	}

	original := []byte("Hello, SRT encryption world!!")
	payload := make([]byte, len(original))
	copy(payload, original)

	packetIndex := uint32(42)

	// Encrypt in-place.
	if err := pc.EncryptPayload(payload, packetIndex); err != nil {
		t.Fatalf("EncryptPayload() error: %v", err)
	}

	// Ciphertext should differ from plaintext.
	if bytes.Equal(payload, original) {
		t.Fatal("EncryptPayload() did not change the payload")
	}

	// Decrypt in-place — should recover the original.
	if err := pc.DecryptPayload(payload, packetIndex); err != nil {
		t.Fatalf("DecryptPayload() error: %v", err)
	}
	if !bytes.Equal(payload, original) {
		t.Errorf("round-trip mismatch\n  got:  %X\n  want: %X", payload, original)
	}
}

// TestPacketCipherDifferentIndices verifies that encrypting the same payload
// with different packet indices produces different ciphertexts. Each packet
// must use a unique keystream.
func TestPacketCipherDifferentIndices(t *testing.T) {
	sek := make([]byte, 16)
	salt := make([]byte, 16)
	for i := range sek {
		sek[i] = byte(i)
	}
	for i := range salt {
		salt[i] = byte(i + 0x80)
	}

	pc, err := NewPacketCipher(sek, salt)
	if err != nil {
		t.Fatalf("NewPacketCipher() error: %v", err)
	}

	plaintext := []byte("same payload for both packets")

	// Encrypt with index 1.
	ct1 := make([]byte, len(plaintext))
	copy(ct1, plaintext)
	if err := pc.EncryptPayload(ct1, 1); err != nil {
		t.Fatalf("EncryptPayload(index=1) error: %v", err)
	}

	// Encrypt with index 2.
	ct2 := make([]byte, len(plaintext))
	copy(ct2, plaintext)
	if err := pc.EncryptPayload(ct2, 2); err != nil {
		t.Fatalf("EncryptPayload(index=2) error: %v", err)
	}

	if bytes.Equal(ct1, ct2) {
		t.Error("different packet indices produced identical ciphertext")
	}
}

// TestPacketCipherDeterministic verifies that encrypting the same payload
// with the same key, salt, and packet index always produces identical
// ciphertext. AES-CTR is deterministic given the same inputs.
func TestPacketCipherDeterministic(t *testing.T) {
	sek := make([]byte, 16)
	salt := make([]byte, 16)
	for i := range sek {
		sek[i] = byte(i)
	}
	for i := range salt {
		salt[i] = byte(i + 0x80)
	}

	pc, err := NewPacketCipher(sek, salt)
	if err != nil {
		t.Fatalf("NewPacketCipher() error: %v", err)
	}

	plaintext := []byte("deterministic test payload!!")
	index := uint32(100)

	// Encrypt twice independently.
	ct1 := make([]byte, len(plaintext))
	copy(ct1, plaintext)
	if err := pc.EncryptPayload(ct1, index); err != nil {
		t.Fatalf("EncryptPayload() #1 error: %v", err)
	}

	ct2 := make([]byte, len(plaintext))
	copy(ct2, plaintext)
	if err := pc.EncryptPayload(ct2, index); err != nil {
		t.Fatalf("EncryptPayload() #2 error: %v", err)
	}

	if !bytes.Equal(ct1, ct2) {
		t.Errorf("same inputs produced different ciphertext\n  ct1: %X\n  ct2: %X", ct1, ct2)
	}
}

// TestPacketCipherKeySizes verifies that all valid AES key sizes work
// correctly: AES-128 (16 bytes), AES-192 (24 bytes), AES-256 (32 bytes).
func TestPacketCipherKeySizes(t *testing.T) {
	tests := []struct {
		name   string
		keyLen int
	}{
		{"AES-128", 16},
		{"AES-192", 24},
		{"AES-256", 32},
	}

	salt := make([]byte, 16)
	for i := range salt {
		salt[i] = byte(i + 0xAA)
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sek := make([]byte, tc.keyLen)
			for i := range sek {
				sek[i] = byte(i)
			}

			pc, err := NewPacketCipher(sek, salt)
			if err != nil {
				t.Fatalf("NewPacketCipher() error: %v", err)
			}

			original := []byte("testing all key sizes works correctly")
			payload := make([]byte, len(original))
			copy(payload, original)

			if err := pc.EncryptPayload(payload, 7); err != nil {
				t.Fatalf("EncryptPayload() error: %v", err)
			}
			if bytes.Equal(payload, original) {
				t.Fatal("EncryptPayload() did not change the payload")
			}
			if err := pc.DecryptPayload(payload, 7); err != nil {
				t.Fatalf("DecryptPayload() error: %v", err)
			}
			if !bytes.Equal(payload, original) {
				t.Errorf("round-trip mismatch\n  got:  %X\n  want: %X", payload, original)
			}
		})
	}
}

// TestPacketCipherEmptyPayload verifies that encrypting an empty or nil
// payload is a no-op and does not return an error.
func TestPacketCipherEmptyPayload(t *testing.T) {
	sek := make([]byte, 16)
	salt := make([]byte, 16)

	pc, err := NewPacketCipher(sek, salt)
	if err != nil {
		t.Fatalf("NewPacketCipher() error: %v", err)
	}

	// Nil payload.
	if err := pc.EncryptPayload(nil, 0); err != nil {
		t.Errorf("EncryptPayload(nil) error: %v", err)
	}

	// Empty slice.
	if err := pc.EncryptPayload([]byte{}, 0); err != nil {
		t.Errorf("EncryptPayload(empty) error: %v", err)
	}

	// Same for decrypt.
	if err := pc.DecryptPayload(nil, 0); err != nil {
		t.Errorf("DecryptPayload(nil) error: %v", err)
	}
	if err := pc.DecryptPayload([]byte{}, 0); err != nil {
		t.Errorf("DecryptPayload(empty) error: %v", err)
	}
}

// TestPacketCipherLargePayload tests encryption of a payload that spans
// multiple AES blocks (each block is 16 bytes). This exercises the CTR
// block counter increment logic.
func TestPacketCipherLargePayload(t *testing.T) {
	sek := make([]byte, 16)
	salt := make([]byte, 16)
	for i := range sek {
		sek[i] = byte(i)
	}
	for i := range salt {
		salt[i] = byte(i + 0x80)
	}

	pc, err := NewPacketCipher(sek, salt)
	if err != nil {
		t.Fatalf("NewPacketCipher() error: %v", err)
	}

	// 1500 bytes — typical MTU size, spans many AES blocks (1500 / 16 = 93.75).
	original := make([]byte, 1500)
	for i := range original {
		original[i] = byte(i % 256)
	}

	payload := make([]byte, len(original))
	copy(payload, original)

	if err := pc.EncryptPayload(payload, 999); err != nil {
		t.Fatalf("EncryptPayload() error: %v", err)
	}
	if bytes.Equal(payload, original) {
		t.Fatal("EncryptPayload() did not change a 1500-byte payload")
	}
	if err := pc.DecryptPayload(payload, 999); err != nil {
		t.Fatalf("DecryptPayload() error: %v", err)
	}
	if !bytes.Equal(payload, original) {
		t.Errorf("large payload round-trip mismatch (first differing byte at some position)")
	}
}

// TestPacketCipherInvalidKeyLength verifies that NewPacketCipher rejects
// SEK lengths that are not valid AES key sizes (16, 24, or 32 bytes).
func TestPacketCipherInvalidKeyLength(t *testing.T) {
	salt := make([]byte, 16)

	badLengths := []int{0, 1, 8, 15, 17, 31, 33, 64}
	for _, sekLen := range badLengths {
		sek := make([]byte, sekLen)
		_, err := NewPacketCipher(sek, salt)
		if err == nil {
			t.Errorf("NewPacketCipher() with %d-byte SEK: expected error, got nil", sekLen)
		}
	}
}

// TestPacketCipherInvalidSaltLength verifies that NewPacketCipher rejects
// salt lengths that are not exactly 16 bytes.
func TestPacketCipherInvalidSaltLength(t *testing.T) {
	sek := make([]byte, 16)

	badLengths := []int{0, 1, 8, 14, 15, 17, 32}
	for _, saltLen := range badLengths {
		salt := make([]byte, saltLen)
		_, err := NewPacketCipher(sek, salt)
		if err == nil {
			t.Errorf("NewPacketCipher() with %d-byte salt: expected error, got nil", saltLen)
		}
	}
}

// TestPacketCipherSymmetry verifies the AES-CTR symmetry property:
// applying encryption twice is equivalent to a no-op (encrypt → encrypt
// recovers the original plaintext), confirming that Encrypt == Decrypt.
func TestPacketCipherSymmetry(t *testing.T) {
	sek := make([]byte, 16)
	salt := make([]byte, 16)
	for i := range sek {
		sek[i] = byte(i + 0x30)
	}
	for i := range salt {
		salt[i] = byte(i + 0x50)
	}

	pc, err := NewPacketCipher(sek, salt)
	if err != nil {
		t.Fatalf("NewPacketCipher() error: %v", err)
	}

	original := []byte("symmetry: encrypt twice = original")
	payload := make([]byte, len(original))
	copy(payload, original)

	index := uint32(55)

	// First encrypt — produces ciphertext.
	if err := pc.EncryptPayload(payload, index); err != nil {
		t.Fatalf("EncryptPayload() #1 error: %v", err)
	}

	// Second encrypt (same index) — should recover plaintext.
	if err := pc.EncryptPayload(payload, index); err != nil {
		t.Fatalf("EncryptPayload() #2 error: %v", err)
	}

	if !bytes.Equal(payload, original) {
		t.Errorf("double-encrypt did not recover original\n  got:  %X\n  want: %X", payload, original)
	}
}

// TestPacketCipherDifferentSalts verifies that the same key but different
// salts produce different ciphertexts. The salt feeds into the IV, so
// changing it must change the keystream.
func TestPacketCipherDifferentSalts(t *testing.T) {
	sek := make([]byte, 16)
	for i := range sek {
		sek[i] = byte(i)
	}

	salt1 := make([]byte, 16)
	salt2 := make([]byte, 16)
	for i := range salt1 {
		salt1[i] = byte(i + 0x10)
		salt2[i] = byte(i + 0x20)
	}

	pc1, err := NewPacketCipher(sek, salt1)
	if err != nil {
		t.Fatalf("NewPacketCipher(salt1) error: %v", err)
	}
	pc2, err := NewPacketCipher(sek, salt2)
	if err != nil {
		t.Fatalf("NewPacketCipher(salt2) error: %v", err)
	}

	plaintext := []byte("same plaintext, different salts!")

	ct1 := make([]byte, len(plaintext))
	copy(ct1, plaintext)
	if err := pc1.EncryptPayload(ct1, 0); err != nil {
		t.Fatalf("EncryptPayload(salt1) error: %v", err)
	}

	ct2 := make([]byte, len(plaintext))
	copy(ct2, plaintext)
	if err := pc2.EncryptPayload(ct2, 0); err != nil {
		t.Fatalf("EncryptPayload(salt2) error: %v", err)
	}

	if bytes.Equal(ct1, ct2) {
		t.Error("different salts produced identical ciphertext")
	}
}

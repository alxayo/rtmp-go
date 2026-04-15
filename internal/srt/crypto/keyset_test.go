package crypto

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

// --- Test helpers ---

// makeKey creates a deterministic byte slice of the given length. Each byte
// is (offset + index) mod 256, so keys created with different offsets will
// differ. This makes it easy to generate distinct keys for even/odd slots.
func makeKey(length, offset int) []byte {
	key := make([]byte, length)
	for i := range key {
		key[i] = byte((offset + i) % 256)
	}
	return key
}

// makeSalt creates a 16-byte salt with values starting from offset.
func makeSalt(offset int) []byte {
	return makeKey(16, offset)
}

// encryptWithCipher is a helper that creates a PacketCipher, encrypts the
// given plaintext (returning a new slice), and returns the ciphertext. This
// is used to create "known ciphertext" that we then try to decrypt through
// the KeySet.
func encryptWithCipher(t *testing.T, sek, salt, plaintext []byte, packetIndex uint32) []byte {
	t.Helper()
	pc, err := NewPacketCipher(sek, salt)
	if err != nil {
		t.Fatalf("encryptWithCipher: NewPacketCipher() error: %v", err)
	}
	ct := make([]byte, len(plaintext))
	copy(ct, plaintext)
	if err := pc.EncryptPayload(ct, packetIndex); err != nil {
		t.Fatalf("encryptWithCipher: EncryptPayload() error: %v", err)
	}
	return ct
}

// --- Tests ---

// TestNewKeySetFromSEK_EvenOnly verifies that creating a KeySet with KKEven
// installs only the even key. Decrypting with the even flag succeeds, while
// decrypting with the odd flag returns a clear "odd key not installed" error.
func TestNewKeySetFromSEK_EvenOnly(t *testing.T) {
	sek := makeKey(16, 0x10)
	salt := makeSalt(0x80)
	plaintext := []byte("even-only test payload data")
	packetIndex := uint32(42)

	ks, err := NewKeySetFromSEK(KKEven, sek, salt, 16)
	if err != nil {
		t.Fatalf("NewKeySetFromSEK(KKEven) error: %v", err)
	}

	// Encrypt with a standalone cipher using the same key.
	ct := encryptWithCipher(t, sek, salt, plaintext, packetIndex)

	// Decrypt through the KeySet using the even flag — should recover plaintext.
	if err := ks.DecryptPayload(ct, packetIndex, encryptionEven); err != nil {
		t.Fatalf("DecryptPayload(even) error: %v", err)
	}
	if !bytes.Equal(ct, plaintext) {
		t.Errorf("even decrypt mismatch\n  got:  %X\n  want: %X", ct, plaintext)
	}

	// Attempting to decrypt with odd flag should fail.
	oddCt := encryptWithCipher(t, sek, salt, plaintext, packetIndex)
	err = ks.DecryptPayload(oddCt, packetIndex, encryptionOdd)
	if err == nil {
		t.Fatal("DecryptPayload(odd) should fail when only even key is installed")
	}
	if !strings.Contains(err.Error(), "odd key not installed") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestNewKeySetFromSEK_OddOnly verifies that creating a KeySet with KKOdd
// installs only the odd key. Decrypting with the odd flag succeeds, while
// decrypting with the even flag returns a clear "even key not installed" error.
func TestNewKeySetFromSEK_OddOnly(t *testing.T) {
	sek := makeKey(16, 0x20)
	salt := makeSalt(0x90)
	plaintext := []byte("odd-only test payload data!!")
	packetIndex := uint32(99)

	ks, err := NewKeySetFromSEK(KKOdd, sek, salt, 16)
	if err != nil {
		t.Fatalf("NewKeySetFromSEK(KKOdd) error: %v", err)
	}

	// Encrypt and decrypt with odd flag — should succeed.
	ct := encryptWithCipher(t, sek, salt, plaintext, packetIndex)
	if err := ks.DecryptPayload(ct, packetIndex, encryptionOdd); err != nil {
		t.Fatalf("DecryptPayload(odd) error: %v", err)
	}
	if !bytes.Equal(ct, plaintext) {
		t.Errorf("odd decrypt mismatch\n  got:  %X\n  want: %X", ct, plaintext)
	}

	// Attempting to decrypt with even flag should fail.
	evenCt := encryptWithCipher(t, sek, salt, plaintext, packetIndex)
	err = ks.DecryptPayload(evenCt, packetIndex, encryptionEven)
	if err == nil {
		t.Fatal("DecryptPayload(even) should fail when only odd key is installed")
	}
	if !strings.Contains(err.Error(), "even key not installed") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestNewKeySetFromSEK_Both verifies that creating a KeySet with KKBoth and
// concatenated SEK data installs both keys. Each key independently produces
// different ciphertext, and decryption routes to the correct cipher.
func TestNewKeySetFromSEK_Both(t *testing.T) {
	evenSEK := makeKey(16, 0x10)
	oddSEK := makeKey(16, 0x30)
	salt := makeSalt(0x80)
	plaintext := []byte("both-keys test payload data")
	packetIndex := uint32(7)

	// Concatenate even + odd keys as required by KKBoth.
	bothSEK := make([]byte, 0, 32)
	bothSEK = append(bothSEK, evenSEK...)
	bothSEK = append(bothSEK, oddSEK...)

	ks, err := NewKeySetFromSEK(KKBoth, bothSEK, salt, 16)
	if err != nil {
		t.Fatalf("NewKeySetFromSEK(KKBoth) error: %v", err)
	}

	// Encrypt with the even key, decrypt through KeySet with even flag.
	evenCt := encryptWithCipher(t, evenSEK, salt, plaintext, packetIndex)
	if err := ks.DecryptPayload(evenCt, packetIndex, encryptionEven); err != nil {
		t.Fatalf("DecryptPayload(even) error: %v", err)
	}
	if !bytes.Equal(evenCt, plaintext) {
		t.Errorf("even decrypt mismatch\n  got:  %X\n  want: %X", evenCt, plaintext)
	}

	// Encrypt with the odd key, decrypt through KeySet with odd flag.
	oddCt := encryptWithCipher(t, oddSEK, salt, plaintext, packetIndex)
	if err := ks.DecryptPayload(oddCt, packetIndex, encryptionOdd); err != nil {
		t.Fatalf("DecryptPayload(odd) error: %v", err)
	}
	if !bytes.Equal(oddCt, plaintext) {
		t.Errorf("odd decrypt mismatch\n  got:  %X\n  want: %X", oddCt, plaintext)
	}

	// Verify that even and odd keys produce different ciphertext (they are different keys).
	evenCt2 := encryptWithCipher(t, evenSEK, salt, plaintext, packetIndex)
	oddCt2 := encryptWithCipher(t, oddSEK, salt, plaintext, packetIndex)
	if bytes.Equal(evenCt2, oddCt2) {
		t.Error("even and odd keys produced identical ciphertext — keys should differ")
	}
}

// TestInstallKey_ReplaceEven verifies that installing a new even key replaces
// the old one. Data encrypted with the old key can no longer be decrypted,
// while data encrypted with the new key decrypts correctly.
func TestInstallKey_ReplaceEven(t *testing.T) {
	oldSEK := makeKey(16, 0x10)
	newSEK := makeKey(16, 0x50)
	salt := makeSalt(0x80)
	plaintext := []byte("key-replace test payload!!")
	packetIndex := uint32(100)

	// Create with old even key.
	ks, err := NewKeySetFromSEK(KKEven, oldSEK, salt, 16)
	if err != nil {
		t.Fatalf("NewKeySetFromSEK() error: %v", err)
	}

	// Verify old key works.
	ct := encryptWithCipher(t, oldSEK, salt, plaintext, packetIndex)
	if err := ks.DecryptPayload(ct, packetIndex, encryptionEven); err != nil {
		t.Fatalf("DecryptPayload(old even) error: %v", err)
	}
	if !bytes.Equal(ct, plaintext) {
		t.Fatal("old even key should decrypt correctly before replacement")
	}

	// Install new even key, replacing the old one.
	if err := ks.InstallKey(KKEven, newSEK, salt, 16); err != nil {
		t.Fatalf("InstallKey(new even) error: %v", err)
	}

	// Data encrypted with the old key should NOT decrypt correctly anymore.
	oldCt := encryptWithCipher(t, oldSEK, salt, plaintext, packetIndex)
	if err := ks.DecryptPayload(oldCt, packetIndex, encryptionEven); err != nil {
		t.Fatalf("DecryptPayload() error: %v", err)
	}
	if bytes.Equal(oldCt, plaintext) {
		t.Error("old key ciphertext decrypted to correct plaintext after key replacement — new key should differ")
	}

	// Data encrypted with the new key should decrypt correctly.
	newCt := encryptWithCipher(t, newSEK, salt, plaintext, packetIndex)
	if err := ks.DecryptPayload(newCt, packetIndex, encryptionEven); err != nil {
		t.Fatalf("DecryptPayload(new even) error: %v", err)
	}
	if !bytes.Equal(newCt, plaintext) {
		t.Errorf("new even key decrypt mismatch\n  got:  %X\n  want: %X", newCt, plaintext)
	}
}

// TestInstallKey_AddOdd verifies that starting with only an even key and then
// installing an odd key results in both keys working simultaneously.
func TestInstallKey_AddOdd(t *testing.T) {
	evenSEK := makeKey(16, 0x10)
	oddSEK := makeKey(16, 0x30)
	salt := makeSalt(0x80)
	plaintext := []byte("add-odd test payload data!!")
	packetIndex := uint32(55)

	// Start with even key only.
	ks, err := NewKeySetFromSEK(KKEven, evenSEK, salt, 16)
	if err != nil {
		t.Fatalf("NewKeySetFromSEK(KKEven) error: %v", err)
	}

	// Odd should fail before installation.
	oddCt := encryptWithCipher(t, oddSEK, salt, plaintext, packetIndex)
	err = ks.DecryptPayload(oddCt, packetIndex, encryptionOdd)
	if err == nil {
		t.Fatal("DecryptPayload(odd) should fail before odd key is installed")
	}

	// Install odd key.
	if err := ks.InstallKey(KKOdd, oddSEK, salt, 16); err != nil {
		t.Fatalf("InstallKey(odd) error: %v", err)
	}

	// Now both should work.
	evenCt := encryptWithCipher(t, evenSEK, salt, plaintext, packetIndex)
	if err := ks.DecryptPayload(evenCt, packetIndex, encryptionEven); err != nil {
		t.Fatalf("DecryptPayload(even) error: %v", err)
	}
	if !bytes.Equal(evenCt, plaintext) {
		t.Errorf("even decrypt mismatch after odd install\n  got:  %X\n  want: %X", evenCt, plaintext)
	}

	oddCt = encryptWithCipher(t, oddSEK, salt, plaintext, packetIndex)
	if err := ks.DecryptPayload(oddCt, packetIndex, encryptionOdd); err != nil {
		t.Fatalf("DecryptPayload(odd) error: %v", err)
	}
	if !bytes.Equal(oddCt, plaintext) {
		t.Errorf("odd decrypt mismatch\n  got:  %X\n  want: %X", oddCt, plaintext)
	}
}

// TestKeySet_ConcurrentAccess exercises the KeySet's thread safety by running
// concurrent decryption goroutines alongside key installation. The race
// detector (-race) will flag any data races.
func TestKeySet_ConcurrentAccess(t *testing.T) {
	evenSEK := makeKey(16, 0x10)
	oddSEK := makeKey(16, 0x30)
	salt := makeSalt(0x80)
	plaintext := []byte("concurrent access test data!")

	// Start with even key.
	ks, err := NewKeySetFromSEK(KKEven, evenSEK, salt, 16)
	if err != nil {
		t.Fatalf("NewKeySetFromSEK() error: %v", err)
	}

	var wg sync.WaitGroup
	const iterations = 500

	// Goroutine 1: repeatedly decrypt with even key.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			ct := encryptWithCipher(t, evenSEK, salt, plaintext, uint32(i))
			// Ignore errors — the even key might be replaced mid-loop.
			_ = ks.DecryptPayload(ct, uint32(i), encryptionEven)
		}
	}()

	// Goroutine 2: repeatedly try decrypt with odd key (may fail until installed).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			ct := encryptWithCipher(t, oddSEK, salt, plaintext, uint32(i))
			_ = ks.DecryptPayload(ct, uint32(i), encryptionOdd)
		}
	}()

	// Goroutine 3: install keys repeatedly (write operations).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			newSEK := makeKey(16, i)
			if i%2 == 0 {
				_ = ks.InstallKey(KKEven, newSEK, salt, 16)
			} else {
				_ = ks.InstallKey(KKOdd, newSEK, salt, 16)
			}
		}
	}()

	// Goroutine 4: call HasKeys repeatedly (read operation).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			ks.HasKeys()
		}
	}()

	wg.Wait()
	// If the race detector doesn't complain, thread safety is verified.
}

// TestNewKeySetFromSEK_InvalidKK verifies that a KK value of 0 (no keys)
// returns an error. KK=0 is not valid in a KM message that carries keys.
func TestNewKeySetFromSEK_InvalidKK(t *testing.T) {
	sek := makeKey(16, 0x10)
	salt := makeSalt(0x80)

	_, err := NewKeySetFromSEK(0x00, sek, salt, 16)
	if err == nil {
		t.Fatal("NewKeySetFromSEK(kk=0) should return an error")
	}
	if !strings.Contains(err.Error(), "invalid KK flag") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestNewKeySetFromSEK_BothWrongLength verifies that KKBoth with SEK data
// shorter than 2×keyLen returns an error.
func TestNewKeySetFromSEK_BothWrongLength(t *testing.T) {
	salt := makeSalt(0x80)

	tests := []struct {
		name   string
		sekLen int
		keyLen int
	}{
		{"16-byte SEK for 2×16", 16, 16}, // Need 32, only have 16
		{"24-byte SEK for 2×16", 24, 16}, // Need 32, only have 24
		{"31-byte SEK for 2×16", 31, 16}, // Need 32, only have 31
		{"48-byte SEK for 2×32", 48, 32}, // Need 64, only have 48
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sek := makeKey(tc.sekLen, 0x10)
			_, err := NewKeySetFromSEK(KKBoth, sek, salt, tc.keyLen)
			if err == nil {
				t.Fatalf("NewKeySetFromSEK(KKBoth) with %d-byte SEK for keyLen=%d should error",
					tc.sekLen, tc.keyLen)
			}
			if !strings.Contains(err.Error(), "KKBoth requires") {
				t.Errorf("unexpected error message: %v", err)
			}
		})
	}
}

// TestDecryptPayload_EmptyPayload verifies that decrypting a nil or empty
// payload is a no-op, regardless of whether the key is installed. This is
// consistent with PacketCipher's behavior.
func TestDecryptPayload_EmptyPayload(t *testing.T) {
	// Even with no keys installed, empty payloads should succeed.
	ks := NewKeySet()

	if err := ks.DecryptPayload(nil, 0, encryptionEven); err != nil {
		t.Errorf("DecryptPayload(nil, even) error: %v", err)
	}
	if err := ks.DecryptPayload([]byte{}, 0, encryptionEven); err != nil {
		t.Errorf("DecryptPayload(empty, even) error: %v", err)
	}
	if err := ks.DecryptPayload(nil, 0, encryptionOdd); err != nil {
		t.Errorf("DecryptPayload(nil, odd) error: %v", err)
	}
	if err := ks.DecryptPayload([]byte{}, 0, encryptionOdd); err != nil {
		t.Errorf("DecryptPayload(empty, odd) error: %v", err)
	}
}

// TestNewKeySet_Empty verifies that a freshly created empty KeySet has no keys
// and reports HasKeys() as false.
func TestNewKeySet_Empty(t *testing.T) {
	ks := NewKeySet()
	if ks.HasKeys() {
		t.Error("empty KeySet should report HasKeys() == false")
	}
}

// TestHasKeys_AfterInstall verifies that HasKeys returns true after installing
// at least one key.
func TestHasKeys_AfterInstall(t *testing.T) {
	sek := makeKey(16, 0x10)
	salt := makeSalt(0x80)

	ks := NewKeySet()
	if ks.HasKeys() {
		t.Fatal("HasKeys() should be false before any key is installed")
	}

	if err := ks.InstallKey(KKEven, sek, salt, 16); err != nil {
		t.Fatalf("InstallKey() error: %v", err)
	}
	if !ks.HasKeys() {
		t.Error("HasKeys() should be true after installing even key")
	}
}

// TestDecryptPayload_InvalidEncFlag verifies that an unsupported encryption
// flag value returns a clear error.
func TestDecryptPayload_InvalidEncFlag(t *testing.T) {
	sek := makeKey(16, 0x10)
	salt := makeSalt(0x80)

	ks, err := NewKeySetFromSEK(KKEven, sek, salt, 16)
	if err != nil {
		t.Fatalf("NewKeySetFromSEK() error: %v", err)
	}

	payload := []byte("some payload data")
	err = ks.DecryptPayload(payload, 0, 0x03) // 0x03 is not a valid single-key flag
	if err == nil {
		t.Fatal("DecryptPayload with encFlag=0x03 should return an error")
	}
	if !strings.Contains(err.Error(), "invalid encryption flag") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestNewKeySetFromSEK_AllKeySizes verifies that KeySet works with all
// valid AES key sizes: 128, 192, and 256 bits.
func TestNewKeySetFromSEK_AllKeySizes(t *testing.T) {
	tests := []struct {
		name   string
		keyLen int
	}{
		{"AES-128", 16},
		{"AES-192", 24},
		{"AES-256", 32},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sek := makeKey(tc.keyLen, 0x10)
			salt := makeSalt(0x80)
			plaintext := []byte("key-size test payload data!")
			packetIndex := uint32(1)

			ks, err := NewKeySetFromSEK(KKEven, sek, salt, tc.keyLen)
			if err != nil {
				t.Fatalf("NewKeySetFromSEK() error: %v", err)
			}

			ct := encryptWithCipher(t, sek, salt, plaintext, packetIndex)
			if err := ks.DecryptPayload(ct, packetIndex, encryptionEven); err != nil {
				t.Fatalf("DecryptPayload() error: %v", err)
			}
			if !bytes.Equal(ct, plaintext) {
				t.Errorf("decrypt mismatch\n  got:  %X\n  want: %X", ct, plaintext)
			}
		})
	}
}

// Package crypto provides cryptographic primitives needed by the SRT protocol.
// This includes AES Key Wrap (RFC 3394) for encrypting/decrypting media
// encryption keys, and PBKDF2 for deriving keys from passphrases.
package crypto

import (
	"crypto/aes"
	"encoding/binary"
	"errors"
	"fmt"
)

// DefaultIV is the default Initial Value defined in RFC 3394 Section 2.2.3.1.
// This 8-byte constant is used as the initial "register A" value during key
// wrapping, and is checked during unwrapping to verify data integrity.
var DefaultIV = [8]byte{0xA6, 0xA6, 0xA6, 0xA6, 0xA6, 0xA6, 0xA6, 0xA6}

// ErrIntegrityCheck is returned when AES Key Unwrap detects that the
// ciphertext was corrupted or the wrong KEK was used. After unwrapping,
// register A must equal DefaultIV; if it doesn't, this error is returned.
var ErrIntegrityCheck = errors.New("aes key wrap: integrity check failed")

// Wrap implements AES Key Wrap per RFC 3394 Section 2.2.1.
//
// Parameters:
//   - kek: Key Encryption Key — the key used to wrap (encrypt) the plaintext.
//     Must be 16, 24, or 32 bytes for AES-128, AES-192, or AES-256.
//   - plaintext: the key material to wrap. Must be a multiple of 8 bytes
//     and at least 16 bytes long (i.e., at least 2 blocks of 8 bytes).
//
// Returns: the wrapped ciphertext, which is 8 bytes longer than plaintext.
//
// Algorithm overview:
//  1. Set register A = DefaultIV (an 8-byte integrity check value).
//  2. Split plaintext into n blocks of 8 bytes each: R[1], R[2], ..., R[n].
//  3. For j = 0 to 5 (6 rounds):
//     For i = 1 to n:
//     - Concatenate A and R[i] into a 16-byte block.
//     - Encrypt that block with AES using the KEK → produces 16 bytes (B).
//     - A = first 8 bytes of B, XORed with the counter value (n*j + i).
//     - R[i] = last 8 bytes of B.
//  4. Output: A || R[1] || R[2] || ... || R[n].
func Wrap(kek, plaintext []byte) ([]byte, error) {
	// Validate the KEK length — AES only accepts 16, 24, or 32 byte keys.
	k := len(kek)
	if k != 16 && k != 24 && k != 32 {
		return nil, fmt.Errorf("aes key wrap: invalid KEK length %d (must be 16, 24, or 32)", k)
	}

	// Plaintext must be at least 16 bytes (2 blocks) and a multiple of 8.
	if len(plaintext) < 16 {
		return nil, fmt.Errorf("aes key wrap: plaintext too short (%d bytes, minimum 16)", len(plaintext))
	}
	if len(plaintext)%8 != 0 {
		return nil, fmt.Errorf("aes key wrap: plaintext length %d is not a multiple of 8", len(plaintext))
	}

	// Create the AES cipher block using the KEK.
	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, fmt.Errorf("aes key wrap: %w", err)
	}

	// n is the number of 8-byte blocks in the plaintext.
	n := len(plaintext) / 8

	// Initialize register A with the default integrity check value.
	var a [8]byte
	copy(a[:], DefaultIV[:])

	// R holds the n plaintext blocks. We copy plaintext into R so we don't
	// modify the caller's slice.
	r := make([]byte, len(plaintext))
	copy(r, plaintext)

	// buf is a 16-byte scratch buffer used for AES encryption:
	// bytes [0:8] = A, bytes [8:16] = R[i].
	var buf [aes.BlockSize]byte

	// Perform 6 rounds of wrapping (j = 0..5) as specified by RFC 3394.
	for j := 0; j < 6; j++ {
		for i := 0; i < n; i++ {
			// Pack A into the first 8 bytes of buf.
			copy(buf[:8], a[:])

			// Pack R[i] into the last 8 bytes of buf.
			// R[i] starts at offset i*8 in our r slice.
			copy(buf[8:], r[i*8:(i+1)*8])

			// Encrypt the 16-byte block in-place with AES-ECB (single block).
			block.Encrypt(buf[:], buf[:])

			// The counter t = n*j + i + 1 (1-based as per the RFC).
			t := uint64(n*j + i + 1)

			// A = MSB(64, B) XOR t
			// Take the first 8 bytes of the encrypted block as the new A,
			// then XOR with the counter encoded as a big-endian uint64.
			copy(a[:], buf[:8])
			tBytes := binary.BigEndian.Uint64(a[:])
			binary.BigEndian.PutUint64(a[:], tBytes^t)

			// R[i] = LSB(64, B) — the last 8 bytes of the encrypted block.
			copy(r[i*8:(i+1)*8], buf[8:])
		}
	}

	// Build the output: A (8 bytes) followed by all R blocks.
	out := make([]byte, 8+len(r))
	copy(out[:8], a[:])
	copy(out[8:], r)

	return out, nil
}

// Unwrap implements AES Key Unwrap per RFC 3394 Section 2.2.2.
//
// This is the reverse of Wrap. It decrypts a wrapped key and verifies its
// integrity by checking that register A equals DefaultIV after unwrapping.
//
// Parameters:
//   - kek: the same Key Encryption Key used during wrapping.
//   - ciphertext: the wrapped key data. Must be a multiple of 8 bytes
//     and at least 24 bytes (8-byte A + at least 16 bytes of key data).
//
// Returns: the original plaintext key material, or ErrIntegrityCheck if
// the ciphertext was tampered with or the wrong KEK was used.
func Unwrap(kek, ciphertext []byte) ([]byte, error) {
	// Validate KEK length.
	k := len(kek)
	if k != 16 && k != 24 && k != 32 {
		return nil, fmt.Errorf("aes key wrap: invalid KEK length %d (must be 16, 24, or 32)", k)
	}

	// Ciphertext must be at least 24 bytes (8 for A + 16 for two blocks)
	// and a multiple of 8.
	if len(ciphertext) < 24 {
		return nil, fmt.Errorf("aes key wrap: ciphertext too short (%d bytes, minimum 24)", len(ciphertext))
	}
	if len(ciphertext)%8 != 0 {
		return nil, fmt.Errorf("aes key wrap: ciphertext length %d is not a multiple of 8", len(ciphertext))
	}

	// Create the AES cipher block using the KEK.
	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, fmt.Errorf("aes key wrap: %w", err)
	}

	// n is the number of 8-byte blocks in the key data (excluding A).
	n := (len(ciphertext) / 8) - 1

	// Extract register A from the first 8 bytes of ciphertext.
	var a [8]byte
	copy(a[:], ciphertext[:8])

	// Copy the remaining ciphertext blocks into R.
	r := make([]byte, n*8)
	copy(r, ciphertext[8:])

	// buf is a 16-byte scratch buffer for AES decryption.
	var buf [aes.BlockSize]byte

	// Reverse the wrapping: iterate in reverse order.
	// For j = 5 down to 0, for i = n down to 1.
	for j := 5; j >= 0; j-- {
		for i := n - 1; i >= 0; i-- {
			// Compute the counter value t = n*j + i + 1 (same as in Wrap).
			t := uint64(n*j + i + 1)

			// Undo the XOR: A = A XOR t
			tBytes := binary.BigEndian.Uint64(a[:])
			binary.BigEndian.PutUint64(a[:], tBytes^t)

			// Pack A and R[i] into buf, then decrypt.
			copy(buf[:8], a[:])
			copy(buf[8:], r[i*8:(i+1)*8])

			// Decrypt the 16-byte block in-place with AES-ECB.
			block.Decrypt(buf[:], buf[:])

			// A = first 8 bytes of the decrypted block.
			copy(a[:], buf[:8])

			// R[i] = last 8 bytes of the decrypted block.
			copy(r[i*8:(i+1)*8], buf[8:])
		}
	}

	// Integrity check: after fully unwrapping, A must equal DefaultIV.
	// If it doesn't, the ciphertext was tampered with or the wrong KEK
	// was used.
	if a != DefaultIV {
		return nil, ErrIntegrityCheck
	}

	return r, nil
}

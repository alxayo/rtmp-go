package crypto

import (
	"crypto/pbkdf2"
	"crypto/sha1"
)

// srtPBKDF2Iterations is the number of PBKDF2 iterations used by SRT.
// The SRT specification mandates 2048 iterations for key derivation.
const srtPBKDF2Iterations = 2048

// DeriveKey uses PBKDF2-HMAC-SHA1 to derive a cryptographic key from a
// human-readable passphrase. This is how SRT converts a user-provided
// passphrase into an AES encryption key.
//
// Parameters:
//   - passphrase: the user-provided secret string (e.g., "my-stream-key").
//   - salt: random bytes that make each derivation unique, preventing
//     precomputed dictionary attacks. In SRT, this comes from the key
//     material exchange during the handshake.
//   - keyLen: desired key length in bytes. Use 16 for AES-128, 24 for
//     AES-192, or 32 for AES-256.
//
// Returns: a derived key of exactly keyLen bytes, suitable for use as an
// AES encryption key.
//
// PBKDF2 works by repeatedly applying HMAC-SHA1 to the passphrase and salt,
// making brute-force attacks much slower than hashing the passphrase once.
func DeriveKey(passphrase string, salt []byte, keyLen int) ([]byte, error) {
	return pbkdf2.Key(sha1.New, passphrase, salt, srtPBKDF2Iterations, keyLen)
}

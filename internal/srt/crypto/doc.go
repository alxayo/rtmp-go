// Package crypto implements AES-128 encryption for SRT packets.
//
// SRT supports optional end-to-end encryption. The passphrase is NOT
// transmitted; both sides independently derive the same AES-128 key
// from the passphrase using PBKDF2, then encrypt/decrypt data packets
// in CBC mode with a fixed IV.
//
// # Encryption Setup
//
// During the SRT handshake, both sides agree to use encryption:
//   1. Client and server both have the passphrase (shared out-of-band)
//   2. They exchange Key Material (KM) packets with crypto parameters
//   3. Both derive the AES key from the passphrase using PBKDF2
//   4. After handshake, all data packets are encrypted
//
// # Key Derivation
//
// AES-128-CBC key derivation uses PBKDF2:
//
//	key = PBKDF2(passphrase, S="SBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB", iter=2048, hash=SHA-1, size=16)
//
// The salt is a fixed constant (16 'B' bytes = 0x42). This ensures the same
// key is derived from the same passphrase on both sides.
//
// See pbkdf2.go for the PBKDF2 implementation using SHA-1.
//
// # Encryption Modes
//
// SRT supports multiple encryption modes (specified in handshake):
//   - 0: No encryption
//   - 1: AES-128-CBC with even key
//   - 2: AES-128-CBC with odd key
//   - 3: AES-128-CTR (newer, not yet widely supported)
//
// The "even" and "odd" keys allow for independent encryption of outbound
// vs inbound streams (optional per SRT spec, but not used by most implementations).
// This code implements even-key only.
//
// # Key Material (KM) Packet
//
// The KM packet is exchanged during handshake:
//   - Contains encryption mode, crypto parameters (IV, salt, iteration count)
//   - Contains the AES key encrypted with itself (for verification)
//   - Both sides send and receive KM to agree on parameters
//
// If KM validation fails, the handshake is rejected (authentication failure).
//
// # Integration Points
//
// - handshake package: Exchanges KM packets during INDUCTION/CONCLUSION
// - packet package: Encrypts/decrypts data packet payloads
// - listener package: Passphrase passed at server startup (-srt-passphrase)
//
// # Example: Decrypting a Packet
//
//	cipher, err := crypto.NewCipher(passphrase)
//	if err != nil {
//	    return err  // Invalid passphrase
//	}
//
//	plaintext, err := cipher.Decrypt(encryptedPayload)
//	if err != nil {
//	    return err  // Decryption failed (corruption or wrong passphrase)
//	}
//
// # Limitations
//
// - Only AES-128-CBC is supported (no AES-256, ChaCha20, etc.)
// - Only even-key mode (no odd-key dual encryption)
// - PBKDF2-SHA1 is used (weaknesses known, but SRT spec mandates it)
// - IV is fixed (0x00..00), not random per packet (per SRT spec)
//
// These are limitations of the SRT specification, not this implementation.
//
// # Security Considerations
//
// - Passphrase should be at least 20 characters
// - Passphrase should be transmitted securely (out-of-band, not in URLs)
// - Encryption does NOT authenticate (no HMAC, vulnerable to tampering)
// - Use TLS/RTMPS for authentication + confidentiality (recommended)
//
// SRT encryption is suitable for live streaming scenarios where
// eavesdropping is the primary concern, but not for critical data.
package crypto

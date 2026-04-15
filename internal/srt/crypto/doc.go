// Package crypto implements AES-CTR encryption for SRT data packets.
//
// SRT supports optional end-to-end encryption of data packet payloads.
// During the handshake, the caller sends a Key Material (KM) request
// containing a random salt and a wrapped Stream Encrypting Key (SEK).
// The listener unwraps the SEK using a Key Encrypting Key (KEK) derived
// from a shared passphrase via PBKDF2-SHA1, then both sides use AES-CTR
// to encrypt and decrypt every data packet for the connection's lifetime.
//
// # Key Sizes
//
// The caller chooses the AES key size. Supported sizes:
//   - AES-128 (16-byte SEK)
//   - AES-192 (24-byte SEK)
//   - AES-256 (32-byte SEK)
//
// # Encryption Flow
//
//  1. Caller generates a random 16-byte salt and a random SEK
//  2. Caller derives KEK = PBKDF2(passphrase, salt, 2048, SHA-1, keyLen)
//  3. Caller wraps SEK with KEK using AES Key Wrap (RFC 3394)
//  4. Caller sends KMREQ extension in the Conclusion handshake packet
//  5. Listener derives the same KEK, unwraps SEK, sends KMRSP
//  6. Both sides create a PacketCipher from the SEK and salt
//  7. All data packets are encrypted/decrypted with AES-CTR
//
// # AES-CTR Counter Construction
//
// Each packet gets a unique 16-byte counter:
//
//	Bytes  0–13: IV (upper 112 bits of the 128-bit salt)
//	Bytes 10–13: XORed with the packet's sequence number (big-endian)
//	Bytes 14–15: Block counter (starts at 0, incremented per AES block)
//
// Because CTR mode generates a keystream XORed with the payload,
// encryption and decryption are the same operation.
//
// # Integration Points
//
//   - handshake package: Exchanges KMREQ/KMRSP during CONCLUSION
//   - conn package: ConnConfig carries the PacketCipher; handleDataPacket
//     decrypts incoming payloads before delivery
//   - listener package: Creates PacketCipher from HandshakeResult
//
// # Security Considerations
//
//   - Passphrase should be at least 10 characters (SRT minimum)
//   - Each connection gets a unique random salt (no salt reuse)
//   - AES-CTR provides confidentiality but not authentication
//   - PBKDF2-SHA1 is mandated by the SRT specification
package crypto

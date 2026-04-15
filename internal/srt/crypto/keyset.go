package crypto

import (
	"fmt"
	"sync"
)

// KeySet holds two PacketCipher slots — "even" and "odd" — for SRT's hitless
// key rotation mechanism.
//
// # Why Two Keys?
//
// SRT data packets carry a 2-bit KK (Key Key) flag in their header:
//
//	KK = 0b00 → payload is not encrypted (plaintext)
//	KK = 0b01 → payload is encrypted with the "even" key
//	KK = 0b10 → payload is encrypted with the "odd" key
//
// During normal streaming, all packets use one key (say, even). When it's time
// to rotate keys (rekey), the sender:
//
//  1. Generates a new SEK for the other slot (odd).
//  2. Sends a KMREQ announcing the new key to the receiver.
//  3. Once the receiver acknowledges, the sender starts encrypting new packets
//     with the odd key (flips KK from 0b01 to 0b10).
//  4. After a grace period, the old even key is retired.
//
// During step 3, packets encrypted with the old even key may still be in flight
// (due to retransmissions or network delay). The receiver needs both keys
// available simultaneously to decrypt everything. This is what KeySet provides.
//
// # Thread Safety
//
// KeySet is safe for concurrent use. Decryption (reads) acquires a read lock,
// and key installation (writes) acquires a write lock. This allows multiple
// goroutines to decrypt packets in parallel while key rotation is serialized.
type KeySet struct {
	even *PacketCipher // Cipher for KK=0b01 (even key slot)
	odd  *PacketCipher // Cipher for KK=0b10 (odd key slot)
	mu   sync.RWMutex  // Protects both cipher slots
}

// NewKeySet creates an empty KeySet with no keys installed. Both slots are nil.
// Call InstallKey to add keys before attempting decryption.
func NewKeySet() *KeySet {
	return &KeySet{}
}

// NewKeySetFromSEK creates a KeySet and installs one or both keys based on the
// KK flag from a KM message.
//
// The kk parameter indicates which keys are provided:
//   - KKEven (0x01): sek contains a single key for the even slot.
//   - KKOdd  (0x02): sek contains a single key for the odd slot.
//   - KKBoth (0x03): sek contains both keys concatenated — even key first
//     (sek[:keyLen]), then odd key (sek[keyLen:2*keyLen]).
//
// The salt must be exactly 16 bytes (as required by NewPacketCipher).
// The keyLen parameter specifies the length of each individual key (16, 24, or 32).
func NewKeySetFromSEK(kk uint8, sek, salt []byte, keyLen int) (*KeySet, error) {
	ks := NewKeySet()
	if err := ks.installKeyLocked(kk, sek, salt, keyLen); err != nil {
		return nil, err
	}
	return ks, nil
}

// InstallKey installs one or both keys into the KeySet. This is used during
// key rotation to add a new key while the old one is still in use.
//
// The kk parameter follows the same convention as NewKeySetFromSEK:
//   - KKEven (0x01): install sek as the even key (replaces any existing even key).
//   - KKOdd  (0x02): install sek as the odd key (replaces any existing odd key).
//   - KKBoth (0x03): sek contains both keys concatenated; replaces both slots.
//
// InstallKey acquires a write lock to prevent concurrent decryption from seeing
// a partially updated key pair.
func (ks *KeySet) InstallKey(kk uint8, sek, salt []byte, keyLen int) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	return ks.installKeyLocked(kk, sek, salt, keyLen)
}

// installKeyLocked is the shared implementation for NewKeySetFromSEK and
// InstallKey. The caller must hold ks.mu (or be in a constructor where
// no other goroutine has access yet).
func (ks *KeySet) installKeyLocked(kk uint8, sek, salt []byte, keyLen int) error {
	switch kk {
	case KKEven:
		// Single key for the even slot.
		if len(sek) < keyLen {
			return fmt.Errorf("keyset: sek length %d is shorter than keyLen %d", len(sek), keyLen)
		}
		cipher, err := NewPacketCipher(sek[:keyLen], salt)
		if err != nil {
			return fmt.Errorf("keyset: even key: %w", err)
		}
		ks.even = cipher

	case KKOdd:
		// Single key for the odd slot.
		if len(sek) < keyLen {
			return fmt.Errorf("keyset: sek length %d is shorter than keyLen %d", len(sek), keyLen)
		}
		cipher, err := NewPacketCipher(sek[:keyLen], salt)
		if err != nil {
			return fmt.Errorf("keyset: odd key: %w", err)
		}
		ks.odd = cipher

	case KKBoth:
		// Both keys concatenated: even key is sek[:keyLen], odd is sek[keyLen:2*keyLen].
		requiredLen := 2 * keyLen
		if len(sek) < requiredLen {
			return fmt.Errorf("keyset: KKBoth requires %d bytes of SEK data (2 × %d), got %d",
				requiredLen, keyLen, len(sek))
		}
		evenCipher, err := NewPacketCipher(sek[:keyLen], salt)
		if err != nil {
			return fmt.Errorf("keyset: even key: %w", err)
		}
		oddCipher, err := NewPacketCipher(sek[keyLen:2*keyLen], salt)
		if err != nil {
			return fmt.Errorf("keyset: odd key: %w", err)
		}
		ks.even = evenCipher
		ks.odd = oddCipher

	default:
		return fmt.Errorf("keyset: invalid KK flag 0x%02X (expected 0x01, 0x02, or 0x03)", kk)
	}

	return nil
}

// DecryptPayload decrypts a data packet payload in-place using the cipher
// corresponding to the packet's encryption flag.
//
// The encFlag should come from the data packet's KK field:
//   - 0b01 (EncryptionEven) → decrypt with the even key
//   - 0b10 (EncryptionOdd)  → decrypt with the odd key
//
// Returns an error if the requested key slot is not installed.
// A nil or empty payload is a no-op (delegated to PacketCipher).
func (ks *KeySet) DecryptPayload(payload []byte, packetIndex uint32, encFlag uint8) error {
	// Empty payload is a no-op regardless of which key is selected.
	if len(payload) == 0 {
		return nil
	}

	ks.mu.RLock()
	defer ks.mu.RUnlock()

	switch encFlag {
	case encryptionEven: // 0b01 — even key
		if ks.even == nil {
			return fmt.Errorf("keyset: even key not installed")
		}
		return ks.even.DecryptPayload(payload, packetIndex)

	case encryptionOdd: // 0b10 — odd key
		if ks.odd == nil {
			return fmt.Errorf("keyset: odd key not installed")
		}
		return ks.odd.DecryptPayload(payload, packetIndex)

	default:
		return fmt.Errorf("keyset: invalid encryption flag 0x%02X (expected 0x01 or 0x02)", encFlag)
	}
}

// HasKeys reports whether at least one key slot (even or odd) is populated.
// This is useful to check if the KeySet is ready for decryption before
// receiving the first encrypted packet.
func (ks *KeySet) HasKeys() bool {
	ks.mu.RLock()
	defer ks.mu.RUnlock()
	return ks.even != nil || ks.odd != nil
}

// Encryption flag values matching the KK field in SRT data packet headers.
// These mirror packet.EncryptionEven and packet.EncryptionOdd, defined here
// to avoid a circular import between the crypto and packet packages.
const (
	encryptionEven = 0x01 // Even key slot (packet.EncryptionEven)
	encryptionOdd  = 0x02 // Odd key slot  (packet.EncryptionOdd)
)

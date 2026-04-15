package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"fmt"
)

// PacketCipher handles AES-CTR encryption and decryption of SRT data packet
// payloads. Each SRT connection that uses encryption gets one PacketCipher
// instance, initialized with the Stream Encrypting Key (SEK) and salt
// negotiated during the handshake.
//
// SRT uses standard AES-CTR mode with a specially constructed counter:
//
//	 0                                                               127
//	 +---- IV (112 bits) ----+-- packet_index (32 bits) --+- bctr (16) -+
//	 |<----  14 bytes  ----->|<-------  4 bytes  --------->|<- 2 bytes ->|
//
// The IV is the upper 112 bits (14 bytes) of the 128-bit salt exchanged in
// the KM message. The packet_index is the data packet's sequence number,
// XORed into bytes 10–13 of the counter. Bytes 14–15 are the block counter,
// which starts at 0 and is incremented automatically by Go's CTR mode for
// each 16-byte AES block within the packet.
//
// Because AES-CTR generates a keystream that is XORed with the payload,
// encryption and decryption are the same operation.
type PacketCipher struct {
	block cipher.Block // AES cipher block (pre-created from SEK)
	iv    [14]byte     // Upper 112 bits of salt (fixed for connection lifetime)
}

// NewPacketCipher creates a cipher from the SEK (Stream Encrypting Key) and
// salt. The SEK must be 16, 24, or 32 bytes (AES-128, AES-192, or AES-256).
// The salt must be exactly 16 bytes; only the upper 14 bytes (112 bits) are
// used as the IV.
func NewPacketCipher(sek, salt []byte) (*PacketCipher, error) {
	// Validate SEK length — AES only accepts 16, 24, or 32 byte keys.
	k := len(sek)
	if k != 16 && k != 24 && k != 32 {
		return nil, fmt.Errorf("packet cipher: invalid SEK length %d (must be 16, 24, or 32)", k)
	}

	// Salt must be exactly 16 bytes (128 bits) as defined by SRT.
	if len(salt) != 16 {
		return nil, fmt.Errorf("packet cipher: invalid salt length %d (must be 16)", len(salt))
	}

	// Create the AES cipher block once; it will be reused for every packet.
	block, err := aes.NewCipher(sek)
	if err != nil {
		return nil, fmt.Errorf("packet cipher: %w", err)
	}

	pc := &PacketCipher{
		block: block,
	}

	// Copy the upper 112 bits (14 bytes) of the salt into the IV.
	copy(pc.iv[:], salt[:14])

	return pc, nil
}

// EncryptPayload encrypts a data packet payload in-place using AES-CTR.
// packetIndex is the packet's 31-bit sequence number, used to construct
// the per-packet counter so that each packet produces a unique keystream.
//
// A nil or empty payload is a no-op (returns nil).
func (c *PacketCipher) EncryptPayload(payload []byte, packetIndex uint32) error {
	return c.xorPayload(payload, packetIndex)
}

// DecryptPayload decrypts a data packet payload in-place using AES-CTR.
// This is identical to EncryptPayload (AES-CTR is symmetric), but having
// a separate method makes the calling code's intent clearer.
//
// A nil or empty payload is a no-op (returns nil).
func (c *PacketCipher) DecryptPayload(payload []byte, packetIndex uint32) error {
	return c.xorPayload(payload, packetIndex)
}

// xorPayload is the shared implementation for EncryptPayload and
// DecryptPayload. It builds the per-packet AES-CTR counter, creates a
// CTR stream, and XORs it with the payload in-place.
func (c *PacketCipher) xorPayload(payload []byte, packetIndex uint32) error {
	// Nothing to do for nil or empty payloads.
	if len(payload) == 0 {
		return nil
	}

	// Build the 16-byte initial counter value for this packet.
	//
	// Layout:
	//   Bytes  0–13: IV (upper 112 bits of salt)
	//   Bytes 10–13: XORed with packetIndex (big-endian uint32)
	//   Bytes 14–15: 0x0000 (block counter, CTR mode increments this)
	var ctr [16]byte
	copy(ctr[0:14], c.iv[:])

	// XOR the packet index into bytes 10–13. This overlaps the last 4 bytes
	// of the IV region, ensuring each packet produces a unique keystream
	// even though the IV itself is fixed for the connection.
	ctr[10] ^= byte(packetIndex >> 24)
	ctr[11] ^= byte(packetIndex >> 16)
	ctr[12] ^= byte(packetIndex >> 8)
	ctr[13] ^= byte(packetIndex)

	// Bytes 14–15 are already zero (block counter starts at 0). Go's
	// cipher.NewCTR will increment these automatically for each 16-byte
	// AES block within the payload.

	// Create a CTR stream and XOR it with the payload in-place.
	stream := cipher.NewCTR(c.block, ctr[:])
	stream.XORKeyStream(payload, payload)

	return nil
}

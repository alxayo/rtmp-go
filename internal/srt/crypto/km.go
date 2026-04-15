package crypto

import (
	"encoding/binary"
	"fmt"
)

// KM message constants define the fixed values required by the SRT Key Material
// protocol (SRT RFC Section 3.2.2). These are validated during parsing and set
// during marshaling.
const (
	// KMSignature is the "HAI" PnP Vendor ID (0x2029) that identifies a valid
	// Key Material message. It occupies bytes 1-2 of the KM header.
	KMSignature = 0x2029

	// KMVersion is the protocol version for Key Material messages. Currently
	// only version 1 is defined.
	KMVersion = 1

	// KMPacketType identifies this as a Key Material message (type 2).
	KMPacketType = 2

	// KMHeaderLen is the fixed portion of the KM message before salt and
	// wrapped key data: 4 bytes (flags) + 4 bytes (KEKI) + 4 bytes (cipher/
	// auth/SE/resv) + 4 bytes (resv3/SLen/KLen) = 16 bytes.
	KMHeaderLen = 16
)

// Cipher type constants identify the encryption algorithm used for media data.
const (
	// CipherNone indicates no encryption (plaintext).
	CipherNone = 0

	// CipherAESCTR indicates AES in Counter mode, the standard cipher for SRT.
	CipherAESCTR = 2
)

// Stream Encapsulation (SE) constants identify the payload format.
const (
	// SELiveSRT indicates MPEG-TS over SRT live mode.
	SELiveSRT = 2
)

// Key flag (KK) constants indicate which Stream Encrypting Keys are present
// in the KM message. SRT supports even/odd key rotation for hitless rekeying.
const (
	// KKEven means only the even SEK is present in the wrapped key data.
	KKEven = 0x01

	// KKOdd means only the odd SEK is present in the wrapped key data.
	KKOdd = 0x02

	// KKBoth means both even and odd SEKs are present in the wrapped key data.
	KKBoth = 0x03
)

// KMMsg represents a parsed SRT Key Material message. This message is exchanged
// during the SRT handshake (as KMREQ from caller, KMRSP from listener) to
// carry the wrapped Stream Encrypting Key(s) and encryption parameters.
//
// Wire format (SRT RFC Section 3.2.2):
//
//	Byte 0:     [S(1 bit)][Version(3 bits)][PacketType(4 bits)]
//	Bytes 1-2:  Signature (0x2029, big-endian)
//	Byte 3:     [Reserved1(6 bits)][KK(2 bits)]
//	Bytes 4-7:  KEKI (Key Encryption Key Index, big-endian uint32)
//	Byte 8:     Cipher type
//	Byte 9:     Auth type
//	Byte 10:    Stream Encapsulation (SE)
//	Byte 11:    Reserved2
//	Bytes 12-13: Reserved3
//	Byte 14:    SLen/4 (salt length in 4-byte units)
//	Byte 15:    KLen/4 (key length in 4-byte units)
//	Variable:   Salt (SLen bytes)
//	Variable:   Wrapped key(s) (8 + n*KLen bytes, where n = number of keys)
type KMMsg struct {
	// Version is the KM protocol version. Must be 1.
	Version uint8

	// PacketType identifies this as a KM message. Must be 2.
	PacketType uint8

	// Sign is the PnP Vendor ID signature. Must be 0x2029.
	Sign uint16

	// KK indicates which Stream Encrypting Keys are carried:
	// 0x01 = even only, 0x02 = odd only, 0x03 = both.
	KK uint8

	// KEKI is the Key Encryption Key Index. 0 means the default passphrase-
	// derived key is used (the common case for SRT).
	KEKI uint32

	// Cipher identifies the encryption algorithm. 0 = none, 2 = AES-CTR.
	Cipher uint8

	// Auth identifies the authentication algorithm. 0 = none (SRT does not
	// currently define any authentication beyond key wrap integrity).
	Auth uint8

	// SE is the Stream Encapsulation type. 2 = MPEG-TS/SRT live mode.
	SE uint8

	// SLen is the salt length in bytes (not the /4 wire encoding). Must be 16.
	SLen uint16

	// KLen is the individual key length in bytes (not the /4 wire encoding).
	// Valid values: 16 (AES-128), 24 (AES-192), 32 (AES-256).
	KLen uint16

	// Salt is the random salt used for PBKDF2 key derivation. Typically 16 bytes.
	Salt []byte

	// WrappedKey is the AES Key Wrap (RFC 3394) ciphertext containing the
	// Stream Encrypting Key(s). Its length is 8 + n*KLen where n is the
	// number of keys (1 for KK=01/10, 2 for KK=11).
	WrappedKey []byte
}

// ParseKMMsg parses a raw byte slice into a KMMsg. It validates all fixed
// fields, checks consistency between declared and actual sizes, and returns
// a descriptive error if any field is invalid.
func ParseKMMsg(data []byte) (*KMMsg, error) {
	// The header alone is 16 bytes. We need at least that to read any fields.
	if len(data) < KMHeaderLen {
		return nil, fmt.Errorf("km: message too short (%d bytes, minimum %d)", len(data), KMHeaderLen)
	}

	km := &KMMsg{}

	// --- Byte 0: S(1) | Version(3) | PacketType(4) ---
	// S is the high bit — reserved, must be 0.
	// Version is the next 3 bits: (byte0 >> 4) & 0x07.
	// PacketType is the low 4 bits: byte0 & 0x0F.
	byte0 := data[0]
	if byte0&0x80 != 0 {
		return nil, fmt.Errorf("km: reserved S bit must be 0, got 1")
	}
	km.Version = (byte0 >> 4) & 0x07
	km.PacketType = byte0 & 0x0F

	if km.Version != KMVersion {
		return nil, fmt.Errorf("km: unsupported version %d (expected %d)", km.Version, KMVersion)
	}
	if km.PacketType != KMPacketType {
		return nil, fmt.Errorf("km: unexpected packet type %d (expected %d)", km.PacketType, KMPacketType)
	}

	// --- Bytes 1-2: Signature (big-endian uint16) ---
	km.Sign = binary.BigEndian.Uint16(data[1:3])
	if km.Sign != KMSignature {
		return nil, fmt.Errorf("km: invalid signature 0x%04X (expected 0x%04X)", km.Sign, KMSignature)
	}

	// --- Byte 3: Reserved1(6) | KK(2) ---
	km.KK = data[3] & 0x03
	if km.KK == 0 {
		return nil, fmt.Errorf("km: KK field is 0 (no keys present)")
	}

	// --- Bytes 4-7: KEKI (big-endian uint32) ---
	km.KEKI = binary.BigEndian.Uint32(data[4:8])

	// --- Byte 8: Cipher ---
	km.Cipher = data[8]
	if km.Cipher != CipherNone && km.Cipher != CipherAESCTR {
		return nil, fmt.Errorf("km: unsupported cipher type %d (expected 0 or 2)", km.Cipher)
	}

	// --- Byte 9: Auth ---
	km.Auth = data[9]

	// --- Byte 10: SE ---
	km.SE = data[10]

	// --- Byte 11: Reserved2 (skip) ---

	// --- Bytes 12-13: Reserved3 (skip) ---

	// --- Byte 14: SLen/4 (salt length in 4-byte units) ---
	slenDiv4 := data[14]
	if slenDiv4 != 4 {
		return nil, fmt.Errorf("km: unsupported SLen/4 = %d (expected 4 for 16-byte salt)", slenDiv4)
	}
	km.SLen = uint16(slenDiv4) * 4

	// --- Byte 15: KLen/4 (key length in 4-byte units) ---
	klenDiv4 := data[15]
	if klenDiv4 != 4 && klenDiv4 != 6 && klenDiv4 != 8 {
		return nil, fmt.Errorf("km: unsupported KLen/4 = %d (expected 4, 6, or 8)", klenDiv4)
	}
	km.KLen = uint16(klenDiv4) * 4

	// Determine how many keys are in the wrapped data (1 or 2).
	numKeys := 1
	if km.KK == KKBoth {
		numKeys = 2
	}

	// Wrapped key size = 8-byte AES Key Wrap overhead + n * KLen.
	wrappedLen := 8 + numKeys*int(km.KLen)
	expectedTotal := KMHeaderLen + int(km.SLen) + wrappedLen

	if len(data) < expectedTotal {
		return nil, fmt.Errorf("km: message too short for declared sizes (%d bytes, expected %d)", len(data), expectedTotal)
	}

	// --- Salt: starts at byte 16, length = SLen ---
	saltStart := KMHeaderLen
	km.Salt = make([]byte, km.SLen)
	copy(km.Salt, data[saltStart:saltStart+int(km.SLen)])

	// --- Wrapped Key: starts after salt, length = wrappedLen ---
	wrapStart := saltStart + int(km.SLen)
	km.WrappedKey = make([]byte, wrappedLen)
	copy(km.WrappedKey, data[wrapStart:wrapStart+wrappedLen])

	return km, nil
}

// Marshal serializes a KMMsg into its binary wire format. The caller must
// ensure all fields are set correctly before calling Marshal; this method
// encodes values as-is without re-validating them.
func (km *KMMsg) Marshal() ([]byte, error) {
	// Validate that salt length matches SLen.
	if len(km.Salt) != int(km.SLen) {
		return nil, fmt.Errorf("km: salt length %d does not match SLen %d", len(km.Salt), km.SLen)
	}

	// Validate KK is non-zero so we can compute the expected wrapped key size.
	if km.KK == 0 {
		return nil, fmt.Errorf("km: KK field is 0 (no keys present)")
	}

	numKeys := 1
	if km.KK == KKBoth {
		numKeys = 2
	}
	expectedWrapLen := 8 + numKeys*int(km.KLen)
	if len(km.WrappedKey) != expectedWrapLen {
		return nil, fmt.Errorf("km: wrapped key length %d does not match expected %d (8 + %d*%d)",
			len(km.WrappedKey), expectedWrapLen, numKeys, km.KLen)
	}

	totalLen := KMHeaderLen + int(km.SLen) + len(km.WrappedKey)
	buf := make([]byte, totalLen)

	// --- Byte 0: S(1) | Version(3) | PacketType(4) ---
	// S is always 0, so the high bit is clear.
	buf[0] = (km.Version&0x07)<<4 | (km.PacketType & 0x0F)

	// --- Bytes 1-2: Signature ---
	binary.BigEndian.PutUint16(buf[1:3], km.Sign)

	// --- Byte 3: Reserved1(6) | KK(2) ---
	buf[3] = km.KK & 0x03

	// --- Bytes 4-7: KEKI ---
	binary.BigEndian.PutUint32(buf[4:8], km.KEKI)

	// --- Byte 8: Cipher ---
	buf[8] = km.Cipher

	// --- Byte 9: Auth ---
	buf[9] = km.Auth

	// --- Byte 10: SE ---
	buf[10] = km.SE

	// --- Byte 11: Reserved2 = 0 (already zero from make) ---
	// --- Bytes 12-13: Reserved3 = 0 (already zero from make) ---

	// --- Byte 14: SLen/4 ---
	buf[14] = byte(km.SLen / 4)

	// --- Byte 15: KLen/4 ---
	buf[15] = byte(km.KLen / 4)

	// --- Salt ---
	copy(buf[KMHeaderLen:], km.Salt)

	// --- Wrapped Key ---
	copy(buf[KMHeaderLen+int(km.SLen):], km.WrappedKey)

	return buf, nil
}

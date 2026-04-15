package crypto

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

// buildValidKMMsg constructs a minimal valid KM message in raw bytes for testing.
// It creates a message with:
//   - Version=1, PacketType=2, Signature=0x2029
//   - The specified KK, cipher, key length, salt, and wrapped key data
//
// This helper keeps test cases clean by handling the bit-packing details.
func buildValidKMMsg(t *testing.T, kk, cipher uint8, klenDiv4 uint8, salt, wrappedKey []byte) []byte {
	t.Helper()

	totalLen := KMHeaderLen + len(salt) + len(wrappedKey)
	buf := make([]byte, totalLen)

	// Byte 0: S=0 | Version=1 (3 bits) | PacketType=2 (4 bits) → 0x12
	buf[0] = (KMVersion << 4) | KMPacketType

	// Bytes 1-2: Signature
	binary.BigEndian.PutUint16(buf[1:3], KMSignature)

	// Byte 3: Resv1=0 | KK
	buf[3] = kk & 0x03

	// Bytes 4-7: KEKI = 0
	// (already zero from make)

	// Byte 8: Cipher
	buf[8] = cipher

	// Byte 9: Auth = 0
	// Byte 10: SE = 2 (live SRT)
	buf[10] = SELiveSRT

	// Byte 11: Reserved2 = 0
	// Bytes 12-13: Reserved3 = 0

	// Byte 14: SLen/4
	buf[14] = byte(len(salt) / 4)

	// Byte 15: KLen/4
	buf[15] = klenDiv4

	// Salt
	copy(buf[KMHeaderLen:], salt)

	// Wrapped key
	copy(buf[KMHeaderLen+len(salt):], wrappedKey)

	return buf
}

// TestParseKMMsg_EvenKey_AES128 verifies parsing of a hand-crafted KM message
// carrying a single even key with AES-128 encryption.
func TestParseKMMsg_EvenKey_AES128(t *testing.T) {
	// 16-byte salt (all 0xAA for easy identification in hex dumps).
	salt := bytes.Repeat([]byte{0xAA}, 16)

	// Wrapped key for a single AES-128 key: 8 (overhead) + 16 (key) = 24 bytes.
	wrappedKey := make([]byte, 24)
	for i := range wrappedKey {
		wrappedKey[i] = byte(i + 1)
	}

	data := buildValidKMMsg(t, KKEven, CipherAESCTR, 4, salt, wrappedKey)

	km, err := ParseKMMsg(data)
	if err != nil {
		t.Fatalf("ParseKMMsg() unexpected error: %v", err)
	}

	// Verify every field was parsed correctly.
	if km.Version != KMVersion {
		t.Errorf("Version = %d, want %d", km.Version, KMVersion)
	}
	if km.PacketType != KMPacketType {
		t.Errorf("PacketType = %d, want %d", km.PacketType, KMPacketType)
	}
	if km.Sign != KMSignature {
		t.Errorf("Sign = 0x%04X, want 0x%04X", km.Sign, KMSignature)
	}
	if km.KK != KKEven {
		t.Errorf("KK = %d, want %d", km.KK, KKEven)
	}
	if km.KEKI != 0 {
		t.Errorf("KEKI = %d, want 0", km.KEKI)
	}
	if km.Cipher != CipherAESCTR {
		t.Errorf("Cipher = %d, want %d", km.Cipher, CipherAESCTR)
	}
	if km.Auth != 0 {
		t.Errorf("Auth = %d, want 0", km.Auth)
	}
	if km.SE != SELiveSRT {
		t.Errorf("SE = %d, want %d", km.SE, SELiveSRT)
	}
	if km.SLen != 16 {
		t.Errorf("SLen = %d, want 16", km.SLen)
	}
	if km.KLen != 16 {
		t.Errorf("KLen = %d, want 16", km.KLen)
	}
	if !bytes.Equal(km.Salt, salt) {
		t.Errorf("Salt mismatch\n  got:  %X\n  want: %X", km.Salt, salt)
	}
	if !bytes.Equal(km.WrappedKey, wrappedKey) {
		t.Errorf("WrappedKey mismatch\n  got:  %X\n  want: %X", km.WrappedKey, wrappedKey)
	}
}

// TestParseKMMsg_BothKeys verifies parsing when KK=0x03 (both even and odd
// keys present). The wrapped key section should be 8 + 2*KLen bytes.
func TestParseKMMsg_BothKeys(t *testing.T) {
	salt := bytes.Repeat([]byte{0xBB}, 16)

	// Both keys with AES-128: 8 (overhead) + 2*16 (two keys) = 40 bytes.
	wrappedKey := make([]byte, 40)
	for i := range wrappedKey {
		wrappedKey[i] = byte(i + 0x10)
	}

	data := buildValidKMMsg(t, KKBoth, CipherAESCTR, 4, salt, wrappedKey)

	km, err := ParseKMMsg(data)
	if err != nil {
		t.Fatalf("ParseKMMsg() unexpected error: %v", err)
	}

	if km.KK != KKBoth {
		t.Errorf("KK = %d, want %d", km.KK, KKBoth)
	}
	if !bytes.Equal(km.WrappedKey, wrappedKey) {
		t.Errorf("WrappedKey mismatch\n  got:  %X\n  want: %X", km.WrappedKey, wrappedKey)
	}
	if len(km.WrappedKey) != 40 {
		t.Errorf("WrappedKey length = %d, want 40", len(km.WrappedKey))
	}
}

// TestParseKMMsg_AES192 verifies parsing with AES-192 key length (KLen/4 = 6).
func TestParseKMMsg_AES192(t *testing.T) {
	salt := bytes.Repeat([]byte{0xCC}, 16)

	// Single key, AES-192: 8 + 24 = 32 bytes wrapped.
	wrappedKey := make([]byte, 32)
	for i := range wrappedKey {
		wrappedKey[i] = byte(i)
	}

	data := buildValidKMMsg(t, KKEven, CipherAESCTR, 6, salt, wrappedKey)

	km, err := ParseKMMsg(data)
	if err != nil {
		t.Fatalf("ParseKMMsg() unexpected error: %v", err)
	}

	if km.KLen != 24 {
		t.Errorf("KLen = %d, want 24", km.KLen)
	}
}

// TestParseKMMsg_AES256 verifies parsing with AES-256 key length (KLen/4 = 8).
func TestParseKMMsg_AES256(t *testing.T) {
	salt := bytes.Repeat([]byte{0xDD}, 16)

	// Single key, AES-256: 8 + 32 = 40 bytes wrapped.
	wrappedKey := make([]byte, 40)
	for i := range wrappedKey {
		wrappedKey[i] = byte(i)
	}

	data := buildValidKMMsg(t, KKOdd, CipherAESCTR, 8, salt, wrappedKey)

	km, err := ParseKMMsg(data)
	if err != nil {
		t.Fatalf("ParseKMMsg() unexpected error: %v", err)
	}

	if km.KLen != 32 {
		t.Errorf("KLen = %d, want 32", km.KLen)
	}
	if km.KK != KKOdd {
		t.Errorf("KK = %d, want %d", km.KK, KKOdd)
	}
}

// TestKMMsgRoundTrip verifies that building a KMMsg, marshaling it, then
// parsing it back produces an identical message. This is the most important
// test — it proves Marshal and ParseKMMsg are inverse operations.
func TestKMMsgRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		kk      uint8
		cipher  uint8
		klen    uint16 // key length in bytes
		numKeys int    // how many keys (derived from KK, but explicit for clarity)
	}{
		{"even key AES-128", KKEven, CipherAESCTR, 16, 1},
		{"odd key AES-128", KKOdd, CipherAESCTR, 16, 1},
		{"both keys AES-128", KKBoth, CipherAESCTR, 16, 2},
		{"even key AES-192", KKEven, CipherAESCTR, 24, 1},
		{"both keys AES-192", KKBoth, CipherAESCTR, 24, 2},
		{"even key AES-256", KKEven, CipherAESCTR, 32, 1},
		{"both keys AES-256", KKBoth, CipherAESCTR, 32, 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Build a KMMsg with deterministic test data.
			salt := make([]byte, 16)
			for i := range salt {
				salt[i] = byte(i + 0x50)
			}
			wrappedLen := 8 + tc.numKeys*int(tc.klen)
			wrappedKey := make([]byte, wrappedLen)
			for i := range wrappedKey {
				wrappedKey[i] = byte(i + 0xA0)
			}

			original := &KMMsg{
				Version:    KMVersion,
				PacketType: KMPacketType,
				Sign:       KMSignature,
				KK:         tc.kk,
				KEKI:       0,
				Cipher:     tc.cipher,
				Auth:       0,
				SE:         SELiveSRT,
				SLen:       16,
				KLen:       tc.klen,
				Salt:       salt,
				WrappedKey: wrappedKey,
			}

			// Marshal to bytes.
			data, err := original.Marshal()
			if err != nil {
				t.Fatalf("Marshal() error: %v", err)
			}

			// Parse back from bytes.
			parsed, err := ParseKMMsg(data)
			if err != nil {
				t.Fatalf("ParseKMMsg() error: %v", err)
			}

			// Compare all fields.
			if parsed.Version != original.Version {
				t.Errorf("Version: got %d, want %d", parsed.Version, original.Version)
			}
			if parsed.PacketType != original.PacketType {
				t.Errorf("PacketType: got %d, want %d", parsed.PacketType, original.PacketType)
			}
			if parsed.Sign != original.Sign {
				t.Errorf("Sign: got 0x%04X, want 0x%04X", parsed.Sign, original.Sign)
			}
			if parsed.KK != original.KK {
				t.Errorf("KK: got %d, want %d", parsed.KK, original.KK)
			}
			if parsed.KEKI != original.KEKI {
				t.Errorf("KEKI: got %d, want %d", parsed.KEKI, original.KEKI)
			}
			if parsed.Cipher != original.Cipher {
				t.Errorf("Cipher: got %d, want %d", parsed.Cipher, original.Cipher)
			}
			if parsed.Auth != original.Auth {
				t.Errorf("Auth: got %d, want %d", parsed.Auth, original.Auth)
			}
			if parsed.SE != original.SE {
				t.Errorf("SE: got %d, want %d", parsed.SE, original.SE)
			}
			if parsed.SLen != original.SLen {
				t.Errorf("SLen: got %d, want %d", parsed.SLen, original.SLen)
			}
			if parsed.KLen != original.KLen {
				t.Errorf("KLen: got %d, want %d", parsed.KLen, original.KLen)
			}
			if !bytes.Equal(parsed.Salt, original.Salt) {
				t.Errorf("Salt mismatch\n  got:  %X\n  want: %X", parsed.Salt, original.Salt)
			}
			if !bytes.Equal(parsed.WrappedKey, original.WrappedKey) {
				t.Errorf("WrappedKey mismatch\n  got:  %X\n  want: %X", parsed.WrappedKey, original.WrappedKey)
			}

			// Also verify the re-marshaled bytes are identical.
			redata, err := parsed.Marshal()
			if err != nil {
				t.Fatalf("re-Marshal() error: %v", err)
			}
			if !bytes.Equal(data, redata) {
				t.Errorf("re-Marshal mismatch\n  got:  %X\n  want: %X", redata, data)
			}
		})
	}
}

// TestParseKMMsg_NonZeroKEKI verifies that a non-zero KEKI value round-trips.
func TestParseKMMsg_NonZeroKEKI(t *testing.T) {
	salt := bytes.Repeat([]byte{0xEE}, 16)
	wrappedKey := make([]byte, 24) // single AES-128 key

	data := buildValidKMMsg(t, KKEven, CipherAESCTR, 4, salt, wrappedKey)

	// Poke KEKI = 42 into bytes 4-7.
	binary.BigEndian.PutUint32(data[4:8], 42)

	km, err := ParseKMMsg(data)
	if err != nil {
		t.Fatalf("ParseKMMsg() unexpected error: %v", err)
	}
	if km.KEKI != 42 {
		t.Errorf("KEKI = %d, want 42", km.KEKI)
	}
}

// TestParseKMMsg_Errors tests that ParseKMMsg correctly rejects malformed
// messages with descriptive errors.
func TestParseKMMsg_Errors(t *testing.T) {
	// A valid baseline message to mutate in each test case.
	salt := bytes.Repeat([]byte{0xAA}, 16)
	wrappedKey := make([]byte, 24) // single AES-128 key
	validMsg := buildValidKMMsg(t, KKEven, CipherAESCTR, 4, salt, wrappedKey)

	tests := []struct {
		name    string
		mutate  func([]byte) []byte // returns a modified copy
		wantErr string              // substring expected in error message
	}{
		{
			name: "truncated header",
			mutate: func(b []byte) []byte {
				return b[:10] // less than 16-byte header
			},
			wantErr: "too short",
		},
		{
			name: "invalid signature",
			mutate: func(b []byte) []byte {
				c := make([]byte, len(b))
				copy(c, b)
				binary.BigEndian.PutUint16(c[1:3], 0xDEAD)
				return c
			},
			wantErr: "invalid signature",
		},
		{
			name: "bad version",
			mutate: func(b []byte) []byte {
				c := make([]byte, len(b))
				copy(c, b)
				// Set version to 3: (3 << 4) | PacketType=2 = 0x32
				c[0] = 0x32
				return c
			},
			wantErr: "unsupported version",
		},
		{
			name: "bad packet type",
			mutate: func(b []byte) []byte {
				c := make([]byte, len(b))
				copy(c, b)
				// Set PacketType to 5: Version=1 stays, low nibble = 5 → 0x15
				c[0] = 0x15
				return c
			},
			wantErr: "unexpected packet type",
		},
		{
			name: "S bit set",
			mutate: func(b []byte) []byte {
				c := make([]byte, len(b))
				copy(c, b)
				c[0] |= 0x80 // set the reserved S bit
				return c
			},
			wantErr: "reserved S bit",
		},
		{
			name: "KK is zero",
			mutate: func(b []byte) []byte {
				c := make([]byte, len(b))
				copy(c, b)
				c[3] = 0x00 // KK = 0
				return c
			},
			wantErr: "KK field is 0",
		},
		{
			name: "unsupported cipher",
			mutate: func(b []byte) []byte {
				c := make([]byte, len(b))
				copy(c, b)
				c[8] = 0x05 // cipher type 5 doesn't exist
				return c
			},
			wantErr: "unsupported cipher",
		},
		{
			name: "wrong SLen/4",
			mutate: func(b []byte) []byte {
				c := make([]byte, len(b))
				copy(c, b)
				c[14] = 8 // SLen/4 = 8 → 32 bytes, but we only support 16
				return c
			},
			wantErr: "unsupported SLen",
		},
		{
			name: "bad KLen/4 value",
			mutate: func(b []byte) []byte {
				c := make([]byte, len(b))
				copy(c, b)
				c[15] = 5 // KLen/4 = 5 → 20 bytes, not a valid AES key size
				return c
			},
			wantErr: "unsupported KLen",
		},
		{
			name: "truncated salt and key data",
			mutate: func(b []byte) []byte {
				// Header is valid but cut off the salt/key data.
				return b[:KMHeaderLen+4]
			},
			wantErr: "too short for declared sizes",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := tc.mutate(validMsg)
			_, err := ParseKMMsg(data)
			if err == nil {
				t.Fatal("ParseKMMsg() expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// TestMarshal_Errors verifies that Marshal rejects inconsistent KMMsg values.
func TestMarshal_Errors(t *testing.T) {
	// Build a valid base KMMsg that we'll mutate in each sub-test.
	base := func() *KMMsg {
		return &KMMsg{
			Version:    KMVersion,
			PacketType: KMPacketType,
			Sign:       KMSignature,
			KK:         KKEven,
			Cipher:     CipherAESCTR,
			SE:         SELiveSRT,
			SLen:       16,
			KLen:       16,
			Salt:       bytes.Repeat([]byte{0xAA}, 16),
			WrappedKey: make([]byte, 24), // 8 + 16
		}
	}

	tests := []struct {
		name    string
		mutate  func(*KMMsg)
		wantErr string
	}{
		{
			name: "salt length mismatch",
			mutate: func(km *KMMsg) {
				km.Salt = make([]byte, 8) // SLen says 16, but salt is 8 bytes
			},
			wantErr: "salt length",
		},
		{
			name: "KK is zero",
			mutate: func(km *KMMsg) {
				km.KK = 0
			},
			wantErr: "KK field is 0",
		},
		{
			name: "wrapped key length mismatch",
			mutate: func(km *KMMsg) {
				km.WrappedKey = make([]byte, 10) // should be 24 for single AES-128
			},
			wantErr: "wrapped key length",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			km := base()
			tc.mutate(km)
			_, err := km.Marshal()
			if err == nil {
				t.Fatal("Marshal() expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// TestParseKMMsg_ExtraTrailingBytes verifies that ParseKMMsg tolerates
// messages with extra data after the expected fields (forward compatibility).
func TestParseKMMsg_ExtraTrailingBytes(t *testing.T) {
	salt := bytes.Repeat([]byte{0xAA}, 16)
	wrappedKey := make([]byte, 24)

	data := buildValidKMMsg(t, KKEven, CipherAESCTR, 4, salt, wrappedKey)

	// Append some extra trailing bytes.
	data = append(data, 0xFF, 0xFF, 0xFF, 0xFF)

	km, err := ParseKMMsg(data)
	if err != nil {
		t.Fatalf("ParseKMMsg() unexpected error with trailing bytes: %v", err)
	}
	if km.KK != KKEven {
		t.Errorf("KK = %d, want %d", km.KK, KKEven)
	}
}

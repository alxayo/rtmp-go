package conn

import (
	"testing"

	"github.com/alxayo/go-rtmp/internal/srt/crypto"
	"github.com/alxayo/go-rtmp/internal/srt/packet"
)

// buildKMREQ constructs a KMREQ control packet from a passphrase, salt, raw
// SEK material, key length, and KK flag. This mirrors what a sender would
// produce when rotating keys: derive the KEK, wrap the SEK, build the KM
// message, and stuff it into a ControlPacket CIF.
func buildKMREQ(t *testing.T, passphrase string, salt []byte, sek []byte, keyLen int, kk uint8) *packet.ControlPacket {
	t.Helper()

	// Derive KEK from passphrase and the last 8 bytes of salt (SRT convention).
	kek, err := crypto.DeriveKey(passphrase, salt[len(salt)-8:], keyLen)
	if err != nil {
		t.Fatalf("DeriveKey: %v", err)
	}

	// Wrap the SEK with the derived KEK.
	wrapped, err := crypto.Wrap(kek, sek)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}

	// Build the KM message.
	km := &crypto.KMMsg{
		Version:    crypto.KMVersion,
		PacketType: crypto.KMPacketType,
		Sign:       crypto.KMSignature,
		KK:         kk,
		KEKI:       0,
		Cipher:     crypto.CipherAESCTR,
		Auth:       0,
		SE:         crypto.SELiveSRT,
		SLen:       uint16(len(salt)),
		KLen:       uint16(keyLen),
		Salt:       salt,
		WrappedKey: wrapped,
	}
	cif, err := km.Marshal()
	if err != nil {
		t.Fatalf("km.Marshal: %v", err)
	}

	return &packet.ControlPacket{
		Header: packet.Header{
			IsControl:    true,
			DestSocketID: 42,
		},
		Type:         packet.CtrlUserDefined,
		TypeSpecific: packet.UserSubtypeKMREQ,
		CIF:          cif,
	}
}

// newTestConn creates a minimal *Conn suitable for unit-testing handleKMREQ.
// It wires up a real UDP socket (for sendControl) and installs the provided
// ConnConfig and crypto KeySet.
func newTestConn(t *testing.T, passphrase string, keyLen int, ks *crypto.KeySet) *Conn {
	t.Helper()

	udpConn, peerAddr := testUDPConn(t)
	cfg := testConfig()
	cfg.Passphrase = passphrase
	cfg.PbKeyLen = keyLen
	cfg.KeySet = ks

	return New(42, 99, peerAddr, udpConn, "live/rekey", cfg, testLogger())
}

// TestHandleKMREQ_EvenKey verifies that a KMREQ carrying a single even-slot
// SEK is correctly unwrapped and installed.
func TestHandleKMREQ_EvenKey(t *testing.T) {
	passphrase := "test-rekey-pass"
	keyLen := 16
	salt := []byte{
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10,
	}
	sek := []byte{
		0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x11, 0x22,
		0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0x00,
	}

	// Start with an empty KeySet (simulates initial state before rotation).
	ks := crypto.NewKeySet()
	c := newTestConn(t, passphrase, keyLen, ks)

	// Build and deliver a KMREQ for the even key.
	ctrl := buildKMREQ(t, passphrase, salt, sek, keyLen, crypto.KKEven)
	c.handleKMREQ(ctrl)

	// Verify the even key was installed by checking the KeySet has keys.
	if !ks.HasKeys() {
		t.Fatal("KeySet should have keys after even-key KMREQ")
	}

	// Verify we can actually decrypt with the installed key. Build a
	// known plaintext, encrypt it with the same SEK+salt, then decrypt
	// via the KeySet.
	pc, err := crypto.NewPacketCipher(sek, salt)
	if err != nil {
		t.Fatalf("NewPacketCipher: %v", err)
	}

	plaintext := []byte("hello SRT rekey!")
	encrypted := make([]byte, len(plaintext))
	copy(encrypted, plaintext)
	if err := pc.EncryptPayload(encrypted, 1); err != nil {
		t.Fatalf("EncryptPayload: %v", err)
	}

	// Decrypt using the KeySet's even slot (encFlag=0x01).
	if err := ks.DecryptPayload(encrypted, 1, 0x01); err != nil {
		t.Fatalf("DecryptPayload with rotated even key: %v", err)
	}

	// The decrypted data should match the original plaintext.
	for i := range plaintext {
		if encrypted[i] != plaintext[i] {
			t.Fatalf("decrypted byte %d: got 0x%02X, want 0x%02X", i, encrypted[i], plaintext[i])
		}
	}
}

// TestHandleKMREQ_BothKeys verifies that a KMREQ carrying both even and odd
// SEKs is correctly unwrapped and both slots are installed.
func TestHandleKMREQ_BothKeys(t *testing.T) {
	passphrase := "both-keys-pass"
	keyLen := 16
	salt := []byte{
		0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88,
		0x99, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x00,
	}
	// Two keys concatenated: even || odd.
	evenSEK := []byte{
		0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80,
		0x90, 0xA0, 0xB0, 0xC0, 0xD0, 0xE0, 0xF0, 0x01,
	}
	oddSEK := []byte{
		0xF1, 0xE2, 0xD3, 0xC4, 0xB5, 0xA6, 0x97, 0x88,
		0x79, 0x6A, 0x5B, 0x4C, 0x3D, 0x2E, 0x1F, 0x02,
	}
	bothSEK := append(evenSEK, oddSEK...)

	ks := crypto.NewKeySet()
	c := newTestConn(t, passphrase, keyLen, ks)

	ctrl := buildKMREQ(t, passphrase, salt, bothSEK, keyLen, crypto.KKBoth)
	c.handleKMREQ(ctrl)

	if !ks.HasKeys() {
		t.Fatal("KeySet should have keys after both-key KMREQ")
	}

	// Verify even key works (encFlag=0x01).
	evenPC, err := crypto.NewPacketCipher(evenSEK, salt)
	if err != nil {
		t.Fatalf("NewPacketCipher even: %v", err)
	}
	payload := []byte("even-key-test!?!")
	encrypted := make([]byte, len(payload))
	copy(encrypted, payload)
	if err := evenPC.EncryptPayload(encrypted, 100); err != nil {
		t.Fatalf("EncryptPayload even: %v", err)
	}
	if err := ks.DecryptPayload(encrypted, 100, 0x01); err != nil {
		t.Fatalf("DecryptPayload even: %v", err)
	}
	for i := range payload {
		if encrypted[i] != payload[i] {
			t.Fatalf("even decrypt mismatch at byte %d", i)
		}
	}

	// Verify odd key works (encFlag=0x02).
	oddPC, err := crypto.NewPacketCipher(oddSEK, salt)
	if err != nil {
		t.Fatalf("NewPacketCipher odd: %v", err)
	}
	payload2 := []byte("odd-key-test!?!!")
	encrypted2 := make([]byte, len(payload2))
	copy(encrypted2, payload2)
	if err := oddPC.EncryptPayload(encrypted2, 200); err != nil {
		t.Fatalf("EncryptPayload odd: %v", err)
	}
	if err := ks.DecryptPayload(encrypted2, 200, 0x02); err != nil {
		t.Fatalf("DecryptPayload odd: %v", err)
	}
	for i := range payload2 {
		if encrypted2[i] != payload2[i] {
			t.Fatalf("odd decrypt mismatch at byte %d", i)
		}
	}
}

// TestHandleKMREQ_WrongPassphrase verifies that a KMREQ wrapped with a
// different passphrase fails unwrap and does not install keys.
func TestHandleKMREQ_WrongPassphrase(t *testing.T) {
	senderPass := "sender-secret"
	receiverPass := "wrong-secret"
	keyLen := 16
	salt := []byte{
		0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE, 0xBA, 0xBE,
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
	}
	sek := []byte{
		0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x11, 0x22,
		0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0x00,
	}

	ks := crypto.NewKeySet()
	c := newTestConn(t, receiverPass, keyLen, ks)

	// Build the KMREQ with the sender's passphrase (different from receiver's).
	ctrl := buildKMREQ(t, senderPass, salt, sek, keyLen, crypto.KKEven)
	c.handleKMREQ(ctrl)

	// The KeySet should remain empty because unwrap should have failed.
	if ks.HasKeys() {
		t.Fatal("KeySet should remain empty after wrong-passphrase KMREQ")
	}
}

// TestHandleKMREQ_NoEncryption verifies that handleKMREQ returns cleanly
// when the connection has no encryption configured (nil KeySet).
func TestHandleKMREQ_NoEncryption(t *testing.T) {
	udpConn, peerAddr := testUDPConn(t)
	cfg := testConfig()
	// No passphrase, no KeySet — unencrypted connection.

	c := New(42, 99, peerAddr, udpConn, "live/plain", cfg, testLogger())

	// Build a dummy control packet with some CIF data.
	ctrl := &packet.ControlPacket{
		Header: packet.Header{
			IsControl:    true,
			DestSocketID: 42,
		},
		Type:         packet.CtrlUserDefined,
		TypeSpecific: packet.UserSubtypeKMREQ,
		CIF:          []byte{0x00, 0x01, 0x02, 0x03},
	}

	// Should return cleanly without panicking.
	c.handleKMREQ(ctrl)
}

// TestHandleKMREQ_InvalidCIF verifies that handleKMREQ handles a malformed
// KM message gracefully (returns without panicking or installing keys).
func TestHandleKMREQ_InvalidCIF(t *testing.T) {
	ks := crypto.NewKeySet()
	c := newTestConn(t, "some-pass", 16, ks)

	// CIF that's too short to be a valid KM message.
	ctrl := &packet.ControlPacket{
		Header: packet.Header{
			IsControl:    true,
			DestSocketID: 42,
		},
		Type:         packet.CtrlUserDefined,
		TypeSpecific: packet.UserSubtypeKMREQ,
		CIF:          []byte{0x12, 0x20, 0x29},
	}

	c.handleKMREQ(ctrl)

	if ks.HasKeys() {
		t.Fatal("KeySet should remain empty after invalid CIF")
	}
}

// TestHandleKMREQ_PreservesExistingKeys verifies that a failed rekey attempt
// does not corrupt or remove keys that were already installed.
func TestHandleKMREQ_PreservesExistingKeys(t *testing.T) {
	passphrase := "preserve-test"
	keyLen := 16
	salt := []byte{
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10,
	}
	sek := []byte{
		0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x11, 0x22,
		0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0x00,
	}

	// Pre-install an even key.
	ks, err := crypto.NewKeySetFromSEK(crypto.KKEven, sek, salt, keyLen)
	if err != nil {
		t.Fatalf("NewKeySetFromSEK: %v", err)
	}
	c := newTestConn(t, passphrase, keyLen, ks)

	// Send a KMREQ with the wrong passphrase — should fail unwrap.
	ctrl := buildKMREQ(t, "wrong-pass", salt, sek, keyLen, crypto.KKOdd)
	c.handleKMREQ(ctrl)

	// The existing even key should still work.
	pc, err := crypto.NewPacketCipher(sek, salt)
	if err != nil {
		t.Fatalf("NewPacketCipher: %v", err)
	}
	payload := []byte("still works!!!?!")
	encrypted := make([]byte, len(payload))
	copy(encrypted, payload)
	if err := pc.EncryptPayload(encrypted, 42); err != nil {
		t.Fatalf("EncryptPayload: %v", err)
	}
	if err := ks.DecryptPayload(encrypted, 42, 0x01); err != nil {
		t.Fatalf("DecryptPayload should still work after failed rekey: %v", err)
	}
	for i := range payload {
		if encrypted[i] != payload[i] {
			t.Fatalf("existing key corrupted: byte %d mismatch", i)
		}
	}
}

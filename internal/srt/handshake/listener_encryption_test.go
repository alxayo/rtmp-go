package handshake

import (
	"bytes"
	"crypto/rand"
	"net"
	"strings"
	"testing"

	"github.com/alxayo/go-rtmp/internal/srt/crypto"
	"github.com/alxayo/go-rtmp/internal/srt/packet"
)

// buildKMREQExtension is a test helper that builds a valid KMREQ extension
// payload by generating a random SEK, deriving a KEK from the passphrase,
// and wrapping the SEK using AES Key Wrap (RFC 3394).
func buildKMREQExtension(t *testing.T, passphrase string, keyLen int) (kmContent []byte, sek []byte, salt []byte) {
	t.Helper()

	// Generate a random SEK (Stream Encrypting Key).
	sek = make([]byte, keyLen)
	if _, err := rand.Read(sek); err != nil {
		t.Fatalf("generate SEK: %v", err)
	}

	// Generate a random 16-byte salt.
	salt = make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		t.Fatalf("generate salt: %v", err)
	}

	// Derive the KEK from the passphrase and salt.
	kek, err := crypto.DeriveKey(passphrase, salt, keyLen)
	if err != nil {
		t.Fatalf("derive KEK: %v", err)
	}

	// Wrap the SEK with the KEK.
	wrappedKey, err := crypto.Wrap(kek, sek)
	if err != nil {
		t.Fatalf("wrap SEK: %v", err)
	}

	// Build the KMMsg and marshal it.
	km := &crypto.KMMsg{
		Version:    crypto.KMVersion,
		PacketType: crypto.KMPacketType,
		Sign:       crypto.KMSignature,
		KK:         crypto.KKEven,
		KEKI:       0,
		Cipher:     crypto.CipherAESCTR,
		Auth:       0,
		SE:         crypto.SELiveSRT,
		SLen:       16,
		KLen:       uint16(keyLen),
		Salt:       salt,
		WrappedKey: wrappedKey,
	}

	kmContent, err = km.Marshal()
	if err != nil {
		t.Fatalf("marshal KMMsg: %v", err)
	}

	return kmContent, sek, salt
}

// doConclusionWithKMREQ is a test helper that performs a full Induction +
// Conclusion exchange, optionally including a KMREQ extension.
func doConclusionWithKMREQ(t *testing.T, l *Listener, from *net.UDPAddr, kmContent []byte) (*packet.HandshakeCIF, *HandshakeResult, error) {
	t.Helper()

	// Phase 1: Induction to get a valid SYN cookie.
	induction := &packet.HandshakeCIF{
		Version:          4,
		InitialSeqNumber: 1000,
		MTU:              1500,
		FlowWindow:       8192,
		Type:             packet.HSTypeInduction,
		SocketID:         99,
		SYNCookie:        0,
	}

	inductionResp, err := l.HandleInduction(induction, from)
	if err != nil {
		t.Fatalf("HandleInduction failed: %v", err)
	}

	// Build the HSREQ extension.
	hsReqContent := BuildHSRsp(
		0x00010500,
		FlagTSBPDSND|FlagTSBPDRCV|FlagCRYPT|FlagTLPKTDROP|FlagPERIODICNAK|FlagREXMITFLG,
		120,
		120,
	)

	// Build extension list.
	extensions := []packet.HSExtension{
		{
			Type:    ExtTypeHSREQ,
			Length:  uint16(len(hsReqContent) / 4),
			Content: hsReqContent,
		},
	}

	// Add KMREQ if provided.
	if kmContent != nil {
		extensions = append(extensions, packet.HSExtension{
			Type:    ExtTypeKMREQ,
			Length:  uint16(len(kmContent) / 4),
			Content: kmContent,
		})
	}

	// Phase 2: Conclusion with the cookie from Induction.
	conclusion := &packet.HandshakeCIF{
		Version:          5,
		InitialSeqNumber: 1000,
		MTU:              1500,
		FlowWindow:       8192,
		Type:             packet.HSTypeConclusion,
		SocketID:         99,
		SYNCookie:        inductionResp.SYNCookie,
		Extensions:       extensions,
	}

	return l.HandleConclusion(conclusion, from)
}

// TestEncryptionHappyPath verifies that a full handshake with matching
// passphrases succeeds: the server unwraps the SEK, the response includes
// the correct EncryptionField and a KMRSP extension, and the result carries
// the unwrapped SEK.
func TestEncryptionHappyPath(t *testing.T) {
	passphrase := "test-secret-123"
	keyLen := 16 // AES-128

	l := NewListener(42, 120, 1500, 8192, passphrase, keyLen, testLogger())
	from := &net.UDPAddr{IP: net.ParseIP("192.168.1.10"), Port: 9000}

	kmContent, originalSEK, originalSalt := buildKMREQExtension(t, passphrase, keyLen)

	resp, result, err := doConclusionWithKMREQ(t, l, from, kmContent)
	if err != nil {
		t.Fatalf("HandleConclusion failed: %v", err)
	}

	// Verify EncryptionField: 2 = AES-128.
	if resp.EncryptionField != 2 {
		t.Errorf("EncryptionField: got %d, want 2 (AES-128)", resp.EncryptionField)
	}

	// Verify ExtensionField includes both HSREQ and KMREQ flags.
	wantExtField := extensionFlagHSREQ | extensionFlagKMREQ
	if resp.ExtensionField != wantExtField {
		t.Errorf("ExtensionField: got 0x%04X, want 0x%04X", resp.ExtensionField, wantExtField)
	}

	// Verify KMRSP extension is present (should be the second extension).
	if len(resp.Extensions) < 2 {
		t.Fatalf("expected at least 2 extensions (HSRSP + KMRSP), got %d", len(resp.Extensions))
	}
	if resp.Extensions[1].Type != ExtTypeKMRSP {
		t.Errorf("second extension type: got %d, want %d (KMRSP)", resp.Extensions[1].Type, ExtTypeKMRSP)
	}

	// Verify the KMRSP content can be parsed.
	kmRsp, err := crypto.ParseKMMsg(resp.Extensions[1].Content)
	if err != nil {
		t.Fatalf("parse KMRSP: %v", err)
	}
	if kmRsp.KLen != uint16(keyLen) {
		t.Errorf("KMRSP KLen: got %d, want %d", kmRsp.KLen, keyLen)
	}

	// Verify the result has the correct encryption fields.
	if !result.Encrypted {
		t.Error("result.Encrypted: got false, want true")
	}
	if result.KeyLen != keyLen {
		t.Errorf("result.KeyLen: got %d, want %d", result.KeyLen, keyLen)
	}
	if !bytes.Equal(result.SEK, originalSEK) {
		t.Error("result.SEK does not match the original SEK")
	}
	if !bytes.Equal(result.Salt, originalSalt) {
		t.Error("result.Salt does not match the original salt")
	}
}

// TestEncryptionAES256 verifies encryption negotiation with AES-256.
func TestEncryptionAES256(t *testing.T) {
	passphrase := "strong-passphrase-256"
	keyLen := 32 // AES-256

	l := NewListener(42, 120, 1500, 8192, passphrase, keyLen, testLogger())
	from := &net.UDPAddr{IP: net.ParseIP("192.168.1.10"), Port: 9000}

	kmContent, originalSEK, _ := buildKMREQExtension(t, passphrase, keyLen)

	resp, result, err := doConclusionWithKMREQ(t, l, from, kmContent)
	if err != nil {
		t.Fatalf("HandleConclusion failed: %v", err)
	}

	// EncryptionField: 4 = AES-256.
	if resp.EncryptionField != 4 {
		t.Errorf("EncryptionField: got %d, want 4 (AES-256)", resp.EncryptionField)
	}

	if result.KeyLen != 32 {
		t.Errorf("result.KeyLen: got %d, want 32", result.KeyLen)
	}
	if !bytes.Equal(result.SEK, originalSEK) {
		t.Error("result.SEK does not match the original SEK")
	}
}

// TestEncryptionRequiredButNoKMREQ verifies that the server rejects a client
// that does not send a KMREQ extension when the server requires encryption.
func TestEncryptionRequiredButNoKMREQ(t *testing.T) {
	l := NewListener(42, 120, 1500, 8192, "my-secret", 16, testLogger())
	from := &net.UDPAddr{IP: net.ParseIP("192.168.1.10"), Port: 9000}

	// Send Conclusion without KMREQ.
	_, _, err := doConclusionWithKMREQ(t, l, from, nil)
	if err == nil {
		t.Fatal("expected error when KMREQ is missing but passphrase is set")
	}
	if !strings.Contains(err.Error(), "encryption required") {
		t.Errorf("error should mention encryption required, got: %v", err)
	}
}

// TestEncryptionClientSendsKMREQButServerHasNoPassphrase verifies that
// the server rejects a client that sends KMREQ when the server has no
// passphrase configured.
func TestEncryptionClientSendsKMREQButServerHasNoPassphrase(t *testing.T) {
	l := NewListener(42, 120, 1500, 8192, "", 0, testLogger())
	from := &net.UDPAddr{IP: net.ParseIP("192.168.1.10"), Port: 9000}

	// Build a KMREQ extension with some passphrase.
	kmContent, _, _ := buildKMREQExtension(t, "client-secret", 16)

	_, _, err := doConclusionWithKMREQ(t, l, from, kmContent)
	if err == nil {
		t.Fatal("expected error when client sends KMREQ but server has no passphrase")
	}
	if !strings.Contains(err.Error(), "no passphrase configured") {
		t.Errorf("error should mention no passphrase configured, got: %v", err)
	}
}

// TestEncryptionWrongPassphrase verifies that the server fails to unwrap
// the SEK when the passphrases don't match.
func TestEncryptionWrongPassphrase(t *testing.T) {
	serverPassphrase := "server-secret"
	clientPassphrase := "wrong-secret"
	keyLen := 16

	l := NewListener(42, 120, 1500, 8192, serverPassphrase, keyLen, testLogger())
	from := &net.UDPAddr{IP: net.ParseIP("192.168.1.10"), Port: 9000}

	// Build KMREQ with the client's (wrong) passphrase.
	kmContent, _, _ := buildKMREQExtension(t, clientPassphrase, keyLen)

	_, _, err := doConclusionWithKMREQ(t, l, from, kmContent)
	if err == nil {
		t.Fatal("expected error when passphrases don't match")
	}
	if !strings.Contains(err.Error(), "unwrap SEK") {
		t.Errorf("error should mention unwrap SEK, got: %v", err)
	}
}

// TestEncryptionKeyLengthMismatch verifies that the server rejects a client
// whose key length doesn't match the server's configured pbKeyLen.
func TestEncryptionKeyLengthMismatch(t *testing.T) {
	passphrase := "my-secret"

	// Server expects AES-256 (32 bytes), client sends AES-128 (16 bytes).
	l := NewListener(42, 120, 1500, 8192, passphrase, 32, testLogger())
	from := &net.UDPAddr{IP: net.ParseIP("192.168.1.10"), Port: 9000}

	kmContent, _, _ := buildKMREQExtension(t, passphrase, 16)

	_, _, err := doConclusionWithKMREQ(t, l, from, kmContent)
	if err == nil {
		t.Fatal("expected error for key length mismatch")
	}
	if !strings.Contains(err.Error(), "key length mismatch") {
		t.Errorf("error should mention key length mismatch, got: %v", err)
	}
}

// TestNoEncryptionBackwardCompatible verifies that a handshake without
// encryption still works when the server has no passphrase configured.
func TestNoEncryptionBackwardCompatible(t *testing.T) {
	l := NewListener(42, 120, 1500, 8192, "", 0, testLogger())
	from := &net.UDPAddr{IP: net.ParseIP("192.168.1.10"), Port: 9000}

	resp, result, err := doConclusionWithKMREQ(t, l, from, nil)
	if err != nil {
		t.Fatalf("HandleConclusion failed: %v", err)
	}

	// Verify no encryption in response.
	if resp.EncryptionField != 0 {
		t.Errorf("EncryptionField: got %d, want 0 (no encryption)", resp.EncryptionField)
	}
	if resp.ExtensionField != extensionFlagHSREQ {
		t.Errorf("ExtensionField: got 0x%04X, want 0x%04X (HSREQ only)", resp.ExtensionField, extensionFlagHSREQ)
	}

	// Should only have HSRSP, no KMRSP.
	if len(resp.Extensions) != 1 {
		t.Fatalf("expected 1 extension (HSRSP only), got %d", len(resp.Extensions))
	}
	if resp.Extensions[0].Type != ExtTypeHSRSP {
		t.Errorf("extension type: got %d, want %d (HSRSP)", resp.Extensions[0].Type, ExtTypeHSRSP)
	}

	// Verify result has no encryption fields.
	if result.Encrypted {
		t.Error("result.Encrypted: got true, want false")
	}
	if result.SEK != nil {
		t.Error("result.SEK: got non-nil, want nil")
	}
	if result.Salt != nil {
		t.Error("result.Salt: got non-nil, want nil")
	}
	if result.KeyLen != 0 {
		t.Errorf("result.KeyLen: got %d, want 0", result.KeyLen)
	}
}

// TestEncryptionPbKeyLenZeroAcceptsAny verifies that when pbKeyLen is 0
// (unset), the server accepts any key length from the client.
func TestEncryptionPbKeyLenZeroAcceptsAny(t *testing.T) {
	passphrase := "flexible-server"

	// Server has pbKeyLen=0, client sends AES-192 (24 bytes).
	l := NewListener(42, 120, 1500, 8192, passphrase, 0, testLogger())
	from := &net.UDPAddr{IP: net.ParseIP("192.168.1.10"), Port: 9000}

	kmContent, originalSEK, _ := buildKMREQExtension(t, passphrase, 24)

	resp, result, err := doConclusionWithKMREQ(t, l, from, kmContent)
	if err != nil {
		t.Fatalf("HandleConclusion failed: %v", err)
	}

	// EncryptionField: 3 = AES-192.
	if resp.EncryptionField != 3 {
		t.Errorf("EncryptionField: got %d, want 3 (AES-192)", resp.EncryptionField)
	}

	if result.KeyLen != 24 {
		t.Errorf("result.KeyLen: got %d, want 24", result.KeyLen)
	}
	if !bytes.Equal(result.SEK, originalSEK) {
		t.Error("result.SEK does not match the original SEK")
	}
}

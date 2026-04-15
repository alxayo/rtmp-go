package handshake

// This file implements the server-side (listener) SRT handshake v5 protocol.
//
// The SRT v5 handshake has two phases:
//
//   Phase 1 — Induction:
//     1. Caller sends a v4-format handshake with cookie=0 and Type=Induction.
//     2. Listener responds with v5 format, a SYN cookie, and extension flags.
//        The SYN cookie proves the caller can receive on its claimed address.
//
//   Phase 2 — Conclusion:
//     1. Caller echoes the SYN cookie and sends extensions (HSREQ + SID).
//     2. Listener validates the cookie, parses extensions, negotiates parameters.
//     3. Listener responds with HSRSP extension containing negotiated values.
//
// After both phases complete, the connection is established and media can flow.

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"math/big"
	"net"

	"github.com/alxayo/go-rtmp/internal/srt/crypto"
	"github.com/alxayo/go-rtmp/internal/srt/packet"
)

// SRTVersion is the SRT protocol version this implementation advertises.
// Encoded as: major * 0x10000 + minor * 0x100 + patch.
// 0x00010500 = version 1.5.0.
const SRTVersion = 0x00010500

// DefaultFlags is the set of SRT features this listener supports by default.
// These are the flags we advertise during the handshake, and the final
// negotiated flags will be the intersection of ours and the peer's.
const DefaultFlags = FlagTSBPDSND | FlagTSBPDRCV | FlagCRYPT | FlagTLPKTDROP | FlagPERIODICNAK | FlagREXMITFLG

// srtMagic is the magic value placed in the ExtensionField of the Induction
// response. It tells the caller that this is an SRT v5 listener (not a
// legacy UDT server). The caller checks for this value before proceeding
// to the Conclusion phase.
const srtMagic uint16 = 0x4A17

// extensionFlagHSREQ is the bit set in the Conclusion's ExtensionField
// to indicate that HSREQ/HSRSP extensions are present.
const extensionFlagHSREQ uint16 = 0x0001

// extensionFlagKMREQ is the bit set in the Conclusion's ExtensionField
// to indicate that KMREQ/KMRSP extensions are present.
const extensionFlagKMREQ uint16 = 0x0002

// extensionFlagSID is the bit set in the Conclusion's ExtensionField
// to indicate that a Stream ID extension is present.
const extensionFlagSID uint16 = 0x0004

// HandshakeResult contains the negotiated parameters after a successful
// handshake. The caller of HandleConclusion uses this to configure the
// new SRT connection with the agreed-upon settings.
type HandshakeResult struct {
	// PeerSocketID is the caller's SRT socket ID. We use this as the
	// DestSocketID when sending packets back to the caller.
	PeerSocketID uint32

	// StreamID is the Stream ID provided by the caller, typically
	// containing the stream key (e.g., "live/mystream").
	StreamID string

	// InitialSeqNum is the negotiated initial sequence number for data
	// packets. Both sides use this as the starting point for their
	// sequence number counters.
	InitialSeqNum uint32

	// MTU is the negotiated Maximum Transmission Unit in bytes.
	// We use the minimum of our MTU and the peer's MTU.
	MTU uint32

	// FlowWindow is the negotiated flow window size in packets.
	// We use the minimum of our flow window and the peer's.
	FlowWindow uint32

	// PeerTSBPD is the peer's TSBPD delay in milliseconds.
	PeerTSBPD uint16

	// LocalTSBPD is our TSBPD delay in milliseconds.
	LocalTSBPD uint16

	// Flags is the negotiated feature flag bitmask (intersection of
	// our flags and the peer's flags).
	Flags uint32

	// EvenSEK is the unwrapped even Stream Encrypting Key. Nil if no even key
	// was negotiated. For KKEven or KKBoth, this contains the even key.
	EvenSEK []byte

	// OddSEK is the unwrapped odd Stream Encrypting Key. Nil if no odd key
	// was negotiated. For KKOdd or KKBoth, this contains the odd key.
	OddSEK []byte

	// KK indicates which key(s) were negotiated during the handshake:
	// KKEven (0x01), KKOdd (0x02), or KKBoth (0x03). Zero if unencrypted.
	KK uint8

	// Salt is the salt from the client's KMREQ, needed to construct
	// the AES-CTR IV. Nil if the connection is not encrypted.
	Salt []byte

	// KeyLen is the negotiated AES key length in bytes (16/24/32).
	// Zero if the connection is not encrypted.
	KeyLen int

	// Encrypted indicates whether this connection uses encryption.
	Encrypted bool

	// Passphrase is the passphrase used for this connection's encryption.
	// This may come from either the static server passphrase or a per-stream
	// resolver — HandleConclusion resolves which one applies and stores it here.
	//
	// Why store it? After the handshake, the SRT spec allows in-band key
	// rotation via new KMREQ control messages. To unwrap a rotated SEK the
	// connection needs to re-derive a KEK using PBKDF2, which requires the
	// original passphrase. By propagating it through HandshakeResult into
	// ConnConfig, the connection has everything it needs for rekeying without
	// calling back into the handshake layer.
	//
	// Empty if the connection is not encrypted.
	Passphrase string
}

// Listener handles the server-side SRT handshake v5 protocol.
// It processes Induction and Conclusion handshakes from callers, validates
// SYN cookies, negotiates parameters, and returns the agreed-upon settings.
type Listener struct {
	// cookie generates and validates SYN cookies for anti-amplification.
	cookie *CookieGenerator

	// localSID is our SRT socket ID that we tell the caller to use as
	// DestSocketID when sending packets to us.
	localSID uint32

	// latency is our configured TSBPD latency in milliseconds.
	latency uint16

	// mtu is our configured Maximum Transmission Unit in bytes.
	mtu uint32

	// flowWindow is our configured flow window size in packets.
	flowWindow uint32

	// passphrase is the server's encryption passphrase. When non-empty,
	// the server expects clients to send a KMREQ extension with matching
	// encryption parameters. Empty means no encryption required.
	// Deprecated: Use passphraseResolver for per-stream passphrases.
	passphrase string

	// passphraseResolver is a function that looks up the passphrase for
	// a given SRT stream ID. When set, it takes precedence over the static
	// passphrase field. The function receives the raw Stream ID string
	// from the handshake extension (e.g., "publish:live/mystream") and
	// returns the passphrase or an error. The caller is responsible for
	// normalizing the stream ID before lookup.
	// This enables per-stream encryption where different streams can
	// use different passphrases.
	passphraseResolver func(rawStreamID string) (string, error)

	// pbKeyLen is the expected AES key length in bytes: 16 (AES-128),
	// 24 (AES-192), or 32 (AES-256). Only used when passphrase is set.
	pbKeyLen int

	// log is the structured logger for handshake events.
	log *slog.Logger
}

// NewListener creates a handshake listener with the given parameters.
// The listener will use the provided socket ID, latency, MTU, and flow
// window when responding to handshakes. When passphrase is non-empty,
// the listener requires clients to present a valid KMREQ extension.
// When passphraseResolver is non-nil, it takes precedence over the static
// passphrase — each connection's passphrase is looked up by stream key.
func NewListener(localSocketID uint32, latency uint16, mtu, flowWindow uint32, passphrase string, pbKeyLen int, passphraseResolver func(string) (string, error), log *slog.Logger) *Listener {
	return &Listener{
		cookie:             NewCookieGenerator(),
		localSID:           localSocketID,
		latency:            latency,
		mtu:                mtu,
		flowWindow:         flowWindow,
		passphrase:         passphrase,
		pbKeyLen:           pbKeyLen,
		passphraseResolver: passphraseResolver,
		log:                log,
	}
}

// HandleInduction processes an Induction handshake from a caller.
// This is Phase 1 of the SRT v5 handshake.
//
// The caller sends a v4-format (or v5) handshake with cookie=0 and
// Type=HSTypeInduction. We respond with:
//   - Version 5 (telling the caller we speak SRT v5)
//   - The caller's fields echoed back (ISN, MTU, FlowWindow, SocketID, PeerIP)
//   - A SYN cookie derived from the caller's address
//   - The SRT magic value (0x4A17) in the ExtensionField
//
// The Induction response echoes most of the caller's CIF fields back.
// This is how libsrt validates that the response belongs to its request.
// Only Version, ExtensionField, and Cookie are changed by the listener.
//
// Returns the Induction response CIF to send back to the caller.
func (l *Listener) HandleInduction(hs *packet.HandshakeCIF, from *net.UDPAddr) (*packet.HandshakeCIF, error) {
	// Step 1: Verify this is actually an Induction handshake.
	// If the caller sent a Conclusion or some other type, reject it.
	if hs.Type != packet.HSTypeInduction {
		return nil, fmt.Errorf("expected Induction handshake (type %d), got type %d",
			packet.HSTypeInduction, hs.Type)
	}

	l.log.Debug("processing induction",
		"from", from.String(),
		"peer_socket_id", hs.SocketID,
		"version", hs.Version,
	)

	// Step 2: Generate a SYN cookie for this caller's address.
	// The caller must echo this cookie back in the Conclusion phase.
	cookie := l.cookie.Generate(from)

	// Step 3: Build the Induction response CIF.
	// Per the SRT reference implementation (libsrt), the Induction response
	// ECHOES most of the caller's CIF fields. This allows the caller to
	// verify the response corresponds to its Induction request. The fields
	// we change are: Version (upgraded to 5), ExtensionField (SRT magic),
	// EncryptionField (cleared), and SYNCookie (our generated cookie).
	resp := &packet.HandshakeCIF{
		// Version 5 tells the caller we support SRT v5 handshake.
		Version: 5,

		// No encryption advertised during Induction (encryption is
		// negotiated in the Conclusion phase via KMREQ/KMRSP).
		EncryptionField: 0,

		// The SRT magic value (0x4A17) signals to the caller that this
		// is an SRT v5 listener, not a legacy UDT server.
		ExtensionField: srtMagic,

		// Echo back the caller's initial sequence number. The real SRT
		// server echoes this rather than generating a new random one.
		InitialSeqNumber: hs.InitialSeqNumber,

		// Echo the caller's MTU and flow window values. Final negotiation
		// of these happens in the Conclusion phase.
		MTU:        hs.MTU,
		FlowWindow: hs.FlowWindow,

		// This is our Induction response.
		Type: packet.HSTypeInduction,

		// Echo the caller's socket ID back. This is critical — libsrt
		// uses this to match the response to the pending request. The
		// listener's own socket ID is sent later in the Conclusion.
		SocketID: hs.SocketID,

		// The SYN cookie the caller must echo back in the Conclusion.
		SYNCookie: cookie,

		// Echo the caller's PeerIP back. The reference SRT implementation
		// echoes the caller's IP as seen in the Induction request.
		PeerIP: hs.PeerIP,
	}

	l.log.Debug("induction response ready",
		"cookie", cookie,
		"peer_socket_id", hs.SocketID,
	)

	return resp, nil
}

// HandleConclusion processes a Conclusion handshake from a caller.
// This is Phase 2 of the SRT v5 handshake.
//
// The caller echoes our SYN cookie and sends extensions (HSREQ + SID).
// We validate the cookie, parse extensions, negotiate parameters, and
// respond with our HSRSP extension.
//
// Returns the Conclusion response CIF and the negotiated HandshakeResult.
func (l *Listener) HandleConclusion(hs *packet.HandshakeCIF, from *net.UDPAddr) (*packet.HandshakeCIF, *HandshakeResult, error) {
	// Step 1: Validate the SYN cookie.
	// The caller must have echoed back the cookie we gave them during
	// Induction. If it doesn't match, this might be a spoofed packet.
	if !l.cookie.Validate(hs.SYNCookie, from) {
		return nil, nil, fmt.Errorf("invalid SYN cookie from %s", from.String())
	}

	l.log.Debug("cookie validated",
		"from", from.String(),
		"peer_socket_id", hs.SocketID,
	)

	// Step 2: Parse the extensions from the caller's Conclusion CIF.
	// We need HSREQ (for version/flags/TSBPD), optionally SID (stream ID),
	// and optionally KMREQ (for encryption key exchange).
	var hsReq *HSReqData
	var streamID string
	var kmReq *crypto.KMMsg

	for _, ext := range hs.Extensions {
		switch ext.Type {
		case ExtTypeHSREQ:
			// Parse the HSREQ extension to get the caller's SRT version,
			// feature flags, and requested TSBPD delays.
			var err error
			hsReq, err = ParseHSReq(ext.Content)
			if err != nil {
				return nil, nil, fmt.Errorf("parse HSREQ extension: %w", err)
			}
			l.log.Debug("parsed HSREQ",
				"srt_version", fmt.Sprintf("0x%08X", hsReq.SRTVersion),
				"flags", fmt.Sprintf("0x%08X", hsReq.SRTFlags),
				"recv_tsbpd", hsReq.RecvTSBPD,
				"sender_tsbpd", hsReq.SenderTSBPD,
			)

		case ExtTypeKMREQ:
			// Parse the KMREQ extension to get encryption parameters
			// and the wrapped Stream Encrypting Key(s).
			var err error
			kmReq, err = crypto.ParseKMMsg(ext.Content)
			if err != nil {
				return nil, nil, fmt.Errorf("parse KMREQ extension: %w", err)
			}
			l.log.Debug("parsed KMREQ",
				"cipher", kmReq.Cipher,
				"kk", kmReq.KK,
				"key_len", kmReq.KLen,
				"salt_len", kmReq.SLen,
			)

		case ExtTypeSID:
			// Parse the Stream ID extension. This is the SRT equivalent
			// of an RTMP stream key (e.g., "live/mystream").
			streamID = ParseStreamIDExtension(ext.Content)
			l.log.Debug("parsed Stream ID", "stream_id", streamID)
		}
	}

	// HSREQ is required — without it we can't negotiate TSBPD and flags.
	if hsReq == nil {
		return nil, nil, fmt.Errorf("missing HSREQ extension in Conclusion from %s", from.String())
	}

	// Encryption validation: determine the effective passphrase for this connection.
	//
	// The server supports two passphrase modes:
	//   1. Static — a single passphrase shared by all streams (legacy).
	//   2. Per-stream — a resolver function maps each Stream ID to its own
	//      passphrase, allowing multi-tenant setups where each publisher has
	//      a unique encryption key.
	//
	// When a resolver is configured it takes precedence over the static value,
	// because per-stream is strictly more specific. We start with the static
	// passphrase as the default and override it if the resolver is present.
	effectivePassphrase := l.passphrase
	if l.passphraseResolver != nil {
		// Per-stream mode: the client MUST include a Stream ID extension so
		// we know which passphrase to look up. Without it, we cannot derive
		// the correct KEK and must reject the handshake.
		if streamID == "" {
			return nil, nil, fmt.Errorf("stream ID required for per-stream encryption but client sent no SID from %s", from.String())
		}
		// Ask the resolver for this stream's passphrase. The resolver may
		// return an error if the stream is unknown or not authorized,
		// which rejects the handshake before any crypto work is done.
		resolved, err := l.passphraseResolver(streamID)
		if err != nil {
			return nil, nil, fmt.Errorf("passphrase lookup failed for stream %q from %s: %w", streamID, from.String(), err)
		}
		effectivePassphrase = resolved
	}

	// Mutual agreement check: both sides must agree on whether encryption is
	// used. A mismatch (one side expects encryption, the other doesn't) is a
	// configuration error that must be caught early.
	if effectivePassphrase != "" && kmReq == nil {
		return nil, nil, fmt.Errorf("encryption required but client sent no KMREQ from %s", from.String())
	}
	if kmReq != nil && effectivePassphrase == "" {
		return nil, nil, fmt.Errorf("client sent KMREQ but server has no passphrase configured")
	}

	// If encryption is negotiated, derive KEK and unwrap the SEK(s).
	// The unwrapped plaintext contains one or two keys depending on the
	// KK flag: KKEven/KKOdd = one key, KKBoth = two keys concatenated
	// (even first, then odd).
	var evenSEK, oddSEK []byte
	if kmReq != nil {
		keyLen := int(kmReq.KLen)

		// Validate that the KMREQ carries a supported crypto profile.
		// We only support AES-CTR, no authentication, MPEG-TS/SRT
		// encapsulation, and the default passphrase-derived KEK.
		if kmReq.Cipher != crypto.CipherAESCTR {
			return nil, nil, fmt.Errorf("unsupported cipher type: %d (only AES-CTR supported)", kmReq.Cipher)
		}
		if kmReq.Auth != 0 {
			return nil, nil, fmt.Errorf("unsupported auth type: %d (only none supported)", kmReq.Auth)
		}
		if kmReq.SE != crypto.SELiveSRT {
			return nil, nil, fmt.Errorf("unsupported stream encapsulation: %d (only MPEG-TS/SRT supported)", kmReq.SE)
		}
		if kmReq.KEKI != 0 {
			return nil, nil, fmt.Errorf("unsupported KEKI: %d (only passphrase-derived KEK supported)", kmReq.KEKI)
		}

		// Validate the key length matches our configuration.
		if l.pbKeyLen != 0 && keyLen != l.pbKeyLen {
			return nil, nil, fmt.Errorf("key length mismatch: client sent %d, server expects %d", keyLen, l.pbKeyLen)
		}

		// Derive the Key Encrypting Key (KEK) from our passphrase and the
		// client's salt using PBKDF2-HMAC-SHA1 with 2048 iterations.
		// Per the SRT spec (§6.2.1), only the LSB 64 bits (last 8 bytes)
		// of the 16-byte salt are used for PBKDF2 derivation. This matches
		// the libsrt reference implementation for interoperability.
		kek, err := crypto.DeriveKey(effectivePassphrase, kmReq.Salt[len(kmReq.Salt)-8:], keyLen)
		if err != nil {
			return nil, nil, fmt.Errorf("derive KEK: %w", err)
		}

		// Unwrap the Stream Encrypting Key(s) using AES Key Wrap (RFC 3394).
		// If the passphrase is wrong, the integrity check will fail here.
		plaintext, err := crypto.Unwrap(kek, kmReq.WrappedKey)
		if err != nil {
			return nil, nil, fmt.Errorf("unwrap SEK (wrong passphrase?): %w", err)
		}

		// Split the unwrapped plaintext into individual keys based on the
		// KK flag. KKEven/KKOdd carry one key; KKBoth carries two keys
		// concatenated (even || odd), each keyLen bytes.
		switch kmReq.KK {
		case crypto.KKEven:
			if len(plaintext) != keyLen {
				return nil, nil, fmt.Errorf("unwrapped key length %d != expected %d", len(plaintext), keyLen)
			}
			evenSEK = plaintext

		case crypto.KKOdd:
			if len(plaintext) != keyLen {
				return nil, nil, fmt.Errorf("unwrapped key length %d != expected %d", len(plaintext), keyLen)
			}
			oddSEK = plaintext

		case crypto.KKBoth:
			if len(plaintext) != 2*keyLen {
				return nil, nil, fmt.Errorf("unwrapped dual key length %d != expected %d", len(plaintext), 2*keyLen)
			}
			evenSEK = plaintext[:keyLen]
			oddSEK = plaintext[keyLen:]

		default:
			return nil, nil, fmt.Errorf("invalid KK flag: %d", kmReq.KK)
		}

		l.log.Info("encryption negotiated",
			"cipher", "AES-CTR",
			"key_len", keyLen,
			"kk", kmReq.KK,
			"from", from.String(),
		)
	}

	// Step 3: Negotiate TSBPD delays.
	// SRT uses a "max wins" rule: the final delay is the maximum of what
	// each side requested. This ensures both sides have enough buffering.
	//
	// The caller's RecvTSBPD is what they want *us* to buffer before
	// delivering packets to *them*. The caller's SenderTSBPD is what they
	// want to buffer on their side.
	peerTSBPD := hsReq.RecvTSBPD
	localTSBPD := l.latency

	// Apply the "max wins" rule for our local delay.
	if hsReq.SenderTSBPD > localTSBPD {
		localTSBPD = hsReq.SenderTSBPD
	}

	// Apply the "max wins" rule for the peer's delay.
	if l.latency > peerTSBPD {
		peerTSBPD = l.latency
	}

	// Step 4: Negotiate feature flags.
	// The negotiated flags are the intersection (bitwise AND) of what
	// both sides support. A feature is only enabled if both sides
	// advertise it.
	negotiatedFlags := DefaultFlags & hsReq.SRTFlags

	// Step 5: Negotiate MTU and flow window.
	// Use the minimum of both sides' values. The connection must work
	// within the constraints of the more limited side.
	negotiatedMTU := l.mtu
	if hs.MTU < negotiatedMTU {
		negotiatedMTU = hs.MTU
	}

	negotiatedFlowWindow := l.flowWindow
	if hs.FlowWindow < negotiatedFlowWindow {
		negotiatedFlowWindow = hs.FlowWindow
	}

	// Step 6: Build the HSRSP extension payload.
	// This tells the caller what we negotiated.
	hsRspContent := BuildHSRsp(SRTVersion, negotiatedFlags, peerTSBPD, localTSBPD)

	// Step 7: Determine encryption field value for the response CIF.
	// 0=none, 2=AES-128, 3=AES-192, 4=AES-256.
	encryptionField := uint16(0)
	if kmReq != nil {
		switch kmReq.KLen {
		case 16:
			encryptionField = 2
		case 24:
			encryptionField = 3
		case 32:
			encryptionField = 4
		}
	}

	// Include the KMREQ flag in the extension field when encryption is used.
	extensionField := extensionFlagHSREQ
	if kmReq != nil {
		extensionField |= extensionFlagKMREQ
	}

	// Step 8: Build the Conclusion response CIF.
	// We include the HSRSP extension and echo back the negotiated parameters.
	resp := &packet.HandshakeCIF{
		Version:          5,
		EncryptionField:  encryptionField,
		ExtensionField:   extensionField,
		InitialSeqNumber: hs.InitialSeqNumber,
		MTU:              negotiatedMTU,
		FlowWindow:       negotiatedFlowWindow,
		Type:             packet.HSTypeConclusion,
		SocketID:         l.localSID,
		SYNCookie:        0, // Cookie is cleared in the response
	}

	// Add the HSRSP extension to the response.
	resp.Extensions = append(resp.Extensions, packet.HSExtension{
		Type:    ExtTypeHSRSP,
		Length:  uint16(len(hsRspContent) / 4), // Length in 4-byte words
		Content: hsRspContent,
	})

	// If encryption was negotiated, build and append a KMRSP extension.
	// The KMRSP mirrors the client's KM message back to confirm that we
	// accepted the encryption parameters and successfully unwrapped the SEK.
	if kmReq != nil {
		kmRsp := &crypto.KMMsg{
			Version:    crypto.KMVersion,
			PacketType: crypto.KMPacketType,
			Sign:       crypto.KMSignature,
			KK:         kmReq.KK,
			KEKI:       kmReq.KEKI,
			Cipher:     kmReq.Cipher,
			Auth:       kmReq.Auth,
			SE:         kmReq.SE,
			SLen:       kmReq.SLen,
			KLen:       kmReq.KLen,
			Salt:       kmReq.Salt,
			WrappedKey: kmReq.WrappedKey,
		}
		kmRspContent, err := kmRsp.Marshal()
		if err != nil {
			return nil, nil, fmt.Errorf("marshal KMRSP: %w", err)
		}
		resp.Extensions = append(resp.Extensions, packet.HSExtension{
			Type:    ExtTypeKMRSP,
			Length:  uint16(len(kmRspContent) / 4),
			Content: kmRspContent,
		})
	}

	// Step 9: Build the result containing all negotiated parameters.
	result := &HandshakeResult{
		PeerSocketID:  hs.SocketID,
		StreamID:      streamID,
		InitialSeqNum: hs.InitialSeqNumber,
		MTU:           negotiatedMTU,
		FlowWindow:    negotiatedFlowWindow,
		PeerTSBPD:     peerTSBPD,
		LocalTSBPD:    localTSBPD,
		Flags:         negotiatedFlags,
	}

	// Set encryption fields on the result if encryption was negotiated.
	// The Passphrase is stored so the connection can re-derive KEKs during
	// post-handshake key rotation (see HandshakeResult.Passphrase doc).
	if kmReq != nil {
		result.EvenSEK = evenSEK
		result.OddSEK = oddSEK
		result.KK = kmReq.KK
		result.Salt = kmReq.Salt
		result.KeyLen = int(kmReq.KLen)
		result.Encrypted = true
		result.Passphrase = effectivePassphrase
	}

	l.log.Info("handshake concluded",
		"from", from.String(),
		"stream_id", streamID,
		"peer_socket_id", hs.SocketID,
		"negotiated_mtu", negotiatedMTU,
		"negotiated_flags", fmt.Sprintf("0x%08X", negotiatedFlags),
		"peer_tsbpd", peerTSBPD,
		"local_tsbpd", localTSBPD,
	)

	return resp, result, nil
}

// generateInitialSeqNum creates a cryptographically random 31-bit sequence
// number. Using a random starting point prevents sequence number prediction
// attacks and avoids collisions when connections are rapidly recycled.
func generateInitialSeqNum() (uint32, error) {
	// Generate a random number in the range [0, 2^31 - 1].
	max := big.NewInt(0x7FFFFFFF)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return 0, err
	}
	return uint32(n.Int64()), nil
}

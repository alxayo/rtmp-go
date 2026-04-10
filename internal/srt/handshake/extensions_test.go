package handshake

import (
	"encoding/binary"
	"testing"
)

// TestParseHSReqRoundTrip verifies that building an HSRSP payload and parsing
// it back as HSREQ produces the same values (since HSREQ and HSRSP share
// the same wire format).
func TestParseHSReqRoundTrip(t *testing.T) {
	// Build an HSRSP payload with known values.
	version := uint32(0x00010500)
	flags := uint32(FlagTSBPDSND | FlagTSBPDRCV | FlagTLPKTDROP)
	recvDelay := uint16(120)
	sendDelay := uint16(200)

	buf := BuildHSRsp(version, flags, recvDelay, sendDelay)

	// Parse it back — HSREQ and HSRSP use the same format.
	req, err := ParseHSReq(buf)
	if err != nil {
		t.Fatalf("ParseHSReq failed: %v", err)
	}

	if req.SRTVersion != version {
		t.Errorf("SRTVersion: got 0x%08X, want 0x%08X", req.SRTVersion, version)
	}
	if req.SRTFlags != flags {
		t.Errorf("SRTFlags: got 0x%08X, want 0x%08X", req.SRTFlags, flags)
	}
	if req.RecvTSBPD != recvDelay {
		t.Errorf("RecvTSBPD: got %d, want %d", req.RecvTSBPD, recvDelay)
	}
	if req.SenderTSBPD != sendDelay {
		t.Errorf("SenderTSBPD: got %d, want %d", req.SenderTSBPD, sendDelay)
	}
}

// TestParseHSReqTooShort verifies that ParseHSReq rejects payloads that
// are shorter than the required 12 bytes.
func TestParseHSReqTooShort(t *testing.T) {
	// An 8-byte payload is too short (need 12).
	_, err := ParseHSReq([]byte{0, 0, 0, 0, 0, 0, 0, 0})
	if err == nil {
		t.Error("ParseHSReq accepted short payload; expected error")
	}
}

// TestBuildHSRspFormat verifies the exact wire format of the HSRSP payload.
func TestBuildHSRspFormat(t *testing.T) {
	buf := BuildHSRsp(0x00010500, 0x0000003B, 120, 200)

	// Should be exactly 12 bytes (3 * uint32).
	if len(buf) != 12 {
		t.Fatalf("BuildHSRsp length: got %d, want 12", len(buf))
	}

	// Verify each 32-bit word.
	if got := binary.BigEndian.Uint32(buf[0:4]); got != 0x00010500 {
		t.Errorf("word 0 (version): got 0x%08X, want 0x00010500", got)
	}
	if got := binary.BigEndian.Uint32(buf[4:8]); got != 0x0000003B {
		t.Errorf("word 1 (flags): got 0x%08X, want 0x0000003B", got)
	}
	if got := binary.BigEndian.Uint16(buf[8:10]); got != 120 {
		t.Errorf("recv TSBPD: got %d, want 120", got)
	}
	if got := binary.BigEndian.Uint16(buf[10:12]); got != 200 {
		t.Errorf("send TSBPD: got %d, want 200", got)
	}
}

// TestStreamIDExtensionRoundTrip verifies that encoding and decoding a
// stream ID produces the original string.
func TestStreamIDExtensionRoundTrip(t *testing.T) {
	tests := []string{
		"live/test",
		"#!::r=live/test,m=publish",
		"a",
		"ab",
		"abc",
		"abcd",
		"abcde",
		"hello world",
		"publish:live/mystream",
		"a/very/long/stream/key/with/many/segments",
	}

	for _, original := range tests {
		t.Run(original, func(t *testing.T) {
			// Encode the stream ID.
			encoded := BuildStreamIDExtension(original)

			// The encoded length must be a multiple of 4 (padded).
			if len(encoded)%4 != 0 {
				t.Errorf("encoded length %d is not a multiple of 4", len(encoded))
			}

			// Decode it back.
			decoded := ParseStreamIDExtension(encoded)

			if decoded != original {
				t.Errorf("round-trip failed: got %q, want %q", decoded, original)
			}
		})
	}
}

// TestParseStreamIDExtensionEmpty verifies that an empty extension payload
// produces an empty string.
func TestParseStreamIDExtensionEmpty(t *testing.T) {
	result := ParseStreamIDExtension(nil)
	if result != "" {
		t.Errorf("ParseStreamIDExtension(nil): got %q, want %q", result, "")
	}

	result = ParseStreamIDExtension([]byte{})
	if result != "" {
		t.Errorf("ParseStreamIDExtension([]byte{}): got %q, want %q", result, "")
	}
}

// TestBuildStreamIDExtensionEmpty verifies that encoding an empty string
// returns nil (no extension content needed).
func TestBuildStreamIDExtensionEmpty(t *testing.T) {
	result := BuildStreamIDExtension("")
	if result != nil {
		t.Errorf("BuildStreamIDExtension(\"\"): got %v, want nil", result)
	}
}

// TestStreamIDExtensionKnownValues verifies decoding of a known byte sequence.
// The string "live" encoded with SRT's byte-reversal produces specific bytes.
func TestStreamIDExtensionKnownValues(t *testing.T) {
	// "live" = 4 bytes, one 32-bit word.
	// Characters: l(0x6C) i(0x69) v(0x76) e(0x65)
	// After byte reversal within the 4-byte word: e(0x65) v(0x76) i(0x69) l(0x6C)
	encoded := []byte{0x65, 0x76, 0x69, 0x6C}

	result := ParseStreamIDExtension(encoded)
	if result != "live" {
		t.Errorf("ParseStreamIDExtension known value: got %q, want %q", result, "live")
	}
}

// TestStreamIDExtensionPadding verifies that null padding bytes are correctly
// stripped during decoding. A 5-character string is padded to 8 bytes.
func TestStreamIDExtensionPadding(t *testing.T) {
	// Encode a 5-character string — it should be padded to 8 bytes.
	encoded := BuildStreamIDExtension("hello")
	if len(encoded) != 8 {
		t.Fatalf("encoded length: got %d, want 8 (padded from 5)", len(encoded))
	}

	// Decode should strip the padding and return the original.
	decoded := ParseStreamIDExtension(encoded)
	if decoded != "hello" {
		t.Errorf("decoded: got %q, want %q", decoded, "hello")
	}
}

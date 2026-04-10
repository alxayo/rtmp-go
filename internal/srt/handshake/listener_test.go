package handshake

import (
	"log/slog"
	"net"
	"testing"

	"github.com/alxayo/go-rtmp/internal/srt/packet"
)

// testLogger returns a quiet logger for tests (discards all output).
func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// TestHandleInductionValid verifies that a valid Induction handshake
// produces a correct response with a SYN cookie and version 5.
func TestHandleInductionValid(t *testing.T) {
	l := NewListener(42, 120, 1500, 8192, testLogger())
	from := &net.UDPAddr{IP: net.ParseIP("192.168.1.10"), Port: 9000}

	// Build a caller's Induction request (v4 format, cookie=0).
	induction := &packet.HandshakeCIF{
		Version:          4,
		EncryptionField:  0,
		ExtensionField:   2, // Caller may put various values here
		InitialSeqNumber: 1000,
		MTU:              1500,
		FlowWindow:       8192,
		Type:             packet.HSTypeInduction,
		SocketID:         99, // Caller's socket ID
		SYNCookie:        0,  // Always 0 in initial Induction
	}

	resp, err := l.HandleInduction(induction, from)
	if err != nil {
		t.Fatalf("HandleInduction failed: %v", err)
	}

	// Verify response fields.
	// The Induction response echoes most of the caller's CIF fields back,
	// per the SRT v5 spec (matching libsrt behavior).
	if resp.Version != 5 {
		t.Errorf("Version: got %d, want 5", resp.Version)
	}
	if resp.Type != packet.HSTypeInduction {
		t.Errorf("Type: got %d, want HSTypeInduction (%d)", resp.Type, packet.HSTypeInduction)
	}
	// SocketID is echoed back from the caller (99), NOT our local SID (42).
	if resp.SocketID != 99 {
		t.Errorf("SocketID: got %d, want 99 (caller's SID echoed)", resp.SocketID)
	}
	if resp.SYNCookie == 0 {
		t.Error("SYNCookie: got 0, expected non-zero cookie")
	}
	if resp.ExtensionField != 0x4A17 {
		t.Errorf("ExtensionField: got 0x%04X, want 0x4A17 (SRT magic)", resp.ExtensionField)
	}
	// MTU, FlowWindow, and ISN are echoed from the caller's request.
	if resp.MTU != 1500 {
		t.Errorf("MTU: got %d, want 1500", resp.MTU)
	}
	if resp.FlowWindow != 8192 {
		t.Errorf("FlowWindow: got %d, want 8192", resp.FlowWindow)
	}
	if resp.InitialSeqNumber != 1000 {
		t.Errorf("InitialSeqNumber: got %d, want 1000 (caller's ISN echoed)", resp.InitialSeqNumber)
	}
}

// TestHandleInductionWrongType verifies that HandleInduction rejects a
// handshake that is not of type Induction (e.g., Conclusion).
func TestHandleInductionWrongType(t *testing.T) {
	l := NewListener(42, 120, 1500, 8192, testLogger())
	from := &net.UDPAddr{IP: net.ParseIP("192.168.1.10"), Port: 9000}

	// Send a Conclusion instead of Induction — should be rejected.
	conclusion := &packet.HandshakeCIF{
		Type: packet.HSTypeConclusion,
	}

	_, err := l.HandleInduction(conclusion, from)
	if err == nil {
		t.Fatal("HandleInduction accepted wrong type; expected error")
	}
}

// TestHandleConclusionValid verifies that a valid Conclusion handshake
// (with correct cookie and extensions) produces the expected result.
func TestHandleConclusionValid(t *testing.T) {
	l := NewListener(42, 120, 1500, 8192, testLogger())
	from := &net.UDPAddr{IP: net.ParseIP("192.168.1.10"), Port: 9000}

	// First, do Induction to get a valid cookie.
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

	// Build the HSREQ extension payload.
	hsReqContent := BuildHSRsp(
		0x00010500,
		FlagTSBPDSND|FlagTSBPDRCV|FlagTLPKTDROP|FlagPERIODICNAK|FlagREXMITFLG,
		120, // Recv TSBPD
		120, // Sender TSBPD
	)

	// Build the SID extension payload.
	sidContent := BuildStreamIDExtension("live/mystream")

	// Build the Conclusion with the cookie from Induction response.
	conclusion := &packet.HandshakeCIF{
		Version:          5,
		EncryptionField:  0,
		ExtensionField:   extensionFlagHSREQ | extensionFlagSID,
		InitialSeqNumber: 1000,
		MTU:              1500,
		FlowWindow:       8192,
		Type:             packet.HSTypeConclusion,
		SocketID:         99,
		SYNCookie:        inductionResp.SYNCookie, // Echo back the cookie
		Extensions: []packet.HSExtension{
			{
				Type:    ExtTypeHSREQ,
				Length:  uint16(len(hsReqContent) / 4),
				Content: hsReqContent,
			},
			{
				Type:    ExtTypeSID,
				Length:  uint16(len(sidContent) / 4),
				Content: sidContent,
			},
		},
	}

	resp, result, err := l.HandleConclusion(conclusion, from)
	if err != nil {
		t.Fatalf("HandleConclusion failed: %v", err)
	}

	// Verify the response CIF.
	if resp.Version != 5 {
		t.Errorf("Response Version: got %d, want 5", resp.Version)
	}
	if resp.Type != packet.HSTypeConclusion {
		t.Errorf("Response Type: got %d, want HSTypeConclusion", resp.Type)
	}
	if resp.SocketID != 42 {
		t.Errorf("Response SocketID: got %d, want 42", resp.SocketID)
	}

	// Verify the HSRSP extension is present.
	if len(resp.Extensions) == 0 {
		t.Fatal("Response has no extensions; expected HSRSP")
	}
	if resp.Extensions[0].Type != ExtTypeHSRSP {
		t.Errorf("Response extension type: got %d, want %d (HSRSP)", resp.Extensions[0].Type, ExtTypeHSRSP)
	}

	// Verify the negotiated result.
	if result.PeerSocketID != 99 {
		t.Errorf("PeerSocketID: got %d, want 99", result.PeerSocketID)
	}
	if result.StreamID != "live/mystream" {
		t.Errorf("StreamID: got %q, want %q", result.StreamID, "live/mystream")
	}
	if result.MTU != 1500 {
		t.Errorf("MTU: got %d, want 1500", result.MTU)
	}
	if result.FlowWindow != 8192 {
		t.Errorf("FlowWindow: got %d, want 8192", result.FlowWindow)
	}
	if result.Flags == 0 {
		t.Error("Flags: got 0, expected negotiated flags")
	}
	if result.PeerTSBPD != 120 {
		t.Errorf("PeerTSBPD: got %d, want 120", result.PeerTSBPD)
	}
	if result.LocalTSBPD != 120 {
		t.Errorf("LocalTSBPD: got %d, want 120", result.LocalTSBPD)
	}
}

// TestHandleConclusionInvalidCookie verifies that a Conclusion with the
// wrong SYN cookie is rejected.
func TestHandleConclusionInvalidCookie(t *testing.T) {
	l := NewListener(42, 120, 1500, 8192, testLogger())
	from := &net.UDPAddr{IP: net.ParseIP("192.168.1.10"), Port: 9000}

	// Build HSREQ extension.
	hsReqContent := BuildHSRsp(0x00010500, DefaultFlags, 120, 120)

	// Build a Conclusion with a bogus cookie (not from our Induction).
	conclusion := &packet.HandshakeCIF{
		Version:    5,
		Type:       packet.HSTypeConclusion,
		SocketID:   99,
		SYNCookie:  0xDEADBEEF, // Wrong cookie
		MTU:        1500,
		FlowWindow: 8192,
		Extensions: []packet.HSExtension{
			{
				Type:    ExtTypeHSREQ,
				Length:  uint16(len(hsReqContent) / 4),
				Content: hsReqContent,
			},
		},
	}

	_, _, err := l.HandleConclusion(conclusion, from)
	if err == nil {
		t.Fatal("HandleConclusion accepted invalid cookie; expected error")
	}
}

// TestHandleConclusionMissingHSREQ verifies that a Conclusion without an
// HSREQ extension is rejected (HSREQ is mandatory).
func TestHandleConclusionMissingHSREQ(t *testing.T) {
	l := NewListener(42, 120, 1500, 8192, testLogger())
	from := &net.UDPAddr{IP: net.ParseIP("192.168.1.10"), Port: 9000}

	// Do Induction to get a valid cookie.
	induction := &packet.HandshakeCIF{
		Version:   4,
		Type:      packet.HSTypeInduction,
		SocketID:  99,
		SYNCookie: 0,
	}

	inductionResp, err := l.HandleInduction(induction, from)
	if err != nil {
		t.Fatalf("HandleInduction failed: %v", err)
	}

	// Send Conclusion with valid cookie but NO HSREQ extension.
	conclusion := &packet.HandshakeCIF{
		Version:    5,
		Type:       packet.HSTypeConclusion,
		SocketID:   99,
		SYNCookie:  inductionResp.SYNCookie,
		MTU:        1500,
		FlowWindow: 8192,
		// No extensions — HSREQ is missing
	}

	_, _, err = l.HandleConclusion(conclusion, from)
	if err == nil {
		t.Fatal("HandleConclusion accepted missing HSREQ; expected error")
	}
}

// TestFullInductionConclusionExchange simulates a complete two-phase handshake:
// Induction → Conclusion, verifying the full flow works end-to-end.
func TestFullInductionConclusionExchange(t *testing.T) {
	l := NewListener(42, 150, 1400, 4096, testLogger())
	from := &net.UDPAddr{IP: net.ParseIP("10.0.0.5"), Port: 3000}

	// --- Phase 1: Induction ---
	inductionReq := &packet.HandshakeCIF{
		Version:          4,
		InitialSeqNumber: 12345,
		MTU:              1300, // Caller has smaller MTU
		FlowWindow:       2048, // Caller has smaller flow window
		Type:             packet.HSTypeInduction,
		SocketID:         777,
		SYNCookie:        0,
	}

	inductionResp, err := l.HandleInduction(inductionReq, from)
	if err != nil {
		t.Fatalf("Induction failed: %v", err)
	}

	// --- Phase 2: Conclusion ---
	hsReqContent := BuildHSRsp(
		0x00010400, // Caller's SRT version (v1.4.0)
		FlagTSBPDSND|FlagTSBPDRCV|FlagTLPKTDROP|FlagREXMITFLG,
		200, // Caller wants us to buffer 200ms
		100, // Caller buffers 100ms
	)

	sidContent := BuildStreamIDExtension("#!::r=sports/football,m=publish,u=broadcaster1")

	conclusionReq := &packet.HandshakeCIF{
		Version:          5,
		ExtensionField:   extensionFlagHSREQ | extensionFlagSID,
		InitialSeqNumber: 12345,
		MTU:              1300,
		FlowWindow:       2048,
		Type:             packet.HSTypeConclusion,
		SocketID:         777,
		SYNCookie:        inductionResp.SYNCookie,
		Extensions: []packet.HSExtension{
			{
				Type:    ExtTypeHSREQ,
				Length:  uint16(len(hsReqContent) / 4),
				Content: hsReqContent,
			},
			{
				Type:    ExtTypeSID,
				Length:  uint16(len(sidContent) / 4),
				Content: sidContent,
			},
		},
	}

	resp, result, err := l.HandleConclusion(conclusionReq, from)
	if err != nil {
		t.Fatalf("Conclusion failed: %v", err)
	}

	// Verify response.
	if resp.Version != 5 {
		t.Errorf("Response Version: got %d, want 5", resp.Version)
	}

	// Verify negotiated values.
	if result.PeerSocketID != 777 {
		t.Errorf("PeerSocketID: got %d, want 777", result.PeerSocketID)
	}
	if result.StreamID != "#!::r=sports/football,m=publish,u=broadcaster1" {
		t.Errorf("StreamID: got %q", result.StreamID)
	}

	// MTU should be min(1400, 1300) = 1300.
	if result.MTU != 1300 {
		t.Errorf("MTU: got %d, want 1300 (min of both sides)", result.MTU)
	}

	// FlowWindow should be min(4096, 2048) = 2048.
	if result.FlowWindow != 2048 {
		t.Errorf("FlowWindow: got %d, want 2048 (min of both sides)", result.FlowWindow)
	}

	// TSBPD: peer wants us to buffer 200ms, we want 150ms → max = 200ms for peer.
	// Our local: we want 150ms, caller's sender delay is 100ms → max = 150ms.
	if result.PeerTSBPD != 200 {
		t.Errorf("PeerTSBPD: got %d, want 200 (max of 200 and our 150)", result.PeerTSBPD)
	}
	if result.LocalTSBPD != 150 {
		t.Errorf("LocalTSBPD: got %d, want 150 (max of 100 and our 150)", result.LocalTSBPD)
	}

	// Flags should be the intersection (AND) of both sides.
	expectedFlags := (FlagTSBPDSND | FlagTSBPDRCV | FlagTLPKTDROP | FlagREXMITFLG) & DefaultFlags
	if result.Flags != expectedFlags {
		t.Errorf("Flags: got 0x%08X, want 0x%08X", result.Flags, expectedFlags)
	}

	// Verify the HSRSP extension in the response can be parsed.
	if len(resp.Extensions) == 0 {
		t.Fatal("no HSRSP extension in response")
	}
	hsRsp, err := ParseHSReq(resp.Extensions[0].Content)
	if err != nil {
		t.Fatalf("parse HSRSP from response: %v", err)
	}
	if hsRsp.SRTVersion != SRTVersion {
		t.Errorf("HSRSP version: got 0x%08X, want 0x%08X", hsRsp.SRTVersion, SRTVersion)
	}
}

// TestHandleConclusionNoStreamID verifies that the handshake succeeds even
// when no Stream ID extension is present (stream ID is optional).
func TestHandleConclusionNoStreamID(t *testing.T) {
	l := NewListener(42, 120, 1500, 8192, testLogger())
	from := &net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 5000}

	// Induction first.
	induction := &packet.HandshakeCIF{
		Version:   4,
		Type:      packet.HSTypeInduction,
		SocketID:  50,
		SYNCookie: 0,
		MTU:       1500,
		FlowWindow: 8192,
	}

	inductionResp, err := l.HandleInduction(induction, from)
	if err != nil {
		t.Fatalf("HandleInduction failed: %v", err)
	}

	// Conclusion with HSREQ but NO SID extension.
	hsReqContent := BuildHSRsp(0x00010500, DefaultFlags, 120, 120)

	conclusion := &packet.HandshakeCIF{
		Version:    5,
		Type:       packet.HSTypeConclusion,
		SocketID:   50,
		SYNCookie:  inductionResp.SYNCookie,
		MTU:        1500,
		FlowWindow: 8192,
		Extensions: []packet.HSExtension{
			{
				Type:    ExtTypeHSREQ,
				Length:  uint16(len(hsReqContent) / 4),
				Content: hsReqContent,
			},
		},
	}

	_, result, err := l.HandleConclusion(conclusion, from)
	if err != nil {
		t.Fatalf("HandleConclusion failed: %v", err)
	}

	// Stream ID should be empty when no SID extension was sent.
	if result.StreamID != "" {
		t.Errorf("StreamID: got %q, want empty string", result.StreamID)
	}
}

package ts

import (
	"testing"
)

// TestParsePacket_ValidPayloadOnly tests parsing a basic TS packet that
// has payload but no adaptation field (adaptation control = 01).
func TestParsePacket_ValidPayloadOnly(t *testing.T) {
	// Build a valid 188-byte packet by hand.
	var data [PacketSize]byte

	// Byte 0: sync byte
	data[0] = SyncByte

	// Bytes 1-2: TEI=0 | PUSI=1 | Priority=0 | PID=0x0100 (256)
	// Binary: 0_1_0_00001 00000000
	data[1] = 0x41 // 0100_0001: PUSI=1, PID high=0x01
	data[2] = 0x00 // PID low=0x00

	// Byte 3: Scrambling=00 | AdaptationControl=01 (payload only) | CC=5
	data[3] = 0x15 // 00_01_0101

	// Fill payload with a recognizable pattern.
	for i := 4; i < PacketSize; i++ {
		data[i] = byte(i & 0xFF)
	}

	pkt, err := ParsePacket(data)
	if err != nil {
		t.Fatalf("ParsePacket failed: %v", err)
	}

	// Verify all header fields.
	if pkt.TEI {
		t.Error("expected TEI=false")
	}
	if !pkt.PayloadUnitStart {
		t.Error("expected PayloadUnitStart=true")
	}
	if pkt.Priority {
		t.Error("expected Priority=false")
	}
	if pkt.PID != 0x0100 {
		t.Errorf("expected PID=0x0100, got 0x%04X", pkt.PID)
	}
	if pkt.Scrambling != 0 {
		t.Errorf("expected Scrambling=0, got %d", pkt.Scrambling)
	}
	if pkt.HasAdaptation {
		t.Error("expected HasAdaptation=false")
	}
	if !pkt.HasPayload {
		t.Error("expected HasPayload=true")
	}
	if pkt.ContinuityCounter != 5 {
		t.Errorf("expected CC=5, got %d", pkt.ContinuityCounter)
	}
	if pkt.AdaptationField != nil {
		t.Error("expected no adaptation field")
	}

	// Payload should be bytes 4..187 (184 bytes).
	if len(pkt.Payload) != 184 {
		t.Errorf("expected payload length 184, got %d", len(pkt.Payload))
	}
	// Verify first payload byte.
	if pkt.Payload[0] != 4 {
		t.Errorf("expected first payload byte=4, got %d", pkt.Payload[0])
	}
}

// TestParsePacket_WithAdaptationField tests parsing a packet that has
// both an adaptation field and payload (adaptation control = 11).
func TestParsePacket_WithAdaptationField(t *testing.T) {
	var data [PacketSize]byte

	// Sync byte
	data[0] = SyncByte

	// PID=0x0021 (33), PUSI=0
	data[1] = 0x00 // TEI=0, PUSI=0, Priority=0, PID high=0x00
	data[2] = 0x21 // PID low=0x21

	// Scrambling=00, AdaptationControl=11 (both), CC=7
	data[3] = 0x37 // 00_11_0111

	// Adaptation field:
	// Byte 4: length = 7 (7 bytes of adaptation field data follow)
	data[4] = 7

	// Byte 5: flags = discontinuity=0, random_access=1, priority=0, PCR=1, ...
	data[5] = 0x50 // 0101_0000: random_access=1, PCR_flag=1

	// Bytes 6-11: PCR (6 bytes)
	// Let's encode PCR base = 12345 (in 90kHz ticks).
	// PCR base is 33 bits spread across 4.125 bytes:
	//   data[6] = base[32:25] = 12345 >> 25 = 0
	//   data[7] = base[24:17] = (12345 >> 17) & 0xFF = 0
	//   data[8] = base[16:9]  = (12345 >> 9) & 0xFF = 24
	//   data[9] = base[8:1]   = (12345 >> 1) & 0xFF = 28 (12345 >> 1 = 6172, &0xFF = 28)
	//   data[10] bit 7 = base[0] = 12345 & 1 = 1 → 0x80, rest reserved+ext
	// Let me compute: 12345 in binary = 11000000111001
	// 33 bits: 0_00000000_00000000_00110000_00111001
	// base[32:25] (8 bits) = 00000000 = 0x00
	// base[24:17] (8 bits) = 00000000 = 0x00
	// base[16:9] (8 bits) = 00110000 = 0x30  (but actually let me recount)
	// 12345 = 0x3039
	// In 33 bits: 0 00000000 00000000 00110000 00111001
	// base >> 25 = 0
	// (base >> 17) & 0xFF = 0
	// (base >> 9) & 0xFF = (12345 >> 9) = 24 = 0x18
	// Wait: 12345 >> 9 = 24 (12345 / 512 = 24.11...)
	// (base >> 1) & 0xFF = 6172 & 0xFF = 0x1C = 28
	// base & 1 = 1
	var pcrVal int64 = 12345
	data[6] = byte(pcrVal >> 25)    // base[32:25]
	data[7] = byte(pcrVal >> 17)    // base[24:17]
	data[8] = byte(pcrVal >> 9)     // base[16:9]
	data[9] = byte(pcrVal >> 1)     // base[8:1]
	data[10] = byte(pcrVal&1) << 7  // base[0] in bit 7, rest = 0
	data[11] = 0x00                 // extension low byte

	// Payload starts at byte 4 + 1 + 7 = 12
	for i := 12; i < PacketSize; i++ {
		data[i] = 0xAA
	}

	pkt, err := ParsePacket(data)
	if err != nil {
		t.Fatalf("ParsePacket failed: %v", err)
	}

	if !pkt.HasAdaptation {
		t.Fatal("expected HasAdaptation=true")
	}
	if !pkt.HasPayload {
		t.Fatal("expected HasPayload=true")
	}
	if pkt.AdaptationField == nil {
		t.Fatal("expected adaptation field to be present")
	}

	af := pkt.AdaptationField
	if af.Length != 7 {
		t.Errorf("expected adaptation field length=7, got %d", af.Length)
	}
	if af.Discontinuity {
		t.Error("expected Discontinuity=false")
	}
	if !af.RandomAccess {
		t.Error("expected RandomAccess=true")
	}
	if af.PCR != pcrVal {
		t.Errorf("expected PCR=%d, got %d", pcrVal, af.PCR)
	}

	// Payload should be PacketSize - 4(header) - 1(af length byte) - 7(af data) = 176 bytes
	expectedPayloadLen := PacketSize - 4 - 1 - 7
	if len(pkt.Payload) != expectedPayloadLen {
		t.Errorf("expected payload length=%d, got %d", expectedPayloadLen, len(pkt.Payload))
	}
}

// TestParsePacket_AdaptationOnly tests a packet with adaptation field
// only and no payload (adaptation control = 10).
func TestParsePacket_AdaptationOnly(t *testing.T) {
	var data [PacketSize]byte
	data[0] = SyncByte
	data[1] = 0x00
	data[2] = 0x42 // PID = 0x0042
	data[3] = 0x20 // AdaptationControl=10 (adaptation only), CC=0

	// Adaptation field fills the rest of the packet.
	data[4] = 183 // Length = 183 (fills remaining bytes)
	data[5] = 0x00 // No flags set

	pkt, err := ParsePacket(data)
	if err != nil {
		t.Fatalf("ParsePacket failed: %v", err)
	}

	if !pkt.HasAdaptation {
		t.Error("expected HasAdaptation=true")
	}
	if pkt.HasPayload {
		t.Error("expected HasPayload=false")
	}
	if pkt.AdaptationField.PCR != -1 {
		t.Errorf("expected PCR=-1 (no PCR), got %d", pkt.AdaptationField.PCR)
	}
}

// TestParsePacket_InvalidSyncByte verifies that a packet with wrong sync byte
// is rejected with an error.
func TestParsePacket_InvalidSyncByte(t *testing.T) {
	var data [PacketSize]byte
	data[0] = 0x00 // Wrong sync byte

	_, err := ParsePacket(data)
	if err == nil {
		t.Fatal("expected error for invalid sync byte")
	}
}

// TestParsePacket_NullPacket tests parsing a null/padding packet (PID 0x1FFF).
func TestParsePacket_NullPacket(t *testing.T) {
	var data [PacketSize]byte
	data[0] = SyncByte
	data[1] = 0x1F // PID high bits = 0x1F
	data[2] = 0xFF // PID low bits = 0xFF → PID = 0x1FFF
	data[3] = 0x10 // Payload only, CC=0

	pkt, err := ParsePacket(data)
	if err != nil {
		t.Fatalf("ParsePacket failed: %v", err)
	}

	if pkt.PID != NullPID {
		t.Errorf("expected PID=0x1FFF, got 0x%04X", pkt.PID)
	}
}

// TestParsePacket_AllHeaderFlags tests that all header flag bits are
// correctly extracted.
func TestParsePacket_AllHeaderFlags(t *testing.T) {
	var data [PacketSize]byte
	data[0] = SyncByte
	data[1] = 0xFF // TEI=1, PUSI=1, Priority=1, PID high=0x1F
	data[2] = 0xFF // PID low=0xFF → PID = 0x1FFF
	data[3] = 0xFF // Scrambling=11, Adaptation=11, CC=15

	// Need adaptation field since adaptation control = 11
	data[4] = 0 // Adaptation field length = 0

	pkt, err := ParsePacket(data)
	if err != nil {
		t.Fatalf("ParsePacket failed: %v", err)
	}

	if !pkt.TEI {
		t.Error("expected TEI=true")
	}
	if !pkt.PayloadUnitStart {
		t.Error("expected PayloadUnitStart=true")
	}
	if !pkt.Priority {
		t.Error("expected Priority=true")
	}
	if pkt.PID != 0x1FFF {
		t.Errorf("expected PID=0x1FFF, got 0x%04X", pkt.PID)
	}
	if pkt.Scrambling != 3 {
		t.Errorf("expected Scrambling=3, got %d", pkt.Scrambling)
	}
	if pkt.ContinuityCounter != 15 {
		t.Errorf("expected CC=15, got %d", pkt.ContinuityCounter)
	}
}

// TestParsePacket_PCRLargeValue tests PCR parsing with a larger value.
func TestParsePacket_PCRLargeValue(t *testing.T) {
	var data [PacketSize]byte
	data[0] = SyncByte
	data[1] = 0x00
	data[2] = 0x50
	data[3] = 0x30 // Adaptation + payload, CC=0

	// Adaptation field with PCR
	data[4] = 7    // AF length
	data[5] = 0x10 // PCR flag set

	// Encode PCR base = 8100000 (90 seconds at 90kHz)
	// 8100000 = 0x7BA940
	// 33 bits: 0 00000000 00000000 01111011 10101001 01000000
	// Hmm let me recalculate:
	// 8100000 in binary: 11110111010100101000000
	// That's 23 bits. In 33 bits:
	// 0000000000_01111011_10101001_01000000
	// Split per parsePCR:
	//   data[6] = base >> 25 = 0
	//   data[7] = (base >> 17) & 0xFF = 0x3D = 61
	//   Actually let me just compute:
	var pcrBase int64 = 8100000
	data[6] = byte(pcrBase >> 25)
	data[7] = byte(pcrBase >> 17)
	data[8] = byte(pcrBase >> 9)
	data[9] = byte(pcrBase >> 1)
	data[10] = byte(pcrBase&1) << 7
	data[11] = 0x00

	pkt, err := ParsePacket(data)
	if err != nil {
		t.Fatalf("ParsePacket failed: %v", err)
	}

	if pkt.AdaptationField.PCR != pcrBase {
		t.Errorf("expected PCR=%d, got %d", pcrBase, pkt.AdaptationField.PCR)
	}
}

// TestStreamTypeName verifies human-readable names for all known stream types.
func TestStreamTypeName(t *testing.T) {
	tests := []struct {
		streamType uint8
		wantName   string
	}{
		{StreamTypeMPEG2Video, "MPEG-2 Video"},
		{StreamTypeMPEG1Audio, "MPEG-1 Audio"},
		{StreamTypeMPEG2Audio, "MPEG-2 Audio"},
		{StreamTypeAAC_ADTS, "AAC (ADTS)"},
		{StreamTypeAAC_LATM, "AAC (LATM)"},
		{StreamTypeH264, "H.264"},
		{StreamTypeH265, "H.265"},
		{0xFF, "Unknown"},
	}

	for _, tc := range tests {
		got := StreamTypeName(tc.streamType)
		if got != tc.wantName {
			t.Errorf("StreamTypeName(0x%02X) = %q, want %q", tc.streamType, got, tc.wantName)
		}
	}
}

package packet

import (
	"bytes"
	"testing"
)

// TestHandshakeCIF_RoundTrip verifies that marshaling a HandshakeCIF and
// unmarshaling it back produces identical results.
func TestHandshakeCIF_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		cif  HandshakeCIF
	}{
		{
			name: "induction_v5",
			cif: HandshakeCIF{
				Version:          5,
				EncryptionField:  0,
				ExtensionField:   0x4A17, // SRT magic
				InitialSeqNumber: 0,
				MTU:              1500,
				FlowWindow:       8192,
				Type:             HSTypeInduction,
				SocketID:         12345,
				SYNCookie:        0,
				PeerIP:           [16]byte{127, 0, 0, 1}, // IPv4 loopback
			},
		},
		{
			name: "conclusion_with_cookie",
			cif: HandshakeCIF{
				Version:          5,
				EncryptionField:  2, // AES-128
				ExtensionField:   0x4A17,
				InitialSeqNumber: 1000,
				MTU:              1500,
				FlowWindow:       25600,
				Type:             HSTypeConclusion,
				SocketID:         67890,
				SYNCookie:        0xDEADBEEF,
				PeerIP:           [16]byte{192, 168, 1, 100},
			},
		},
		{
			name: "max_values",
			cif: HandshakeCIF{
				Version:          0xFFFFFFFF,
				EncryptionField:  0xFFFF,
				ExtensionField:   0xFFFF,
				InitialSeqNumber: maxSequenceNumber,
				MTU:              0xFFFFFFFF,
				FlowWindow:       0xFFFFFFFF,
				Type:             HSTypeConclusion,
				SocketID:         0xFFFFFFFF,
				SYNCookie:        0xFFFFFFFF,
				PeerIP:           [16]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
			},
		},
		{
			name: "zero_values",
			cif: HandshakeCIF{
				Type: HSTypeWaveAHand,
			},
		},
		{
			name: "agreement_type",
			cif: HandshakeCIF{
				Version:          5,
				MTU:              1500,
				FlowWindow:       8192,
				Type:             HSTypeAgreement,
				SocketID:         99,
				InitialSeqNumber: 42,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := tt.cif.MarshalBinary()
			if err != nil {
				t.Fatalf("MarshalBinary failed: %v", err)
			}

			// The fixed CIF should be at least 48 bytes.
			if len(data) < HandshakeCIFSize {
				t.Fatalf("marshaled data too short: got %d, want >= %d", len(data), HandshakeCIFSize)
			}

			got, err := UnmarshalHandshakeCIF(data)
			if err != nil {
				t.Fatalf("UnmarshalHandshakeCIF failed: %v", err)
			}

			// Compare every field.
			if got.Version != tt.cif.Version {
				t.Errorf("Version: got %d, want %d", got.Version, tt.cif.Version)
			}
			if got.EncryptionField != tt.cif.EncryptionField {
				t.Errorf("EncryptionField: got %d, want %d", got.EncryptionField, tt.cif.EncryptionField)
			}
			if got.ExtensionField != tt.cif.ExtensionField {
				t.Errorf("ExtensionField: got %d, want %d", got.ExtensionField, tt.cif.ExtensionField)
			}
			if got.InitialSeqNumber != tt.cif.InitialSeqNumber {
				t.Errorf("InitialSeqNumber: got %d, want %d", got.InitialSeqNumber, tt.cif.InitialSeqNumber)
			}
			if got.MTU != tt.cif.MTU {
				t.Errorf("MTU: got %d, want %d", got.MTU, tt.cif.MTU)
			}
			if got.FlowWindow != tt.cif.FlowWindow {
				t.Errorf("FlowWindow: got %d, want %d", got.FlowWindow, tt.cif.FlowWindow)
			}
			if got.Type != tt.cif.Type {
				t.Errorf("Type: got %d, want %d", got.Type, tt.cif.Type)
			}
			if got.SocketID != tt.cif.SocketID {
				t.Errorf("SocketID: got %d, want %d", got.SocketID, tt.cif.SocketID)
			}
			if got.SYNCookie != tt.cif.SYNCookie {
				t.Errorf("SYNCookie: got 0x%08X, want 0x%08X", got.SYNCookie, tt.cif.SYNCookie)
			}
			if got.PeerIP != tt.cif.PeerIP {
				t.Errorf("PeerIP: got %v, want %v", got.PeerIP, tt.cif.PeerIP)
			}
		})
	}
}

// TestHandshakeCIF_WithExtensions verifies that handshake extensions
// survive the round-trip marshal→unmarshal cycle.
func TestHandshakeCIF_WithExtensions(t *testing.T) {
	cif := HandshakeCIF{
		Version:          5,
		ExtensionField:   0x4A17,
		MTU:              1500,
		FlowWindow:       8192,
		Type:             HSTypeConclusion,
		SocketID:         100,
		SYNCookie:        0xCAFEBABE,
		Extensions: []HSExtension{
			{
				Type:    1, // e.g., SRT_CMD_HSREQ
				Length:  3, // 3 * 4 = 12 bytes of content
				Content: []byte{
					0x00, 0x05, 0x01, 0x32, // Version + flags
					0x00, 0x00, 0x00, 0x00,
					0x00, 0x00, 0x00, 0x00,
				},
			},
			{
				Type:    2, // e.g., SRT_CMD_HSRSP
				Length:  1, // 1 * 4 = 4 bytes of content
				Content: []byte{0xAA, 0xBB, 0xCC, 0xDD},
			},
		},
	}

	data, err := cif.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary failed: %v", err)
	}

	// Expected size: 48 (fixed) + 4+12 (ext1) + 4+4 (ext2) = 72
	expectedSize := 48 + 4 + 12 + 4 + 4
	if len(data) != expectedSize {
		t.Fatalf("marshaled size: got %d, want %d", len(data), expectedSize)
	}

	got, err := UnmarshalHandshakeCIF(data)
	if err != nil {
		t.Fatalf("UnmarshalHandshakeCIF failed: %v", err)
	}

	if len(got.Extensions) != 2 {
		t.Fatalf("Extensions count: got %d, want 2", len(got.Extensions))
	}

	// Check first extension.
	if got.Extensions[0].Type != 1 {
		t.Errorf("ext[0].Type: got %d, want 1", got.Extensions[0].Type)
	}
	if got.Extensions[0].Length != 3 {
		t.Errorf("ext[0].Length: got %d, want 3", got.Extensions[0].Length)
	}
	if !bytes.Equal(got.Extensions[0].Content, cif.Extensions[0].Content) {
		t.Errorf("ext[0].Content mismatch")
	}

	// Check second extension.
	if got.Extensions[1].Type != 2 {
		t.Errorf("ext[1].Type: got %d, want 2", got.Extensions[1].Type)
	}
	if got.Extensions[1].Length != 1 {
		t.Errorf("ext[1].Length: got %d, want 1", got.Extensions[1].Length)
	}
	if !bytes.Equal(got.Extensions[1].Content, cif.Extensions[1].Content) {
		t.Errorf("ext[1].Content mismatch")
	}
}

// TestHandshakeCIF_NoExtensions verifies that a CIF with no extensions
// marshals to exactly 48 bytes.
func TestHandshakeCIF_NoExtensions(t *testing.T) {
	cif := HandshakeCIF{
		Version: 5,
		Type:    HSTypeInduction,
	}

	data, err := cif.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary failed: %v", err)
	}

	if len(data) != HandshakeCIFSize {
		t.Errorf("size: got %d, want %d", len(data), HandshakeCIFSize)
	}
}

// TestUnmarshalHandshakeCIF_TooShort verifies error handling for buffers
// shorter than the minimum 48 bytes.
func TestUnmarshalHandshakeCIF_TooShort(t *testing.T) {
	tests := []struct {
		name string
		size int
	}{
		{"empty", 0},
		{"one_byte", 1},
		{"almost", 47},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := UnmarshalHandshakeCIF(make([]byte, tt.size))
			if err == nil {
				t.Error("expected error for short buffer, got nil")
			}
		})
	}
}

// TestUnmarshalHandshakeCIF_TruncatedExtension verifies that a truncated
// extension (header present but content cut short) produces an error.
func TestUnmarshalHandshakeCIF_TruncatedExtension(t *testing.T) {
	// Build a valid 48-byte CIF + 4-byte extension header claiming 2 blocks
	// (8 bytes of content), but only provide 4 bytes of content.
	buf := make([]byte, 48+4+4) // 48 fixed + 4 ext header + 4 bytes (not 8)
	// Extension header at offset 48: type=1, length=2 (needs 8 bytes)
	buf[48] = 0x00
	buf[49] = 0x01 // type = 1
	buf[50] = 0x00
	buf[51] = 0x02 // length = 2 (needs 2*4=8 bytes content)
	// Only 4 bytes of content follow, which is not enough.

	_, err := UnmarshalHandshakeCIF(buf)
	if err == nil {
		t.Error("expected error for truncated extension, got nil")
	}
}

// TestHandshakeCIF_ExtensionZeroLength verifies that an extension with
// Length=0 (no content) round-trips correctly.
func TestHandshakeCIF_ExtensionZeroLength(t *testing.T) {
	cif := HandshakeCIF{
		Version: 5,
		Type:    HSTypeConclusion,
		Extensions: []HSExtension{
			{Type: 0x00FF, Length: 0, Content: nil},
		},
	}

	data, err := cif.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary failed: %v", err)
	}

	// 48 bytes fixed + 4 bytes extension header + 0 bytes content
	if len(data) != 52 {
		t.Fatalf("size: got %d, want 52", len(data))
	}

	got, err := UnmarshalHandshakeCIF(data)
	if err != nil {
		t.Fatalf("UnmarshalHandshakeCIF failed: %v", err)
	}

	if len(got.Extensions) != 1 {
		t.Fatalf("Extensions count: got %d, want 1", len(got.Extensions))
	}
	if got.Extensions[0].Type != 0x00FF {
		t.Errorf("ext.Type: got %d, want 255", got.Extensions[0].Type)
	}
	if got.Extensions[0].Length != 0 {
		t.Errorf("ext.Length: got %d, want 0", got.Extensions[0].Length)
	}
}

// TestACKCIF_RoundTrip verifies the ACK CIF round-trip.
func TestACKCIF_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		ack  ACKCIF
	}{
		{
			name: "typical_values",
			ack: ACKCIF{
				LastACKPacketSeq: 5000,
				RTT:             25000,   // 25ms
				RTTVariance:     5000,    // 5ms
				AvailableBuffer: 1000,
				PacketsReceiving: 300,
				EstBandwidth:    500,
				ReceivingRate:   625000,  // ~5 Mbps
			},
		},
		{
			name: "zero_values",
			ack:  ACKCIF{},
		},
		{
			name: "max_values",
			ack: ACKCIF{
				LastACKPacketSeq: 0xFFFFFFFF,
				RTT:             0xFFFFFFFF,
				RTTVariance:     0xFFFFFFFF,
				AvailableBuffer: 0xFFFFFFFF,
				PacketsReceiving: 0xFFFFFFFF,
				EstBandwidth:    0xFFFFFFFF,
				ReceivingRate:   0xFFFFFFFF,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := tt.ack.MarshalBinary()
			if err != nil {
				t.Fatalf("MarshalBinary failed: %v", err)
			}

			if len(data) != ACKCIFSize {
				t.Fatalf("size: got %d, want %d", len(data), ACKCIFSize)
			}

			got, err := UnmarshalACKCIF(data)
			if err != nil {
				t.Fatalf("UnmarshalACKCIF failed: %v", err)
			}

			if got.LastACKPacketSeq != tt.ack.LastACKPacketSeq {
				t.Errorf("LastACKPacketSeq: got %d, want %d", got.LastACKPacketSeq, tt.ack.LastACKPacketSeq)
			}
			if got.RTT != tt.ack.RTT {
				t.Errorf("RTT: got %d, want %d", got.RTT, tt.ack.RTT)
			}
			if got.RTTVariance != tt.ack.RTTVariance {
				t.Errorf("RTTVariance: got %d, want %d", got.RTTVariance, tt.ack.RTTVariance)
			}
			if got.AvailableBuffer != tt.ack.AvailableBuffer {
				t.Errorf("AvailableBuffer: got %d, want %d", got.AvailableBuffer, tt.ack.AvailableBuffer)
			}
			if got.PacketsReceiving != tt.ack.PacketsReceiving {
				t.Errorf("PacketsReceiving: got %d, want %d", got.PacketsReceiving, tt.ack.PacketsReceiving)
			}
			if got.EstBandwidth != tt.ack.EstBandwidth {
				t.Errorf("EstBandwidth: got %d, want %d", got.EstBandwidth, tt.ack.EstBandwidth)
			}
			if got.ReceivingRate != tt.ack.ReceivingRate {
				t.Errorf("ReceivingRate: got %d, want %d", got.ReceivingRate, tt.ack.ReceivingRate)
			}
		})
	}
}

// TestUnmarshalACKCIF_TooShort verifies error handling for short buffers.
func TestUnmarshalACKCIF_TooShort(t *testing.T) {
	_, err := UnmarshalACKCIF(make([]byte, 27))
	if err == nil {
		t.Error("expected error for short buffer, got nil")
	}
}

// TestNAK_EncodeDecode_RoundTrip verifies NAK loss range encoding/decoding.
func TestNAK_EncodeDecode_RoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		ranges [][2]uint32
	}{
		{
			name:   "single_loss",
			ranges: [][2]uint32{{5, 5}},
		},
		{
			name:   "single_range",
			ranges: [][2]uint32{{10, 20}},
		},
		{
			name: "mixed_singles_and_ranges",
			ranges: [][2]uint32{
				{1, 1},
				{5, 10},
				{15, 15},
				{100, 200},
			},
		},
		{
			name:   "empty",
			ranges: [][2]uint32{},
		},
		{
			name:   "max_seqno",
			ranges: [][2]uint32{{maxSequenceNumber - 1, maxSequenceNumber}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := EncodeLossRanges(tt.ranges)
			decoded := DecodeLossRanges(encoded)

			if len(tt.ranges) == 0 {
				if len(decoded) != 0 {
					t.Errorf("expected empty, got %d ranges", len(decoded))
				}
				return
			}

			if len(decoded) != len(tt.ranges) {
				t.Fatalf("range count: got %d, want %d", len(decoded), len(tt.ranges))
			}
			for i := range tt.ranges {
				if decoded[i] != tt.ranges[i] {
					t.Errorf("range[%d]: got %v, want %v", i, decoded[i], tt.ranges[i])
				}
			}
		})
	}
}

// TestNAK_EncodeSize verifies the encoded size for various loss patterns.
func TestNAK_EncodeSize(t *testing.T) {
	// Single loss = 4 bytes, range = 8 bytes.
	tests := []struct {
		name     string
		ranges   [][2]uint32
		wantSize int
	}{
		{"one_single", [][2]uint32{{1, 1}}, 4},
		{"one_range", [][2]uint32{{1, 5}}, 8},
		{"two_singles", [][2]uint32{{1, 1}, {3, 3}}, 8},
		{"single_plus_range", [][2]uint32{{1, 1}, {5, 10}}, 12},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := EncodeLossRanges(tt.ranges)
			if len(encoded) != tt.wantSize {
				t.Errorf("encoded size: got %d, want %d", len(encoded), tt.wantSize)
			}
		})
	}
}

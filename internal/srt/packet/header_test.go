package packet

import (
	"testing"
)

// TestParseHeader_DataPacket verifies that ParseHeader correctly reads a
// data packet (F bit = 0) with known timestamp and destination socket ID.
func TestParseHeader_DataPacket(t *testing.T) {
	// Build a 16-byte buffer by hand:
	// Byte 0: 0x00 (F=0, data packet)
	// Bytes 8-11: timestamp = 0x00010000 = 65536 µs
	// Bytes 12-15: dest socket ID = 0x00000042 = 66
	buf := []byte{
		0x00, 0x00, 0x00, 0x01, // Bytes 0-3: F=0, seqno=1
		0x00, 0x00, 0x00, 0x00, // Bytes 4-7: type-specific
		0x00, 0x01, 0x00, 0x00, // Bytes 8-11: timestamp = 65536
		0x00, 0x00, 0x00, 0x42, // Bytes 12-15: dest socket ID = 66
	}

	h, err := ParseHeader(buf)
	if err != nil {
		t.Fatalf("ParseHeader failed: %v", err)
	}

	if h.IsControl {
		t.Error("expected data packet (IsControl=false), got control")
	}
	if h.Timestamp != 65536 {
		t.Errorf("timestamp: got %d, want 65536", h.Timestamp)
	}
	if h.DestSocketID != 66 {
		t.Errorf("dest socket ID: got %d, want 66", h.DestSocketID)
	}
}

// TestParseHeader_ControlPacket verifies that ParseHeader correctly reads a
// control packet (F bit = 1).
func TestParseHeader_ControlPacket(t *testing.T) {
	buf := []byte{
		0x80, 0x00, 0x00, 0x00, // Bytes 0-3: F=1 (control)
		0x00, 0x00, 0x00, 0x00, // Bytes 4-7: type-specific
		0xFF, 0xFF, 0xFF, 0xFF, // Bytes 8-11: timestamp = max
		0x12, 0x34, 0x56, 0x78, // Bytes 12-15: dest socket ID
	}

	h, err := ParseHeader(buf)
	if err != nil {
		t.Fatalf("ParseHeader failed: %v", err)
	}

	if !h.IsControl {
		t.Error("expected control packet (IsControl=true), got data")
	}
	if h.Timestamp != 0xFFFFFFFF {
		t.Errorf("timestamp: got %d, want %d", h.Timestamp, uint32(0xFFFFFFFF))
	}
	if h.DestSocketID != 0x12345678 {
		t.Errorf("dest socket ID: got 0x%08X, want 0x12345678", h.DestSocketID)
	}
}

// TestParseHeader_TooShort verifies that ParseHeader returns an error when
// given a buffer that is too small to hold the 16-byte header.
func TestParseHeader_TooShort(t *testing.T) {
	tests := []struct {
		name string
		buf  []byte
	}{
		{"empty", []byte{}},
		{"one_byte", []byte{0x00}},
		{"fifteen_bytes", make([]byte, 15)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseHeader(tt.buf)
			if err == nil {
				t.Error("expected error for short buffer, got nil")
			}
		})
	}
}

// TestParseHeader_ExactSize verifies parsing works with exactly 16 bytes.
func TestParseHeader_ExactSize(t *testing.T) {
	buf := make([]byte, 16)
	buf[0] = 0x80 // control packet
	buf[11] = 0x01 // timestamp = 1
	buf[15] = 0x02 // dest socket ID = 2

	h, err := ParseHeader(buf)
	if err != nil {
		t.Fatalf("ParseHeader failed: %v", err)
	}
	if !h.IsControl {
		t.Error("expected control packet")
	}
	if h.Timestamp != 1 {
		t.Errorf("timestamp: got %d, want 1", h.Timestamp)
	}
	if h.DestSocketID != 2 {
		t.Errorf("dest socket ID: got %d, want 2", h.DestSocketID)
	}
}

// TestParseHeader_ZeroValues verifies that all-zero bytes parse as a data
// packet with zero timestamp and zero socket ID.
func TestParseHeader_ZeroValues(t *testing.T) {
	buf := make([]byte, 16)
	h, err := ParseHeader(buf)
	if err != nil {
		t.Fatalf("ParseHeader failed: %v", err)
	}
	if h.IsControl {
		t.Error("all-zero buffer should be data packet")
	}
	if h.Timestamp != 0 {
		t.Errorf("timestamp: got %d, want 0", h.Timestamp)
	}
	if h.DestSocketID != 0 {
		t.Errorf("dest socket ID: got %d, want 0", h.DestSocketID)
	}
}

// TestParseHeader_ExtraBytes verifies that ParseHeader ignores extra bytes
// beyond the 16-byte header (as would happen with payload data).
func TestParseHeader_ExtraBytes(t *testing.T) {
	buf := make([]byte, 100)
	buf[8] = 0x00
	buf[9] = 0x00
	buf[10] = 0x00
	buf[11] = 0x0A // timestamp = 10
	buf[15] = 0x05  // dest socket ID = 5

	h, err := ParseHeader(buf)
	if err != nil {
		t.Fatalf("ParseHeader failed: %v", err)
	}
	if h.Timestamp != 10 {
		t.Errorf("timestamp: got %d, want 10", h.Timestamp)
	}
	if h.DestSocketID != 5 {
		t.Errorf("dest socket ID: got %d, want 5", h.DestSocketID)
	}
}

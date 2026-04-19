package media

import "testing"

// TestParseModEx_TimestampOffset1Byte verifies parsing a nanosecond timestamp
// offset stored in a single byte (dataSizeCode=0 → 1 byte of data).
func TestParseModEx_TimestampOffset1Byte(t *testing.T) {
	// byte 0: ModExType=0 (TimestampOffsetNano) in high nibble,
	//         DataSizeCode=0 (1 byte) in low nibble → 0x00
	// byte 1: nano offset value = 42
	// bytes 2-3: wrapped payload (0xAA, 0xBB)
	data := []byte{0x00, 42, 0xAA, 0xBB}
	msg, err := ParseModEx(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if msg.Type != ModExTypeTimestampOffsetNano {
		_tFatalf(t, "type mismatch: got %d, want %d", msg.Type, ModExTypeTimestampOffsetNano)
	}
	if msg.NanosecondOffset != 42 {
		_tFatalf(t, "nano offset mismatch: got %d, want 42", msg.NanosecondOffset)
	}
	if len(msg.Data) != 1 || msg.Data[0] != 42 {
		_tFatalf(t, "raw data mismatch: %v", msg.Data)
	}
	if len(msg.WrappedPayload) != 2 || msg.WrappedPayload[0] != 0xAA {
		_tFatalf(t, "wrapped payload mismatch: %v", msg.WrappedPayload)
	}
}

// TestParseModEx_TimestampOffset2Bytes verifies parsing a nanosecond offset
// stored in 2 bytes (dataSizeCode=1), value=1000 (0x03E8).
func TestParseModEx_TimestampOffset2Bytes(t *testing.T) {
	// byte 0: type=0, dataSizeCode=1 → 0x01
	// bytes 1-2: 0x03 0xE8 = 1000
	// byte 3: wrapped payload (0xCC)
	data := []byte{0x01, 0x03, 0xE8, 0xCC}
	msg, err := ParseModEx(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if msg.NanosecondOffset != 1000 {
		_tFatalf(t, "nano offset mismatch: got %d, want 1000", msg.NanosecondOffset)
	}
	if len(msg.WrappedPayload) != 1 || msg.WrappedPayload[0] != 0xCC {
		_tFatalf(t, "wrapped payload mismatch: %v", msg.WrappedPayload)
	}
}

// TestParseModEx_TimestampOffset3Bytes verifies parsing a nanosecond offset
// stored in 3 bytes (dataSizeCode=2), value=500000 (0x07A120).
func TestParseModEx_TimestampOffset3Bytes(t *testing.T) {
	// byte 0: type=0, dataSizeCode=2 → 0x02
	// bytes 1-3: 0x07 0xA1 0x20 = 500000
	// byte 4: wrapped payload (0xDD)
	data := []byte{0x02, 0x07, 0xA1, 0x20, 0xDD}
	msg, err := ParseModEx(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if msg.NanosecondOffset != 500000 {
		_tFatalf(t, "nano offset mismatch: got %d, want 500000", msg.NanosecondOffset)
	}
}

// TestParseModEx_TimestampOffset4Bytes verifies parsing a nanosecond offset
// stored in 4 bytes (dataSizeCode=3), value=999999 (0x000F423F).
func TestParseModEx_TimestampOffset4Bytes(t *testing.T) {
	// byte 0: type=0, dataSizeCode=3 → 0x03
	// bytes 1-4: 0x00 0x0F 0x42 0x3F = 999999
	// byte 5: wrapped payload (0xEE)
	data := []byte{0x03, 0x00, 0x0F, 0x42, 0x3F, 0xEE}
	msg, err := ParseModEx(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if msg.NanosecondOffset != 999999 {
		_tFatalf(t, "nano offset mismatch: got %d, want 999999", msg.NanosecondOffset)
	}
	if len(msg.WrappedPayload) != 1 || msg.WrappedPayload[0] != 0xEE {
		_tFatalf(t, "wrapped payload mismatch: %v", msg.WrappedPayload)
	}
}

// TestParseModEx_NoWrappedPayload verifies that ModEx with no wrapped payload
// after the modifier data is valid (e.g., a modifier-only signal).
func TestParseModEx_NoWrappedPayload(t *testing.T) {
	// 1-byte nano offset with no trailing data
	data := []byte{0x00, 0x05}
	msg, err := ParseModEx(data)
	if err != nil {
		_tFatalf(t, "unexpected error: %v", err)
	}
	if msg.NanosecondOffset != 5 {
		_tFatalf(t, "nano offset mismatch: got %d, want 5", msg.NanosecondOffset)
	}
	if len(msg.WrappedPayload) != 0 {
		_tFatalf(t, "expected empty wrapped payload, got %v", msg.WrappedPayload)
	}
}

// TestParseModEx_UnknownType verifies that an unknown ModExType is parsed
// without error (forward compatibility) — the raw data is still extracted.
func TestParseModEx_UnknownType(t *testing.T) {
	// ModExType=1 (unknown), dataSizeCode=0 (1 byte)
	data := []byte{0x10, 0xFF, 0xAA}
	msg, err := ParseModEx(data)
	if err != nil {
		_tFatalf(t, "unexpected error for unknown type: %v", err)
	}
	if msg.Type != 1 {
		_tFatalf(t, "type mismatch: got %d, want 1", msg.Type)
	}
	// NanosecondOffset should be zero since we don't parse unknown types
	if msg.NanosecondOffset != 0 {
		_tFatalf(t, "nano offset should be 0 for unknown type, got %d", msg.NanosecondOffset)
	}
	if len(msg.Data) != 1 || msg.Data[0] != 0xFF {
		_tFatalf(t, "raw data mismatch: %v", msg.Data)
	}
}

// TestParseModEx_TooShort verifies that a 1-byte input is rejected
// (need at least 2 bytes: header + 1 byte of data).
func TestParseModEx_TooShort(t *testing.T) {
	_, err := ParseModEx([]byte{0x00})
	if err == nil {
		t.Fatal("expected error for too-short data")
	}
}

// TestParseModEx_EmptyPayload verifies that an empty input is rejected.
func TestParseModEx_EmptyPayload(t *testing.T) {
	_, err := ParseModEx([]byte{})
	if err == nil {
		t.Fatal("expected error for empty data")
	}
}

// TestParseModEx_ReservedDataSize verifies that dataSizeCode >= 4 is rejected
// (reserved by the E-RTMP v2 spec).
func TestParseModEx_ReservedDataSize(t *testing.T) {
	// dataSizeCode=4 (reserved) with plenty of trailing data
	_, err := ParseModEx([]byte{0x04, 0x00, 0x00, 0x00, 0x00, 0x00})
	if err == nil {
		t.Fatal("expected error for reserved data size code")
	}
}

// TestParseModEx_TruncatedData verifies that truncated modifier data is rejected
// (e.g., dataSizeCode=1 means 2 bytes of data, but only 1 byte provided).
func TestParseModEx_TruncatedData(t *testing.T) {
	// dataSizeCode=1 (2 bytes of data needed), but only 1 byte after header
	_, err := ParseModEx([]byte{0x01, 0x03})
	if err == nil {
		t.Fatal("expected error for truncated modifier data")
	}
}

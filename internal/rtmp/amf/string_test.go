// string_test.go – tests for the AMF0 String type.
//
// AMF0 strings are encoded as: 1 marker byte (0x02) + 2-byte big-endian
// length (max 65535) + UTF-8 payload bytes. These tests verify golden-file
// fidelity, round-trip correctness, boundary conditions, and error paths.
package amf

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readGoldenString loads a golden vector; reuses goldenDir from number_test.go.
func readGoldenString(t *testing.T, name string) []byte {
	t.Helper()
	p := filepath.Join(goldenDir, name)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return b
}

// TestEncodeString_Golden_Test verifies encoding the string "test" against
// its golden binary (0x02 0x00 0x04 t e s t).
func TestEncodeString_Golden_Test(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodeString(&buf, "test"); err != nil {
		t.Fatalf("EncodeString(test) error: %v", err)
	}
	golden := readGoldenString(t, "amf0_string_test.bin")
	if !bytes.Equal(buf.Bytes(), golden) {
		t.Fatalf("encoded mismatch for 'test'\n got: %x\nwant: %x", buf.Bytes(), golden)
	}
}

// TestEncodeString_Golden_Empty – the empty string encodes as marker + 0x0000.
func TestEncodeString_Golden_Empty(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodeString(&buf, ""); err != nil {
		t.Fatalf("EncodeString(empty) error: %v", err)
	}
	golden := readGoldenString(t, "amf0_string_empty.bin")
	if !bytes.Equal(buf.Bytes(), golden) {
		t.Fatalf("encoded mismatch for empty string\n got: %x\nwant: %x", buf.Bytes(), golden)
	}
}

func TestDecodeString_Golden_Test(t *testing.T) {
	golden := readGoldenString(t, "amf0_string_test.bin")
	v, err := DecodeString(bytes.NewReader(golden))
	if err != nil {
		t.Fatalf("DecodeString(test) error: %v", err)
	}
	if v != "test" {
		t.Fatalf("expected 'test' got %q", v)
	}
}

func TestDecodeString_Golden_Empty(t *testing.T) {
	golden := readGoldenString(t, "amf0_string_empty.bin")
	v, err := DecodeString(bytes.NewReader(golden))
	if err != nil {
		t.Fatalf("DecodeString(empty) error: %v", err)
	}
	if v != "" {
		t.Fatalf("expected empty got %q", v)
	}
}

// TestString_RoundTrip_Multibyte ensures multi-byte UTF-8 (e.g. Chinese
// characters) survives the round trip. AMF0 string length is in bytes, not
// characters, so "世界" (6 UTF-8 bytes) must encode with length=6.
func TestString_RoundTrip_Multibyte(t *testing.T) {
	in := "世界" // multibyte UTF-8
	var buf bytes.Buffer
	if err := EncodeString(&buf, in); err != nil {
		t.Fatalf("encode multibyte: %v", err)
	}
	out, err := DecodeString(&buf)
	if err != nil {
		t.Fatalf("decode multibyte: %v", err)
	}
	if in != out {
		t.Fatalf("mismatch: in=%q out=%q", in, out)
	}
}

// TestString_MaxLength checks that the absolute maximum AMF0 string length
// (65535 bytes = 0xFFFF) encodes and decodes correctly.
func TestString_MaxLength(t *testing.T) {
	// 65535 bytes
	in := strings.Repeat("a", 65535)
	var buf bytes.Buffer
	if err := EncodeString(&buf, in); err != nil {
		t.Fatalf("encode max length: %v", err)
	}
	out, err := DecodeString(&buf)
	if err != nil {
		t.Fatalf("decode max length: %v", err)
	}
	if out != in {
		t.Fatalf("expected same string after round trip")
	}
}

// TestString_TooLong verifies that strings exceeding 65535 bytes are
// rejected. The 2-byte length field cannot represent longer values.
func TestString_TooLong(t *testing.T) {
	in := strings.Repeat("b", 65536)
	if err := EncodeString(&bytes.Buffer{}, in); err == nil {
		t.Fatalf("expected error for length > 65535")
	}
}

// TestDecodeString_InvalidMarker sends a number marker (0x00) where a
// string marker (0x02) is expected.
func TestDecodeString_InvalidMarker(t *testing.T) {
	// number marker (0x00) followed by dummy length bytes
	data := []byte{0x00, 0x00, 0x00}
	if _, err := DecodeString(bytes.NewReader(data)); err == nil {
		t.Fatalf("expected error for invalid marker")
	}
}

// TestDecodeString_ShortLength provides only 1 length byte where 2 are
// needed (marker + one byte).
func TestDecodeString_ShortLength(t *testing.T) {
	// marker + only one length byte (should need 2)
	data := []byte{0x02, 0x00}
	if _, err := DecodeString(bytes.NewReader(data)); err == nil {
		t.Fatalf("expected error for short length read")
	}
}

// TestDecodeString_TruncatedBody claims length=4 but only supplies 2
// body bytes – the decoder must not silently return partial data.
func TestDecodeString_TruncatedBody(t *testing.T) {
	// marker + length=0x0004 but only 2 bytes of body
	data := []byte{0x02, 0x00, 0x04, 't', 'e'}
	if _, err := DecodeString(bytes.NewReader(data)); err == nil {
		t.Fatalf("expected error for truncated body")
	}
}

func BenchmarkEncodeString(b *testing.B) {
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		_ = EncodeString(&buf, "benchmark-string-value")
	}
}

func BenchmarkDecodeString(b *testing.B) {
	golden := readGoldenString(&testing.T{}, "amf0_string_test.bin")
	for i := 0; i < b.N; i++ {
		_, _ = DecodeString(bytes.NewReader(golden))
	}
}

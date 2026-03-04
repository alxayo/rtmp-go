// null_test.go – tests for the AMF0 Null type.
//
// AMF0 Null is the simplest type: just a single marker byte 0x05.
// Null is used in RTMP commands where optional parameters are omitted.
package amf

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// readGoldenNull loads the golden file for null tests.
func readGoldenNull(t *testing.T, name string) []byte {
	t.Helper()
	p := filepath.Join(goldenDir, name)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return b
}

// TestEncodeNull_Golden verifies that EncodeNull writes exactly one byte: 0x05.
func TestEncodeNull_Golden(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodeNull(&buf); err != nil {
		t.Fatalf("EncodeNull error: %v", err)
	}
	golden := readGoldenNull(t, "amf0_null.bin")
	if !bytes.Equal(buf.Bytes(), golden) {
		t.Fatalf("encoded null mismatch\n got: %x\nwant: %x", buf.Bytes(), golden)
	}
}

// TestDecodeNull_Golden decodes the golden null binary and asserts that the
// returned Go value is nil (the Go equivalent of AMF0 Null).
func TestDecodeNull_Golden(t *testing.T) {
	golden := readGoldenNull(t, "amf0_null.bin")
	v, err := DecodeNull(bytes.NewReader(golden))
	if err != nil {
		t.Fatalf("DecodeNull error: %v", err)
	}
	if v != nil {
		t.Fatalf("expected nil got %#v", v)
	}
}

// TestDecodeNull_InvalidMarker sends a string marker where null is expected.
func TestDecodeNull_InvalidMarker(t *testing.T) {
	// Use string marker 0x02 to trigger mismatch.
	data := []byte{0x02}
	if v, err := DecodeNull(bytes.NewReader(data)); err == nil || v != nil {
		t.Fatalf("expected error for invalid marker")
	}
}

// TestDecodeNull_ShortRead provides an empty reader – zero bytes available.
func TestDecodeNull_ShortRead(t *testing.T) {
	data := []byte{} // empty -> short read
	if v, err := DecodeNull(bytes.NewReader(data)); err == nil || v != nil {
		t.Fatalf("expected error for short read")
	}
}

func BenchmarkEncodeNull(b *testing.B) {
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		_ = EncodeNull(&buf)
	}
}

func BenchmarkDecodeNull(b *testing.B) {
	golden := readGoldenNull(&testing.T{}, "amf0_null.bin")
	for i := 0; i < b.N; i++ {
		_, _ = DecodeNull(bytes.NewReader(golden))
	}
}

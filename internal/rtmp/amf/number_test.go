// number_test.go – tests for the AMF0 Number type (IEEE 754 float64).
//
// AMF0 encodes numbers as: 1 marker byte (0x00) + 8 bytes big-endian double.
// These tests use golden binary vectors stored in tests/golden/ to ensure
// exact wire-format fidelity.
//
// Key concepts demonstrated:
//   - Golden file testing – canonical binary files generated once and
//     compared byte-for-byte against encoder output.
//   - Edge-case coverage – NaN, ±Inf, negative numbers, short buffers.
//   - Benchmarks – BenchmarkXxx functions for tracking encode/decode
//     throughput (run via `go test -bench .`).
package amf

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"testing"
)

const goldenDir = "../../../tests/golden" // relative to this test file directory

// readGolden loads a golden binary vector from tests/golden/.
// It panics on failure because missing golden files indicate a broken
// test environment, not a test failure.
func readGolden(t *testing.T, name string) []byte {
	// Using filepath.Join for Windows compatibility.
	p := filepath.Join(goldenDir, name)
	b, err := os.ReadFile(p)
	if err != nil {
		// Provide context but fail fast; golden vectors are required.
		panic(err)
	}
	return b
}

// TestEncodeNumber_Golden_0 encodes 0.0 and checks the result byte-for-byte
// against the golden file amf0_number_0.bin (should be: 0x00 + 8 zero bytes).
func TestEncodeNumber_Golden_0(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodeNumber(&buf, 0.0); err != nil {
		t.Fatalf("EncodeNumber(0.0) error: %v", err)
	}
	golden := readGolden(t, "amf0_number_0.bin")
	if !bytes.Equal(buf.Bytes(), golden) {
		t.Fatalf("encoded bytes mismatch for 0.0\n got: %x\nwant: %x", buf.Bytes(), golden)
	}
}

// TestEncodeNumber_Golden_1_5 encodes 1.5 and verifies against the golden file.
// 1.5 in IEEE 754 double is 0x3FF8000000000000.
func TestEncodeNumber_Golden_1_5(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodeNumber(&buf, 1.5); err != nil {
		t.Fatalf("EncodeNumber(1.5) error: %v", err)
	}
	golden := readGolden(t, "amf0_number_1_5.bin")
	if !bytes.Equal(buf.Bytes(), golden) {
		t.Fatalf("encoded bytes mismatch for 1.5\n got: %x\nwant: %x", buf.Bytes(), golden)
	}
}

// TestDecodeNumber_Golden_0 reads the golden binary for 0.0 and checks the
// decoded float64 value.
func TestDecodeNumber_Golden_0(t *testing.T) {
	golden := readGolden(t, "amf0_number_0.bin")
	v, err := DecodeNumber(bytes.NewReader(golden))
	if err != nil {
		t.Fatalf("DecodeNumber(0.0) error: %v", err)
	}
	if v != 0.0 {
		t.Fatalf("expected 0.0 got %v", v)
	}
}

// TestDecodeNumber_Golden_1_5 reads the golden binary for 1.5 and checks the
// decoded value.
func TestDecodeNumber_Golden_1_5(t *testing.T) {
	golden := readGolden(t, "amf0_number_1_5.bin")
	v, err := DecodeNumber(bytes.NewReader(golden))
	if err != nil {
		t.Fatalf("DecodeNumber(1.5) error: %v", err)
	}
	if v != 1.5 {
		t.Fatalf("expected 1.5 got %v", v)
	}
}

// TestNumber_EdgeCases_RoundTrip exercises encode→decode for edge values:
// positive/negative numbers and ±infinity. Each value must survive the
// round trip unchanged.
func TestNumber_EdgeCases_RoundTrip(t *testing.T) {
	cases := []float64{1.0, -1.0, math.Inf(1), math.Inf(-1)}
	for _, in := range cases {
		var buf bytes.Buffer
		if err := EncodeNumber(&buf, in); err != nil {
			t.Fatalf("encode %v: %v", in, err)
		}
		out, err := DecodeNumber(&buf)
		if err != nil {
			t.Fatalf("decode %v: %v", in, err)
		}
		if in != out {
			t.Fatalf("mismatch: in=%v out=%v", in, out)
		}
	}
}

// TestNumber_NaN_RoundTrip verifies NaN handling. NaN != NaN in IEEE 754,
// so we use math.IsNaN instead of == to check the decoded value.
func TestNumber_NaN_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodeNumber(&buf, math.NaN()); err != nil {
		t.Fatalf("encode NaN: %v", err)
	}
	out, err := DecodeNumber(&buf)
	if err != nil {
		t.Fatalf("decode NaN: %v", err)
	}
	if !math.IsNaN(out) {
		t.Fatalf("expected NaN got %v", out)
	}
}

// TestDecodeNumber_InvalidMarker sends a string marker (0x02) where a number
// marker (0x00) is expected. The decoder must reject it with an error.
func TestDecodeNumber_InvalidMarker(t *testing.T) {
	bad := []byte{0x02 /* string marker */, 0, 0, 0, 0, 0, 0, 0, 0}
	if _, err := DecodeNumber(bytes.NewReader(bad)); err == nil {
		t.Fatalf("expected error for invalid marker")
	}
}

// TestDecodeNumber_ShortBuffer provides only 3 bytes (marker + partial
// payload). The decoder must return an error, not read garbage.
func TestDecodeNumber_ShortBuffer(t *testing.T) {
	short := []byte{0x00, 0x00, 0x01} // truncated payload
	if _, err := DecodeNumber(bytes.NewReader(short)); err == nil {
		t.Fatalf("expected error for short read")
	}
}

// Benchmark (optional performance signal for future).
func BenchmarkEncodeNumber(b *testing.B) {
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		_ = EncodeNumber(&buf, 123.456)
	}
}

func BenchmarkDecodeNumber(b *testing.B) {
	golden := readGolden(&testing.T{}, "amf0_number_1_5.bin")
	for i := 0; i < b.N; i++ {
		_, _ = DecodeNumber(bytes.NewReader(golden))
	}
}

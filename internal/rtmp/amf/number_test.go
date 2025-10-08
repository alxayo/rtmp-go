package amf

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"testing"
)

const goldenDir = "../../../tests/golden" // relative to this test file directory

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

func TestDecodeNumber_InvalidMarker(t *testing.T) {
	bad := []byte{0x02 /* string marker */, 0, 0, 0, 0, 0, 0, 0, 0}
	if _, err := DecodeNumber(bytes.NewReader(bad)); err == nil {
		t.Fatalf("expected error for invalid marker")
	}
}

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

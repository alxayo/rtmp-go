// boolean_test.go – tests for the AMF0 Boolean type.
//
// AMF0 booleans are encoded as: 1 marker byte (0x01) + 1 value byte
// (0x00=false, 0x01=true). These tests use t.Run subtests grouped by
// encode/decode for cleaner output.
package amf

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Reuse goldenDir constant pattern from number_test.go (keep consistency even if duplicated).
const goldenDirBoolean = "../../../tests/golden"

// readGoldenBoolean loads a golden binary vector.
func readGoldenBoolean(t *testing.T, name string) []byte {
	t.Helper()
	p := filepath.Join(goldenDirBoolean, name)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return b
}

// TestEncodeBoolean_Golden uses t.Run subtests to check both true and false
// against their golden binaries. t.Run creates named sub-tests that can be
// run individually with `go test -run TestEncodeBoolean_Golden/true`.
func TestEncodeBoolean_Golden(t *testing.T) {
	cases := []struct {
		name   string
		value  bool
		golden string
	}{
		{"true", true, "amf0_boolean_true.bin"},
		{"false", false, "amf0_boolean_false.bin"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := EncodeBoolean(&buf, tc.value); err != nil {
				t.Fatalf("EncodeBoolean(%v): %v", tc.value, err)
			}
			golden := readGoldenBoolean(t, tc.golden)
			if !bytes.Equal(buf.Bytes(), golden) {
				t.Fatalf("encoded mismatch for %s\n got: %x\nwant: %x", tc.name, buf.Bytes(), golden)
			}
		})
	}
}

// TestDecodeBoolean_Golden reads golden files and checks decoded values.
func TestDecodeBoolean_Golden(t *testing.T) {
	cases := []struct {
		name   string
		golden string
		want   bool
	}{
		{"true", "amf0_boolean_true.bin", true},
		{"false", "amf0_boolean_false.bin", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			golden := readGoldenBoolean(t, tc.golden)
			v, err := DecodeBoolean(bytes.NewReader(golden))
			if err != nil {
				t.Fatalf("DecodeBoolean(%s) error: %v", tc.name, err)
			}
			if v != tc.want {
				t.Fatalf("expected %v got %v", tc.want, v)
			}
		})
	}
}

// TestDecodeBoolean_InvalidMarker sends a string marker where boolean is
// expected. The decoder must reject it.
func TestDecodeBoolean_InvalidMarker(t *testing.T) {
	// Marker 0x02 is string, should fail.
	data := []byte{0x02, 0x01}
	if _, err := DecodeBoolean(bytes.NewReader(data)); err == nil {
		t.Fatalf("expected error for invalid marker")
	}
}

// TestDecodeBoolean_ShortRead_MarkerOnly provides only the marker byte with
// no value byte – the decoder must not read beyond available data.
func TestDecodeBoolean_ShortRead_MarkerOnly(t *testing.T) {
	data := []byte{0x01} // missing value byte
	if _, err := DecodeBoolean(bytes.NewReader(data)); err == nil {
		t.Fatalf("expected error for short read of value byte")
	}
}

func BenchmarkEncodeBoolean(b *testing.B) {
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		_ = EncodeBoolean(&buf, i%2 == 0)
	}
}

func BenchmarkDecodeBoolean(b *testing.B) {
	golden := readGoldenBoolean(&testing.T{}, "amf0_boolean_true.bin")
	for i := 0; i < b.N; i++ {
		_, _ = DecodeBoolean(bytes.NewReader(golden))
	}
}

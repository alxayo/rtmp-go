package amf

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Reuse goldenDir constant pattern from number_test.go (keep consistency even if duplicated).
const goldenDirBoolean = "../../../tests/golden"

func readGoldenBoolean(t *testing.T, name string) []byte {
	t.Helper()
	p := filepath.Join(goldenDirBoolean, name)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return b
}

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

func TestDecodeBoolean_InvalidMarker(t *testing.T) {
	// Marker 0x02 is string, should fail.
	data := []byte{0x02, 0x01}
	if _, err := DecodeBoolean(bytes.NewReader(data)); err == nil {
		t.Fatalf("expected error for invalid marker")
	}
}

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

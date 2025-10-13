package amf

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Reuse goldenDir constant from number_test.go.
func readGoldenArray(t *testing.T, name string) []byte {
	p := filepath.Join(goldenDir, name)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return b
}

func TestEncodeStrictArray_Golden(t *testing.T) {
	arr := []interface{}{1.0, 2.0, 3.0}
	var buf bytes.Buffer
	if err := EncodeStrictArray(&buf, arr); err != nil {
		t.Fatalf("EncodeStrictArray error: %v", err)
	}
	golden := readGoldenArray(t, "amf0_array_strict.bin")
	if !bytes.Equal(buf.Bytes(), golden) {
		t.Fatalf("encoded strict array mismatch\n got: %x\nwant: %x", buf.Bytes(), golden)
	}
}

func TestDecodeStrictArray_Golden(t *testing.T) {
	golden := readGoldenArray(t, "amf0_array_strict.bin")
	v, err := DecodeStrictArray(bytes.NewReader(golden))
	if err != nil {
		t.Fatalf("DecodeStrictArray error: %v", err)
	}
	if len(v) != 3 {
		t.Fatalf("expected len=3 got %d", len(v))
	}
	want := []interface{}{1.0, 2.0, 3.0}
	for i := range want {
		if v[i] != want[i] {
			t.Fatalf("index %d mismatch got %v want %v", i, v[i], want[i])
		}
	}
}

func TestStrictArray_Nested_RoundTrip(t *testing.T) {
	in := []interface{}{[]interface{}{1.0, 2.0}, []interface{}{"a", nil}}
	var buf bytes.Buffer
	if err := EncodeStrictArray(&buf, in); err != nil {
		t.Fatalf("encode nested: %v", err)
	}
	out, err := DecodeStrictArray(&buf)
	if err != nil {
		t.Fatalf("decode nested: %v", err)
	}
	// Simple structural assertion.
	if len(out) != 2 {
		t.Fatalf("expected 2 top-level elements, got %d", len(out))
	}
	first, ok := out[0].([]interface{})
	if !ok || len(first) != 2 {
		t.Fatalf("expected first element nested len 2 got %#v", out[0])
	}
	second, ok := out[1].([]interface{})
	if !ok || len(second) != 2 {
		t.Fatalf("expected second element nested len 2 got %#v", out[1])
	}
}

func TestDecodeStrictArray_InvalidMarker(t *testing.T) {
	// 0x02 is string marker – should fail when expecting array marker.
	bad := []byte{0x02, 0x00, 0x00}
	if _, err := DecodeStrictArray(bytes.NewReader(bad)); err == nil {
		t.Fatalf("expected error for invalid marker")
	}
}

func TestDecodeStrictArray_TruncatedElement(t *testing.T) {
	// Declares 1 element but only provides marker 0x00 (number) without 8 bytes.
	bad := []byte{0x0A, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00}
	if _, err := DecodeStrictArray(bytes.NewReader(bad)); err == nil {
		t.Fatalf("expected error for truncated element payload")
	}
}

func TestStrictArray_RoundTrip_VariedTypes(t *testing.T) {
	in := []interface{}{1.0, true, "x", nil, map[string]interface{}{"k": 2.0}}
	out, err := roundTripStrictArray(in)
	if err != nil {
		t.Fatalf("round trip failed: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("length mismatch got %d want %d", len(out), len(in))
	}
	// Shallow compare (maps compare by reflect for single key only here)
	for i := range in {
		switch v := in[i].(type) {
		case map[string]interface{}:
			ov, ok := out[i].(map[string]interface{})
			if !ok || !reflect.DeepEqual(v, ov) {
				t.Fatalf("map mismatch at %d got %#v want %#v", i, out[i], v)
			}
		default:
			if out[i] != v {
				t.Fatalf("value mismatch at %d got %#v want %#v", i, out[i], v)
			}
		}
	}
}

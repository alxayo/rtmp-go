// object_test.go – tests for the AMF0 Object type.
//
// AMF0 Objects are encoded as: marker 0x03, then repeated (key-string, value)
// pairs, terminated by the 3-byte end-of-object marker 0x00 0x00 0x09.
// Keys are "bare" UTF-8 strings (length-prefixed but NO type marker).
//
// These tests validate golden-file fidelity, nesting support, deterministic
// key ordering, unsupported-type rejection, and invalid-end-marker detection.
package amf

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// readGoldenObject loads a golden binary vector for object tests.
// Reuses the goldenDir constant defined in number_test.go.
func readGoldenObject(t *testing.T, name string) []byte {
	t.Helper()
	p := filepath.Join(goldenDir, name)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return b
}

// TestEncodeObject_Simple_Golden encodes {"key": "value"} and compares
// byte-for-byte against the golden file.
func TestEncodeObject_Simple_Golden(t *testing.T) {
	obj := map[string]interface{}{ // single key
		"key": "value",
	}
	var buf bytes.Buffer
	if err := EncodeObject(&buf, obj); err != nil {
		t.Fatalf("EncodeObject(simple) error: %v", err)
	}
	golden := readGoldenObject(t, "amf0_object_simple.bin")
	if !bytes.Equal(buf.Bytes(), golden) {
		t.Fatalf("encoded simple object mismatch\n got: %x\nwant: %x", buf.Bytes(), golden)
	}
}

// TestDecodeObject_Simple_Golden reads the golden binary and checks the
// decoded map has exactly {"key": "value"}.
func TestDecodeObject_Simple_Golden(t *testing.T) {
	golden := readGoldenObject(t, "amf0_object_simple.bin")
	m, err := DecodeObject(bytes.NewReader(golden))
	if err != nil {
		t.Fatalf("DecodeObject(simple) error: %v", err)
	}
	if len(m) != 1 || m["key"] != "value" {
		t.Fatalf("unexpected map content: %#v", m)
	}
}

// TestEncodeObject_Nested_Golden verifies that nested objects (object inside
// object) encode correctly. RTMP "connect" commands send nested command
// objects in practice.
func TestEncodeObject_Nested_Golden(t *testing.T) {
	obj := map[string]interface{}{
		"a": map[string]interface{}{
			"b": 1.0,
		},
	}
	var buf bytes.Buffer
	if err := EncodeObject(&buf, obj); err != nil {
		t.Fatalf("EncodeObject(nested) error: %v", err)
	}
	golden := readGoldenObject(t, "amf0_object_nested.bin")
	if !bytes.Equal(buf.Bytes(), golden) {
		t.Fatalf("encoded nested object mismatch\n got: %x\nwant: %x", buf.Bytes(), golden)
	}
}

// TestDecodeObject_Nested_Golden exercises nested-object decoding – the
// decoder must recursively handle inner objects.
func TestDecodeObject_Nested_Golden(t *testing.T) {
	golden := readGoldenObject(t, "amf0_object_nested.bin")
	m, err := DecodeObject(bytes.NewReader(golden))
	if err != nil {
		t.Fatalf("DecodeObject(nested) error: %v", err)
	}
	inner, ok := m["a"].(map[string]interface{})
	if !ok || len(inner) != 1 {
		t.Fatalf("expected nested map under 'a', got %#v", m["a"])
	}
	if inner["b"] != 1.0 {
		t.Fatalf("expected b=1.0 got %v", inner["b"])
	}
}

// TestEncodeObject_KeyOrderDeterministic checks that encoding the same map
// twice produces identical bytes. Go maps iterate in random order, so the
// encoder must sort keys to ensure deterministic wire output (important for
// golden-file testing and reproducible packet captures).
func TestEncodeObject_KeyOrderDeterministic(t *testing.T) {
	obj := map[string]interface{}{"z": 1.0, "a": 2.0, "m": 3.0}
	var buf1, buf2 bytes.Buffer
	if err := EncodeObject(&buf1, obj); err != nil {
		t.Fatalf("first encode: %v", err)
	}
	if err := EncodeObject(&buf2, obj); err != nil {
		t.Fatalf("second encode: %v", err)
	}
	if !bytes.Equal(buf1.Bytes(), buf2.Bytes()) {
		t.Fatalf("determinism failed: encodings differ: %x vs %x", buf1.Bytes(), buf2.Bytes())
	}
}

// TestEncodeObject_UnsupportedType verifies that non-AMF0 Go types (like
// plain int) produce a clear error rather than silent corruption.
func TestEncodeObject_UnsupportedType(t *testing.T) {
	obj := map[string]interface{}{"x": 5} // int unsupported
	var buf bytes.Buffer
	if err := EncodeObject(&buf, obj); err == nil {
		t.Fatalf("expected error for unsupported type int")
	}
}

// --- Benchmarks ---

// BenchmarkEncodeObject benchmarks encoding a typical RTMP connect-style object.
func BenchmarkEncodeObject(b *testing.B) {
	b.ReportAllocs()
	obj := map[string]interface{}{
		"app":      "live",
		"type":     "nonprivate",
		"flashVer": "FMLE/3.0",
		"tcUrl":    "rtmp://localhost/live",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		_ = EncodeObject(&buf, obj)
	}
}

// BenchmarkDecodeObject benchmarks decoding a typical RTMP connect-style object.
func BenchmarkDecodeObject(b *testing.B) {
	b.ReportAllocs()
	obj := map[string]interface{}{
		"app":      "live",
		"type":     "nonprivate",
		"flashVer": "FMLE/3.0",
		"tcUrl":    "rtmp://localhost/live",
	}
	var buf bytes.Buffer
	if err := EncodeObject(&buf, obj); err != nil {
		b.Fatalf("encode: %v", err)
	}
	data := buf.Bytes()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := bytes.NewReader(data)
		_, _ = DecodeObject(r)
	}
}

// TestDecodeObject_InvalidEndMarker crafts bytes where the end-of-object
// sentinel is wrong (0x08 instead of 0x09). The decoder must detect this.
func TestDecodeObject_InvalidEndMarker(t *testing.T) {
	// Construct object: 0x03 | 0x00 0x00 0x08 (invalid end marker instead of 0x09)
	bad := []byte{0x03, 0x00, 0x00, 0x08}
	if _, err := DecodeObject(bytes.NewReader(bad)); err == nil {
		t.Fatalf("expected error for invalid end marker")
	}
}

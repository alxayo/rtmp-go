package amf

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Reuse goldenDir constant from number_test.go (package-level), but redefine helper
// to avoid export requirements.
func readGoldenObject(t *testing.T, name string) []byte {
	t.Helper()
	p := filepath.Join(goldenDir, name)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return b
}

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

func TestEncodeObject_UnsupportedType(t *testing.T) {
	obj := map[string]interface{}{"x": 5} // int unsupported
	var buf bytes.Buffer
	if err := EncodeObject(&buf, obj); err == nil {
		t.Fatalf("expected error for unsupported type int")
	}
}

func TestDecodeObject_InvalidEndMarker(t *testing.T) {
	// Construct object: 0x03 | 0x00 0x00 0x08 (invalid end marker instead of 0x09)
	bad := []byte{0x03, 0x00, 0x00, 0x08}
	if _, err := DecodeObject(bytes.NewReader(bad)); err == nil {
		t.Fatalf("expected error for invalid end marker")
	}
}

// ecma_array_test.go – tests for the AMF0 ECMA Array type.
//
// AMF0 ECMA Arrays are encoded as: marker 0x08 + 4-byte big-endian approximate
// count + repeated (key-string, value) pairs, terminated by the 3-byte
// end-of-object marker 0x00 0x00 0x09. Nearly identical to Object (0x03) but
// with the count prefix.
//
// These tests validate empty encoding, property encoding, round-trip fidelity,
// deterministic key ordering, and type dispatch via EncodeValue/ECMAArray wrapper.
package amf

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"
)

// TestEncodeECMAArray_Empty verifies that an empty map produces:
// 0x08 | 0x00 0x00 0x00 0x00 | 0x00 0x00 0x09
func TestEncodeECMAArray_Empty(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodeECMAArray(&buf, map[string]interface{}{}); err != nil {
		t.Fatalf("EncodeECMAArray(empty) error: %v", err)
	}
	want := []byte{0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x09}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("empty ECMA array mismatch\n got: %x\nwant: %x", buf.Bytes(), want)
	}
}

// TestEncodeECMAArray_Properties encodes a map with Number, String, Boolean
// values and verifies the wire format byte-by-byte.
func TestEncodeECMAArray_Properties(t *testing.T) {
	m := map[string]interface{}{
		"duration": 10.0,
		"stereo":   true,
		"title":    "test",
	}
	var buf bytes.Buffer
	if err := EncodeECMAArray(&buf, m); err != nil {
		t.Fatalf("EncodeECMAArray error: %v", err)
	}
	data := buf.Bytes()

	// Verify marker.
	if data[0] != 0x08 {
		t.Fatalf("expected marker 0x08 got 0x%02x", data[0])
	}

	// Verify count = 3.
	count := binary.BigEndian.Uint32(data[1:5])
	if count != 3 {
		t.Fatalf("expected count 3 got %d", count)
	}

	// Verify end marker (last 3 bytes).
	end := data[len(data)-3:]
	if !bytes.Equal(end, []byte{0x00, 0x00, 0x09}) {
		t.Fatalf("expected end marker 0x00 0x00 0x09 got %x", end)
	}

	// Verify round-trip to confirm all values are correct.
	decoded, err := DecodeECMAArray(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("DecodeECMAArray error: %v", err)
	}
	if decoded["duration"] != 10.0 {
		t.Fatalf("duration mismatch got %v", decoded["duration"])
	}
	if decoded["stereo"] != true {
		t.Fatalf("stereo mismatch got %v", decoded["stereo"])
	}
	if decoded["title"] != "test" {
		t.Fatalf("title mismatch got %v", decoded["title"])
	}
}

// TestDecodeECMAArray_RoundTrip encodes then decodes a map and verifies equality.
func TestDecodeECMAArray_RoundTrip(t *testing.T) {
	in := map[string]interface{}{
		"width":    1920.0,
		"height":   1080.0,
		"hasAudio": true,
		"hasVideo": true,
		"codec":    "h264",
	}
	out, err := roundTripECMAArray(in)
	if err != nil {
		t.Fatalf("round trip failed: %v", err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip mismatch\n got: %#v\nwant: %#v", out, in)
	}
}

// TestEncodeECMAArray_SortedKeys verifies that keys are emitted in
// lexicographic order by checking the wire bytes directly.
func TestEncodeECMAArray_SortedKeys(t *testing.T) {
	m := map[string]interface{}{
		"z": 1.0,
		"a": 2.0,
		"m": 3.0,
	}
	var buf bytes.Buffer
	if err := EncodeECMAArray(&buf, m); err != nil {
		t.Fatalf("EncodeECMAArray error: %v", err)
	}
	data := buf.Bytes()

	// Skip marker (1) + count (4) = offset 5. Keys should appear as a, m, z.
	offset := 5

	expectedKeys := []string{"a", "m", "z"}
	for _, wantKey := range expectedKeys {
		if offset+2 > len(data) {
			t.Fatalf("truncated at key %q", wantKey)
		}
		klen := binary.BigEndian.Uint16(data[offset : offset+2])
		offset += 2
		if int(klen) != len(wantKey) {
			t.Fatalf("key length mismatch for %q: got %d", wantKey, klen)
		}
		gotKey := string(data[offset : offset+int(klen)])
		offset += int(klen)
		if gotKey != wantKey {
			t.Fatalf("key order mismatch: got %q want %q", gotKey, wantKey)
		}
		// Skip the value (Number = 1 marker + 8 bytes = 9 bytes).
		offset += 9
	}
}

// TestECMAArray_ViaEncodeValue verifies that EncodeValue with an ECMAArray
// wrapper produces marker 0x08 (not 0x03).
func TestECMAArray_ViaEncodeValue(t *testing.T) {
	var buf bytes.Buffer
	ecma := ECMAArray{"key": "value"}
	if err := EncodeValue(&buf, ecma); err != nil {
		t.Fatalf("EncodeValue(ECMAArray) error: %v", err)
	}
	data := buf.Bytes()
	if len(data) == 0 {
		t.Fatal("empty output")
	}
	if data[0] != 0x08 {
		t.Fatalf("expected marker 0x08 got 0x%02x", data[0])
	}

	// Also verify it decodes back correctly via DecodeValue.
	decoded, err := DecodeValue(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("DecodeValue error: %v", err)
	}
	m, ok := decoded.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{}, got %T", decoded)
	}
	if m["key"] != "value" {
		t.Fatalf("expected key=value got %v", m["key"])
	}
}

// TestEncodeECMAArray_Nil verifies that a nil map produces a valid empty ECMA array.
func TestEncodeECMAArray_Nil(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodeECMAArray(&buf, nil); err != nil {
		t.Fatalf("EncodeECMAArray(nil) error: %v", err)
	}
	want := []byte{0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x09}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("nil ECMA array mismatch\n got: %x\nwant: %x", buf.Bytes(), want)
	}
}

// TestDecodeECMAArray_InvalidMarker sends an object marker where an ECMA array
// marker (0x08) is expected.
func TestDecodeECMAArray_InvalidMarker(t *testing.T) {
	bad := []byte{0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x09}
	if _, err := DecodeECMAArray(bytes.NewReader(bad)); err == nil {
		t.Fatalf("expected error for invalid marker")
	}
}

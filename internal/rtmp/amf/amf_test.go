// Package amf – high-level round-trip and integration tests for the AMF0 codec.
//
// AMF0 (Action Message Format version 0) is the binary serialization format
// used by RTMP to encode command parameters: numbers (float64), booleans,
// strings, null, objects (string→value maps), and arrays.
//
// These tests exercise the top-level Marshal/Unmarshal and EncodeAll/DecodeAll
// APIs, which delegate to type-specific encoders/decoders in sibling files.
//
// Key Go concepts demonstrated:
//   - interface{} (any) to represent dynamically-typed AMF values.
//   - Custom deepEqual instead of reflect.DeepEqual for explicit, safe
//     comparison of the supported AMF0 type subset.
package amf

import (
	"bytes"
	"testing"
)

// TestEncodeDecodeRoundTrip_Primitives encodes each AMF0 value, then decodes
// it and checks that the result matches the original. This is the primary
// correctness test for the codec.
//
// The test covers every AMF0 type supported by this project:
//   - float64 (AMF0 Number)
//   - bool (AMF0 Boolean)
//   - string (AMF0 String)
//   - nil (AMF0 Null)
//   - map[string]interface{} (AMF0 Object)
//   - []interface{} (AMF0 Strict Array)
//   - Nested combinations of the above
func TestEncodeDecodeRoundTrip_Primitives(t *testing.T) {
	cases := []interface{}{
		float64(0),
		float64(1.5),
		true,
		false,
		"test",
		"",  // empty string
		nil, // null
		map[string]interface{}{"a": float64(1), "b": "x"},
		[]interface{}{float64(1), "x", false, nil},
		map[string]interface{}{"nested": map[string]interface{}{"n": float64(42)}},
		[]interface{}{[]interface{}{float64(1), float64(2)}, map[string]interface{}{"k": "v"}},
	}
	for i, v := range cases {
		b, err := Marshal(v)
		if err != nil {
			t.Fatalf("case %d marshal error: %v", i, err)
		}
		rv, err := Unmarshal(b)
		if err != nil {
			t.Fatalf("case %d unmarshal error: %v", i, err)
		}
		if !deepEqual(v, rv) {
			t.Fatalf("case %d mismatch\norig=%#v\nrtnd=%#v", i, v, rv)
		}
	}
}

// TestEncodeAllDecodeAll_Sequence simulates a real RTMP command sequence.
// In RTMP, commands like "connect" are sent as a sequence of AMF0 values:
//
//	[command-name, transaction-id, command-object, optional-args...]
//
// EncodeAll writes multiple values back-to-back into one byte stream, and
// DecodeAll reads them all back. This tests the multi-value API.
func TestEncodeAllDecodeAll_Sequence(t *testing.T) {
	seq := []interface{}{
		"connect",
		float64(1),
		map[string]interface{}{"app": "live", "tcUrl": "rtmp://example/live"},
		nil,
	}
	b, err := EncodeAll(seq...)
	if err != nil {
		t.Fatalf("encode all: %v", err)
	}
	out, err := DecodeAll(b)
	if err != nil {
		t.Fatalf("decode all: %v", err)
	}
	if len(out) != len(seq) {
		t.Fatalf("length mismatch expected %d got %d", len(seq), len(out))
	}
	for i := range seq {
		if !deepEqual(seq[i], out[i]) {
			t.Fatalf("index %d mismatch\nexp=%#v\ngot=%#v", i, seq[i], out[i])
		}
	}
}

// TestDecodeValue_UnsupportedMarkers ensures that AMF0 marker bytes this
// implementation intentionally does not support (Undefined 0x06, Reference
// 0x07, Date 0x0B, AMF3-switch 0x11) return a clear error.
//
// Production RTMP clients (FFmpeg, OBS) never send these markers, so
// rejecting them is the safest path.
func TestDecodeValue_UnsupportedMarkers(t *testing.T) {
	// Markers explicitly rejected: 0x06 (Undefined), 0x07 (Reference), 0x0B (Date), 0x11 (AMF3 switch)
	markers := []byte{0x06, 0x07, 0x0B, 0x11}
	for _, m := range markers {
		_, err := DecodeValue(bytes.NewReader([]byte{m}))
		if err == nil {
			t.Fatalf("marker 0x%02x expected error", m)
		}
	}
}

// deepEqual is a custom comparison function tailored to the AMF0 type subset.
// We avoid reflect.DeepEqual to keep dependencies explicit and to allow
// custom handling (e.g. NaN comparison) in the future. It recursively
// compares maps and slices, and uses Go's == operator for primitives.
func deepEqual(a, b interface{}) bool {
	switch av := a.(type) {
	case nil:
		return b == nil
	case float64:
		bv, ok := b.(float64)
		return ok && av == bv
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case map[string]interface{}:
		bv, ok := b.(map[string]interface{})
		if !ok {
			return false
		}
		if len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			if !deepEqual(v, bv[k]) {
				return false
			}
		}
		return true
	case []interface{}:
		bv, ok := b.([]interface{})
		if !ok {
			return false
		}
		if len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !deepEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// --- Benchmarks ---

// BenchmarkEncodeAll_ConnectCommand benchmarks multi-value encoding of a full connect command.
func BenchmarkEncodeAll_ConnectCommand(b *testing.B) {
	b.ReportAllocs()
	obj := map[string]interface{}{
		"app":      "live",
		"type":     "nonprivate",
		"flashVer": "FMLE/3.0",
		"tcUrl":    "rtmp://localhost/live",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = EncodeAll("connect", 1.0, obj)
	}
}

// BenchmarkDecodeAll_ConnectCommand benchmarks multi-value decoding of a full connect command.
func BenchmarkDecodeAll_ConnectCommand(b *testing.B) {
	b.ReportAllocs()
	obj := map[string]interface{}{
		"app":      "live",
		"type":     "nonprivate",
		"flashVer": "FMLE/3.0",
		"tcUrl":    "rtmp://localhost/live",
	}
	data, err := EncodeAll("connect", 1.0, obj)
	if err != nil {
		b.Fatalf("encode: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = DecodeAll(data)
	}
}

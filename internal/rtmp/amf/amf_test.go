package amf

import (
	"bytes"
	"testing"
)

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

// deepEqual tailored for the supported AMF0 subset – we could use reflect.DeepEqual
// but implement a minimal version to keep dependencies explicit and allow custom logic later.
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

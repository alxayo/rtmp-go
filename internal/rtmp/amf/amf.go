package amf

// Generic AMF0 encoder/decoder (T032)
//
// This file provides the public entry points for encoding and decoding AMF0
// values. It builds on the type‑specific implementations (number, boolean,
// string, null, object, strict array) implemented in earlier tasks (T026–T031).
// The generic encoder dispatches on Go value types. The generic decoder reads
// the leading marker byte and dispatches to the appropriate type‑specific
// decoder. Unsupported markers (0x06 Undefined, 0x07 Reference, 0x0B+ future /
// AMF3 types) are rejected with an *errors.AMFError.
//
// Supported markers here: 0x00 Number, 0x01 Boolean, 0x02 String, 0x03 Object,
// 0x05 Null, 0x0A Strict Array.

import (
	"bytes"
	"fmt"
	"io"

	amferrors "github.com/alxayo/go-rtmp/internal/errors"
)

// EncodeValue encodes a single AMF0 value to w using dynamic dispatch based on
// the Go type. Supported Go types:
//
//	nil -> Null (0x05)
//	float64 -> Number (0x00)
//	bool -> Boolean (0x01)
//	string -> String (0x02)
//	map[string]interface{} -> Object (0x03)
//	[]interface{} -> Strict Array (0x0A)
//
// Any other type results in *errors.AMFError.
func EncodeValue(w io.Writer, v interface{}) error {
	if err := encodeAny(w, v); err != nil { // encodeAny already returns plain error; wrap.
		return amferrors.NewAMFError("encode.value", err)
	}
	return nil
}

// EncodeAll encodes a sequence of AMF0 values in order and returns the bytes.
// This is convenient for building RTMP command message payloads which are a
// concatenation of multiple AMF0 values (e.g. ["connect", 1, {...}]).
func EncodeAll(values ...interface{}) ([]byte, error) {
	var buf bytes.Buffer
	for i, v := range values {
		if err := EncodeValue(&buf, v); err != nil {
			return nil, fmt.Errorf("value %d: %w", i, err)
		}
	}
	return buf.Bytes(), nil
}

// DecodeValue decodes a single AMF0 value from r. It reads the leading marker
// byte and dispatches to the concrete decoder. Returned interface{} will be one
// of the supported Go types listed in EncodeValue docs.
func DecodeValue(r io.Reader) (interface{}, error) {
	var marker [1]byte
	if _, err := io.ReadFull(r, marker[:]); err != nil {
		return nil, amferrors.NewAMFError("decode.value.marker.read", err)
	}
	// Fast path for supported markers via existing helper (object.go) which
	// expects the marker already consumed and reconstructs a reader with it.
	switch marker[0] {
	case markerNumber, markerBoolean, markerString, markerNull, markerObject, markerStrictArray:
		v, err := decodeValueWithMarker(marker[0], r)
		if err != nil {
			return nil, amferrors.NewAMFError("decode.value.dispatch", err)
		}
		return v, nil
	}
	if unsupportedMarker(marker[0]) {
		return nil, amferrors.NewAMFError("decode.value.unsupported", fmt.Errorf("unsupported marker 0x%02x", marker[0]))
	}
	// Any other marker (including 0x04, 0x08, 0x09 as standalone) we treat as unsupported per task scope.
	return nil, amferrors.NewAMFError("decode.value.unsupported", fmt.Errorf("unsupported marker 0x%02x", marker[0]))
}

// DecodeAll decodes a concatenated sequence of AMF0 values from data until
// exhaustion. This is helpful for parsing command payloads. It stops at EOF.
func DecodeAll(data []byte) ([]interface{}, error) {
	r := bytes.NewReader(data)
	var out []interface{}
	for r.Len() > 0 { // while unread bytes remain
		v, err := DecodeValue(r)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// Marshal is a convenience alias for EncodeValue returning the produced bytes.
func Marshal(v interface{}) ([]byte, error) { return EncodeAll(v) }

// Unmarshal decodes a single AMF0 value from data. If extra bytes remain after
// one value they are ignored (mirroring common JSON-like unmarshal semantics).
func Unmarshal(data []byte) (interface{}, error) {
	r := bytes.NewReader(data)
	v, err := DecodeValue(r)
	if err != nil {
		return nil, err
	}
	return v, nil
}

// unsupportedMarker returns true if the marker is explicitly listed by task
// spec to be rejected (Undefined, Reference, AMF3+ reserved range).
func unsupportedMarker(m byte) bool {
	if m == 0x06 || m == 0x07 { // Undefined, Reference
		return true
	}
	if m >= 0x0B { // Date (0x0B) and anything above (AMF3 etc) out of scope / rejected
		return true
	}
	return false
}

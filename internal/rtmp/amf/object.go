package amf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"sort"

	amferrors "github.com/alxayo/go-rtmp/internal/errors"
)

// markerObject is the AMF0 type marker for Object (0x03). The object end marker is 0x00 0x00 0x09.
const (
	markerObject    = 0x03
	markerObjectEnd = 0x09 // after 0x00 0x00 key length sentinel
)

// EncodeObject encodes an AMF0 Object value (map[string]interface{}).
// Wire format:
//
//	0x03 | repeated { 2-byte key length | UTF-8 key bytes | AMF0 value } | 0x00 0x00 0x09
//
// Keys are emitted in lexicographic order for deterministic output (required for golden tests).
// Supported value Go types (recursively):
//   - nil -> Null
//   - float64 -> Number
//   - bool -> Boolean
//   - string -> String
//   - map[string]interface{} -> Object
//
// Unsupported types result in an *errors.AMFError.
func EncodeObject(w io.Writer, m map[string]interface{}) error {
	if m == nil { // Treat nil map as empty object.
		if _, err := w.Write([]byte{markerObject, 0x00, 0x00, markerObjectEnd}); err != nil {
			return amferrors.NewAMFError("encode.object.empty.write", err)
		}
		return nil
	}

	if _, err := w.Write([]byte{markerObject}); err != nil {
		return amferrors.NewAMFError("encode.object.marker.write", err)
	}

	// Stable ordering for reproducibility.
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var hdr [2]byte
	for _, k := range keys {
		kb := []byte(k)
		if len(kb) > 0xFFFF {
			return amferrors.NewAMFError("encode.object.key.length", fmt.Errorf("key '%s' length %d exceeds 65535", k, len(kb)))
		}
		binary.BigEndian.PutUint16(hdr[:], uint16(len(kb)))
		if _, err := w.Write(hdr[:]); err != nil {
			return amferrors.NewAMFError("encode.object.key.length.write", err)
		}
		if len(kb) > 0 {
			if _, err := w.Write(kb); err != nil {
				return amferrors.NewAMFError("encode.object.key.write", err)
			}
		}
		// Encode value by dynamic dispatch over supported primitives / nested objects.
		if err := encodeAny(w, m[k]); err != nil {
			return amferrors.NewAMFError("encode.object.value", fmt.Errorf("key '%s': %w", k, err))
		}
	}

	// Object end marker: empty key (0x00 0x00) + 0x09.
	if _, err := w.Write([]byte{0x00, 0x00, markerObjectEnd}); err != nil {
		return amferrors.NewAMFError("encode.object.end.write", err)
	}
	return nil
}

// encodeAny is a minimal internal dispatcher for the subset of AMF0 types implemented so far
// (Number, Boolean, String, Null, Object). Arrays (0x0A) and others are not yet supported here
// because they are implemented in later tasks.
func encodeAny(w io.Writer, v interface{}) error {
	switch vv := v.(type) {
	case nil:
		return EncodeNull(w)
	case float64:
		return EncodeNumber(w, vv)
	case bool:
		return EncodeBoolean(w, vv)
	case string:
		return EncodeString(w, vv)
	case map[string]interface{}:
		return EncodeObject(w, vv)
	default:
		return fmt.Errorf("unsupported AMF0 value type %T", v)
	}
}

// DecodeObject decodes an AMF0 Object into a map[string]interface{}.
// It expects the marker 0x03 at the current reader position.
func DecodeObject(r io.Reader) (map[string]interface{}, error) {
	var mMarker [1]byte
	if _, err := io.ReadFull(r, mMarker[:]); err != nil {
		return nil, amferrors.NewAMFError("decode.object.marker.read", err)
	}
	if mMarker[0] != markerObject {
		return nil, amferrors.NewAMFError("decode.object.marker", fmt.Errorf("expected 0x%02x got 0x%02x", markerObject, mMarker[0]))
	}
	out := make(map[string]interface{})
	for {
		var klenBuf [2]byte
		if _, err := io.ReadFull(r, klenBuf[:]); err != nil {
			return nil, amferrors.NewAMFError("decode.object.key.length.read", err)
		}
		klen := binary.BigEndian.Uint16(klenBuf[:])
		if klen == 0 { // Potential end marker.
			var end [1]byte
			if _, err := io.ReadFull(r, end[:]); err != nil {
				return nil, amferrors.NewAMFError("decode.object.end.read", err)
			}
			if end[0] != markerObjectEnd {
				return nil, amferrors.NewAMFError("decode.object.end.marker", fmt.Errorf("expected 0x%02x got 0x%02x", markerObjectEnd, end[0]))
			}
			break
		}
		keyBytes := make([]byte, klen)
		if _, err := io.ReadFull(r, keyBytes); err != nil {
			return nil, amferrors.NewAMFError("decode.object.key.read", err)
		}
		key := string(keyBytes)

		// Peek marker for value to dispatch. We read one byte, then re-create a reader with it prefixed.
		var valMarker [1]byte
		if _, err := io.ReadFull(r, valMarker[:]); err != nil {
			return nil, amferrors.NewAMFError("decode.object.value.marker.read", err)
		}

		val, err := decodeValueWithMarker(valMarker[0], r)
		if err != nil {
			return nil, amferrors.NewAMFError("decode.object.value", fmt.Errorf("key '%s': %w", key, err))
		}
		out[key] = val
	}
	return out, nil
}

// decodeValueWithMarker dispatches based on an already-consumed marker byte. It consumes the
// remaining payload from r appropriate to the marker.
func decodeValueWithMarker(marker byte, r io.Reader) (interface{}, error) {
	switch marker {
	case markerNumber:
		// Reconstruct a reader including the marker to reuse existing decoder.
		return DecodeNumber(io.MultiReader(bytes.NewReader([]byte{marker}), r))
	case markerBoolean:
		return DecodeBoolean(io.MultiReader(bytes.NewReader([]byte{marker}), r))
	case markerString:
		return DecodeString(io.MultiReader(bytes.NewReader([]byte{marker}), r))
	case markerNull:
		v, err := DecodeNull(io.MultiReader(bytes.NewReader([]byte{marker}), r))
		return v, err
	case markerObject:
		// Nested object: reuse DecodeObject by reconstructing the marker.
		return DecodeObject(io.MultiReader(bytes.NewReader([]byte{marker}), r))
	default:
		return nil, fmt.Errorf("unsupported marker 0x%02x", marker)
	}
}

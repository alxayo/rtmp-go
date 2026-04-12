package amf

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
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

// encodeAny is an internal dispatcher for the AMF0 types supported by this package:
// Number, Boolean, String, Null, Object, ECMA Array, and Strict Array.
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
	case ECMAArray:
		return EncodeECMAArray(w, map[string]interface{}(vv))
	case []interface{}: // Strict Array
		return EncodeStrictArray(w, vv)
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
	return decodeObjectPayload(r)
}

// decodeValueWithMarker dispatches based on an already-consumed marker byte.
// It reads the remaining payload from r without re-reading the marker, avoiding
// the allocation overhead of io.MultiReader.
func decodeValueWithMarker(marker byte, r io.Reader) (interface{}, error) {
	switch marker {
	case markerNumber:
		var num [8]byte
		if _, err := io.ReadFull(r, num[:]); err != nil {
			return nil, amferrors.NewAMFError("decode.number.read", err)
		}
		u := binary.BigEndian.Uint64(num[:])
		return math.Float64frombits(u), nil
	case markerBoolean:
		var b [1]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return nil, amferrors.NewAMFError("decode.boolean.read", err)
		}
		return b[0] != 0x00, nil
	case markerString:
		return decodeStringPayload(r)
	case markerNull:
		return nil, nil // null has no payload beyond the marker
	case markerObject:
		return decodeObjectPayload(r)
	case markerECMAArray:
		return decodeECMAArrayPayload(r)
	case markerStrictArray:
		return decodeStrictArrayPayload(r)
	default:
		return nil, fmt.Errorf("unsupported marker 0x%02x", marker)
	}
}

// decodeStringPayload reads an AMF0 string payload (length + bytes) after the
// marker has already been consumed.
func decodeStringPayload(r io.Reader) (string, error) {
	var ln [2]byte
	if _, err := io.ReadFull(r, ln[:]); err != nil {
		return "", amferrors.NewAMFError("decode.string.length.read", err)
	}
	l := binary.BigEndian.Uint16(ln[:])
	if l == 0 {
		return "", nil
	}
	buf := make([]byte, l)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", amferrors.NewAMFError("decode.string.read", err)
	}
	return string(buf), nil
}

// decodeObjectPayload reads an AMF0 object payload (key-value pairs + end marker)
// after the object marker has already been consumed.
func decodeObjectPayload(r io.Reader) (map[string]interface{}, error) {
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

		// Decode the value (reads marker internally).
		val, err := DecodeValue(r)
		if err != nil {
			return nil, amferrors.NewAMFError("decode.object.value", fmt.Errorf("key '%s': %w", key, err))
		}
		out[key] = val
	}
	return out, nil
}

// decodeStrictArrayPayload reads an AMF0 strict array payload (count + elements)
// after the array marker has already been consumed.
func decodeStrictArrayPayload(r io.Reader) ([]interface{}, error) {
	var countBuf [4]byte
	if _, err := io.ReadFull(r, countBuf[:]); err != nil {
		return nil, amferrors.NewAMFError("decode.array.count.read", err)
	}
	count := binary.BigEndian.Uint32(countBuf[:])
	out := make([]interface{}, 0, count)
	for i := uint32(0); i < count; i++ {
		val, err := DecodeValue(r)
		if err != nil {
			return nil, amferrors.NewAMFError("decode.array.element", fmt.Errorf("index %d: %w", i, err))
		}
		out = append(out, val)
	}
	return out, nil
}

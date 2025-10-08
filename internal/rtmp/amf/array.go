package amf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	amferrors "github.com/alxayo/go-rtmp/internal/errors"
)

// markerStrictArray is the AMF0 type marker for Strict Array (0x0A).
const markerStrictArray = 0x0A

// EncodeStrictArray encodes an AMF0 Strict Array (marker 0x0A) comprised of a fixed
// count of values. Wire format:
//
//	0x0A | 4-byte big-endian count | repeated AMF0 values (each with its own marker)
//
// Supported element Go types are the same subset handled by encodeAny (Number, Boolean,
// String, Null, Object, Strict Array). Nested arrays are therefore handled recursively.
// Unsupported element types yield an *errors.AMFError.
func EncodeStrictArray(w io.Writer, arr []interface{}) error {
	var hdr [1 + 4]byte
	hdr[0] = markerStrictArray
	binary.BigEndian.PutUint32(hdr[1:], uint32(len(arr)))
	if _, err := w.Write(hdr[:]); err != nil {
		return amferrors.NewAMFError("encode.array.header.write", err)
	}
	for i, v := range arr {
		if err := encodeAny(w, v); err != nil {
			return amferrors.NewAMFError("encode.array.element", fmt.Errorf("index %d: %w", i, err))
		}
	}
	return nil
}

// DecodeStrictArray decodes an AMF0 Strict Array from r returning a slice of interface{}.
// Error cases include:
//   - Marker mismatch (decode.array.marker)
//   - Short reads for header or elements (decode.array.header.read / decode.array.element.read)
//   - Unsupported nested type markers (bubbled from decodeValueWithMarker)
func DecodeStrictArray(r io.Reader) ([]interface{}, error) {
	var marker [1]byte
	if _, err := io.ReadFull(r, marker[:]); err != nil {
		return nil, amferrors.NewAMFError("decode.array.marker.read", err)
	}
	if marker[0] != markerStrictArray {
		return nil, amferrors.NewAMFError("decode.array.marker", fmt.Errorf("expected 0x%02x got 0x%02x", markerStrictArray, marker[0]))
	}
	var countBuf [4]byte
	if _, err := io.ReadFull(r, countBuf[:]); err != nil {
		return nil, amferrors.NewAMFError("decode.array.count.read", err)
	}
	count := binary.BigEndian.Uint32(countBuf[:])
	out := make([]interface{}, 0, count)
	for i := uint32(0); i < count; i++ {
		// Read marker for element then dispatch.
		var elemMarker [1]byte
		if _, err := io.ReadFull(r, elemMarker[:]); err != nil {
			return nil, amferrors.NewAMFError("decode.array.element.marker.read", err)
		}
		val, err := decodeValueWithMarker(elemMarker[0], r)
		if err != nil {
			return nil, amferrors.NewAMFError("decode.array.element", fmt.Errorf("index %d: %w", i, err))
		}
		out = append(out, val)
	}
	return out, nil
}

// decodeArrayValue is a helper used by decodeValueWithMarker when it already consumed the
// marker byte. It reconstructs a reader with the marker for DecodeStrictArray.
func decodeArrayValue(r io.Reader) ([]interface{}, error) {
	return DecodeStrictArray(r)
}

// Helper for tests & internal usage: round-trip an array (used in future generic encoder).
func roundTripStrictArray(arr []interface{}) ([]interface{}, error) {
	var buf bytes.Buffer
	if err := EncodeStrictArray(&buf, arr); err != nil {
		return nil, err
	}
	return DecodeStrictArray(&buf)
}

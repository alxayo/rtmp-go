package amf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"sort"

	amferrors "github.com/alxayo/go-rtmp/internal/errors"
)

// markerECMAArray is the AMF0 type marker for ECMA Array (0x08).
// Wire format: 0x08 | count (uint32 big-endian, approximate) |
// repeated { 2-byte key length | UTF-8 key bytes | AMF0 value } | 0x00 0x00 0x09
const markerECMAArray = 0x08

// ECMAArray is a map type that signals ECMA Array encoding (marker 0x08)
// instead of Object encoding (marker 0x03). Use this wrapper when building
// onMetaData payloads.
type ECMAArray map[string]interface{}

// EncodeECMAArray encodes an AMF0 ECMA Array value (map[string]interface{}).
// The count field is advisory — it reflects the number of entries in the map
// at encode time. Keys are emitted in lexicographic order for deterministic output.
func EncodeECMAArray(w io.Writer, m map[string]interface{}) error {
	if m == nil {
		// Empty ECMA array: marker + count(0) + end marker.
		if _, err := w.Write([]byte{markerECMAArray, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, markerObjectEnd}); err != nil {
			return amferrors.NewAMFError("encode.ecma_array.empty.write", err)
		}
		return nil
	}

	// Write marker + approximate count.
	var hdr [1 + 4]byte
	hdr[0] = markerECMAArray
	binary.BigEndian.PutUint32(hdr[1:], uint32(len(m)))
	if _, err := w.Write(hdr[:]); err != nil {
		return amferrors.NewAMFError("encode.ecma_array.header.write", err)
	}

	// Stable ordering for reproducibility (same as EncodeObject).
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var keyHdr [2]byte
	for _, k := range keys {
		kb := []byte(k)
		if len(kb) > 0xFFFF {
			return amferrors.NewAMFError("encode.ecma_array.key.length", fmt.Errorf("key '%s' length %d exceeds 65535", k, len(kb)))
		}
		binary.BigEndian.PutUint16(keyHdr[:], uint16(len(kb)))
		if _, err := w.Write(keyHdr[:]); err != nil {
			return amferrors.NewAMFError("encode.ecma_array.key.length.write", err)
		}
		if len(kb) > 0 {
			if _, err := w.Write(kb); err != nil {
				return amferrors.NewAMFError("encode.ecma_array.key.write", err)
			}
		}
		if err := encodeAny(w, m[k]); err != nil {
			return amferrors.NewAMFError("encode.ecma_array.value", fmt.Errorf("key '%s': %w", k, err))
		}
	}

	// Object end marker: empty key (0x00 0x00) + 0x09.
	if _, err := w.Write([]byte{0x00, 0x00, markerObjectEnd}); err != nil {
		return amferrors.NewAMFError("encode.ecma_array.end.write", err)
	}
	return nil
}

// DecodeECMAArray decodes an AMF0 ECMA Array from r returning a map[string]interface{}.
// It expects marker 0x08 at the current reader position.
func DecodeECMAArray(r io.Reader) (map[string]interface{}, error) {
	var mMarker [1]byte
	if _, err := io.ReadFull(r, mMarker[:]); err != nil {
		return nil, amferrors.NewAMFError("decode.ecma_array.marker.read", err)
	}
	if mMarker[0] != markerECMAArray {
		return nil, amferrors.NewAMFError("decode.ecma_array.marker", fmt.Errorf("expected 0x%02x got 0x%02x", markerECMAArray, mMarker[0]))
	}
	return decodeECMAArrayPayload(r)
}

// decodeECMAArrayPayload reads an AMF0 ECMA Array payload after the marker
// has already been consumed. It reads the advisory count, then delegates to
// decodeObjectPayload for the key-value pairs and end marker.
func decodeECMAArrayPayload(r io.Reader) (map[string]interface{}, error) {
	// Read and discard the advisory count — we rely on the end marker.
	var countBuf [4]byte
	if _, err := io.ReadFull(r, countBuf[:]); err != nil {
		return nil, amferrors.NewAMFError("decode.ecma_array.count.read", err)
	}
	return decodeObjectPayload(r)
}

// roundTripECMAArray is a helper for tests: encode then decode for round-trip verification.
func roundTripECMAArray(m map[string]interface{}) (map[string]interface{}, error) {
	var buf bytes.Buffer
	if err := EncodeECMAArray(&buf, m); err != nil {
		return nil, err
	}
	return DecodeECMAArray(&buf)
}

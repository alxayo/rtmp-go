package amf

import (
	"fmt"
	"io"

	amferrors "github.com/alxayo/go-rtmp/internal/errors"
)

// markerNull is the AMF0 type marker for Null (0x05).
const markerNull = 0x05

// EncodeNull writes an AMF0 Null value (single marker byte 0x05) to w.
// Contract:
//   - Writes exactly 1 byte on success.
//   - Returns *errors.AMFError on any write failure.
func EncodeNull(w io.Writer) error {
	var b [1]byte
	b[0] = markerNull
	if _, err := w.Write(b[:]); err != nil {
		return amferrors.NewAMFError("encode.null.write", err)
	}
	return nil
}

// DecodeNull reads an AMF0 Null value from r.
// Expected wire format: single marker byte 0x05 (no payload).
// Returns (nil, nil) on success.
// Error cases:
//   - Short read of marker -> decode.null.marker.read
//   - Marker mismatch -> decode.null.marker
func DecodeNull(r io.Reader) (interface{}, error) {
	var b [1]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return nil, amferrors.NewAMFError("decode.null.marker.read", err)
	}
	if b[0] != markerNull {
		return nil, amferrors.NewAMFError("decode.null.marker", fmt.Errorf("expected 0x%02x got 0x%02x", markerNull, b[0]))
	}
	return nil, nil
}

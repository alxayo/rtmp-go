package amf

import (
	"fmt"
	"io"

	amferrors "github.com/alxayo/go-rtmp/internal/errors"
)

// markerBoolean is the AMF0 type marker for Boolean (0x01).
const markerBoolean = 0x01

// EncodeBoolean writes an AMF0 Boolean value to w.
// Wire format: marker 0x01 followed by a single byte 0x00 (false) or 0x01 (true).
// Contract:
//   - Always writes exactly 2 bytes on success.
//   - Returns *errors.AMFError on any failure.
func EncodeBoolean(w io.Writer, v bool) error {
	var buf [2]byte
	buf[0] = markerBoolean
	if v {
		buf[1] = 0x01
	} else {
		buf[1] = 0x00
	}
	if _, err := w.Write(buf[:]); err != nil {
		return amferrors.NewAMFError("encode.boolean.write", err)
	}
	return nil
}

// DecodeBoolean reads an AMF0 Boolean from r.
// Expected wire format: marker 0x01 then 1 data byte (0x00=false, anything else=true per spec liberal read).
// Error cases:
//   - Short reads -> wrapped io error (decode.boolean.read)
//   - Marker mismatch -> decode.boolean.marker
func DecodeBoolean(r io.Reader) (bool, error) {
	var hdr [2]byte
	if _, err := io.ReadFull(r, hdr[:1]); err != nil { // read marker first for clearer error separation
		return false, amferrors.NewAMFError("decode.boolean.marker.read", err)
	}
	if hdr[0] != markerBoolean {
		return false, amferrors.NewAMFError("decode.boolean.marker", fmt.Errorf("expected 0x%02x got 0x%02x", markerBoolean, hdr[0]))
	}
	if _, err := io.ReadFull(r, hdr[1:2]); err != nil {
		return false, amferrors.NewAMFError("decode.boolean.read", err)
	}
	return hdr[1] != 0x00, nil
}

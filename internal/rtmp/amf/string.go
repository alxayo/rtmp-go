package amf

import (
	"encoding/binary"
	"fmt"
	"io"

	amferrors "github.com/alxayo/go-rtmp/internal/errors"
)

// markerString is the AMF0 type marker for String (0x02).
const markerString = 0x02

// EncodeString writes an AMF0 String to w.
// Wire format: 0x02 | 2-byte big-endian length | UTF-8 bytes.
// Contracts:
//   - Returns *errors.AMFError on failure.
//   - Rejects strings whose byte length exceeds 65535 (AMF0 short string limit).
func EncodeString(w io.Writer, s string) error {
	b := []byte(s) // UTF-8 in Go string already.
	if len(b) > 0xFFFF {
		return amferrors.NewAMFError("encode.string.length", fmt.Errorf("string length %d exceeds 65535", len(b)))
	}
	var hdr [1 + 2]byte
	hdr[0] = markerString
	binary.BigEndian.PutUint16(hdr[1:], uint16(len(b)))
	if _, err := w.Write(hdr[:]); err != nil {
		return amferrors.NewAMFError("encode.string.write.header", err)
	}
	if len(b) == 0 { // empty string done.
		return nil
	}
	if _, err := w.Write(b); err != nil {
		return amferrors.NewAMFError("encode.string.write.body", err)
	}
	return nil
}

// DecodeString reads an AMF0 String from r.
// Error cases:
//   - Marker mismatch -> decode.string.marker
//   - Short reads -> decode.string.marker.read / decode.string.length.read / decode.string.read
func DecodeString(r io.Reader) (string, error) {
	var m [1]byte
	if _, err := io.ReadFull(r, m[:]); err != nil {
		return "", amferrors.NewAMFError("decode.string.marker.read", err)
	}
	if m[0] != markerString {
		return "", amferrors.NewAMFError("decode.string.marker", fmt.Errorf("expected 0x%02x got 0x%02x", markerString, m[0]))
	}
	var ln [2]byte
	if _, err := io.ReadFull(r, ln[:]); err != nil {
		return "", amferrors.NewAMFError("decode.string.length.read", err)
	}
	l := binary.BigEndian.Uint16(ln[:])
	if l == 0 { // empty string
		return "", nil
	}
	buf := make([]byte, l)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", amferrors.NewAMFError("decode.string.read", err)
	}
	return string(buf), nil
}

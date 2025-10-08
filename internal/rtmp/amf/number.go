package amf

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"

	amferrors "github.com/alxayo/go-rtmp/internal/errors"
)

// AMF0 type markers (subset used here). Keeping as constants for later types.
const (
	markerNumber = 0x00
)

// EncodeNumber writes an AMF0 Number (marker 0x00 + 8‑byte IEEE754 double, big-endian)
// to the provided writer.
//
// Contract:
//   - Always writes exactly 9 bytes on success.
//   - Returns *errors.AMFError wrapped with context on failure.
func EncodeNumber(w io.Writer, v float64) error {
	var buf [1 + 8]byte
	buf[0] = markerNumber
	binary.BigEndian.PutUint64(buf[1:], math.Float64bits(v))
	if _, err := w.Write(buf[:]); err != nil {
		return amferrors.NewAMFError("encode.number.write", err)
	}
	return nil
}

// DecodeNumber reads an AMF0 Number (expects marker 0x00 followed by an
// 8‑byte IEEE754 double in big-endian order) from r and returns the float64.
//
// Error cases:
//   - Unexpected marker -> amf error (decode.number.marker)
//   - Short reads -> underlying io error wrapped (decode.number.read)
func DecodeNumber(r io.Reader) (float64, error) {
	var m [1]byte
	if _, err := io.ReadFull(r, m[:]); err != nil {
		return 0, amferrors.NewAMFError("decode.number.marker.read", err)
	}
	if m[0] != markerNumber {
		return 0, amferrors.NewAMFError("decode.number.marker", fmt.Errorf("expected 0x%02x got 0x%02x", markerNumber, m[0]))
	}
	var num [8]byte
	if _, err := io.ReadFull(r, num[:]); err != nil {
		return 0, amferrors.NewAMFError("decode.number.read", err)
	}
	u := binary.BigEndian.Uint64(num[:])
	return math.Float64frombits(u), nil
}

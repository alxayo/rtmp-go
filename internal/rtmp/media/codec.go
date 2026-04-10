package media

import "encoding/binary"

// fourCC converts a 4-byte ASCII string (e.g. "hvc1", "mp4a") to a big-endian
// uint32 for use as a map key. Used by both videoFourCCMap and audioFourCCMap
// at package init time to build O(1) lookup tables from FourCC wire values.
func fourCC(s string) uint32 {
	return binary.BigEndian.Uint32([]byte(s))
}

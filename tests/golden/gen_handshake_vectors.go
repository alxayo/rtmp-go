//go:build ignore

// Generates deterministic RTMP handshake golden vector binary files.
// Run: go run ./tests/golden/gen_handshake_vectors.go
// Files:
//   - handshake_valid_c0c1.bin (C0 + C1)
//   - handshake_valid_s0s1s2.bin (S0 + S1 + S2)
//   - handshake_valid_c2.bin (C2)
//   - handshake_invalid_version.bin (C0=0x06 + C1)
//
// Handshake layout per spec (simple handshake):
//
//	C0/S0: 1 byte version (0x03)
//	C1/S1/C2/S2: 1536 bytes = timestamp(4, BE) + zero(4) + random(1528)
//
// Deterministic patterns chosen for reproducibility:
//
//	C1 timestamp = 0x00000001, random[i] = byte((i*7 + 3) & 0xFF)
//	S1 timestamp = 0x00000002, random[i] = byte((i*13 + 11) & 0xFF)
//	S2 = C1 (echo), C2 = S1 (echo)
package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

const (
	chunkSize = 1536
)

func buildBlock(ts uint32, pattern func(i int) byte) []byte {
	b := make([]byte, chunkSize)
	binary.BigEndian.PutUint32(b[0:4], ts)
	// bytes 4..8 already zero (timestamp+zero)
	for i := 8; i < chunkSize; i++ {
		b[i] = pattern(i - 8)
	}
	return b
}

func writeFile(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func main() {
	dir, _ := os.Getwd()
	fmt.Println("Generating RTMP handshake golden vectors in", dir)

	c1 := buildBlock(1, func(i int) byte { return byte((i*7 + 3) & 0xFF) })
	s1 := buildBlock(2, func(i int) byte { return byte((i*13 + 11) & 0xFF) })
	s2 := c1 // echo
	c2 := s1 // echo

	validC0C1 := append([]byte{0x03}, c1...)
	validS0S1S2 := append([]byte{0x03}, append(s1, s2...)...)
	invalidC0C1 := append([]byte{0x06}, c1...) // invalid version (RTMPE)

	files := []struct {
		name string
		data []byte
	}{
		{"handshake_valid_c0c1.bin", validC0C1},
		{"handshake_valid_s0s1s2.bin", validS0S1S2},
		{"handshake_valid_c2.bin", c2},
		{"handshake_invalid_version.bin", invalidC0C1},
	}

	for _, f := range files {
		p := filepath.Join(dir, f.name)
		if err := writeFile(p, f.data); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		h := sha256.Sum256(f.data)
		fmt.Printf("Wrote %-32s size=%4d sha256=%s\n", f.name, len(f.data), hex.EncodeToString(h[:8]))
	}
}

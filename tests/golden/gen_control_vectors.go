//go:build ignore

// Code generated for golden test vectors (RTMP control messages). DO NOT EDIT MANUALLY.
// Generation script for T008: Create Golden Test Vectors for Control Messages.
// Run: go run tests/golden/gen_control_vectors.go
// Produces the following files in tests/golden/:
//   - control_set_chunk_size_4096.bin                 (Set Chunk Size = 4096)
//   - control_acknowledgement_1M.bin                  (Acknowledgement = 1,000,000)
//   - control_window_ack_size_2_5M.bin                (Window Acknowledgement Size = 2,500,000)
//   - control_set_peer_bandwidth_dynamic.bin          (Set Peer Bandwidth = 2,500,000, limit type = 2 Dynamic)
//   - control_user_control_stream_begin.bin           (User Control: Stream Begin, stream ID = 1)
//
// Control message payload layouts (per RTMP spec):
//
//	Type 1 Set Chunk Size:            4 bytes (uint32 BE)
//	Type 3 Acknowledgement:           4 bytes (uint32 BE)
//	Type 5 Window Ack Size:           4 bytes (uint32 BE)
//	Type 6 Set Peer Bandwidth:        4 bytes (uint32 BE) + 1 byte limit type (0=Hard,1=Soft,2=Dynamic)
//	Type 4 User Control (StreamBegin): 2 bytes event type (0) + 4 bytes stream ID (uint32 BE)
//
// These golden vectors contain ONLY the message payload bytes (not RTMP chunk headers).
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func write(path string, data []byte) {
	must(os.WriteFile(path, data, 0o644))
	fmt.Printf("Wrote %-40s size=%d bytes\n", filepath.Base(path), len(data))
}

func main() {
	outDir := filepath.Join("tests", "golden")
	must(os.MkdirAll(outDir, 0o755))

	// 1. Set Chunk Size = 4096 (0x00001000)
	scs := []byte{0x00, 0x00, 0x10, 0x00}
	write(filepath.Join(outDir, "control_set_chunk_size_4096.bin"), scs)

	// 2. Acknowledgement = 1,000,000 (0x000F4240)
	ack := []byte{0x00, 0x0F, 0x42, 0x40}
	write(filepath.Join(outDir, "control_acknowledgement_1M.bin"), ack)

	// 3. Window Ack Size = 2,500,000 (0x002625A0)
	was := []byte{0x00, 0x26, 0x25, 0xA0}
	write(filepath.Join(outDir, "control_window_ack_size_2_5M.bin"), was)

	// 4. Set Peer Bandwidth = 2,500,000 + limit type Dynamic (0x02)
	spb := append(append([]byte{}, was...), 0x02)
	write(filepath.Join(outDir, "control_set_peer_bandwidth_dynamic.bin"), spb)

	// 5. User Control Stream Begin (event type=0x0000, stream ID=1)
	ucsb := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x01}
	write(filepath.Join(outDir, "control_user_control_stream_begin.bin"), ucsb)

	fmt.Println("Control message golden vectors generated in", outDir)
}

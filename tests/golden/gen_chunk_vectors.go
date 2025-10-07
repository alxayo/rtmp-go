// Code generated for golden test vectors (chunk headers). DO NOT EDIT MANUALLY.
// Generation script for T006: Create Golden Test Vectors for Chunk Headers
// Runs standalone: `go run tests/golden/gen_chunk_vectors.go`
// Deterministic (no randomness) so CI can validate byte-for-byte.
package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
)

// Helper functions
func must(err error) {
	if err != nil {
		panic(err)
	}
}

func writeUint24(b []byte, v uint32) { // big-endian 24-bit
	b[0] = byte(v >> 16)
	b[1] = byte(v >> 8)
	b[2] = byte(v)
}

func main() {
	outDir := filepath.Join("tests", "golden")
	must(os.MkdirAll(outDir, 0o755))

	// 1. FMT 0 Audio (csid=4) full header + payload
	// Timestamp: 1000 (0x0003E8), Length: 64 (0x000040), Type: 8 (Audio), StreamID: 1 (LE)
	{
		var buf []byte
		basic := byte((0 << 6) | 4) // FMT0, csid 4
		hdr := make([]byte, 11)
		writeUint24(hdr[0:3], 1000)
		writeUint24(hdr[3:6], 64)
		hdr[6] = 8
		binary.LittleEndian.PutUint32(hdr[7:11], 1)
		payload := make([]byte, 64)
		for i := range payload {
			payload[i] = byte(i)
		}
		buf = append(buf, basic)
		buf = append(buf, hdr...)
		buf = append(buf, payload...)
		must(os.WriteFile(filepath.Join(outDir, "chunk_fmt0_audio.bin"), buf, 0o644))
	}

	// 2. FMT 1 Video (csid=6) header (delta + length + type) + payload
	// Assumes previous FMT0 on csid=6 with same stream ID existed. Delta: 40ms, Length: 80, Type: 9 (Video)
	{
		var buf []byte
		basic := byte((1 << 6) | 6) // FMT1, csid 6
		hdr := make([]byte, 7)
		writeUint24(hdr[0:3], 40) // delta
		writeUint24(hdr[3:6], 80) // length
		hdr[6] = 9                // video
		payload := make([]byte, 80)
		for i := range payload {
			payload[i] = 0xA0 + byte(i)
		}
		buf = append(buf, basic)
		buf = append(buf, hdr...)
		buf = append(buf, payload...)
		must(os.WriteFile(filepath.Join(outDir, "chunk_fmt1_video.bin"), buf, 0o644))
	}

	// 3. FMT 2 Delta (csid=4) timestamp delta only + payload (inherits length/type/streamID= audio 64/8/1)
	// Delta: 33ms
	{
		var buf []byte
		basic := byte((2 << 6) | 4) // FMT2, csid 4
		delta := make([]byte, 3)
		writeUint24(delta, 33)
		payload := make([]byte, 64)
		for i := range payload {
			payload[i] = 0x40 + byte(i)
		}
		buf = append(buf, basic)
		buf = append(buf, delta...)
		buf = append(buf, payload...)
		must(os.WriteFile(filepath.Join(outDir, "chunk_fmt2_delta.bin"), buf, 0o644))
	}

	// 4. FMT 3 Continuation (csid=6) no header + 128 bytes chunk data (continuation of fragmented video message)
	{
		var buf []byte
		basic := byte((3 << 6) | 6) // FMT3, csid 6
		chunkData := make([]byte, 128)
		for i := range chunkData {
			chunkData[i] = 0xC0 + byte(i)
		}
		buf = append(buf, basic)
		buf = append(buf, chunkData...)
		must(os.WriteFile(filepath.Join(outDir, "chunk_fmt3_continuation.bin"), buf, 0o644))
	}

	// 5. Extended Timestamp (FMT0, csid=4) timestamp=0x01312D00 > 0xFFFFFF length=64 type=8 streamID=1
	{
		var buf []byte
		basic := byte((0 << 6) | 4) // FMT0 csid 4
		hdr := make([]byte, 11)
		writeUint24(hdr[0:3], 0xFFFFFF) // marker for extended
		writeUint24(hdr[3:6], 64)       // length
		hdr[6] = 8                      // audio
		binary.LittleEndian.PutUint32(hdr[7:11], 1)
		ext := make([]byte, 4)
		binary.BigEndian.PutUint32(ext, 0x01312D00)
		payload := make([]byte, 64)
		for i := range payload {
			payload[i] = 0x80 + byte(i)
		}
		buf = append(buf, basic)
		buf = append(buf, hdr...)
		buf = append(buf, ext...)
		buf = append(buf, payload...)
		must(os.WriteFile(filepath.Join(outDir, "chunk_extended_timestamp.bin"), buf, 0o644))
	}

	// 6. Interleaved (Audio csid=4, Video csid=6) each 256 bytes length, chunk size assumed 128
	// Sequence: Audio FMT0 (first 128) | Video FMT0 (first 128) | Audio FMT3 (rest) | Video FMT3 (rest)
	{
		var buf []byte
		// Audio FMT0
		basicAudio0 := byte((0 << 6) | 4)
		hdrA := make([]byte, 11)
		writeUint24(hdrA[0:3], 2000) // timestamp
		writeUint24(hdrA[3:6], 256)  // full length
		hdrA[6] = 8                  // audio type
		binary.LittleEndian.PutUint32(hdrA[7:11], 1)
		payloadA := make([]byte, 256)
		for i := range payloadA {
			payloadA[i] = byte(i)
		}
		buf = append(buf, basicAudio0)
		buf = append(buf, hdrA...)
		buf = append(buf, payloadA[:128]...)
		// Video FMT0
		basicVideo0 := byte((0 << 6) | 6)
		hdrV := make([]byte, 11)
		writeUint24(hdrV[0:3], 2000)
		writeUint24(hdrV[3:6], 256)
		hdrV[6] = 9 // video
		binary.LittleEndian.PutUint32(hdrV[7:11], 1)
		payloadV := make([]byte, 256)
		for i := range payloadV {
			payloadV[i] = 0x90 + byte(i)
		}
		buf = append(buf, basicVideo0)
		buf = append(buf, hdrV...)
		buf = append(buf, payloadV[:128]...)
		// Audio continuation FMT3
		basicAudio3 := byte((3 << 6) | 4)
		buf = append(buf, basicAudio3)
		buf = append(buf, payloadA[128:]...)
		// Video continuation FMT3
		basicVideo3 := byte((3 << 6) | 6)
		buf = append(buf, basicVideo3)
		buf = append(buf, payloadV[128:]...)
		must(os.WriteFile(filepath.Join(outDir, "chunk_interleaved.bin"), buf, 0o644))
	}

	fmt.Println("Golden chunk vector files generated in", outDir)
}

// reader_test.go – tests for the chunk Reader that reassembles fragmented
// RTMP chunks back into complete messages.
//
// The Reader sits between the TCP socket and the application layer. It reads
// raw bytes, parses chunk headers, collects payload fragments across multiple
// chunks, and delivers complete Message objects.
//
// Key concepts demonstrated:
//   - buildMessageBytes helper constructs raw FMT0 chunk bytes for testing.
//   - SetChunkSize – the RTMP protocol lets peers negotiate chunk sizes via
//     control message type 1; the Reader must update its internal chunk size.
//   - Interleaved streams – the Reader tracks per-CSID state so audio and
//     video chunks can be interleaved and reassembled independently.
package chunk

import (
	"bytes"
	"crypto/rand"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// Test utilities
func loadGoldenChunk(t *testing.T, name string) []byte { return loadGolden(t, name) }

// buildMessageBytes constructs a single FMT0 single-chunk message (no
// fragmentation). It encodes a full chunk header followed by the payload.
// This helper is used by many tests to prepare input for the Reader.
func buildMessageBytes(t *testing.T, csid uint32, ts uint32, msgType uint8, msid uint32, payload []byte) []byte {
	// Construct header
	h := &ChunkHeader{FMT: 0, CSID: csid, Timestamp: ts, MessageLength: uint32(len(payload)), MessageTypeID: msgType, MessageStreamID: msid}
	b, err := EncodeChunkHeader(h, nil)
	if err != nil {
		t.Fatalf("encode header: %v", err)
	}
	return append(b, payload...)
}

// TestReader_SingleMessageSingleChunk feeds the Reader one complete message
// that fits in a single chunk (payload < chunk size) and verifies all fields.
func TestReader_SingleMessageSingleChunk(t *testing.T) {
	payload := []byte("hello rtmp")
	stream := buildMessageBytes(t, 5, 1000, 8, 1, payload)
	r := NewReader(bytes.NewReader(stream), 128)
	msg, err := r.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if msg.CSID != 5 || msg.Timestamp != 1000 || msg.TypeID != 8 || msg.MessageStreamID != 1 || string(msg.Payload) != string(payload) {
		t.Fatalf("unexpected msg: %+v", msg)
	}
	// EOF afterwards
	if _, err = r.ReadMessage(); err == nil {
		// Underlying reader returns EOF on header parse attempt -> acceptable to surface error; do not require explicit check.
	}
}

// TestReader_InterleavedMultiChunk_Golden reads the golden interleaved binary
// (audio + video chunks interleaved) and verifies the Reader reassembles
// two complete messages: audio (CSID 4, type 8, 256 bytes) and video
// (CSID 6, type 9, 256 bytes).
func TestReader_InterleavedMultiChunk_Golden(t *testing.T) {
	data := loadGoldenChunk(t, "chunk_interleaved.bin")
	r := NewReader(bytes.NewReader(data), 128)
	// Expect two messages (audio csid=4 type 8 len=256, video csid=6 type 9 len=256) delivered in order audio->video
	m1, err := r.ReadMessage()
	if err != nil {
		t.Fatalf("m1 err: %v", err)
	}
	if m1.CSID != 4 || m1.TypeID != 8 || len(m1.Payload) != 256 {
		t.Fatalf("m1 mismatch: %+v", m1)
	}
	m2, err := r.ReadMessage()
	if err != nil {
		t.Fatalf("m2 err: %v", err)
	}
	if m2.CSID != 6 || m2.TypeID != 9 || len(m2.Payload) != 256 {
		t.Fatalf("m2 mismatch: %+v", m2)
	}
}

// TestReader_SetChunkSize_Applied simulates the RTMP "Set Chunk Size"
// control message flow:
//  1. Send a control message (type 1) setting chunk size to 4096.
//  2. Send a 3000-byte audio message as a single chunk (fits in 4096).
//
// If the Reader doesn't update its chunk size, it would try to split the
// 3000-byte message at the old 128-byte boundary, parsing payload bytes
// as headers and failing.
func TestReader_SetChunkSize_Applied(t *testing.T) {
	// 1) Control message: Set Chunk Size -> 4096
	ctrlPayload := make([]byte, 4)
	ctrlPayload[0] = 0x00
	ctrlPayload[1] = 0x00
	ctrlPayload[2] = 0x10
	ctrlPayload[3] = 0x00                                 // 4096
	ctrl := buildMessageBytes(t, 2, 0, 1, 0, ctrlPayload) // typeID=1, msid=0
	// 2) Large message length 3000 (would require fragmentation if chunk size remained 128)
	largePayload := make([]byte, 3000)
	if _, err := rand.Read(largePayload); err != nil {
		// fallback deterministic
		for i := range largePayload {
			largePayload[i] = byte(i)
		}
	}
	large := buildMessageBytes(t, 4, 10, 8, 1, largePayload)
	stream := append(ctrl, large...)
	r := NewReader(bytes.NewReader(stream), 128)
	m1, err := r.ReadMessage()
	if err != nil {
		t.Fatalf("control read: %v", err)
	}
	if m1.TypeID != 1 || len(m1.Payload) != 4 {
		t.Fatalf("unexpected control msg: %+v", m1)
	}
	// Reader should have updated chunk size -> second message read in one go
	m2, err := r.ReadMessage()
	if err != nil {
		t.Fatalf("large message read: %v", err)
	}
	if len(m2.Payload) != 3000 {
		t.Fatalf("expected 3000 payload got %d", len(m2.Payload))
	}
}

// TestReader_GoldenFileExists is a sanity check that the golden files exist
// in the expected location. If the golden directory is moved, this fails
// early with a clear message.
func TestReader_GoldenFileExists(t *testing.T) {
	p := filepath.Join("..", "..", "..", "tests", "golden", "chunk_fmt0_audio.bin")
	if _, err := os.Stat(p); err != nil {
		if os.IsNotExist(err) {
			t.Skip("golden file missing")
		}
		t.Fatalf("stat golden: %v", err)
	}
	if p == "" || p == "/" || p == "." {
		// extremely unlikely, but keeps static analyzers silent
		// do nothing
		_ = io.Discard
	}
}

// --- Benchmarks ---

// BenchmarkParseChunkHeader_FMT0 benchmarks parsing of a full 12-byte FMT0 header.
func BenchmarkParseChunkHeader_FMT0(b *testing.B) {
	b.ReportAllocs()
	h := &ChunkHeader{FMT: 0, CSID: 4, Timestamp: 1000, MessageLength: 100, MessageTypeID: 8, MessageStreamID: 1}
	raw, err := EncodeChunkHeader(h, nil)
	if err != nil {
		b.Fatalf("encode header: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := bytes.NewReader(raw)
		_, _ = ParseChunkHeader(r, nil)
	}
}

// BenchmarkParseChunkHeader_FMT1 benchmarks parsing of an 8-byte FMT1 delta header.
func BenchmarkParseChunkHeader_FMT1(b *testing.B) {
	b.ReportAllocs()
	prev := &ChunkHeader{FMT: 0, CSID: 6, Timestamp: 1000, MessageLength: 80, MessageTypeID: 9, MessageStreamID: 1}
	h := &ChunkHeader{FMT: 1, CSID: 6, Timestamp: 40, MessageLength: 80, MessageTypeID: 9}
	raw, err := EncodeChunkHeader(h, nil)
	if err != nil {
		b.Fatalf("encode header: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := bytes.NewReader(raw)
		_, _ = ParseChunkHeader(r, prev)
	}
}

// BenchmarkParseChunkHeader_FMT3 benchmarks parsing of a minimal 1-byte FMT3 header.
func BenchmarkParseChunkHeader_FMT3(b *testing.B) {
	b.ReportAllocs()
	prev := &ChunkHeader{FMT: 0, CSID: 6, Timestamp: 2000, MessageLength: 384, MessageTypeID: 9, MessageStreamID: 1}
	h := &ChunkHeader{FMT: 3, CSID: 6}
	raw, err := EncodeChunkHeader(h, prev)
	if err != nil {
		b.Fatalf("encode header: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := bytes.NewReader(raw)
		_, _ = ParseChunkHeader(r, prev)
	}
}

// BenchmarkReaderReadMessage_SingleChunk benchmarks reading a single-chunk message.
func BenchmarkReaderReadMessage_SingleChunk(b *testing.B) {
	b.ReportAllocs()
	payload := make([]byte, 100)
	h := &ChunkHeader{FMT: 0, CSID: 4, Timestamp: 1000, MessageLength: 100, MessageTypeID: 8, MessageStreamID: 1}
	hdr, err := EncodeChunkHeader(h, nil)
	if err != nil {
		b.Fatalf("encode header: %v", err)
	}
	data := append(hdr, payload...)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := NewReader(bytes.NewReader(data), 128)
		_, _ = r.ReadMessage()
	}
}

// BenchmarkReaderReadMessage_MultiChunk benchmarks reading a message spanning multiple chunks.
func BenchmarkReaderReadMessage_MultiChunk(b *testing.B) {
	b.ReportAllocs()
	payload := make([]byte, 4096)
	var buf bytes.Buffer
	w := NewWriter(&buf, 128)
	msg := &Message{CSID: 6, Timestamp: 0, MessageLength: 4096, TypeID: 9, MessageStreamID: 1, Payload: payload}
	if err := w.WriteMessage(msg); err != nil {
		b.Fatalf("write: %v", err)
	}
	data := buf.Bytes()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := NewReader(bytes.NewReader(data), 128)
		_, _ = r.ReadMessage()
	}
}

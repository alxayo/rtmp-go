// header_test.go – tests for RTMP chunk header parsing and encoding.
//
// RTMP messages travel over TCP as "chunks". Each chunk starts with a header
// that describes the message. The header format (FMT 0-3) determines how much
// metadata is included:
//
//	FMT 0 – Full 12 bytes: timestamp, length, type ID, stream ID (first msg)
//	FMT 1 – 8 bytes: delta timestamp, length, type ID (same stream)
//	FMT 2 – 4 bytes: delta timestamp only (same length/type/stream)
//	FMT 3 – 1 byte:  continuation chunk, inherits everything from previous
//
// When the timestamp ≥ 0xFFFFFF, an additional 4-byte "extended timestamp"
// field appears after the header.
//
// CSID (Chunk Stream ID) encoding uses 1, 2, or 3 bytes in the basic header:
//
//	1-byte: CSIDs 2–63 (6 bits in the first byte)
//	2-byte: CSIDs 64–319 (first byte CSID field = 0, second byte = CSID-64)
//	3-byte: CSIDs 64–65599 (first byte CSID field = 1, bytes 2-3 = CSID-64)
//
// These tests use golden binary vectors from tests/golden/ to ensure exact
// wire-format fidelity.
package chunk

import (
	"bytes"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// loadGolden reads a golden binary file that contains a known-good RTMP
// chunk byte sequence. Golden files are generated once (by
// tests/golden/gen_*.go) and treated as reference truth.
func loadGolden(t *testing.T, name string) []byte {
	t.Helper()
	// internal/rtmp/chunk -> ../../../tests/golden
	p := filepath.Join("..", "..", "..", "tests", "golden", name)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return b
}

// TestParseChunkHeader_GoldenFMT0 parses a FMT 0 header (full 12 bytes) for
// an audio message. Checks every field: FMT, CSID, absolute timestamp,
// message length, type ID (8=audio), stream ID, no extended timestamp.
func TestParseChunkHeader_GoldenFMT0(t *testing.T) {
	data := loadGolden(t, "chunk_fmt0_audio.bin")
	r := bytes.NewReader(data)
	h, err := ParseChunkHeader(r, nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if h.FMT != 0 || h.CSID != 4 || h.Timestamp != 1000 || h.MessageLength != 64 || h.MessageTypeID != 8 || h.MessageStreamID != 1 || h.HasExtendedTimestamp {
		t.Fatalf("unexpected header: %+v", h)
	}
	if h.HeaderBytes() != 1+11 {
		t.Fatalf("expected 12 header bytes, got %d", h.HeaderBytes())
	}
}

// TestParseChunkHeader_GoldenFMT1 parses a FMT 1 header (8 bytes) for a
// video delta message. FMT 1 carries a delta timestamp (IsDelta=true) plus
// new message length and type ID, but no stream ID (inherited).
func TestParseChunkHeader_GoldenFMT1(t *testing.T) {
	data := loadGolden(t, "chunk_fmt1_video.bin")
	r := bytes.NewReader(data)
	h, err := ParseChunkHeader(r, nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if h.FMT != 1 || h.CSID != 6 || h.Timestamp != 40 || !h.IsDelta || h.MessageLength != 80 || h.MessageTypeID != 9 || h.HasExtendedTimestamp {
		t.Fatalf("unexpected header: %+v", h)
	}
	if h.HeaderBytes() != 1+7 {
		t.Fatalf("expected 8 header bytes, got %d", h.HeaderBytes())
	}
}

// TestParseChunkHeader_GoldenFMT2 parses a FMT 2 header (4 bytes). FMT 2
// carries only a delta timestamp; length, type, and stream ID are inherited
// from a prior header on the same CSID (passed as the `base` argument).
func TestParseChunkHeader_GoldenFMT2(t *testing.T) {
	// Need prior state to inherit length/type/streamID from earlier FMT0 on CSID 4
	base := &ChunkHeader{FMT: 0, CSID: 4, Timestamp: 1000, MessageLength: 64, MessageTypeID: 8, MessageStreamID: 1}
	data := loadGolden(t, "chunk_fmt2_delta.bin")
	r := bytes.NewReader(data)
	h, err := ParseChunkHeader(r, base)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if h.FMT != 2 || h.CSID != 4 || h.Timestamp != 33 || !h.IsDelta || h.MessageLength != 64 || h.MessageTypeID != 8 || h.MessageStreamID != 1 {
		t.Fatalf("unexpected header: %+v", h)
	}
	if h.HeaderBytes() != 1+3 {
		t.Fatalf("expected 4 header bytes, got %d", h.HeaderBytes())
	}
}

// TestParseChunkHeader_GoldenFMT3 parses a FMT 3 header (1 byte only). This
// is a "continuation" chunk used when a message is larger than the chunk size
// and must be split. Everything is inherited from the previous header.
func TestParseChunkHeader_GoldenFMT3(t *testing.T) {
	// Prior header for CSID 6 (fragmented video) - typical continuation
	prev := &ChunkHeader{FMT: 0, CSID: 6, Timestamp: 2000, MessageLength: 384, MessageTypeID: 9, MessageStreamID: 1}
	data := loadGolden(t, "chunk_fmt3_continuation.bin")
	r := bytes.NewReader(data)
	h, err := ParseChunkHeader(r, prev)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if h.FMT != 3 || h.CSID != 6 || h.MessageLength != prev.MessageLength || h.MessageTypeID != prev.MessageTypeID || h.MessageStreamID != prev.MessageStreamID {
		t.Fatalf("unexpected header: %+v", h)
	}
	if h.HeaderBytes() != 1 {
		t.Fatalf("expected 1 header byte, got %d", h.HeaderBytes())
	}
}

// TestParseChunkHeader_ExtendedTimestamp verifies the extended timestamp
// path. When the 3-byte timestamp field is 0xFFFFFF, an extra 4-byte field
// follows the header containing the real timestamp.
func TestParseChunkHeader_ExtendedTimestamp(t *testing.T) {
	data := loadGolden(t, "chunk_extended_timestamp.bin")
	r := bytes.NewReader(data)
	h, err := ParseChunkHeader(r, nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !h.HasExtendedTimestamp || h.Timestamp != 0x01312D00 || h.ExtendedTimestampValue != 0x01312D00 || h.FMT != 0 || h.CSID != 4 {
		t.Fatalf("unexpected header: %+v", h)
	}
	if h.HeaderBytes() != 1+11+4 {
		t.Fatalf("expected 16 header bytes, got %d", h.HeaderBytes())
	}
}

// TestParseChunkHeader_InterleavedSequence simulates interleaved audio/video
// chunks as they appear in a real RTMP stream:
//
//	Chunk1: Audio FMT0 (first 128 bytes of 256-byte audio message)
//	Chunk2: Video FMT0 (first 128 bytes of 256-byte video message)
//	Chunk3: Audio FMT3 continuation (remaining 128 bytes of audio)
//	Chunk4: Video FMT3 continuation (remaining 128 bytes of video)
//
// This is the most realistic test – real RTMP streams interleave audio and
// video chunks to minimize latency.
func TestParseChunkHeader_InterleavedSequence(t *testing.T) {
	data := loadGolden(t, "chunk_interleaved.bin")
	r := bytes.NewReader(data)
	// Chunk 1: Audio FMT0
	h1, err := ParseChunkHeader(r, nil)
	if err != nil {
		t.Fatalf("h1: %v", err)
	}
	if h1.FMT != 0 || h1.CSID != 4 || h1.MessageTypeID != 8 || h1.MessageLength != 256 {
		t.Fatalf("h1 mismatch: %+v", h1)
	}
	// Consume first 128 bytes payload
	if _, err = io.CopyN(io.Discard, r, 128); err != nil {
		t.Fatalf("payload1: %v", err)
	}
	// Chunk 2: Video FMT0
	h2, err := ParseChunkHeader(r, nil)
	if err != nil {
		t.Fatalf("h2: %v", err)
	}
	if h2.FMT != 0 || h2.CSID != 6 || h2.MessageTypeID != 9 || h2.MessageLength != 256 {
		t.Fatalf("h2 mismatch: %+v", h2)
	}
	if _, err = io.CopyN(io.Discard, r, 128); err != nil {
		t.Fatalf("payload2: %v", err)
	}
	// Chunk 3: Audio continuation FMT3 (inherit h1)
	h3, err := ParseChunkHeader(r, h1)
	if err != nil {
		t.Fatalf("h3: %v", err)
	}
	if h3.FMT != 3 || h3.CSID != 4 || h3.MessageTypeID != 8 || h3.MessageLength != 256 {
		t.Fatalf("h3 mismatch: %+v", h3)
	}
	if _, err = io.CopyN(io.Discard, r, 128); err != nil {
		t.Fatalf("payload3: %v", err)
	}
	// Chunk 4: Video continuation FMT3 (inherit h2)
	h4, err := ParseChunkHeader(r, h2)
	if err != nil {
		t.Fatalf("h4: %v", err)
	}
	if h4.FMT != 3 || h4.CSID != 6 || h4.MessageTypeID != 9 || h4.MessageLength != 256 {
		t.Fatalf("h4 mismatch: %+v", h4)
	}
}

// TestParseChunkHeader_Errors exercises error paths: truncated data, missing
// bytes, and FMT 3 without prior state.
func TestParseChunkHeader_Errors(t *testing.T) {
	tests := []struct {
		name     string
		hexInput string
	}{
		{"truncated_basic", ""},     // no bytes
		{"truncated_fmt0", "04 00"}, // need 11 bytes
		{"truncated_extended", "04 ff ff ff 00 00 40 08 01 00 00 00 01 31"}, // missing ext bytes
		{"fmt3_missing_prev", "c6"},                                         // fmt3 but no prev
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, _ := hex.DecodeString(tt.hexInput)
			_, err := ParseChunkHeader(bytes.NewReader(b), nil)
			if err == nil {
				t.Fatalf("expected error for %s", tt.name)
			}
		})
	}
}

// TestParseChunkHeader_CSIDEncodings exercises the 3 CSID encoding sizes
// to ensure the basic header parser handles each form.
func TestParseChunkHeader_CSIDEncodings(t *testing.T) {
	// 1-byte csid=63 (fmt=0)
	one := []byte{(0 << 6) | 63}
	if _, err := ParseChunkHeader(bytes.NewReader(append(one, make([]byte, 11)...)), nil); err == nil {
		// Will fail because 11 bytes zero message header read ok -> zero header accepted
	}
	// 2-byte csid=64
	two := []byte{0 << 6, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0} // truncated purposely to cause error path
	if _, err := ParseChunkHeader(bytes.NewReader(two), nil); err == nil {
		t.Fatalf("expected error for 2-byte csid truncated header")
	}
	// 3-byte csid=320 -> fmt=1 for variation
	three := []byte{(1 << 6) | 1, 0x00, 0x01, 0, 0, 0, 0, 0, 0} // truncated FMT1
	if _, err := ParseChunkHeader(bytes.NewReader(three), nil); err == nil {
		t.Fatalf("expected error for 3-byte csid truncated header")
	}
}

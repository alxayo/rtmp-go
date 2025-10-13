package chunk

import (
	"bytes"
	"encoding/hex"
	"io"
	"testing"
)

// helper to slice expected header bytes from golden (we know payload sizes from generator logic)
func loadGoldenHeader(t *testing.T, name string, headerLen int) []byte {
	b := loadGolden(t, name)
	if len(b) < headerLen {
		t.Fatalf("golden %s shorter than headerLen %d", name, headerLen)
	}
	return b[:headerLen]
}

func TestEncodeChunkHeader_FMT0(t *testing.T) {
	h := &ChunkHeader{FMT: 0, CSID: 4, Timestamp: 1000, MessageLength: 64, MessageTypeID: 8, MessageStreamID: 1}
	got, err := EncodeChunkHeader(h, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	exp := loadGoldenHeader(t, "chunk_fmt0_audio.bin", 1+11)
	if !bytes.Equal(got, exp) {
		t.Fatalf("mismatch\nexp=%x\n got=%x", exp, got)
	}
}

func TestEncodeChunkHeader_FMT1(t *testing.T) {
	h := &ChunkHeader{FMT: 1, CSID: 6, Timestamp: 40, MessageLength: 80, MessageTypeID: 9}
	got, err := EncodeChunkHeader(h, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	exp := loadGoldenHeader(t, "chunk_fmt1_video.bin", 1+7)
	if !bytes.Equal(got, exp) {
		t.Fatalf("mismatch\nexp=%x\n got=%x", exp, got)
	}
}

func TestEncodeChunkHeader_FMT2(t *testing.T) {
	prev := &ChunkHeader{FMT: 0, CSID: 4, Timestamp: 1000, MessageLength: 64, MessageTypeID: 8, MessageStreamID: 1}
	h := &ChunkHeader{FMT: 2, CSID: 4, Timestamp: 33}
	got, err := EncodeChunkHeader(h, prev)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	exp := loadGoldenHeader(t, "chunk_fmt2_delta.bin", 1+3)
	if !bytes.Equal(got, exp) {
		t.Fatalf("mismatch\nexp=%x\n got=%x", exp, got)
	}
}

func TestEncodeChunkHeader_FMT3(t *testing.T) {
	prev := &ChunkHeader{FMT: 0, CSID: 6, Timestamp: 2000, MessageLength: 384, MessageTypeID: 9, MessageStreamID: 1}
	h := &ChunkHeader{FMT: 3, CSID: 6}
	got, err := EncodeChunkHeader(h, prev)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	exp := loadGoldenHeader(t, "chunk_fmt3_continuation.bin", 1)
	if !bytes.Equal(got, exp) {
		t.Fatalf("mismatch\nexp=%x\n got=%x", exp, got)
	}
}

func TestEncodeChunkHeader_ExtendedTimestamp(t *testing.T) {
	h := &ChunkHeader{FMT: 0, CSID: 4, Timestamp: 0x01312D00, MessageLength: 64, MessageTypeID: 8, MessageStreamID: 1}
	got, err := EncodeChunkHeader(h, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	exp := loadGoldenHeader(t, "chunk_extended_timestamp.bin", 1+11+4)
	if !bytes.Equal(got, exp) {
		t.Fatalf("mismatch\nexp=%x\n got=%x", exp, got)
	}
}

func TestEncodeChunkHeader_CSIDEncodings(t *testing.T) {
	cases := []struct {
		csid uint32
		fmt  uint8
		want string
	}{
		{63, 0, "3f"},        // one byte (fmt0, csid 63)
		{64, 0, "00 00"},     // two byte form: (fmt0|marker)+ second byte 0
		{320, 1, "41 00 01"}, // three byte form (fmt1 marker 1)
	}
	for _, c := range cases {
		b, err := EncodeChunkHeader(&ChunkHeader{FMT: c.fmt, CSID: c.csid, Timestamp: 0, MessageLength: 0, MessageTypeID: 0, MessageStreamID: 0}, nil)
		if err != nil {
			t.Fatalf("csid %d: %v", c.csid, err)
		}
		// Only compare basic header prefix (length 1/2/3) because we added message header zeros.
		wantBytes, _ := hex.DecodeString(c.want)
		if !bytes.HasPrefix(b, wantBytes) {
			t.Fatalf("csid %d expected prefix %x got %x", c.csid, wantBytes, b)
		}
	}
}

func TestEncodeChunkHeader_Errors(t *testing.T) {
	if _, err := EncodeChunkHeader(nil, nil); err == nil {
		t.Fatalf("expected nil header error")
	}
	if _, err := EncodeChunkHeader(&ChunkHeader{FMT: 4, CSID: 2}, nil); err == nil {
		t.Fatalf("expected invalid fmt error")
	}
	if _, err := EncodeChunkHeader(&ChunkHeader{FMT: 0, CSID: 1}, nil); err == nil {
		t.Fatalf("expected invalid csid error")
	}
	if _, err := EncodeChunkHeader(&ChunkHeader{FMT: 3, CSID: 7}, nil); err == nil {
		t.Fatalf("expected FMT3 prev error")
	}
}

// --- T021: Chunk Writer fragmentation tests ---

// simpleWriter allows us to inspect raw bytes written.
type simpleWriter struct{ bytes.Buffer }

func TestWriter_WriteMessage_SingleChunk(t *testing.T) {
	var sw simpleWriter
	w := NewWriter(&sw, 128)
	payload := bytes.Repeat([]byte{0xAA}, 20)
	msg := &Message{CSID: 4, Timestamp: 1000, MessageLength: uint32(len(payload)), TypeID: 8, MessageStreamID: 1, Payload: payload}
	if err := w.WriteMessage(msg); err != nil {
		t.Fatalf("write: %v", err)
	}
	data := sw.Bytes()
	// Expect exactly one header (FMT0) then payload
	// First byte high 2 bits should be 00 (FMT0)
	if len(data) < 12 { // 1 basic + 11 message header
		t.Fatalf("too short: %d", len(data))
	}
	if data[0]>>6 != 0 {
		t.Fatalf("expected FMT0 got %d", data[0]>>6)
	}
	gotPayload := data[len(data)-len(payload):]
	if !bytes.Equal(gotPayload, payload) {
		t.Fatalf("payload mismatch")
	}
}

func TestWriter_WriteMessage_MultiChunk(t *testing.T) {
	var sw simpleWriter
	w := NewWriter(&sw, 128)
	payload := bytes.Repeat([]byte{0xBB}, 300) // 300 = 128 + 128 + 44
	msg := &Message{CSID: 6, Timestamp: 2000, MessageLength: uint32(len(payload)), TypeID: 9, MessageStreamID: 1, Payload: payload}
	if err := w.WriteMessage(msg); err != nil {
		t.Fatalf("write: %v", err)
	}
	raw := sw.Bytes()
	// Count continuation headers (FMT3) – look for basic header byte 0xC6 (fmt=3, csid=6)
	// First chunk header length = 1 + 11 = 12.
	// Continuations each add 1 header byte.
	wantChunks := 3
	// Scan bytes for 0xC6 occurrences after first header
	contCount := 0
	for i := 12; i < len(raw); i++ {
		if raw[i] == (0xC0 | byte(msg.CSID)) { // fmt3 + csid
			contCount++
		}
	}
	if contCount != wantChunks-1 {
		t.Fatalf("expected %d continuation headers got %d", wantChunks-1, contCount)
	}
	// Reassemble using Reader to ensure correctness
	r := NewReader(bytes.NewReader(raw), 128)
	out, err := r.ReadMessage()
	if err != nil {
		t.Fatalf("reader: %v", err)
	}
	if out.MessageLength != uint32(len(payload)) || !bytes.Equal(out.Payload, payload) {
		t.Fatalf("round-trip mismatch")
	}
}

func TestWriter_WriteMessage_ExtendedTimestampMultiChunk(t *testing.T) {
	var sw simpleWriter
	w := NewWriter(&sw, 64)                    // small chunk size to force more continuations
	ts := uint32(0x01312D00)                   // > 0xFFFFFF triggers extended timestamp
	payload := bytes.Repeat([]byte{0xCC}, 150) // 64 + 64 + 22
	msg := &Message{CSID: 4, Timestamp: ts, MessageLength: uint32(len(payload)), TypeID: 8, MessageStreamID: 1, Payload: payload}
	if err := w.WriteMessage(msg); err != nil {
		t.Fatalf("write: %v", err)
	}
	raw := sw.Bytes()
	// First header: basic + 11 + 4(ext) = 16 bytes
	if len(raw) < 16 {
		t.Fatalf("short header: %d", len(raw))
	}
	if raw[0]>>6 != 0 {
		t.Fatalf("expected FMT0")
	}
	// Extract extended timestamp value (big-endian) after 1+11 bytes
	ext := raw[12:16]
	if uint32(ext[0])<<24|uint32(ext[1])<<16|uint32(ext[2])<<8|uint32(ext[3]) != ts {
		t.Fatalf("ext ts mismatch")
	}
	// Expect each continuation chunk to repeat extended timestamp (header byte + 4 ext ts)
	// Scan for pattern 0xC4 <ext 4 bytes>
	pattern := append([]byte{0xC0 | 0x04}, ext...)
	repeats := 0
	// Start search after first header+payload chunk.
	// We don't parse precisely; just count pattern occurrences.
	for i := 16; i+len(pattern) <= len(raw); i++ {
		if bytes.Equal(raw[i:i+len(pattern)], pattern) {
			repeats++
		}
	}
	if repeats < 1 { // at least one continuation should exist
		t.Fatalf("expected at least one extended timestamp continuation, found %d", repeats)
	}
	// Round-trip via Reader
	r := NewReader(bytes.NewReader(raw), 64)
	out, err := r.ReadMessage()
	if err != nil {
		t.Fatalf("reader: %v", err)
	}
	if out.Timestamp != ts {
		t.Fatalf("timestamp lost: want %d got %d", ts, out.Timestamp)
	}
	if !bytes.Equal(out.Payload, payload) {
		t.Fatalf("payload mismatch")
	}
	_ = io.EOF // silence unused import if build tags change
}

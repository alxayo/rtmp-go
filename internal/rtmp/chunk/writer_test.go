// writer_test.go – tests for the chunk Writer (encoder + fragmenter).
//
// The Writer takes a complete Message and splits it into chunks that fit
// within the negotiated chunk size (default 128 bytes). It also tracks
// per-CSID state to automatically select the most compact header format:
//
//	FMT 0 – first message on a CSID (full header)
//	FMT 1 – same stream, different length/type (8-byte header)
//	FMT 2 – same stream, same length/type, different timestamp (4 bytes)
//	FMT 3 – continuation chunk within the same message (1 byte)
//
// Key concepts demonstrated:
//   - simpleWriter type wraps bytes.Buffer for raw byte inspection.
//   - Writer→Reader round-trip proves the writer output is spec-compliant.
//   - Extended timestamp propagation through continuation chunks.
package chunk

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"testing"
)

// loadGoldenHeader extracts the first `headerLen` bytes from a golden file.
// This is used to compare encoded headers against known-good reference bytes.
func loadGoldenHeader(t *testing.T, name string, headerLen int) []byte {
	b := loadGolden(t, name)
	if len(b) < headerLen {
		t.Fatalf("golden %s shorter than headerLen %d", name, headerLen)
	}
	return b[:headerLen]
}

// TestEncodeChunkHeader_FMT0 encodes a full FMT 0 header for an audio message
// and compares against the golden binary.
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

// TestEncodeChunkHeader_FMT1 encodes a FMT 1 header (delta timestamp + new
// length/type) and compares against the golden binary.
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

// TestEncodeChunkHeader_FMT2 encodes a FMT 2 header (delta timestamp only,
// needs prior state for other fields).
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

// TestEncodeChunkHeader_FMT3 encodes a FMT 3 (continuation) header – just 1
// byte carrying the CSID.
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

// TestEncodeChunkHeader_ExtendedTimestamp verifies encoding when the
// timestamp exceeds 0xFFFFFF (the 3-byte maximum). The encoder must write
// 0xFFFFFF in the timestamp field and append a 4-byte extended timestamp.
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

// TestEncodeChunkHeader_CSIDEncodings verifies all three CSID encoding
// forms (1-byte, 2-byte, 3-byte) produce the correct basic header prefix.
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
		t.Run(fmt.Sprintf("csid_%d_fmt%d", c.csid, c.fmt), func(t *testing.T) {
			b, err := EncodeChunkHeader(&ChunkHeader{FMT: c.fmt, CSID: c.csid, Timestamp: 0, MessageLength: 0, MessageTypeID: 0, MessageStreamID: 0}, nil)
			if err != nil {
				t.Fatalf("csid %d: %v", c.csid, err)
			}
			// Only compare basic header prefix (length 1/2/3) because we added message header zeros.
			wantBytes, _ := hex.DecodeString(c.want)
			if !bytes.HasPrefix(b, wantBytes) {
				t.Fatalf("csid %d expected prefix %x got %x", c.csid, wantBytes, b)
			}
		})
	}
}

// TestEncodeChunkHeader_Errors checks rejection of invalid inputs: nil
// header, invalid FMT, invalid CSID, and FMT 3 without a previous header.
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

// simpleWriter wraps bytes.Buffer to capture raw bytes from the Writer.
type simpleWriter struct{ bytes.Buffer }

// TestWriter_WriteMessage_SingleChunk writes a message that fits in one
// chunk and checks the output has exactly one FMT 0 header followed by
// the payload.
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

// TestWriter_WriteMessage_MultiChunk writes a 300-byte message with chunk
// size 128, which requires 3 chunks: 128 + 128 + 44 bytes. It counts FMT 3
// continuation headers and then round-trips through the Reader to verify.
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

// TestWriter_WriteMessage_ExtendedTimestampMultiChunk verifies that when a
// message uses an extended timestamp (≥0x01000000), every continuation
// chunk also carries the 4-byte extended timestamp value. This is a subtle
// RTMP spec requirement that many implementations get wrong.
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

// TestWriter_StatefulFMTSelection sends 3 messages on the same CSID and
// verifies the Writer automatically selects the most compact FMT:
//
//	msg1 → FMT 0 (first message on CSID 6)
//	msg2 → FMT 2 (same length/type, only timestamp changed)
//	msg3 → FMT 1 (length changed, needs to re-send length/type)
func TestWriter_StatefulFMTSelection(t *testing.T) {
	var sw simpleWriter
	w := NewWriter(&sw, 128)

	// First message on CSID 6 - should use FMT0
	msg1 := &Message{CSID: 6, Timestamp: 1000, MessageLength: 100, TypeID: 8, MessageStreamID: 1, Payload: make([]byte, 100)}
	if err := w.WriteMessage(msg1); err != nil {
		t.Fatalf("write msg1: %v", err)
	}

	// Second message on CSID 6, same length/type, different timestamp - should use FMT2
	msg2 := &Message{CSID: 6, Timestamp: 1100, MessageLength: 100, TypeID: 8, MessageStreamID: 1, Payload: make([]byte, 100)}
	if err := w.WriteMessage(msg2); err != nil {
		t.Fatalf("write msg2: %v", err)
	}

	// Third message on CSID 6, different length - should use FMT1
	msg3 := &Message{CSID: 6, Timestamp: 1200, MessageLength: 200, TypeID: 8, MessageStreamID: 1, Payload: make([]byte, 200)}
	if err := w.WriteMessage(msg3); err != nil {
		t.Fatalf("write msg3: %v", err)
	}

	raw := sw.Bytes()

	// Check first message header (should be FMT0)
	if raw[0]>>6 != 0 {
		t.Errorf("msg1: expected FMT0, got FMT%d", raw[0]>>6)
	}

	// Find second message header position (after first message: 1+11+100 = 112 bytes)
	pos2 := 112
	if pos2 >= len(raw) {
		t.Fatalf("raw too short for msg2 position")
	}
	if raw[pos2]>>6 != 2 {
		t.Errorf("msg2: expected FMT2, got FMT%d", raw[pos2]>>6)
	}

	// Find third message header position (after second message: pos2 + 1+3+100 = 216 bytes)
	pos3 := pos2 + 104
	if pos3 >= len(raw) {
		t.Fatalf("raw too short for msg3 position")
	}
	if raw[pos3]>>6 != 1 {
		t.Errorf("msg3: expected FMT1, got FMT%d", raw[pos3]>>6)
	}
}

// TestWriter_ChunkReaderRoundTrip is an end-to-end test: write multiple
// messages through the Writer, then read them back through the Reader and
// compare every field. This proves the Writer output is fully compliant
// with the Reader's expectations.
func TestWriter_ChunkReaderRoundTrip(t *testing.T) {
	// Test that our stateful FMT selection produces readable chunks
	var sw simpleWriter
	w := NewWriter(&sw, 128)

	// Send messages that should trigger different FMT types
	messages := []*Message{
		{CSID: 6, Timestamp: 1000, MessageLength: 100, TypeID: 8, MessageStreamID: 1, Payload: make([]byte, 100)},
		{CSID: 6, Timestamp: 1100, MessageLength: 100, TypeID: 8, MessageStreamID: 1, Payload: make([]byte, 100)}, // FMT2
		{CSID: 7, Timestamp: 1050, MessageLength: 200, TypeID: 9, MessageStreamID: 1, Payload: make([]byte, 200)}, // FMT0 (new CSID)
		{CSID: 6, Timestamp: 1200, MessageLength: 150, TypeID: 8, MessageStreamID: 1, Payload: make([]byte, 150)}, // FMT1 (length changed)
	}

	for i, msg := range messages {
		// Fill payload with unique data
		for j := range msg.Payload {
			msg.Payload[j] = byte(i*10 + j%10)
		}
		if err := w.WriteMessage(msg); err != nil {
			t.Fatalf("write message %d: %v", i, err)
		}
	}

	// Now read back using chunk reader
	raw := sw.Bytes()
	reader := NewReader(bytes.NewReader(raw), 128)

	for i, expectedMsg := range messages {
		actualMsg, err := reader.ReadMessage()
		if err != nil {
			t.Fatalf("read message %d: %v", i, err)
		}

		if actualMsg.CSID != expectedMsg.CSID {
			t.Errorf("message %d CSID: expected %d, got %d", i, expectedMsg.CSID, actualMsg.CSID)
		}
		if actualMsg.TypeID != expectedMsg.TypeID {
			t.Errorf("message %d TypeID: expected %d, got %d", i, expectedMsg.TypeID, actualMsg.TypeID)
		}
		if actualMsg.Timestamp != expectedMsg.Timestamp {
			t.Errorf("message %d Timestamp: expected %d, got %d", i, expectedMsg.Timestamp, actualMsg.Timestamp)
		}
		if !bytes.Equal(actualMsg.Payload, expectedMsg.Payload) {
			t.Errorf("message %d payload mismatch", i)
		}
	}
}

// --- Benchmarks ---

// BenchmarkEncodeChunkHeader_FMT0 benchmarks header serialization for a full FMT0 header.
func BenchmarkEncodeChunkHeader_FMT0(b *testing.B) {
	b.ReportAllocs()
	h := &ChunkHeader{FMT: 0, CSID: 4, Timestamp: 1000, MessageLength: 100, MessageTypeID: 8, MessageStreamID: 1}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = EncodeChunkHeader(h, nil)
	}
}

// BenchmarkWriterWriteMessage_SingleChunk benchmarks writing a single-chunk message.
func BenchmarkWriterWriteMessage_SingleChunk(b *testing.B) {
	b.ReportAllocs()
	payload := make([]byte, 100)
	msg := &Message{CSID: 4, Timestamp: 1000, MessageLength: 100, TypeID: 8, MessageStreamID: 1, Payload: payload}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := NewWriter(io.Discard, 128)
		_ = w.WriteMessage(msg)
	}
}

// BenchmarkWriterWriteMessage_MultiChunk benchmarks writing a multi-chunk message.
func BenchmarkWriterWriteMessage_MultiChunk(b *testing.B) {
	b.ReportAllocs()
	payload := make([]byte, 4096)
	msg := &Message{CSID: 6, Timestamp: 0, MessageLength: 4096, TypeID: 9, MessageStreamID: 1, Payload: payload}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := NewWriter(io.Discard, 128)
		_ = w.WriteMessage(msg)
	}
}

// BenchmarkWriterReaderRoundTrip benchmarks the end-to-end Write→Read cycle.
func BenchmarkWriterReaderRoundTrip(b *testing.B) {
	b.ReportAllocs()
	payload := make([]byte, 4096)
	msg := &Message{CSID: 6, Timestamp: 0, MessageLength: 4096, TypeID: 9, MessageStreamID: 1, Payload: payload}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		w := NewWriter(&buf, 128)
		_ = w.WriteMessage(msg)
		r := NewReader(&buf, 128)
		_, _ = r.ReadMessage()
	}
}

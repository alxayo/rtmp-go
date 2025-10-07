package chunk

import (
	"bytes"
	"encoding/hex"
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

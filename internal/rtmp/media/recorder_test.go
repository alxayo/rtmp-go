package media

import (
    "bytes"
    "encoding/binary"
    "io"
    "os"
    "path/filepath"
    "testing"

    "github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// limitedWriter simulates disk full by failing after N bytes.
type limitedWriter struct {
    limit int
    buf   bytes.Buffer
    closed bool
}

func (l *limitedWriter) Write(p []byte) (int, error) {
    if l.limit <= 0 { return 0, io.ErrShortWrite }
    if len(p) > l.limit { p = p[:l.limit] }
    n, _ := l.buf.Write(p)
    l.limit -= n
    if l.limit == 0 { return n, io.ErrShortWrite }
    return n, nil
}
func (l *limitedWriter) Close() error { l.closed = true; return nil }

func TestRecorder_Header(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "test.flv")
    r, err := NewRecorder(path, NullLogger())
    if err != nil { t.Fatalf("NewRecorder error: %v", err) }
    defer r.Close()

    data, err := os.ReadFile(path)
    if err != nil { t.Fatalf("read file: %v", err) }
    if len(data) < 13 { t.Fatalf("file too small: %d", len(data)) }
    // FLV signature
    if string(data[:3]) != "FLV" { t.Fatalf("bad signature: %q", data[:3]) }
    if data[3] != 0x01 { t.Fatalf("version expected 1 got %d", data[3]) }
    if data[4] != 0x05 { t.Fatalf("flags expected 0x05 got 0x%02X", data[4]) }
    // header length 9
    if off := binary.BigEndian.Uint32(data[5:9]); off != 9 { t.Fatalf("data offset expected 9 got %d", off) }
}

func writeMsg(ts uint32, typeID uint8, payload []byte) *chunk.Message {
    return &chunk.Message{Timestamp: ts, TypeID: typeID, Payload: payload, MessageLength: uint32(len(payload))}
}

func TestRecorder_WriteAudioVideo(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "av.flv")
    r, err := NewRecorder(path, NullLogger())
    if err != nil { t.Fatalf("NewRecorder: %v", err) }
    defer r.Close()

    audioPayload := []byte{0xAF, 0x00, 0x11, 0x22} // AAC seq header
    videoPayload := []byte{0x17, 0x00, 0x01}       // AVC seq header
    r.WriteMessage(writeMsg(1000, 8, audioPayload))
    r.WriteMessage(writeMsg(1025, 9, videoPayload))

    b, err := os.ReadFile(path)
    if err != nil { t.Fatalf("read file: %v", err) }
    // Expect header (13) + first tag (11+len+4) + second tag (11+len+4)
    expected := 13 + (11+len(audioPayload)+4) + (11+len(videoPayload)+4)
    if len(b) != expected { t.Fatalf("file size mismatch got %d want %d", len(b), expected) }

    // Parse first tag
    idx := 13
    if b[idx] != 0x08 { t.Fatalf("first tag type want 0x08 got 0x%02X", b[idx]) }
    dataSize := int(b[idx+1])<<16 | int(b[idx+2])<<8 | int(b[idx+3])
    if dataSize != len(audioPayload) { t.Fatalf("audio data size mismatch %d", dataSize) }
    ts := uint32(b[idx+4])<<16 | uint32(b[idx+5])<<8 | uint32(b[idx+6]) | uint32(b[idx+7])<<24
    if ts != 1000 { t.Fatalf("audio timestamp want 1000 got %d", ts) }

    // Skip audio tag
    idx += 11 + len(audioPayload) + 4
    if b[idx] != 0x09 { t.Fatalf("second tag type want 0x09 got %02X", b[idx]) }
}

func TestRecorder_DiskFullSimulation(t *testing.T) {
    lw := &limitedWriter{limit: 8} // smaller than header (13) so header write fails
    r := newRecorderWithWriter(lw, NullLogger())
    if !r.Disabled() { t.Fatalf("recorder should be disabled after header failure") }
    // Attempt to write message; should no‑op and not panic
    r.WriteMessage(writeMsg(0, 8, []byte{0xAF, 0x00}))
}

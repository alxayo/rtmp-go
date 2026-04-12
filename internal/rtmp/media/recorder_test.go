// recorder_test.go – tests for the FLV file recorder.
//
// The Recorder writes incoming RTMP audio/video messages to an FLV file.
// FLV file format:
//   - 13-byte header: "FLV" + version(1) + flags(0x05) + offset(9) + tag0size(0)
//   - Tags: 11-byte header + payload + 4-byte previous-tag-size
//
// Tests verify:
//   - Header correctness (signature, version, flags, offset).
//   - Audio/video tag writing (tag type, data size, timestamps).
//   - Disk-full simulation using a limitedWriter that fails after N bytes.
//
// Key Go concepts:
//   - t.TempDir(): creates a temp directory automatically cleaned up.
//   - Custom io.Writer (limitedWriter) for simulating I/O failures.
package media

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/alxayo/go-rtmp/internal/rtmp/amf"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// limitedWriter simulates disk full by failing after limit bytes written.
// Once the limit is reached, all subsequent writes return io.ErrShortWrite.
type limitedWriter struct {
	limit  int
	buf    bytes.Buffer
	closed bool
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.limit <= 0 {
		return 0, io.ErrShortWrite
	}
	if len(p) > l.limit {
		p = p[:l.limit]
	}
	n, _ := l.buf.Write(p)
	l.limit -= n
	if l.limit == 0 {
		return n, io.ErrShortWrite
	}
	return n, nil
}
func (l *limitedWriter) Close() error { l.closed = true; return nil }

// TestRecorder_Header creates a new FLV recorder and verifies the
// 13-byte FLV header: "FLV" signature, version 1, flags 0x05
// (audio+video), and data offset = 9.
func TestRecorder_Header(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.flv")
	r, err := NewRecorder(path, "H264", NullLogger())
	if err != nil {
		t.Fatalf("NewRecorder error: %v", err)
	}
	defer r.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if len(data) < 13 {
		t.Fatalf("file too small: %d", len(data))
	}
	// FLV signature
	if string(data[:3]) != "FLV" {
		t.Fatalf("bad signature: %q", data[:3])
	}
	if data[3] != 0x01 {
		t.Fatalf("version expected 1 got %d", data[3])
	}
	if data[4] != 0x05 {
		t.Fatalf("flags expected 0x05 got 0x%02X", data[4])
	}
	// header length 9
	if off := binary.BigEndian.Uint32(data[5:9]); off != 9 {
		t.Fatalf("data offset expected 9 got %d", off)
	}
}

// writeMsg is a helper that constructs a *chunk.Message with the given
// timestamp, typeID, and payload – avoids boilerplate in each test.
func writeMsg(ts uint32, typeID uint8, payload []byte) *chunk.Message {
	return &chunk.Message{Timestamp: ts, TypeID: typeID, Payload: payload, MessageLength: uint32(len(payload))}
}

// TestRecorder_WriteAudioVideo writes one audio and one video tag, then
// reads the file back and validates: first tag is onMetaData (TypeID 18),
// then audio tag (0x08), then video tag (0x09) with correct timestamps.
func TestRecorder_WriteAudioVideo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "av.flv")
	r, err := NewRecorder(path, "H264", NullLogger())
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	defer r.Close()

	audioPayload := []byte{0xAF, 0x00, 0x11, 0x22} // AAC seq header
	videoPayload := []byte{0x17, 0x00, 0x01}       // AVC seq header
	r.WriteMessage(writeMsg(1000, 8, audioPayload))
	r.WriteMessage(writeMsg(1025, 9, videoPayload))

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if len(b) < 13 {
		t.Fatalf("file too small: %d", len(b))
	}

	// First tag after header should be onMetaData (TypeID 18)
	idx := 13
	if b[idx] != 18 {
		t.Fatalf("first tag type want 18 (script data) got %d", b[idx])
	}
	metaDataSize := int(b[idx+1])<<16 | int(b[idx+2])<<8 | int(b[idx+3])
	// Skip the onMetaData tag (11 header + dataSize + 4 prevTagSize)
	idx += 11 + metaDataSize + 4

	// Second tag: audio (0x08)
	if idx >= len(b) {
		t.Fatalf("file too small for audio tag at offset %d", idx)
	}
	if b[idx] != 0x08 {
		t.Fatalf("second tag type want 0x08 got 0x%02X", b[idx])
	}
	dataSize := int(b[idx+1])<<16 | int(b[idx+2])<<8 | int(b[idx+3])
	if dataSize != len(audioPayload) {
		t.Fatalf("audio data size mismatch %d", dataSize)
	}
	ts := uint32(b[idx+4])<<16 | uint32(b[idx+5])<<8 | uint32(b[idx+6]) | uint32(b[idx+7])<<24
	if ts != 1000 {
		t.Fatalf("audio timestamp want 1000 got %d", ts)
	}

	// Third tag: video (0x09)
	idx += 11 + len(audioPayload) + 4
	if idx >= len(b) {
		t.Fatalf("file too small for video tag at offset %d", idx)
	}
	if b[idx] != 0x09 {
		t.Fatalf("third tag type want 0x09 got 0x%02X", b[idx])
	}
}

// TestRecorder_DiskFullSimulation uses a limitedWriter that allows only
// 8 bytes (less than the 13-byte FLV header). The recorder must detect
// the write failure and mark itself Disabled – subsequent writes are no-ops.
func TestRecorder_DiskFullSimulation(t *testing.T) {
	lw := &limitedWriter{limit: 8} // smaller than header (13) so header write fails
	r := newFLVRecorderWithWriter(lw, NullLogger())
	if !r.Disabled() {
		t.Fatalf("recorder should be disabled after header failure")
	}
	// Attempt to write message; should no‑op and not panic
	r.WriteMessage(writeMsg(0, 8, []byte{0xAF, 0x00}))
}

// TestRecorder_OnMetaDataContent verifies the onMetaData tag contains the
// correct AMF0 payload with video/audio properties.
func TestRecorder_OnMetaDataContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meta.flv")

	meta := FLVMetadata{
		Width:           1920,
		Height:          1080,
		VideoCodecID:    7,
		AudioCodecID:    10,
		AudioSampleRate: 44100,
		AudioChannels:   2,
		Stereo:          true,
	}

	rec, err := NewFLVRecorder(path, NullLogger(), meta)
	if err != nil {
		t.Fatalf("NewFLVRecorder: %v", err)
	}
	rec.Close()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	// Parse the onMetaData tag at offset 13 (after FLV header)
	idx := 13
	if b[idx] != 18 {
		t.Fatalf("tag type want 18 got %d", b[idx])
	}
	dataSize := int(b[idx+1])<<16 | int(b[idx+2])<<8 | int(b[idx+3])
	tagPayload := b[idx+11 : idx+11+dataSize]

	// Decode AMF0 payload
	values, err := amf.DecodeAll(tagPayload)
	if err != nil {
		t.Fatalf("decode AMF: %v", err)
	}
	if len(values) < 2 {
		t.Fatalf("expected at least 2 AMF values, got %d", len(values))
	}

	// First value: string "onMetaData"
	name, ok := values[0].(string)
	if !ok || name != "onMetaData" {
		t.Fatalf("expected 'onMetaData' string, got %v", values[0])
	}

	// Second value: ECMA Array with properties
	arr, ok := values[1].(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{}, got %T", values[1])
	}

	// Verify key properties
	if w, ok := arr["width"].(float64); !ok || w != 1920 {
		t.Errorf("width: got %v want 1920", arr["width"])
	}
	if h, ok := arr["height"].(float64); !ok || h != 1080 {
		t.Errorf("height: got %v want 1080", arr["height"])
	}
	if v, ok := arr["videocodecid"].(float64); !ok || v != 7 {
		t.Errorf("videocodecid: got %v want 7", arr["videocodecid"])
	}
	if a, ok := arr["audiocodecid"].(float64); !ok || a != 10 {
		t.Errorf("audiocodecid: got %v want 10", arr["audiocodecid"])
	}
	if sr, ok := arr["audiosamplerate"].(float64); !ok || sr != 44100 {
		t.Errorf("audiosamplerate: got %v want 44100", arr["audiosamplerate"])
	}
	if s, ok := arr["stereo"].(bool); !ok || !s {
		t.Errorf("stereo: got %v want true", arr["stereo"])
	}
}

// TestRecorder_DurationPatching verifies that Close() patches the duration
// and filesize fields in the onMetaData tag.
func TestRecorder_DurationPatching(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "duration.flv")

	rec, err := NewFLVRecorder(path, NullLogger(), FLVMetadata{})
	if err != nil {
		t.Fatalf("NewFLVRecorder: %v", err)
	}

	// Write media tags spanning 5 seconds
	rec.WriteMessage(writeMsg(0, 9, []byte{0x17, 0x00, 0x01}))    // video at 0ms
	rec.WriteMessage(writeMsg(1000, 8, []byte{0xAF, 0x00, 0x11})) // audio at 1000ms
	rec.WriteMessage(writeMsg(5000, 9, []byte{0x17, 0x01, 0x02})) // video at 5000ms

	// Close to trigger duration patching
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	// Parse the onMetaData ECMA array
	idx := 13 // skip FLV header
	dataSize := int(b[idx+1])<<16 | int(b[idx+2])<<8 | int(b[idx+3])
	tagPayload := b[idx+11 : idx+11+dataSize]

	values, err := amf.DecodeAll(tagPayload)
	if err != nil {
		t.Fatalf("decode AMF: %v", err)
	}
	arr, ok := values[1].(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{}, got %T", values[1])
	}

	// Duration should be 5.0 seconds (5000ms - 0ms)
	dur, ok := arr["duration"].(float64)
	if !ok {
		t.Fatalf("duration not a float64: %T", arr["duration"])
	}
	if math.Abs(dur-5.0) > 0.01 {
		t.Errorf("duration: got %.3f want 5.000", dur)
	}

	// Filesize should be non-zero
	fs, ok := arr["filesize"].(float64)
	if !ok || fs == 0 {
		t.Errorf("filesize: got %v, want >0", arr["filesize"])
	}
	if int(fs) != len(b) {
		t.Errorf("filesize: got %.0f want %d", fs, len(b))
	}
}

// TestRecorder_ZeroMetadata verifies that when no metadata is provided,
// onMetaData is still written with zero/default values and recording works.
func TestRecorder_ZeroMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zero.flv")

	rec, err := NewFLVRecorder(path, NullLogger(), FLVMetadata{})
	if err != nil {
		t.Fatalf("NewFLVRecorder: %v", err)
	}

	// Write one media tag to confirm recording still works
	rec.WriteMessage(writeMsg(100, 9, []byte{0x17, 0x00, 0x01}))
	rec.Close()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	// Verify onMetaData tag is present
	idx := 13
	if b[idx] != 18 {
		t.Fatalf("first tag should be script data (18), got %d", b[idx])
	}

	// Verify media tag follows
	dataSize := int(b[idx+1])<<16 | int(b[idx+2])<<8 | int(b[idx+3])
	idx += 11 + dataSize + 4
	if idx >= len(b) {
		t.Fatalf("file too small for media tag")
	}
	if b[idx] != 0x09 {
		t.Fatalf("second tag want 0x09 (video) got 0x%02X", b[idx])
	}
}

// TestRecorder_CloseWithNoMedia verifies that closing a recorder with no media
// messages written produces a valid FLV file with duration 0.
func TestRecorder_CloseWithNoMedia(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.flv")

	rec, err := NewFLVRecorder(path, NullLogger(), FLVMetadata{Width: 1920, Height: 1080})
	if err != nil {
		t.Fatalf("NewFLVRecorder: %v", err)
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	// Should have FLV header + onMetaData tag, no media tags
	if len(b) < 13 {
		t.Fatalf("file too small: %d", len(b))
	}
	// Duration should be 0 (patched)
	idx := 13
	dataSize := int(b[idx+1])<<16 | int(b[idx+2])<<8 | int(b[idx+3])
	tagPayload := b[idx+11 : idx+11+dataSize]
	values, err := amf.DecodeAll(tagPayload)
	if err != nil {
		t.Fatalf("decode AMF: %v", err)
	}
	arr := values[1].(map[string]interface{})
	dur := arr["duration"].(float64)
	if dur != 0.0 {
		t.Errorf("duration: got %f want 0.0", dur)
	}
}

// TestRecorder_TimestampOutOfOrder verifies that out-of-order timestamps
// produce correct duration (based on max timestamp, not last written).
func TestRecorder_TimestampOutOfOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ooo.flv")

	rec, err := NewFLVRecorder(path, NullLogger(), FLVMetadata{})
	if err != nil {
		t.Fatalf("NewFLVRecorder: %v", err)
	}

	// Write timestamps out of order: 0, 3000, 1000, 2000
	rec.WriteMessage(writeMsg(0, 9, []byte{0x17, 0x00, 0x01}))
	rec.WriteMessage(writeMsg(3000, 9, []byte{0x17, 0x01, 0x02}))
	rec.WriteMessage(writeMsg(1000, 8, []byte{0xAF, 0x00, 0x11}))
	rec.WriteMessage(writeMsg(2000, 9, []byte{0x17, 0x01, 0x03}))
	rec.Close()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	idx := 13
	dataSize := int(b[idx+1])<<16 | int(b[idx+2])<<8 | int(b[idx+3])
	tagPayload := b[idx+11 : idx+11+dataSize]
	values, err := amf.DecodeAll(tagPayload)
	if err != nil {
		t.Fatalf("decode AMF: %v", err)
	}
	arr := values[1].(map[string]interface{})
	dur := arr["duration"].(float64)
	// Max timestamp is 3000, first is 0 → duration should be 3.0s
	if math.Abs(dur-3.0) > 0.01 {
		t.Errorf("duration: got %.3f want 3.000", dur)
	}
}

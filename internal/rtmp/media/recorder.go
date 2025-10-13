package media

// FLV Recorder (Task T045)
// ------------------------
// Minimal FLV file writer used to optionally persist a published stream to disk.
// Scope is intentionally small:
//   * Writes fixed FLV header (both audio+video flags set) once
//   * Writes audio (type 0x08) and video (type 0x09) tags from RTMP chunk.Message
//   * Tag format: 11 byte tag header + data + 4 byte PreviousTagSize
//   * Graceful degradation: on any write error the recorder is disabled (future
//     live streaming continues unaffected)
// No metadata / script tags are written; this is sufficient for basic playback
// in ffplay/ffmpeg for validation tests.

import (
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"

	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// Recorder persists RTMP audio/video messages into a single FLV file.
// It is safe for single‑goroutine use (the media relay loop). A mutex is
// included only to guard against accidental concurrent calls in future
// extensions.
type Recorder struct {
	mu           sync.Mutex
	w            io.WriteCloser
	logger       *slog.Logger
	wroteHeader  bool
	bytesWritten uint64
}

// NewRecorder creates a recorder writing to the supplied file path. If file
// creation fails it returns a nil *Recorder and the error.
func NewRecorder(path string, logger *slog.Logger) (*Recorder, error) {
	if logger == nil {
		logger = slog.Default()
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("recorder.create: %w", err)
	}
	r := &Recorder{w: f, logger: logger}
	if err := r.writeHeader(); err != nil {
		// writeHeader already closed on failure
		return nil, err
	}
	return r, nil
}

// newRecorderWithWriter allows tests to inject a failing writer (disk full simulation).
func newRecorderWithWriter(w io.WriteCloser, logger *slog.Logger) *Recorder {
	if logger == nil {
		logger = slog.Default()
	}
	r := &Recorder{w: w, logger: logger}
	_ = r.writeHeader() // Ignore error in helper; tests can assert state.
	return r
}

// Disabled returns true if the recorder encountered a fatal write error.
func (r *Recorder) Disabled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.w == nil
}

// writeHeader writes the 13‑byte FLV header: 9 bytes header + 4 bytes PreviousTagSize0
// Structure:
//
//	Signature: 'F','L','V'
//	Version:   0x01
//	Flags:     0x05 (audio + video present)
//	DataOffset: 0x00000009 (header length) big‑endian
//	PreviousTagSize0: 0x00000000
func (r *Recorder) writeHeader() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.w == nil || r.wroteHeader {
		return nil
	}
	header := []byte{'F', 'L', 'V', 0x01, 0x05, 0x00, 0x00, 0x00, 0x09, 0x00, 0x00, 0x00, 0x00}
	if _, err := r.w.Write(header); err != nil {
		r.logger.Error("recorder write header failed", "err", err)
		r.closeLocked()
		return fmt.Errorf("recorder.header: %w", err)
	}
	r.wroteHeader = true
	r.bytesWritten += uint64(len(header))
	return nil
}

// WriteMessage persists an RTMP media message (audio=8, video=9). Other message
// types are ignored silently. Safe to call after a failure; it no‑ops when disabled.
func (r *Recorder) WriteMessage(msg *chunk.Message) {
	if msg == nil || (msg.TypeID != 8 && msg.TypeID != 9) {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.w == nil { // disabled
		return
	}
	if !r.wroteHeader {
		if err := r.writeHeader(); err != nil {
			return
		}
	}
	if err := r.writeTagLocked(msg.TypeID, msg.Timestamp, msg.Payload); err != nil {
		r.logger.Error("recorder tag write failed", "err", err)
		r.closeLocked()
	}
}

// writeTagLocked writes a single FLV tag and its PreviousTagSize.
// Tag header (11 bytes):
//
//	0:  TagType
//	1-3 DataSize (big‑endian 24‑bit)
//	4-6 Timestamp Lower 24 bits
//	7:  Timestamp Extended (upper 8 bits)
//	8-10 StreamID (always 0)
func (r *Recorder) writeTagLocked(tagType uint8, timestamp uint32, payload []byte) error {
	dataSize := len(payload)
	if dataSize > 0xFFFFFF { // Out of FLV 24‑bit range (unlikely here)
		return fmt.Errorf("recorder.tag: payload too large: %d", dataSize)
	}
	var hdr [11]byte
	hdr[0] = tagType
	hdr[1] = byte(dataSize >> 16)
	hdr[2] = byte(dataSize >> 8)
	hdr[3] = byte(dataSize)
	hdr[4] = byte(timestamp >> 16)
	hdr[5] = byte(timestamp >> 8)
	hdr[6] = byte(timestamp)
	hdr[7] = byte(timestamp >> 24) // Extended timestamp
	// StreamID 0 (bytes 8-10 already zero)

	// Write header + data + previous tag size
	if _, err := r.w.Write(hdr[:]); err != nil {
		return err
	}
	if dataSize > 0 {
		if _, err := r.w.Write(payload); err != nil {
			return err
		}
	}
	// previous tag size = header(11) + dataSize (uint32 big‑endian)
	prevSize := uint32(11 + dataSize)
	var szBuf [4]byte
	binary.BigEndian.PutUint32(szBuf[:], prevSize)
	if _, err := r.w.Write(szBuf[:]); err != nil {
		return err
	}
	r.bytesWritten += uint64(11 + dataSize + 4)
	return nil
}

// Close releases the underlying file.
func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closeLocked()
}

func (r *Recorder) closeLocked() error {
	if r.w == nil {
		return nil
	}
	err := r.w.Close()
	r.w = nil
	return err
}

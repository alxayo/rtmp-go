package media

// Media Recorder (Task T045 / T046)
// ----------------------------------
// Minimal media file writer that automatically selects container format based on video codec:
//   * H.264 → FLV (legacy format, optimal for H.264)
//   * H.265, AV1, VP9, VVC, others → MP4 (supports all modern codecs)
//
// Design:
//   * MediaWriter interface: unified API (WriteMessage, Close, Disabled)
//   * FLVRecorder: writes FLV tags (existing format for H.264)
//   * MP4Recorder: writes MP4 atoms (simple mdat + moov for H.265+)
//   * NewRecorder factory: routes to appropriate implementation based on codec
//
// Graceful degradation: on any write error the recorder is disabled (future
// live streaming continues unaffected). File extension is automatically set
// based on selected format (.flv or .mp4).

import (
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/alxayo/go-rtmp/internal/rtmp/amf"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// MediaWriter is a unified interface for recording media to different container formats.
type MediaWriter interface {
	WriteMessage(msg *chunk.Message)
	Close() error
	Disabled() bool
}

// SelectContainerFormat returns the recommended container format for the given video codec.
// Defaults to FLV for backward compatibility when codec is empty or unknown.
func SelectContainerFormat(codec string) string {
	switch codec {
	case "H265", "AV1", "VP9", "VP8", "VVC": // Modern codecs that FLV doesn't support
		return "mp4"
	case "H264", "": // H.264 or unknown → FLV (backward compatible)
		return "flv"
	default:
		return "mp4" // Conservative: use MP4 for any unknown codec
	}
}

// UpdateRecordingPath modifies the file extension based on the selected container format.
// E.g., "recordings/stream_20260411_103406.flv" → "recordings/stream_20260411_103406.mp4" for H.265
func UpdateRecordingPath(path string, format string) string {
	if format == "mp4" {
		ext := filepath.Ext(path)
		if ext == ".flv" {
			return strings.TrimSuffix(path, ext) + ".mp4"
		}
	}
	return path
}

// NewRecorder creates a recorder using the appropriate container format for the given codec.
// If file creation fails it returns a nil recorder and the error.
// The codec parameter determines output format: H.265+ → MP4, H.264 → FLV (default).
// The optional metadata parameter provides video/audio properties for the FLV onMetaData tag.
func NewRecorder(path, codec string, logger *slog.Logger, meta ...FLVMetadata) (MediaWriter, error) {
	if logger == nil {
		logger = slog.Default()
	}

	format := SelectContainerFormat(codec)
	finalPath := UpdateRecordingPath(path, format)

	if format == "mp4" {
		return NewMP4Recorder(finalPath, logger)
	}
	var m FLVMetadata
	if len(meta) > 0 {
		m = meta[0]
	}
	return NewFLVRecorder(finalPath, logger, m)
}

// FLVRecorder persists RTMP audio/video messages into a single FLV file.
// It writes an onMetaData script tag (TypeID 18) as the first tag after
// the FLV header, and patches the duration and filesize fields on Close()
// using WriteAt. It is safe for single‑goroutine use (the media relay loop).
// A mutex is included only to guard against accidental concurrent calls in
// future extensions.
type FLVRecorder struct {
	mu           sync.Mutex
	f            *os.File // need WriteAt for duration patching
	logger       *slog.Logger
	wroteHeader  bool
	bytesWritten uint64
	meta         FLVMetadata

	// Offsets within the file where the duration and filesize AMF0 Number
	// values are stored. These point to the 8-byte IEEE-754 double payload
	// (after the AMF0 Number marker 0x00). Set by writeOnMetaData.
	durationOffset int64
	fileSizeOffset int64

	// Timestamp tracking for duration calculation on Close().
	firstTimestamp int64 // -1 means unset
	lastTimestamp  uint32
}

// NewFLVRecorder creates an FLV recorder writing to the supplied file path.
// The metadata parameter provides video/audio properties for the onMetaData tag.
// If file creation fails it returns a nil *FLVRecorder and the error.
func NewFLVRecorder(path string, logger *slog.Logger, meta FLVMetadata) (*FLVRecorder, error) {
	if logger == nil {
		logger = slog.Default()
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("recorder.create: %w", err)
	}
	r := &FLVRecorder{f: f, logger: logger, meta: meta, firstTimestamp: -1}
	if err := r.writeHeader(); err != nil {
		return nil, err
	}
	if err := r.writeOnMetaData(); err != nil {
		r.logger.Warn("recorder: failed to write onMetaData, continuing without it", "err", err)
	}
	return r, nil
}

// newFLVRecorderWithWriter allows tests to inject a failing writer (disk full simulation).
// Duration patching is not available through this path (requires *os.File).
func newFLVRecorderWithWriter(w io.WriteCloser, logger *slog.Logger) *FLVRecorder {
	if logger == nil {
		logger = slog.Default()
	}
	r := &FLVRecorder{logger: logger, firstTimestamp: -1}
	// If w is an *os.File, use it directly for WriteAt support.
	if f, ok := w.(*os.File); ok {
		r.f = f
		_ = r.writeHeader()
		return r
	}
	// Non-file writers: used by tests for disk-full simulation.
	// Write header directly to the writer. The recorder will be disabled
	// if the write fails (same as before).
	header := []byte{'F', 'L', 'V', 0x01, 0x05, 0x00, 0x00, 0x00, 0x09, 0x00, 0x00, 0x00, 0x00}
	if _, err := w.Write(header); err != nil {
		logger.Error("recorder write header failed", "err", err)
		w.Close()
		return r // r.f is nil → Disabled() returns true
	}
	r.bytesWritten = uint64(len(header))
	r.wroteHeader = true
	return r
}

// Disabled returns true if the recorder encountered a fatal write error.
func (r *FLVRecorder) Disabled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.f == nil
}

// writeHeader writes the 13‑byte FLV header: 9 bytes header + 4 bytes PreviousTagSize0
// Structure:
//
//	Signature: 'F','L','V'
//	Version:   0x01
//	Flags:     0x05 (audio + video present)
//	DataOffset: 0x00000009 (header length) big‑endian
//	PreviousTagSize0: 0x00000000
func (r *FLVRecorder) writeHeader() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil || r.wroteHeader {
		return nil
	}
	header := []byte{'F', 'L', 'V', 0x01, 0x05, 0x00, 0x00, 0x00, 0x09, 0x00, 0x00, 0x00, 0x00}
	if _, err := r.f.Write(header); err != nil {
		r.logger.Error("recorder write header failed", "err", err)
		r.closeLocked()
		return fmt.Errorf("recorder.header: %w", err)
	}
	r.wroteHeader = true
	r.bytesWritten += uint64(len(header))
	return nil
}

// writeOnMetaData writes an FLV script data tag (TypeID 18) containing the
// onMetaData ECMA Array. It records the file offsets of the "duration" and
// "filesize" Number values so they can be patched on Close().
func (r *FLVRecorder) writeOnMetaData() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return fmt.Errorf("recorder.metadata: file closed")
	}

	// Build the AMF0 payload: String("onMetaData") + ECMAArray({...})
	props := amf.ECMAArray{
		"duration":        0.0, // patched on Close()
		"filesize":        0.0, // patched on Close()
		"width":           float64(r.meta.Width),
		"height":          float64(r.meta.Height),
		"videocodecid":    r.meta.VideoCodecID,
		"audiocodecid":    r.meta.AudioCodecID,
		"audiosamplerate": r.meta.AudioSampleRate,
		"audiosamplesize": float64(16),
		"stereo":          r.meta.Stereo,
	}

	payload, err := amf.EncodeAll("onMetaData", props)
	if err != nil {
		return fmt.Errorf("recorder.metadata.encode: %w", err)
	}

	// Record the file offset where duration and filesize values are stored.
	// The tag starts at r.bytesWritten, then 11 bytes of FLV tag header,
	// then the AMF0 payload. We need to find the byte offsets of the
	// "duration" and "filesize" Number values within the payload.
	tagBodyStart := int64(r.bytesWritten) + 11 // after FLV tag header
	if off := findAMFNumberOffset(payload, "duration"); off >= 0 {
		r.durationOffset = tagBodyStart + off
	}
	if off := findAMFNumberOffset(payload, "filesize"); off >= 0 {
		r.fileSizeOffset = tagBodyStart + off
	}

	// Write as FLV script data tag (TypeID 18, timestamp 0)
	if err := r.writeTagLocked(18, 0, payload); err != nil {
		r.durationOffset = 0 // clear stale offsets on write failure
		r.fileSizeOffset = 0
		return fmt.Errorf("recorder.metadata.write: %w", err)
	}
	return nil
}

// findAMFNumberOffset finds the byte offset of the Number value (the 8-byte
// IEEE-754 double after the 0x00 marker) for a given key in an AMF0 payload
// that starts with a String + ECMAArray. Returns -1 if not found.
func findAMFNumberOffset(payload []byte, key string) int64 {
	// Search for the key in the payload. Key format: [2B len][key bytes][0x00 marker][8B double]
	keyBytes := []byte(key)
	searchFor := make([]byte, 2+len(keyBytes))
	binary.BigEndian.PutUint16(searchFor[:2], uint16(len(keyBytes)))
	copy(searchFor[2:], keyBytes)

	for i := 0; i+len(searchFor) < len(payload); i++ {
		match := true
		for j := range searchFor {
			if payload[i+j] != searchFor[j] {
				match = false
				break
			}
		}
		if match {
			// After the key, expect AMF0 Number marker (0x00) + 8 bytes of double
			markerPos := i + len(searchFor)
			if markerPos < len(payload) && payload[markerPos] == 0x00 {
				return int64(markerPos + 1) // offset of the 8-byte double value
			}
		}
	}
	return -1
}

// WriteMessage persists an RTMP media message (audio=8, video=9). Other message
// types are ignored silently. Safe to call after a failure; it no‑ops when disabled.
func (r *FLVRecorder) WriteMessage(msg *chunk.Message) {
	if msg == nil || (msg.TypeID != 8 && msg.TypeID != 9) {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil { // disabled
		return
	}
	if !r.wroteHeader {
		if err := r.writeHeader(); err != nil {
			return
		}
	}

	// Track timestamps for duration calculation (use max to handle out-of-order)
	if r.firstTimestamp < 0 {
		r.firstTimestamp = int64(msg.Timestamp)
	}
	if msg.Timestamp > r.lastTimestamp {
		r.lastTimestamp = msg.Timestamp
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
func (r *FLVRecorder) writeTagLocked(tagType uint8, timestamp uint32, payload []byte) error {
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
	if _, err := r.f.Write(hdr[:]); err != nil {
		return err
	}
	if dataSize > 0 {
		if _, err := r.f.Write(payload); err != nil {
			return err
		}
	}
	// previous tag size = header(11) + dataSize (uint32 big‑endian)
	prevSize := uint32(11 + dataSize)
	var szBuf [4]byte
	binary.BigEndian.PutUint32(szBuf[:], prevSize)
	if _, err := r.f.Write(szBuf[:]); err != nil {
		return err
	}
	r.bytesWritten += uint64(11 + dataSize + 4)
	return nil
}

// Close patches the duration and filesize in the onMetaData tag, then releases
// the underlying file.
func (r *FLVRecorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closeLocked()
}

func (r *FLVRecorder) closeLocked() error {
	if r.f == nil {
		return nil
	}

	// Patch duration and filesize in the onMetaData tag via WriteAt
	r.patchMetadata()

	err := r.f.Close()
	r.f = nil
	return err
}

// patchMetadata updates the duration and filesize values in the onMetaData
// tag by seeking to their recorded offsets and overwriting the 8-byte doubles.
func (r *FLVRecorder) patchMetadata() {
	if r.f == nil {
		return
	}

	// Calculate duration in seconds
	var duration float64
	if r.firstTimestamp >= 0 && r.lastTimestamp >= uint32(r.firstTimestamp) {
		duration = float64(r.lastTimestamp-uint32(r.firstTimestamp)) / 1000.0
	}

	// Patch duration
	if r.durationOffset > 0 {
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], math.Float64bits(duration))
		if _, err := r.f.WriteAt(buf[:], r.durationOffset); err != nil {
			r.logger.Warn("recorder: failed to patch duration", "err", err)
		}
	}

	// Patch filesize
	if r.fileSizeOffset > 0 {
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], math.Float64bits(float64(r.bytesWritten)))
		if _, err := r.f.WriteAt(buf[:], r.fileSizeOffset); err != nil {
			r.logger.Warn("recorder: failed to patch filesize", "err", err)
		}
	}
}

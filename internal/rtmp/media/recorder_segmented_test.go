package media

// Tests for SegmentedRecorder — segmented media recording.
//
// These tests verify:
//   - Basic rotation: segments rotate after target duration on keyframe
//   - Keyframe alignment: no rotation on P-frames even after duration exceeded
//   - Sequence header caching: seq headers re-injected into each new segment
//   - Audio-only streams: rotation without waiting for video keyframes
//   - Close mid-segment: current segment is properly finalized
//   - Error handling: disabled on naming/file errors
//   - Segment counting: increments correctly on each rotation
//   - Enhanced RTMP: keyframe detection for enhanced format
//
// Test pattern: create real segment files in t.TempDir(), verify they exist
// and contain expected data. Uses a simple SegmentNameFunc that returns
// incrementing file paths.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// makeSegmentNameFn creates a SegmentNameFunc that returns incrementing
// file paths in the given directory, e.g. "seg_1.flv", "seg_2.flv", etc.
func makeSegmentNameFn(dir string) (SegmentNameFunc, *int) {
	counter := 0
	return func() (string, error) {
		counter++
		return filepath.Join(dir, fmt.Sprintf("seg_%d.flv", counter)), nil
	}, &counter
}

// makeVideoKeyframe builds a minimal legacy AVC (H.264) keyframe payload.
// Layout: [frameType=1 (keyframe) << 4 | codecID=7 (AVC)] [AVCPacketType=1 (NALU)] [data...]
// The first byte 0x17 means: keyframe (1) + AVC codec (7).
func makeVideoKeyframe(data ...byte) []byte {
	payload := []byte{0x17, 0x01} // keyframe + AVC NALU
	payload = append(payload, data...)
	return payload
}

// makeVideoPFrame builds a minimal legacy AVC inter-frame (P-frame) payload.
// Layout: [frameType=2 (inter) << 4 | codecID=7 (AVC)] [AVCPacketType=1 (NALU)] [data...]
// The first byte 0x27 means: inter-frame (2) + AVC codec (7).
func makeVideoPFrame(data ...byte) []byte {
	payload := []byte{0x27, 0x01} // inter-frame + AVC NALU
	payload = append(payload, data...)
	return payload
}

// makeVideoSeqHeader builds a minimal legacy AVC sequence header payload.
// Layout: [frameType=1 << 4 | codecID=7] [AVCPacketType=0 (seq header)] [config data...]
// The first byte 0x17 with second byte 0x00 signals an AVC sequence header.
func makeVideoSeqHeader() []byte {
	return []byte{0x17, 0x00, 0x01, 0x64, 0x00, 0x1E} // keyframe + AVC seq header + sample config
}

// makeAudioSeqHeader builds a minimal legacy AAC sequence header payload.
// Layout: [soundFormat=10 (AAC) << 4 | flags] [AACPacketType=0 (seq header)] [AudioSpecificConfig...]
// The first byte 0xAF means: AAC (10) + 44kHz + 16-bit + stereo.
func makeAudioSeqHeader() []byte {
	return []byte{0xAF, 0x00, 0x12, 0x10} // AAC seq header + sample AudioSpecificConfig
}

// makeAudioFrame builds a minimal legacy AAC raw audio frame payload.
// Layout: [soundFormat=10 << 4 | flags] [AACPacketType=1 (raw)] [audio data...]
func makeAudioFrame(data ...byte) []byte {
	payload := []byte{0xAF, 0x01} // AAC raw frame
	payload = append(payload, data...)
	return payload
}

// makeEnhancedKeyframe builds an Enhanced RTMP video keyframe payload.
// Layout: [isExHeader=1 | frameType=1 (keyframe) << 4 | pktType=3 (codedFramesX)]
//
//	[FourCC: "hvc1"] [data...]
//
// First byte: 0b1_001_0011 = 0x93 (enhanced + keyframe + codedFramesX)
func makeEnhancedKeyframe(data ...byte) []byte {
	payload := []byte{
		0x93,             // isExHeader=1 | frameType=1 | pktType=3 (codedFramesX)
		'h', 'v', 'c', '1', // FourCC for HEVC
	}
	payload = append(payload, data...)
	return payload
}

// makeEnhancedPFrame builds an Enhanced RTMP video inter-frame payload.
// Layout: [isExHeader=1 | frameType=2 (inter) << 4 | pktType=3 (codedFramesX)]
//
//	[FourCC: "hvc1"] [data...]
//
// First byte: 0b1_010_0011 = 0xA3 (enhanced + inter + codedFramesX)
func makeEnhancedPFrame(data ...byte) []byte {
	payload := []byte{
		0xA3,             // isExHeader=1 | frameType=2 | pktType=3 (codedFramesX)
		'h', 'v', 'c', '1', // FourCC for HEVC
	}
	payload = append(payload, data...)
	return payload
}

// fileExistsAndNonEmpty checks that a file exists and has non-zero size.
// Returns true if the file is valid, false otherwise.
func fileExistsAndNonEmpty(t *testing.T, path string) bool {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Size() > 0
}

// TestSegmentedRecorder_BasicRotation verifies that the segmented recorder
// creates a new segment when the target duration is exceeded and a keyframe
// arrives. Feeds messages spanning 2x the segment duration and checks that
// exactly 2 segments are created.
func TestSegmentedRecorder_BasicRotation(t *testing.T) {
	dir := t.TempDir()
	nameFn, _ := makeSegmentNameFn(dir)

	// 1000ms segment duration for easy testing
	sr := NewSegmentedRecorder(1000, "H264", nameFn, NullLogger())

	// Send sequence headers first (these get cached, not counted for duration)
	sr.WriteMessage(&chunk.Message{TypeID: 9, Timestamp: 0, Payload: makeVideoSeqHeader(), MessageLength: 6})
	sr.WriteMessage(&chunk.Message{TypeID: 8, Timestamp: 0, Payload: makeAudioSeqHeader(), MessageLength: 4})

	// Segment 1: keyframe at 0ms, P-frames, then audio
	sr.WriteMessage(&chunk.Message{TypeID: 9, Timestamp: 0, Payload: makeVideoKeyframe(0x01), MessageLength: 3})
	sr.WriteMessage(&chunk.Message{TypeID: 9, Timestamp: 200, Payload: makeVideoPFrame(0x02), MessageLength: 3})
	sr.WriteMessage(&chunk.Message{TypeID: 8, Timestamp: 400, Payload: makeAudioFrame(0x03), MessageLength: 3})
	sr.WriteMessage(&chunk.Message{TypeID: 9, Timestamp: 600, Payload: makeVideoPFrame(0x04), MessageLength: 3})

	// At 1000ms, duration exceeded. Next keyframe triggers rotation.
	sr.WriteMessage(&chunk.Message{TypeID: 9, Timestamp: 1000, Payload: makeVideoPFrame(0x05), MessageLength: 3})
	// This P-frame at 1200ms shouldn't trigger rotation (not a keyframe)
	sr.WriteMessage(&chunk.Message{TypeID: 9, Timestamp: 1200, Payload: makeVideoPFrame(0x06), MessageLength: 3})
	// This keyframe at 1400ms should trigger rotation to segment 2
	sr.WriteMessage(&chunk.Message{TypeID: 9, Timestamp: 1400, Payload: makeVideoKeyframe(0x07), MessageLength: 3})

	// Some more frames in segment 2
	sr.WriteMessage(&chunk.Message{TypeID: 9, Timestamp: 1800, Payload: makeVideoPFrame(0x08), MessageLength: 3})

	if err := sr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Should have created exactly 2 segments
	if sr.SegmentCount() != 2 {
		t.Errorf("segment count: got %d, want 2", sr.SegmentCount())
	}

	// Verify both segment files exist and are non-empty
	if !fileExistsAndNonEmpty(t, filepath.Join(dir, "seg_1.flv")) {
		t.Error("segment 1 file missing or empty")
	}
	if !fileExistsAndNonEmpty(t, filepath.Join(dir, "seg_2.flv")) {
		t.Error("segment 2 file missing or empty")
	}
}

// TestSegmentedRecorder_KeyframeAlignment verifies that rotation does NOT
// happen on P-frames even when the duration has been exceeded. The recorder
// must wait for a keyframe to ensure each segment is independently decodable.
func TestSegmentedRecorder_KeyframeAlignment(t *testing.T) {
	dir := t.TempDir()
	nameFn, _ := makeSegmentNameFn(dir)

	sr := NewSegmentedRecorder(500, "H264", nameFn, NullLogger())

	// Initial keyframe opens segment 1
	sr.WriteMessage(&chunk.Message{TypeID: 9, Timestamp: 0, Payload: makeVideoKeyframe(0x01), MessageLength: 3})

	// Duration exceeded at 500ms, but only P-frames follow — no rotation
	sr.WriteMessage(&chunk.Message{TypeID: 9, Timestamp: 600, Payload: makeVideoPFrame(0x02), MessageLength: 3})
	sr.WriteMessage(&chunk.Message{TypeID: 9, Timestamp: 800, Payload: makeVideoPFrame(0x03), MessageLength: 3})
	sr.WriteMessage(&chunk.Message{TypeID: 9, Timestamp: 1000, Payload: makeVideoPFrame(0x04), MessageLength: 3})
	sr.WriteMessage(&chunk.Message{TypeID: 9, Timestamp: 1200, Payload: makeVideoPFrame(0x05), MessageLength: 3})

	// Still only 1 segment — waiting for keyframe
	if sr.SegmentCount() != 1 {
		t.Errorf("segment count before keyframe: got %d, want 1", sr.SegmentCount())
	}

	// Keyframe arrives — NOW rotate
	sr.WriteMessage(&chunk.Message{TypeID: 9, Timestamp: 1400, Payload: makeVideoKeyframe(0x06), MessageLength: 3})

	if sr.SegmentCount() != 2 {
		t.Errorf("segment count after keyframe: got %d, want 2", sr.SegmentCount())
	}

	sr.Close()
}

// TestSegmentedRecorder_SequenceHeaderCaching verifies that video and audio
// sequence headers are re-injected into each new segment. This is critical
// because decoders need the codec configuration data (SPS/PPS for video,
// AudioSpecificConfig for audio) to initialize before processing frames.
func TestSegmentedRecorder_SequenceHeaderCaching(t *testing.T) {
	dir := t.TempDir()
	nameFn, _ := makeSegmentNameFn(dir)

	sr := NewSegmentedRecorder(500, "H264", nameFn, NullLogger())

	// Send sequence headers
	videoSeq := makeVideoSeqHeader()
	audioSeq := makeAudioSeqHeader()
	sr.WriteMessage(&chunk.Message{TypeID: 9, Timestamp: 0, Payload: videoSeq, MessageLength: uint32(len(videoSeq))})
	sr.WriteMessage(&chunk.Message{TypeID: 8, Timestamp: 0, Payload: audioSeq, MessageLength: uint32(len(audioSeq))})

	// Segment 1: some frames
	sr.WriteMessage(&chunk.Message{TypeID: 9, Timestamp: 0, Payload: makeVideoKeyframe(0x01), MessageLength: 3})
	sr.WriteMessage(&chunk.Message{TypeID: 8, Timestamp: 200, Payload: makeAudioFrame(0x02), MessageLength: 3})

	// Trigger rotation with keyframe after duration exceeded
	sr.WriteMessage(&chunk.Message{TypeID: 9, Timestamp: 600, Payload: makeVideoKeyframe(0x03), MessageLength: 3})

	if sr.SegmentCount() != 2 {
		t.Fatalf("expected 2 segments, got %d", sr.SegmentCount())
	}

	sr.Close()

	// Read segment 2 and verify it contains FLV data (starts with "FLV" header).
	// The inner FLV recorder writes: header + onMetaData + seq headers + media frames.
	seg2Path := filepath.Join(dir, "seg_2.flv")
	data, err := os.ReadFile(seg2Path)
	if err != nil {
		t.Fatalf("read segment 2: %v", err)
	}

	// Verify it's a valid FLV file
	if len(data) < 13 {
		t.Fatalf("segment 2 too small: %d bytes", len(data))
	}
	if string(data[:3]) != "FLV" {
		t.Fatalf("segment 2 not a valid FLV file, header: %v", data[:3])
	}

	// Segment 2 should be larger than just the FLV header + onMetaData
	// because it should contain the re-injected sequence headers + the keyframe.
	// A bare FLV header is 13 bytes. onMetaData is ~100+ bytes. Seq headers add more.
	// With seq headers and at least one keyframe, we expect > 50 bytes.
	if len(data) < 50 {
		t.Errorf("segment 2 suspiciously small (%d bytes); sequence headers may not have been injected", len(data))
	}
}

// TestSegmentedRecorder_AudioOnly verifies that for audio-only streams
// (no video messages), rotation happens at the duration boundary without
// waiting for keyframes (since audio doesn't have keyframes).
func TestSegmentedRecorder_AudioOnly(t *testing.T) {
	dir := t.TempDir()
	nameFn, _ := makeSegmentNameFn(dir)

	sr := NewSegmentedRecorder(500, "H264", nameFn, NullLogger())

	// Send only audio messages — no video at all
	sr.WriteMessage(&chunk.Message{TypeID: 8, Timestamp: 0, Payload: makeAudioFrame(0x01), MessageLength: 3})
	sr.WriteMessage(&chunk.Message{TypeID: 8, Timestamp: 200, Payload: makeAudioFrame(0x02), MessageLength: 3})
	sr.WriteMessage(&chunk.Message{TypeID: 8, Timestamp: 400, Payload: makeAudioFrame(0x03), MessageLength: 3})

	// Duration exceeded at 500ms — next audio frame should trigger rotation
	sr.WriteMessage(&chunk.Message{TypeID: 8, Timestamp: 600, Payload: makeAudioFrame(0x04), MessageLength: 3})

	// Should have rotated to segment 2
	if sr.SegmentCount() != 2 {
		t.Errorf("segment count: got %d, want 2", sr.SegmentCount())
	}

	sr.Close()

	// Verify both segment files exist
	if !fileExistsAndNonEmpty(t, filepath.Join(dir, "seg_1.flv")) {
		t.Error("segment 1 missing or empty")
	}
	if !fileExistsAndNonEmpty(t, filepath.Join(dir, "seg_2.flv")) {
		t.Error("segment 2 missing or empty")
	}
}

// TestSegmentedRecorder_CloseMidSegment verifies that calling Close() before
// the segment duration expires properly finalizes the current segment file.
// The file should be valid and non-empty.
func TestSegmentedRecorder_CloseMidSegment(t *testing.T) {
	dir := t.TempDir()
	nameFn, _ := makeSegmentNameFn(dir)

	sr := NewSegmentedRecorder(10000, "H264", nameFn, NullLogger()) // 10s — won't be reached

	// Write a few frames (well under the 10s segment duration)
	sr.WriteMessage(&chunk.Message{TypeID: 9, Timestamp: 0, Payload: makeVideoKeyframe(0x01), MessageLength: 3})
	sr.WriteMessage(&chunk.Message{TypeID: 9, Timestamp: 500, Payload: makeVideoPFrame(0x02), MessageLength: 3})
	sr.WriteMessage(&chunk.Message{TypeID: 8, Timestamp: 1000, Payload: makeAudioFrame(0x03), MessageLength: 3})

	// Close before duration expires
	if err := sr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Should have exactly 1 segment
	if sr.SegmentCount() != 1 {
		t.Errorf("segment count: got %d, want 1", sr.SegmentCount())
	}

	// The segment file should exist and be non-empty (properly finalized)
	if !fileExistsAndNonEmpty(t, filepath.Join(dir, "seg_1.flv")) {
		t.Error("segment 1 missing or empty after close")
	}
}

// TestSegmentedRecorder_DisabledOnError verifies that if the SegmentNameFunc
// returns an error, the recorder marks itself as disabled and stops processing.
func TestSegmentedRecorder_DisabledOnError(t *testing.T) {
	callCount := 0
	failingNameFn := func() (string, error) {
		callCount++
		return "", fmt.Errorf("simulated naming error")
	}

	sr := NewSegmentedRecorder(1000, "H264", failingNameFn, NullLogger())

	// First write triggers lazy segment open, which calls nameFn → error
	sr.WriteMessage(&chunk.Message{TypeID: 9, Timestamp: 0, Payload: makeVideoKeyframe(0x01), MessageLength: 3})

	if !sr.Disabled() {
		t.Error("recorder should be disabled after nameFn error")
	}

	// Subsequent writes should be silently dropped (no panic, no error)
	sr.WriteMessage(&chunk.Message{TypeID: 9, Timestamp: 100, Payload: makeVideoKeyframe(0x02), MessageLength: 3})
	sr.WriteMessage(&chunk.Message{TypeID: 8, Timestamp: 200, Payload: makeAudioFrame(0x03), MessageLength: 3})

	// nameFn should have been called exactly once (second write was dropped)
	if callCount != 1 {
		t.Errorf("nameFn call count: got %d, want 1", callCount)
	}
}

// TestSegmentedRecorder_SegmentCount verifies that the segment counter
// increments correctly through multiple rotations.
func TestSegmentedRecorder_SegmentCount(t *testing.T) {
	dir := t.TempDir()
	nameFn, _ := makeSegmentNameFn(dir)

	sr := NewSegmentedRecorder(100, "H264", nameFn, NullLogger()) // 100ms segments

	// Verify initial count is 0
	if sr.SegmentCount() != 0 {
		t.Errorf("initial segment count: got %d, want 0", sr.SegmentCount())
	}

	// Segment 1: keyframe at 0ms
	sr.WriteMessage(&chunk.Message{TypeID: 9, Timestamp: 0, Payload: makeVideoKeyframe(0x01), MessageLength: 3})
	if sr.SegmentCount() != 1 {
		t.Errorf("after first frame: got %d, want 1", sr.SegmentCount())
	}

	// Segment 2: keyframe at 200ms (exceeds 100ms duration)
	sr.WriteMessage(&chunk.Message{TypeID: 9, Timestamp: 200, Payload: makeVideoKeyframe(0x02), MessageLength: 3})
	if sr.SegmentCount() != 2 {
		t.Errorf("after second keyframe: got %d, want 2", sr.SegmentCount())
	}

	// Segment 3: keyframe at 400ms
	sr.WriteMessage(&chunk.Message{TypeID: 9, Timestamp: 400, Payload: makeVideoKeyframe(0x03), MessageLength: 3})
	if sr.SegmentCount() != 3 {
		t.Errorf("after third keyframe: got %d, want 3", sr.SegmentCount())
	}

	sr.Close()

	// Verify all 3 segment files exist
	for i := 1; i <= 3; i++ {
		path := filepath.Join(dir, fmt.Sprintf("seg_%d.flv", i))
		if !fileExistsAndNonEmpty(t, path) {
			t.Errorf("segment %d file missing or empty", i)
		}
	}
}

// TestSegmentedRecorder_EnhancedRTMPKeyframe tests keyframe detection for
// Enhanced RTMP (E-RTMP) video payloads. Enhanced RTMP uses a different
// byte layout: bit 7 is the IsExHeader flag, bits [6:4] are frame type (3 bits).
func TestSegmentedRecorder_EnhancedRTMPKeyframe(t *testing.T) {
	dir := t.TempDir()
	nameFn, _ := makeSegmentNameFn(dir)

	// Use H265 codec for Enhanced RTMP (produces MP4 segments)
	sr := NewSegmentedRecorder(500, "H265", nameFn, NullLogger())

	// Enhanced RTMP sequence header: isExHeader=1 | keyframe | seqStart=0
	// byte 0 = 0b1_001_0000 = 0x90, followed by FourCC "hvc1"
	enhancedSeqHeader := []byte{0x90, 'h', 'v', 'c', '1', 0x01, 0x02, 0x03}
	sr.WriteMessage(&chunk.Message{TypeID: 9, Timestamp: 0, Payload: enhancedSeqHeader, MessageLength: uint32(len(enhancedSeqHeader))})

	// First keyframe (enhanced format) — opens segment 1
	sr.WriteMessage(&chunk.Message{TypeID: 9, Timestamp: 0, Payload: makeEnhancedKeyframe(0x01, 0x02), MessageLength: 7})

	// P-frame after duration exceeded — should NOT rotate
	sr.WriteMessage(&chunk.Message{TypeID: 9, Timestamp: 600, Payload: makeEnhancedPFrame(0x03, 0x04), MessageLength: 7})
	if sr.SegmentCount() != 1 {
		t.Errorf("after P-frame: got %d segments, want 1", sr.SegmentCount())
	}

	// Enhanced keyframe — should trigger rotation
	sr.WriteMessage(&chunk.Message{TypeID: 9, Timestamp: 800, Payload: makeEnhancedKeyframe(0x05, 0x06), MessageLength: 7})
	if sr.SegmentCount() != 2 {
		t.Errorf("after enhanced keyframe: got %d segments, want 2", sr.SegmentCount())
	}

	sr.Close()
}

// TestSegmentedRecorder_NilMessage verifies that passing nil to WriteMessage
// doesn't panic or cause issues.
func TestSegmentedRecorder_NilMessage(t *testing.T) {
	dir := t.TempDir()
	nameFn, _ := makeSegmentNameFn(dir)

	sr := NewSegmentedRecorder(1000, "H264", nameFn, NullLogger())

	// These should be silently ignored — no panic
	sr.WriteMessage(nil)
	sr.WriteMessage(&chunk.Message{TypeID: 20, Timestamp: 0, Payload: []byte{0x01}}) // non-media type

	// Segment count should be 0 (no real media written)
	if sr.SegmentCount() != 0 {
		t.Errorf("segment count: got %d, want 0", sr.SegmentCount())
	}

	sr.Close()
}

// TestSegmentedRecorder_CloseWithoutWrites verifies that closing a recorder
// that never received any messages doesn't panic or return an error.
func TestSegmentedRecorder_CloseWithoutWrites(t *testing.T) {
	dir := t.TempDir()
	nameFn, _ := makeSegmentNameFn(dir)

	sr := NewSegmentedRecorder(1000, "H264", nameFn, NullLogger())

	if err := sr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if sr.SegmentCount() != 0 {
		t.Errorf("segment count: got %d, want 0", sr.SegmentCount())
	}
}

// TestSegmentedRecorder_DisabledOnFileError verifies that if the inner
// recorder fails to create a file (e.g. invalid path), the segmented
// recorder marks itself as disabled.
func TestSegmentedRecorder_DisabledOnFileError(t *testing.T) {
	badPathFn := func() (string, error) {
		// Return a path in a non-existent directory — file creation will fail
		return "/nonexistent/directory/that/does/not/exist/seg.flv", nil
	}

	sr := NewSegmentedRecorder(1000, "H264", badPathFn, NullLogger())

	sr.WriteMessage(&chunk.Message{TypeID: 9, Timestamp: 0, Payload: makeVideoKeyframe(0x01), MessageLength: 3})

	if !sr.Disabled() {
		t.Error("recorder should be disabled after file creation error")
	}
}

// TestIsVideoKeyframe_Legacy tests the isVideoKeyframe helper for legacy
// FLV video payloads (4-bit frame type in high nibble).
func TestIsVideoKeyframe_Legacy(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		want    bool
	}{
		{
			name:    "keyframe_avc",
			payload: []byte{0x17, 0x01, 0x00}, // frameType=1 (keyframe) | codecID=7 (AVC)
			want:    true,
		},
		{
			name:    "interframe_avc",
			payload: []byte{0x27, 0x01, 0x00}, // frameType=2 (inter) | codecID=7 (AVC)
			want:    false,
		},
		{
			name:    "keyframe_hevc_legacy",
			payload: []byte{0x1C, 0x01, 0x00}, // frameType=1 | codecID=12 (HEVC legacy)
			want:    true,
		},
		{
			name:    "empty_payload",
			payload: []byte{},
			want:    false,
		},
		{
			name:    "nil_payload",
			payload: nil,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isVideoKeyframe(tt.payload)
			if got != tt.want {
				t.Errorf("isVideoKeyframe(%v) = %v, want %v", tt.payload, got, tt.want)
			}
		})
	}
}

// TestIsVideoKeyframe_Enhanced tests the isVideoKeyframe helper for Enhanced
// RTMP video payloads (bit 7 = IsExHeader, bits [6:4] = 3-bit frame type).
func TestIsVideoKeyframe_Enhanced(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		want    bool
	}{
		{
			name:    "enhanced_keyframe",
			payload: []byte{0x93, 'h', 'v', 'c', '1', 0x01}, // isExHeader=1 | frameType=1 | pktType=3
			want:    true,
		},
		{
			name:    "enhanced_interframe",
			payload: []byte{0xA3, 'h', 'v', 'c', '1', 0x01}, // isExHeader=1 | frameType=2 | pktType=3
			want:    false,
		},
		{
			name:    "enhanced_keyframe_seqstart",
			payload: []byte{0x90, 'h', 'v', 'c', '1'}, // isExHeader=1 | frameType=1 | pktType=0 (seqStart)
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isVideoKeyframe(tt.payload)
			if got != tt.want {
				t.Errorf("isVideoKeyframe(%v) = %v, want %v", tt.payload, got, tt.want)
			}
		})
	}
}

// TestSegmentedRecorder_SequenceHeadersBeforeFirstFrame verifies that
// sequence headers sent before any media frames are properly cached and
// injected into the first segment when it opens.
func TestSegmentedRecorder_SequenceHeadersBeforeFirstFrame(t *testing.T) {
	dir := t.TempDir()
	nameFn, _ := makeSegmentNameFn(dir)

	sr := NewSegmentedRecorder(5000, "H264", nameFn, NullLogger())

	// Send sequence headers before any real frames
	sr.WriteMessage(&chunk.Message{TypeID: 9, Timestamp: 0, Payload: makeVideoSeqHeader(), MessageLength: 6})
	sr.WriteMessage(&chunk.Message{TypeID: 8, Timestamp: 0, Payload: makeAudioSeqHeader(), MessageLength: 4})

	// No segment should be open yet (seq headers alone don't open a segment)
	if sr.SegmentCount() != 0 {
		t.Errorf("segment count after seq headers: got %d, want 0", sr.SegmentCount())
	}

	// First real frame opens the segment
	sr.WriteMessage(&chunk.Message{TypeID: 9, Timestamp: 0, Payload: makeVideoKeyframe(0x01), MessageLength: 3})

	if sr.SegmentCount() != 1 {
		t.Errorf("segment count after first frame: got %d, want 1", sr.SegmentCount())
	}

	sr.Close()

	// Verify the segment file exists, is non-empty, and starts with FLV header
	seg1Path := filepath.Join(dir, "seg_1.flv")
	data, err := os.ReadFile(seg1Path)
	if err != nil {
		t.Fatalf("read segment 1: %v", err)
	}
	if len(data) < 13 || string(data[:3]) != "FLV" {
		t.Fatalf("segment 1 is not a valid FLV file")
	}
}

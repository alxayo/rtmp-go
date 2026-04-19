package mkv

import (
	"bytes"
	"log/slog"
	"testing"
)

// ─── Test helpers: EBML element builders ────────────────────────────────────
//
// These helpers build binary EBML elements programmatically. They're used
// to construct synthetic Matroska streams for testing without needing real
// media files.

// encodeID converts an EBML element ID (uint32) to its wire-format bytes.
// The ID value already contains the VINT width marker bit, so we just need
// to determine how many bytes to emit based on the value's magnitude.
func encodeID(id uint32) []byte {
	switch {
	case id <= 0xFF:
		return []byte{byte(id)}
	case id <= 0xFFFF:
		return []byte{byte(id >> 8), byte(id)}
	case id <= 0xFFFFFF:
		return []byte{byte(id >> 16), byte(id >> 8), byte(id)}
	default:
		return []byte{byte(id >> 24), byte(id >> 16), byte(id >> 8), byte(id)}
	}
}

// encodeSize encodes an element size as an EBML VINT (with marker bit set).
// This is the inverse of ReadVINTValue. Chooses the smallest encoding that
// can represent the value. The special value UnknownSize (-1) encodes as
// the 8-byte all-ones pattern.
func encodeSize(value int64) []byte {
	if value == UnknownSize {
		return []byte{0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	}
	switch {
	case value <= 126:
		// 1-byte: value bits = 7, max = 2^7 - 2 = 126 (127 = unknown sentinel)
		return []byte{byte(value) | 0x80}
	case value <= 16382:
		// 2-byte: value bits = 14, max = 2^14 - 2 = 16382
		return []byte{byte(value>>8) | 0x40, byte(value)}
	case value <= 2097150:
		// 3-byte: value bits = 21, max = 2^21 - 2 = 2097150
		return []byte{byte(value>>16) | 0x20, byte(value >> 8), byte(value)}
	default:
		// 4-byte: value bits = 28, max = 2^28 - 2 = 268435454
		return []byte{byte(value>>24) | 0x10, byte(value >> 16), byte(value >> 8), byte(value)}
	}
}

// buildElement constructs a complete EBML element: ID bytes + size VINT + body.
func buildElement(id uint32, body []byte) []byte {
	var buf []byte
	buf = append(buf, encodeID(id)...)
	buf = append(buf, encodeSize(int64(len(body)))...)
	buf = append(buf, body...)
	return buf
}

// buildUintElement creates an EBML unsigned integer element with the
// smallest encoding for the given value.
func buildUintElement(id uint32, value uint64) []byte {
	var body []byte
	switch {
	case value <= 0xFF:
		body = []byte{byte(value)}
	case value <= 0xFFFF:
		body = []byte{byte(value >> 8), byte(value)}
	case value <= 0xFFFFFF:
		body = []byte{byte(value >> 16), byte(value >> 8), byte(value)}
	default:
		body = []byte{byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value)}
	}
	return buildElement(id, body)
}

// buildStringElement creates an EBML string element.
func buildStringElement(id uint32, s string) []byte {
	return buildElement(id, []byte(s))
}

// ─── Test helpers: stream builders ──────────────────────────────────────────

// buildEBMLHeader creates a complete EBML header element with the given DocType.
func buildEBMLHeader(docType string) []byte {
	var body []byte
	body = append(body, buildUintElement(IDEBMLVersion, 1)...)
	body = append(body, buildUintElement(IDEBMLReadVersion, 1)...)
	body = append(body, buildUintElement(IDEBMLMaxIDLength, 4)...)
	body = append(body, buildUintElement(IDEBMLMaxSizeLength, 8)...)
	body = append(body, buildStringElement(IDDocType, docType)...)
	body = append(body, buildUintElement(IDDocTypeVersion, 4)...)
	body = append(body, buildUintElement(IDDocTypeReadVersion, 2)...)
	return buildElement(IDEBMLHeader, body)
}

// buildSegmentHeader creates a Segment element header with unknown size
// (as used in live streaming).
func buildSegmentHeader() []byte {
	var buf []byte
	buf = append(buf, encodeID(IDSegment)...)
	buf = append(buf, encodeSize(UnknownSize)...)
	return buf
}

// buildInfo creates an Info element with the given TimecodeScale.
func buildInfo(timecodeScale uint64) []byte {
	body := buildUintElement(IDTimecodeScale, timecodeScale)
	return buildElement(IDInfo, body)
}

// buildTrackEntry creates a TrackEntry element with the given parameters.
func buildTrackEntry(trackNum uint8, trackType uint8, codecID string, codecPrivate []byte) []byte {
	var body []byte
	body = append(body, buildUintElement(IDTrackNumber, uint64(trackNum))...)
	body = append(body, buildUintElement(IDTrackType, uint64(trackType))...)
	body = append(body, buildStringElement(IDCodecID, codecID)...)
	if codecPrivate != nil {
		body = append(body, buildElement(IDCodecPrivate, codecPrivate)...)
	}
	return buildElement(IDTrackEntry, body)
}

// buildTracks creates a Tracks element containing the given TrackEntry bodies.
func buildTracks(entries ...[]byte) []byte {
	var body []byte
	for _, e := range entries {
		body = append(body, e...)
	}
	return buildElement(IDTracks, body)
}

// buildClusterStart creates a Cluster header (unknown size) followed by
// a Timecode element with the given value.
func buildClusterStart(timecode uint64) []byte {
	var buf []byte
	buf = append(buf, encodeID(IDCluster)...)
	buf = append(buf, encodeSize(UnknownSize)...)
	buf = append(buf, buildUintElement(IDTimecode, timecode)...)
	return buf
}

// buildSimpleBlock creates a SimpleBlock body (without the element wrapper).
// This is the raw content that goes inside a SimpleBlock element.
func buildSimpleBlock(trackNum uint8, timecodeOffset int16, keyframe bool, data []byte) []byte {
	var buf []byte

	// Track number as 1-byte VINT value (marker bit set).
	buf = append(buf, byte(trackNum)|0x80)

	// Signed 16-bit timecode offset (big-endian).
	buf = append(buf, byte(timecodeOffset>>8), byte(timecodeOffset))

	// Flags byte: bit 7 = keyframe, bits 2-1 = lacing (0 = none).
	var flags byte
	if keyframe {
		flags |= 0x80
	}
	buf = append(buf, flags)

	// Raw frame data.
	buf = append(buf, data...)

	return buf
}

// buildSimpleBlockElement wraps a SimpleBlock body in an element header.
func buildSimpleBlockElement(trackNum uint8, timecodeOffset int16, keyframe bool, data []byte) []byte {
	body := buildSimpleBlock(trackNum, timecodeOffset, keyframe, data)
	return buildElement(IDSimpleBlock, body)
}

// buildSimpleBlockLaced creates a SimpleBlock body with lacing.
// lacingType: 0=none, 1=Xiph, 2=fixed-size, 3=EBML
func buildSimpleBlockLaced(trackNum uint8, timecodeOffset int16, keyframe bool, lacingType uint8, frames [][]byte) []byte {
	var buf []byte

	// Track number
	buf = append(buf, byte(trackNum)|0x80)

	// Timecode offset
	buf = append(buf, byte(timecodeOffset>>8), byte(timecodeOffset))

	// Flags with lacing type
	var flags byte
	if keyframe {
		flags |= 0x80
	}
	flags |= (lacingType & 0x03) << 1
	buf = append(buf, flags)

	// Frame count - 1 (for laced blocks)
	if lacingType != 0 {
		buf = append(buf, byte(len(frames)-1))
	}

	// Frame data
	for _, frame := range frames {
		buf = append(buf, frame...)
	}

	return buf
}

// buildMinimalStream creates a complete minimal Matroska stream with the
// given tracks and returns the bytes up to (but not including) the first Cluster.
func buildMinimalStream(docType string, timecodeScale uint64, trackEntries ...[]byte) []byte {
	var stream []byte
	stream = append(stream, buildEBMLHeader(docType)...)
	stream = append(stream, buildSegmentHeader()...)
	stream = append(stream, buildInfo(timecodeScale)...)
	stream = append(stream, buildTracks(trackEntries...)...)
	return stream
}

// newTestDemuxer creates a demuxer with a frame collector for testing.
func newTestDemuxer() (*Demuxer, *[]*Frame) {
	var frames []*Frame
	handler := func(f *Frame) {
		frames = append(frames, f)
	}
	d := NewDemuxer(handler, slog.Default())
	return d, &frames
}

// ─── Tests ──────────────────────────────────────────────────────────────────

func TestDemuxer_EBMLHeaderParsing(t *testing.T) {
	// Build a complete stream: EBML header → Segment → Info → Tracks(VP9) →
	// Cluster → SimpleBlock(keyframe)
	d, frames := newTestDemuxer()
	frameData := []byte{0xDE, 0xAD, 0xBE, 0xEF}

	var stream []byte
	stream = append(stream, buildMinimalStream("webm", 1_000_000,
		buildTrackEntry(1, TrackTypeVideo, "V_VP9", nil))...)
	stream = append(stream, buildClusterStart(0)...)
	stream = append(stream, buildSimpleBlockElement(1, 0, true, frameData)...)

	if err := d.Feed(stream); err != nil {
		t.Fatalf("Feed: %v", err)
	}

	if len(*frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(*frames))
	}

	f := (*frames)[0]
	if f.Codec != "VP9" {
		t.Errorf("codec: got %q, want %q", f.Codec, "VP9")
	}
	if !f.IsVideo {
		t.Error("expected IsVideo=true")
	}
	if f.Timestamp != 0 {
		t.Errorf("timestamp: got %d, want 0", f.Timestamp)
	}
	if !bytes.Equal(f.Data, frameData) {
		t.Errorf("data: got %x, want %x", f.Data, frameData)
	}
	if !f.IsKey {
		t.Error("expected IsKey=true")
	}
}

func TestDemuxer_VP9Stream(t *testing.T) {
	// Two VP9 frames: keyframe at t=0, interframe at t=33
	d, frames := newTestDemuxer()
	keyData := []byte{0x01, 0x02, 0x03}
	interData := []byte{0x04, 0x05, 0x06}

	var stream []byte
	stream = append(stream, buildMinimalStream("webm", 1_000_000,
		buildTrackEntry(1, TrackTypeVideo, "V_VP9", nil))...)
	stream = append(stream, buildClusterStart(0)...)
	stream = append(stream, buildSimpleBlockElement(1, 0, true, keyData)...)
	stream = append(stream, buildSimpleBlockElement(1, 33, false, interData)...)

	if err := d.Feed(stream); err != nil {
		t.Fatalf("Feed: %v", err)
	}

	if len(*frames) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(*frames))
	}

	// Frame 0: keyframe at t=0
	if (*frames)[0].Codec != "VP9" {
		t.Errorf("frame 0 codec: %q", (*frames)[0].Codec)
	}
	if !(*frames)[0].IsKey {
		t.Error("frame 0 should be keyframe")
	}
	if (*frames)[0].Timestamp != 0 {
		t.Errorf("frame 0 timestamp: %d", (*frames)[0].Timestamp)
	}
	if !bytes.Equal((*frames)[0].Data, keyData) {
		t.Errorf("frame 0 data mismatch")
	}

	// Frame 1: interframe at t=33
	if (*frames)[1].IsKey {
		t.Error("frame 1 should not be keyframe")
	}
	if (*frames)[1].Timestamp != 33 {
		t.Errorf("frame 1 timestamp: got %d, want 33", (*frames)[1].Timestamp)
	}
	if !bytes.Equal((*frames)[1].Data, interData) {
		t.Errorf("frame 1 data mismatch")
	}
}

func TestDemuxer_OpusAudio(t *testing.T) {
	// Opus audio with CodecPrivate (fake OpusHead)
	d, frames := newTestDemuxer()
	opusHead := []byte{0x4F, 0x70, 0x75, 0x73, 0x48, 0x65, 0x61, 0x64}
	audioData1 := []byte{0xAA, 0xBB, 0xCC}
	audioData2 := []byte{0xDD, 0xEE, 0xFF}

	var stream []byte
	stream = append(stream, buildMinimalStream("webm", 1_000_000,
		buildTrackEntry(1, TrackTypeAudio, "A_OPUS", opusHead))...)
	stream = append(stream, buildClusterStart(0)...)
	stream = append(stream, buildSimpleBlockElement(1, 0, true, audioData1)...)
	stream = append(stream, buildSimpleBlockElement(1, 20, true, audioData2)...)

	if err := d.Feed(stream); err != nil {
		t.Fatalf("Feed: %v", err)
	}

	if len(*frames) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(*frames))
	}

	// First frame should have CodecPrivate
	if (*frames)[0].Codec != "Opus" {
		t.Errorf("frame 0 codec: %q", (*frames)[0].Codec)
	}
	if (*frames)[0].IsVideo {
		t.Error("frame 0 should not be video")
	}
	if !bytes.Equal((*frames)[0].CodecPrivate, opusHead) {
		t.Errorf("frame 0 CodecPrivate: got %x, want %x", (*frames)[0].CodecPrivate, opusHead)
	}

	// Second frame should NOT have CodecPrivate
	if (*frames)[1].CodecPrivate != nil {
		t.Errorf("frame 1 CodecPrivate should be nil, got %x", (*frames)[1].CodecPrivate)
	}
}

func TestDemuxer_VideoAndAudio(t *testing.T) {
	// Both VP9 video (track 1) and Opus audio (track 2)
	d, frames := newTestDemuxer()
	videoData := []byte{0x01, 0x02}
	audioData := []byte{0x03, 0x04}

	var stream []byte
	stream = append(stream, buildMinimalStream("webm", 1_000_000,
		buildTrackEntry(1, TrackTypeVideo, "V_VP9", nil),
		buildTrackEntry(2, TrackTypeAudio, "A_OPUS", nil))...)
	stream = append(stream, buildClusterStart(0)...)
	stream = append(stream, buildSimpleBlockElement(1, 0, true, videoData)...)
	stream = append(stream, buildSimpleBlockElement(2, 0, true, audioData)...)

	if err := d.Feed(stream); err != nil {
		t.Fatalf("Feed: %v", err)
	}

	if len(*frames) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(*frames))
	}

	// Verify correct dispatching by track
	var gotVideo, gotAudio bool
	for _, f := range *frames {
		if f.IsVideo && f.Codec == "VP9" && bytes.Equal(f.Data, videoData) {
			gotVideo = true
		}
		if !f.IsVideo && f.Codec == "Opus" && bytes.Equal(f.Data, audioData) {
			gotAudio = true
		}
	}
	if !gotVideo {
		t.Error("missing video frame")
	}
	if !gotAudio {
		t.Error("missing audio frame")
	}
}

func TestDemuxer_TimecodeScale(t *testing.T) {
	// TimecodeScale = 500,000 ns (0.5 ms per unit)
	// Cluster timecode = 100 → timestamp = 100 * 500,000 / 1,000,000 = 50 ms
	d, frames := newTestDemuxer()
	frameData := []byte{0xAA}

	var stream []byte
	stream = append(stream, buildMinimalStream("matroska", 500_000,
		buildTrackEntry(1, TrackTypeVideo, "V_VP9", nil))...)
	stream = append(stream, buildClusterStart(100)...)
	stream = append(stream, buildSimpleBlockElement(1, 0, true, frameData)...)

	if err := d.Feed(stream); err != nil {
		t.Fatalf("Feed: %v", err)
	}

	if len(*frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(*frames))
	}

	// 100 * 500,000 / 1,000,000 = 50 ms
	if (*frames)[0].Timestamp != 50 {
		t.Errorf("timestamp: got %d, want 50", (*frames)[0].Timestamp)
	}
}

func TestDemuxer_ClusterTimecodeOffset(t *testing.T) {
	// Cluster timecode=1000, SimpleBlock offset=-10 → timestamp=990ms
	d, frames := newTestDemuxer()
	frameData := []byte{0xBB}

	var stream []byte
	stream = append(stream, buildMinimalStream("webm", 1_000_000,
		buildTrackEntry(1, TrackTypeVideo, "V_VP9", nil))...)
	stream = append(stream, buildClusterStart(1000)...)
	stream = append(stream, buildSimpleBlockElement(1, -10, true, frameData)...)

	if err := d.Feed(stream); err != nil {
		t.Fatalf("Feed: %v", err)
	}

	if len(*frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(*frames))
	}

	// (1000 + (-10)) * 1,000,000 / 1,000,000 = 990 ms
	if (*frames)[0].Timestamp != 990 {
		t.Errorf("timestamp: got %d, want 990", (*frames)[0].Timestamp)
	}
}

func TestDemuxer_PartialFeed(t *testing.T) {
	// Feed the same stream in small chunks (10 bytes at a time).
	// All frames should still be emitted correctly.
	d, frames := newTestDemuxer()
	frameData := []byte{0x01, 0x02, 0x03, 0x04}

	var stream []byte
	stream = append(stream, buildMinimalStream("webm", 1_000_000,
		buildTrackEntry(1, TrackTypeVideo, "V_VP9", nil))...)
	stream = append(stream, buildClusterStart(0)...)
	stream = append(stream, buildSimpleBlockElement(1, 0, true, frameData)...)
	stream = append(stream, buildSimpleBlockElement(1, 33, false, frameData)...)

	// Feed in small chunks
	chunkSize := 10
	for i := 0; i < len(stream); i += chunkSize {
		end := i + chunkSize
		if end > len(stream) {
			end = len(stream)
		}
		if err := d.Feed(stream[i:end]); err != nil {
			t.Fatalf("Feed chunk [%d:%d]: %v", i, end, err)
		}
	}

	if len(*frames) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(*frames))
	}

	if !(*frames)[0].IsKey {
		t.Error("frame 0 should be keyframe")
	}
	if (*frames)[1].IsKey {
		t.Error("frame 1 should not be keyframe")
	}
	if (*frames)[1].Timestamp != 33 {
		t.Errorf("frame 1 timestamp: got %d, want 33", (*frames)[1].Timestamp)
	}
}

func TestDemuxer_UnknownElements(t *testing.T) {
	// Insert Void and CRC-32 elements between known elements.
	// They should be skipped and frames should still work.
	d, frames := newTestDemuxer()
	frameData := []byte{0x42}

	// Build the stream with Void elements inserted at various points.
	voidElem := buildElement(IDVoid, []byte{0x00, 0x00, 0x00, 0x00})
	crc32Elem := buildElement(IDCRC32, []byte{0x01, 0x02, 0x03, 0x04})

	var stream []byte
	stream = append(stream, buildEBMLHeader("webm")...)
	stream = append(stream, buildSegmentHeader()...)
	stream = append(stream, voidElem...)  // Void between Segment and Info
	stream = append(stream, buildInfo(1_000_000)...)
	stream = append(stream, crc32Elem...) // CRC-32 between Info and Tracks
	stream = append(stream, buildTracks(
		buildTrackEntry(1, TrackTypeVideo, "V_VP9", nil))...)
	stream = append(stream, buildClusterStart(0)...)
	stream = append(stream, buildSimpleBlockElement(1, 0, true, frameData)...)

	if err := d.Feed(stream); err != nil {
		t.Fatalf("Feed: %v", err)
	}

	if len(*frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(*frames))
	}
	if !bytes.Equal((*frames)[0].Data, frameData) {
		t.Errorf("frame data mismatch")
	}
}

func TestDemuxer_CodecPrivateOnFirstFrame(t *testing.T) {
	// Verify CodecPrivate is set on the first frame per track, nil afterwards.
	d, frames := newTestDemuxer()
	codecPriv := []byte{0xCA, 0xFE, 0xBA, 0xBE}

	var stream []byte
	stream = append(stream, buildMinimalStream("webm", 1_000_000,
		buildTrackEntry(1, TrackTypeVideo, "V_MPEG4/ISO/AVC", codecPriv))...)
	stream = append(stream, buildClusterStart(0)...)
	stream = append(stream, buildSimpleBlockElement(1, 0, true, []byte{0x01})...)
	stream = append(stream, buildSimpleBlockElement(1, 33, false, []byte{0x02})...)
	stream = append(stream, buildSimpleBlockElement(1, 66, false, []byte{0x03})...)

	if err := d.Feed(stream); err != nil {
		t.Fatalf("Feed: %v", err)
	}

	if len(*frames) != 3 {
		t.Fatalf("expected 3 frames, got %d", len(*frames))
	}

	// First frame: CodecPrivate should be present
	if !bytes.Equal((*frames)[0].CodecPrivate, codecPriv) {
		t.Errorf("frame 0 CodecPrivate: got %x, want %x", (*frames)[0].CodecPrivate, codecPriv)
	}
	if (*frames)[0].Codec != "H264" {
		t.Errorf("frame 0 codec: got %q, want %q", (*frames)[0].Codec, "H264")
	}

	// Subsequent frames: CodecPrivate should be nil
	if (*frames)[1].CodecPrivate != nil {
		t.Errorf("frame 1 CodecPrivate should be nil")
	}
	if (*frames)[2].CodecPrivate != nil {
		t.Errorf("frame 2 CodecPrivate should be nil")
	}
}

func TestDemuxer_FixedSizeLacing(t *testing.T) {
	// Build a SimpleBlock with fixed-size lacing (2 frames of 5 bytes each).
	d, frames := newTestDemuxer()
	frame1Data := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	frame2Data := []byte{0x06, 0x07, 0x08, 0x09, 0x0A}

	var stream []byte
	stream = append(stream, buildMinimalStream("webm", 1_000_000,
		buildTrackEntry(1, TrackTypeAudio, "A_OPUS", nil))...)
	stream = append(stream, buildClusterStart(0)...)

	// Build laced SimpleBlock element
	lacedBody := buildSimpleBlockLaced(1, 0, true, 2, [][]byte{frame1Data, frame2Data})
	stream = append(stream, buildElement(IDSimpleBlock, lacedBody)...)

	if err := d.Feed(stream); err != nil {
		t.Fatalf("Feed: %v", err)
	}

	if len(*frames) != 2 {
		t.Fatalf("expected 2 frames from fixed-size lacing, got %d", len(*frames))
	}

	if !bytes.Equal((*frames)[0].Data, frame1Data) {
		t.Errorf("frame 0 data: got %x, want %x", (*frames)[0].Data, frame1Data)
	}
	if !bytes.Equal((*frames)[1].Data, frame2Data) {
		t.Errorf("frame 1 data: got %x, want %x", (*frames)[1].Data, frame2Data)
	}

	// Only first laced frame should be keyframe
	if !(*frames)[0].IsKey {
		t.Error("first laced frame should be keyframe")
	}
	if (*frames)[1].IsKey {
		t.Error("second laced frame should not be keyframe")
	}
}

func TestDemuxer_UnsupportedLacing(t *testing.T) {
	// Xiph lacing (type 1) should return an error.
	d, _ := newTestDemuxer()

	var stream []byte
	stream = append(stream, buildMinimalStream("webm", 1_000_000,
		buildTrackEntry(1, TrackTypeVideo, "V_VP9", nil))...)
	stream = append(stream, buildClusterStart(0)...)

	// Build a SimpleBlock with Xiph lacing
	lacedBody := buildSimpleBlockLaced(1, 0, true, 1, [][]byte{
		{0xAA, 0xBB},
		{0xCC, 0xDD},
	})
	stream = append(stream, buildElement(IDSimpleBlock, lacedBody)...)

	err := d.Feed(stream)
	if err == nil {
		t.Fatal("expected error for Xiph lacing, got nil")
	}

	// Also test EBML lacing (type 3)
	d2, _ := newTestDemuxer()
	var stream2 []byte
	stream2 = append(stream2, buildMinimalStream("webm", 1_000_000,
		buildTrackEntry(1, TrackTypeVideo, "V_VP9", nil))...)
	stream2 = append(stream2, buildClusterStart(0)...)
	lacedBody2 := buildSimpleBlockLaced(1, 0, true, 3, [][]byte{
		{0xEE, 0xFF},
		{0x11, 0x22},
	})
	stream2 = append(stream2, buildElement(IDSimpleBlock, lacedBody2)...)

	err = d2.Feed(stream2)
	if err == nil {
		t.Fatal("expected error for EBML lacing, got nil")
	}
}

func TestDemuxer_BufferOverflow(t *testing.T) {
	// Feed >2MB of data without valid elements to trigger buffer overflow.
	d, _ := newTestDemuxer()

	// Start with a valid EBML header and Segment so we get past that state.
	var stream []byte
	stream = append(stream, buildEBMLHeader("webm")...)
	stream = append(stream, buildSegmentHeader()...)

	// Feed the valid header first
	if err := d.Feed(stream); err != nil {
		t.Fatalf("Feed header: %v", err)
	}

	// Now feed a large element with a huge size that will never complete.
	// Build an Info element header with size = 3MB, but don't provide the body.
	hugeHeader := encodeID(IDInfo)
	// Encode a 4-byte size of 3MB
	hugeSize := int64(3 * 1024 * 1024)
	sizeBytes := make([]byte, 4)
	sizeBytes[0] = byte(hugeSize>>24) | 0x10
	sizeBytes[1] = byte(hugeSize >> 16)
	sizeBytes[2] = byte(hugeSize >> 8)
	sizeBytes[3] = byte(hugeSize)
	hugeHeader = append(hugeHeader, sizeBytes...)

	if err := d.Feed(hugeHeader); err != nil {
		t.Fatalf("Feed huge header: %v", err)
	}

	// Feed 2MB+ of garbage to exceed the buffer limit
	garbage := make([]byte, maxBufferSize)
	err := d.Feed(garbage)
	if err == nil {
		t.Fatal("expected buffer overflow error")
	}
	if err != errBufferOverflow {
		t.Errorf("expected errBufferOverflow, got: %v", err)
	}
}

func TestDemuxer_ExtraTracksSkipped(t *testing.T) {
	// Two video tracks — only the first one's frames should be emitted.
	d, frames := newTestDemuxer()
	video1Data := []byte{0x11, 0x22}
	video2Data := []byte{0x33, 0x44}

	var stream []byte
	stream = append(stream, buildMinimalStream("webm", 1_000_000,
		buildTrackEntry(1, TrackTypeVideo, "V_VP9", nil),
		buildTrackEntry(2, TrackTypeVideo, "V_VP8", nil))...)
	stream = append(stream, buildClusterStart(0)...)
	stream = append(stream, buildSimpleBlockElement(1, 0, true, video1Data)...)
	stream = append(stream, buildSimpleBlockElement(2, 0, true, video2Data)...)

	if err := d.Feed(stream); err != nil {
		t.Fatalf("Feed: %v", err)
	}

	// Only track 1's frame should appear
	if len(*frames) != 1 {
		t.Fatalf("expected 1 frame (from first video track only), got %d", len(*frames))
	}
	if !bytes.Equal((*frames)[0].Data, video1Data) {
		t.Errorf("frame data: got %x, want %x", (*frames)[0].Data, video1Data)
	}
	if (*frames)[0].Codec != "VP9" {
		t.Errorf("codec: got %q, want %q", (*frames)[0].Codec, "VP9")
	}
}

func TestDemuxer_AllCodecMappings(t *testing.T) {
	// Table-driven: verify each Matroska CodecID maps to the correct
	// internal codec constant.
	tests := []struct {
		codecID string
		want    string
	}{
		{"V_VP8", "VP8"},
		{"V_VP9", "VP9"},
		{"V_AV1", "AV1"},
		{"V_MPEG4/ISO/AVC", "H264"},
		{"V_MPEGH/ISO/HEVC", "H265"},
		{"A_OPUS", "Opus"},
		{"A_FLAC", "FLAC"},
		{"A_AC3", "AC3"},
		{"A_EAC3", "EAC3"},
		{"A_AAC", "AAC"},
		{"A_MP3", "MP3"},
	}

	for _, tt := range tests {
		t.Run(tt.codecID, func(t *testing.T) {
			got, ok := codecMap[tt.codecID]
			if !ok {
				t.Fatalf("codecMap missing entry for %q", tt.codecID)
			}
			if got != tt.want {
				t.Errorf("codecMap[%q] = %q, want %q", tt.codecID, got, tt.want)
			}
		})
	}
}

// ─── Additional edge case tests ─────────────────────────────────────────────

func TestDemuxer_MatroskaDocType(t *testing.T) {
	// Verify "matroska" DocType is accepted (not just "webm").
	d, frames := newTestDemuxer()
	frameData := []byte{0xFF}

	var stream []byte
	stream = append(stream, buildMinimalStream("matroska", 1_000_000,
		buildTrackEntry(1, TrackTypeVideo, "V_MPEG4/ISO/AVC", []byte{0xCA, 0xFE}))...)
	stream = append(stream, buildClusterStart(0)...)
	stream = append(stream, buildSimpleBlockElement(1, 0, true, frameData)...)

	if err := d.Feed(stream); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if len(*frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(*frames))
	}
	if (*frames)[0].Codec != "H264" {
		t.Errorf("codec: got %q, want %q", (*frames)[0].Codec, "H264")
	}
}

func TestDemuxer_InvalidDocType(t *testing.T) {
	d, _ := newTestDemuxer()

	var stream []byte
	stream = append(stream, buildEBMLHeader("mp4")...)
	stream = append(stream, buildSegmentHeader()...)

	err := d.Feed(stream)
	if err == nil {
		t.Fatal("expected error for invalid DocType")
	}
}

func TestDemuxer_MultipleCluster(t *testing.T) {
	// Test transition between two Clusters with different base timecodes.
	d, frames := newTestDemuxer()
	frameData := []byte{0xAA}

	var stream []byte
	stream = append(stream, buildMinimalStream("webm", 1_000_000,
		buildTrackEntry(1, TrackTypeVideo, "V_VP9", nil))...)

	// First cluster at t=0
	stream = append(stream, buildClusterStart(0)...)
	stream = append(stream, buildSimpleBlockElement(1, 0, true, frameData)...)

	// Second cluster at t=1000
	stream = append(stream, buildClusterStart(1000)...)
	stream = append(stream, buildSimpleBlockElement(1, 0, true, frameData)...)

	if err := d.Feed(stream); err != nil {
		t.Fatalf("Feed: %v", err)
	}

	if len(*frames) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(*frames))
	}
	if (*frames)[0].Timestamp != 0 {
		t.Errorf("frame 0 timestamp: got %d, want 0", (*frames)[0].Timestamp)
	}
	if (*frames)[1].Timestamp != 1000 {
		t.Errorf("frame 1 timestamp: got %d, want 1000", (*frames)[1].Timestamp)
	}
}

func TestDemuxer_BlockGroup(t *testing.T) {
	// Test BlockGroup parsing with and without ReferenceBlock.
	d, frames := newTestDemuxer()
	frameData := []byte{0x01, 0x02}

	var stream []byte
	stream = append(stream, buildMinimalStream("webm", 1_000_000,
		buildTrackEntry(1, TrackTypeVideo, "V_VP9", nil))...)
	stream = append(stream, buildClusterStart(0)...)

	// Build a BlockGroup with no ReferenceBlock (keyframe)
	blockBody := buildSimpleBlock(1, 0, false, frameData) // flags don't matter for Block
	blockGroupBody := buildElement(IDBlock, blockBody)
	stream = append(stream, buildElement(IDBlockGroup, blockGroupBody)...)

	// Build a BlockGroup WITH ReferenceBlock (not a keyframe)
	blockBody2 := buildSimpleBlock(1, 33, false, frameData)
	var bgBody2 []byte
	bgBody2 = append(bgBody2, buildElement(IDBlock, blockBody2)...)
	bgBody2 = append(bgBody2, buildElement(idReferenceBlock, []byte{0xFF})...) // ref = -1
	stream = append(stream, buildElement(IDBlockGroup, bgBody2)...)

	if err := d.Feed(stream); err != nil {
		t.Fatalf("Feed: %v", err)
	}

	if len(*frames) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(*frames))
	}

	// First BlockGroup: no ReferenceBlock → keyframe
	if !(*frames)[0].IsKey {
		t.Error("frame 0 should be keyframe (no ReferenceBlock)")
	}

	// Second BlockGroup: has ReferenceBlock → not keyframe
	if (*frames)[1].IsKey {
		t.Error("frame 1 should not be keyframe (has ReferenceBlock)")
	}
}

func TestDemuxer_EmptyFeed(t *testing.T) {
	// Feeding empty data should be a no-op.
	d, _ := newTestDemuxer()
	if err := d.Feed([]byte{}); err != nil {
		t.Fatalf("Feed empty: %v", err)
	}
	if err := d.Feed(nil); err != nil {
		t.Fatalf("Feed nil: %v", err)
	}
}

func TestDemuxer_ByteByByteFeed(t *testing.T) {
	// Extreme partial feeding: one byte at a time.
	d, frames := newTestDemuxer()
	frameData := []byte{0xDE, 0xAD}

	var stream []byte
	stream = append(stream, buildMinimalStream("webm", 1_000_000,
		buildTrackEntry(1, TrackTypeVideo, "V_VP9", nil))...)
	stream = append(stream, buildClusterStart(0)...)
	stream = append(stream, buildSimpleBlockElement(1, 0, true, frameData)...)

	// Feed one byte at a time
	for i := 0; i < len(stream); i++ {
		if err := d.Feed(stream[i : i+1]); err != nil {
			t.Fatalf("Feed byte %d: %v", i, err)
		}
	}

	if len(*frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(*frames))
	}
	if !bytes.Equal((*frames)[0].Data, frameData) {
		t.Errorf("data mismatch")
	}
}

func TestDemuxer_DefaultTimecodeScale(t *testing.T) {
	// If no Info element is present, TimecodeScale should default to 1,000,000.
	d, frames := newTestDemuxer()
	frameData := []byte{0xAA}

	var stream []byte
	stream = append(stream, buildEBMLHeader("webm")...)
	stream = append(stream, buildSegmentHeader()...)
	// No Info element — go straight to Tracks
	stream = append(stream, buildTracks(
		buildTrackEntry(1, TrackTypeVideo, "V_VP9", nil))...)
	stream = append(stream, buildClusterStart(100)...)
	stream = append(stream, buildSimpleBlockElement(1, 0, true, frameData)...)

	if err := d.Feed(stream); err != nil {
		t.Fatalf("Feed: %v", err)
	}

	if len(*frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(*frames))
	}

	// Default timecodeScale: 100 * 1,000,000 / 1,000,000 = 100 ms
	if (*frames)[0].Timestamp != 100 {
		t.Errorf("timestamp: got %d, want 100", (*frames)[0].Timestamp)
	}
}

func TestDemuxer_UnknownTrackSkipped(t *testing.T) {
	// SimpleBlock for an unknown track number should be silently skipped.
	d, frames := newTestDemuxer()

	var stream []byte
	stream = append(stream, buildMinimalStream("webm", 1_000_000,
		buildTrackEntry(1, TrackTypeVideo, "V_VP9", nil))...)
	stream = append(stream, buildClusterStart(0)...)
	// SimpleBlock for track 99 (not in Tracks)
	stream = append(stream, buildSimpleBlockElement(99, 0, true, []byte{0xFF})...)
	// Followed by a valid track 1 block
	stream = append(stream, buildSimpleBlockElement(1, 0, true, []byte{0x42})...)

	if err := d.Feed(stream); err != nil {
		t.Fatalf("Feed: %v", err)
	}

	// Only track 1's frame should appear
	if len(*frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(*frames))
	}
	if !bytes.Equal((*frames)[0].Data, []byte{0x42}) {
		t.Errorf("wrong frame data")
	}
}

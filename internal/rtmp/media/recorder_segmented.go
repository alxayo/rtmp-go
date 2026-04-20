package media

// Segmented Media Recorder
// -------------------------
// Splits a continuous media stream into multiple files of configurable duration.
// Each segment is independently playable because sequence headers (codec init
// data) are re-injected at the start of every new segment.
//
// Segment boundaries align to video keyframes so players can start decoding
// immediately. For audio-only streams (no video), rotation occurs on any frame
// boundary once the target duration is reached.
//
// Usage:
//
//	nameFn := func() (string, error) { return "/path/to/segment_001.flv", nil }
//	sr := NewSegmentedRecorder(30000, "H264", nameFn, logger)
//	for msg := range messages {
//	    sr.WriteMessage(msg)
//	}
//	sr.Close()

import (
	"log/slog"
	"sync"

	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// SegmentNameFunc is called each time a new segment needs to be created.
// It returns the full file path for the next segment file.
// This callback is provided by the caller and will be wired to a SegmentNamer
// externally during integration.
type SegmentNameFunc func() (string, error)

// SegmentedRecorder splits a media stream into multiple files of configurable
// duration. It implements MediaWriter and wraps the existing FLV/MP4 recorders.
//
// How it works:
//  1. Each incoming media message is checked against the duration target
//  2. When target elapsed AND next video keyframe arrives → rotate to new file
//  3. Each new segment gets the cached sequence headers (so decoders can init)
//  4. On Close(), the current segment is finalized normally
//
// Segment boundaries align to video keyframes to ensure each segment is
// independently playable. For audio-only streams, rotation happens on any
// frame boundary after the target duration elapses.
type SegmentedRecorder struct {
	// mu protects all mutable state. The recorder may be called from the
	// media relay goroutine and closed from a different goroutine on disconnect.
	mu sync.Mutex

	// --- Configuration (immutable after construction) ---

	// segmentDuration is the target duration for each segment in milliseconds.
	// Actual segment length may exceed this slightly because we wait for a
	// video keyframe before rotating.
	segmentDuration uint32

	// codec identifies the video codec (e.g. "H264", "H265") and determines
	// which container format (FLV or MP4) the inner recorder uses.
	codec string

	// nameFn generates the file path for each new segment. It's called once
	// per segment rotation.
	nameFn SegmentNameFunc

	// logger is used for structured logging with context fields.
	logger *slog.Logger

	// meta holds FLV metadata (dimensions, codecs) passed to each inner recorder.
	meta FLVMetadata

	// OnSegmentComplete is called (if non-nil) after a segment file is
	// finalized and closed. It receives the file path, 1-based segment index,
	// and duration in milliseconds. The callback is invoked under the
	// recorder's mutex — implementations must not block or call back into
	// the recorder.
	OnSegmentComplete func(path string, index int, durationMs uint32)

	// --- Current segment state ---

	// current is the active inner recorder (FLV or MP4) for the current segment.
	// nil before the first segment is opened or after a fatal error.
	current MediaWriter

	// currentPath is the file path of the current segment being written.
	// Set by openSegmentLocked, used by rotateLocked to report completion.
	currentPath string

	// segmentStartTS is the RTMP timestamp (ms) of the first frame in the
	// current segment. Used to calculate elapsed time for rotation decisions.
	segmentStartTS uint32

	// firstTSSeen tracks whether we've received any timestamped message yet.
	// Until this is true, we can't compute elapsed duration.
	firstTSSeen bool

	// needKeyframe is set to true once the segment duration has been exceeded.
	// The recorder then waits for a video keyframe (or any audio frame in
	// audio-only streams) before actually rotating.
	needKeyframe bool

	// hasVideo is set to true once we've seen at least one video message.
	// This determines the rotation strategy: video streams wait for keyframes,
	// audio-only streams rotate immediately at the duration boundary.
	hasVideo bool

	// --- Cached sequence headers ---
	// These are the raw payloads of the most recent sequence header messages.
	// They are written into the beginning of each new segment so decoders
	// can initialize without needing the original segment.

	videoSeqHeader []byte // raw payload of the video sequence header
	audioSeqHeader []byte // raw payload of the audio sequence header

	// --- Stats ---

	// segmentCount tracks the total number of segments created so far.
	segmentCount int

	// disabled is set to true if a fatal error occurred (e.g. nameFn failed,
	// or creating a segment file failed). Once disabled, all writes are no-ops.
	disabled bool
}

// NewSegmentedRecorder creates a segmented recorder that splits media into
// multiple files of the given duration.
//
// Parameters:
//   - segmentDuration: target segment length in milliseconds (e.g. 30000 for 30s)
//   - codec: video codec string (e.g. "H264", "H265") for container format selection
//   - nameFn: callback that returns the file path for each new segment
//   - logger: structured logger (nil safe — uses slog.Default())
//   - meta: optional FLV metadata for the onMetaData tag in FLV segments
func NewSegmentedRecorder(segmentDuration uint32, codec string, nameFn SegmentNameFunc, logger *slog.Logger, meta ...FLVMetadata) *SegmentedRecorder {
	if logger == nil {
		logger = slog.Default()
	}

	var m FLVMetadata
	if len(meta) > 0 {
		m = meta[0]
	}

	return &SegmentedRecorder{
		segmentDuration: segmentDuration,
		codec:           codec,
		nameFn:          nameFn,
		logger:          logger,
		meta:            m,
	}
}

// WriteMessage processes each incoming audio/video message, handling segment
// rotation when the target duration is exceeded.
//
// The rotation logic:
//  1. Cache any sequence headers (codec config) — these are re-injected on rotation
//  2. Track the first timestamp to compute elapsed time
//  3. When elapsed >= segmentDuration, set a flag to wait for a keyframe
//  4. On keyframe arrival (or any audio frame for audio-only streams), rotate
//  5. Lazily open the first segment on the first non-sequence-header frame
//  6. Forward the message to the current inner recorder
func (s *SegmentedRecorder) WriteMessage(msg *chunk.Message) {
	if msg == nil {
		return
	}

	// Only process audio (TypeID=8) and video (TypeID=9) messages.
	if msg.TypeID != 8 && msg.TypeID != 9 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Once disabled (fatal error), all writes are silently dropped.
	if s.disabled {
		return
	}

	// --- Step 1: Cache sequence headers ---
	// Sequence headers contain codec initialization data (SPS/PPS for video,
	// AudioSpecificConfig for audio). We cache them so they can be written
	// at the start of each new segment.
	if msg.TypeID == 9 && len(msg.Payload) >= 2 && IsVideoSequenceHeader(msg.Payload) {
		// Make a copy so we don't hold a reference to the original buffer.
		s.videoSeqHeader = make([]byte, len(msg.Payload))
		copy(s.videoSeqHeader, msg.Payload)

		// Write sequence headers to the current segment if one is open.
		// They need to be in the file for decoders, but they don't affect
		// duration timing (they carry no displayable content).
		if s.current != nil {
			s.current.WriteMessage(msg)
		}
		return
	}

	if msg.TypeID == 8 && len(msg.Payload) >= 2 && IsAudioSequenceHeader(msg.Payload) {
		s.audioSeqHeader = make([]byte, len(msg.Payload))
		copy(s.audioSeqHeader, msg.Payload)

		if s.current != nil {
			s.current.WriteMessage(msg)
		}
		return
	}

	// --- Step 2: Track first timestamp ---
	// We need to know when this segment started so we can compute elapsed time.
	if !s.firstTSSeen {
		s.segmentStartTS = msg.Timestamp
		s.firstTSSeen = true
	}

	// --- Step 3: Track whether stream has video ---
	if msg.TypeID == 9 {
		s.hasVideo = true
	}

	// --- Step 4: Check if segment duration exceeded ---
	elapsed := msg.Timestamp - s.segmentStartTS
	if elapsed >= s.segmentDuration {
		s.needKeyframe = true
	}

	// --- Step 5: Rotate on keyframe (or audio boundary for audio-only) ---
	// For video streams: only rotate on a video keyframe so each segment
	// starts with an independently decodable frame.
	// For audio-only streams: rotate on any audio frame since there are
	// no keyframe boundaries to wait for.
	if s.needKeyframe {
		shouldRotate := false
		if msg.TypeID == 9 && isVideoKeyframe(msg.Payload) {
			shouldRotate = true
		} else if !s.hasVideo && msg.TypeID == 8 {
			// Audio-only stream: rotate on any audio frame
			shouldRotate = true
		}

		if shouldRotate {
			s.rotateLocked(msg.Timestamp)
		}
	}

	// --- Step 6: Lazy open first segment ---
	// We wait until the first real media frame (not sequence headers) to open
	// the first segment. This ensures segmentStartTS is set correctly.
	if s.current == nil {
		s.openSegmentLocked(msg.Timestamp)
		if s.disabled {
			return
		}
	}

	// --- Step 7: Forward to inner recorder ---
	s.current.WriteMessage(msg)
}

// Close finalizes the current segment. Any in-progress segment is properly
// closed so the file is a valid, playable media file.
func (s *SegmentedRecorder) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.current != nil {
		closedPath := s.currentPath
		closedIndex := s.segmentCount
		// For the final segment, estimate duration from last known timestamp.
		// This is approximate since we don't have the "next" timestamp.
		err := s.current.Close()
		if err == nil && s.OnSegmentComplete != nil {
			// Use 0 for duration of final segment — caller can compute from file metadata
			s.OnSegmentComplete(closedPath, closedIndex, 0)
		}
		s.current = nil
		return err
	}
	return nil
}

// Disabled returns true if a fatal error occurred (e.g. failed to create a
// segment file). Once disabled, all future WriteMessage calls are no-ops.
func (s *SegmentedRecorder) Disabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.disabled
}

// SegmentCount returns the total number of segments created so far.
// This is useful for monitoring and testing.
func (s *SegmentedRecorder) SegmentCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.segmentCount
}

// rotateLocked closes the current segment and opens a new one.
// It re-injects cached sequence headers into the new segment so decoders
// can initialize without the previous segment.
//
// Must be called with s.mu held.
func (s *SegmentedRecorder) rotateLocked(newStartTS uint32) {
	// Close the current segment (this finalizes the file — patches duration, etc.)
	if s.current != nil {
		closedPath := s.currentPath
		closedIndex := s.segmentCount
		durationMs := newStartTS - s.segmentStartTS

		if err := s.current.Close(); err != nil {
			s.logger.Error("segmented recorder: segment close error",
				"error", err,
				"segment", s.segmentCount,
			)
		} else if s.OnSegmentComplete != nil {
			s.OnSegmentComplete(closedPath, closedIndex, durationMs)
		}
		s.current = nil
	}

	// Open the new segment
	s.openSegmentLocked(newStartTS)
}

// openSegmentLocked creates a new inner recorder for the next segment.
// It calls the nameFn to get the file path, creates the recorder, increments
// the segment count, and writes cached sequence headers into the new segment.
//
// If any step fails, the recorder is marked as disabled (fatal error).
//
// Must be called with s.mu held.
func (s *SegmentedRecorder) openSegmentLocked(startTS uint32) {
	// Ask the naming callback for the next segment file path.
	path, err := s.nameFn()
	if err != nil {
		s.logger.Error("segmented recorder: name function failed",
			"error", err,
			"segment", s.segmentCount,
		)
		s.disabled = true
		return
	}

	// Create the inner recorder (FLV for H.264, MP4 for H.265+).
	// NewRecorder handles container format selection and file creation.
	recorder, err := NewRecorder(path, s.codec, s.logger, s.meta)
	if err != nil {
		s.logger.Error("segmented recorder: failed to create segment",
			"error", err,
			"path", path,
			"segment", s.segmentCount,
		)
		s.disabled = true
		return
	}

	s.current = recorder
	s.currentPath = path
	s.segmentCount++
	s.segmentStartTS = startTS
	s.needKeyframe = false

	// Re-inject cached sequence headers into the new segment.
	// These contain codec initialization data (SPS/PPS for video,
	// AudioSpecificConfig for audio) that decoders need before they
	// can process any frames. We use the new segment's start timestamp
	// and standard CSID/MSID values matching the RTMP convention.
	if s.videoSeqHeader != nil {
		s.current.WriteMessage(&chunk.Message{
			CSID:            6, // standard video CSID
			Timestamp:       startTS,
			TypeID:          9, // video
			MessageStreamID: 1,
			Payload:         s.videoSeqHeader,
			MessageLength:   uint32(len(s.videoSeqHeader)),
		})
	}
	if s.audioSeqHeader != nil {
		s.current.WriteMessage(&chunk.Message{
			CSID:            4, // standard audio CSID
			Timestamp:       startTS,
			TypeID:          8, // audio
			MessageStreamID: 1,
			Payload:         s.audioSeqHeader,
			MessageLength:   uint32(len(s.audioSeqHeader)),
		})
	}
}

// isVideoKeyframe checks if a video message payload represents a keyframe
// (an independently decodable frame, also known as an I-frame or IDR frame).
//
// Supports both legacy FLV and Enhanced RTMP formats:
//   - Legacy: The top 4 bits of byte 0 encode the frame type. frameType=1 means keyframe.
//   - Enhanced RTMP: Bit 7 of byte 0 is the IsExHeader flag. When set, bits [6:4]
//     encode the frame type (3 bits instead of 4). frameType=1 still means keyframe.
func isVideoKeyframe(payload []byte) bool {
	if len(payload) == 0 {
		return false
	}

	b0 := payload[0]

	// Check if this is an Enhanced RTMP packet (bit 7 set).
	isExHeader := (b0 >> 7) & 1
	if isExHeader == 1 {
		// Enhanced RTMP: frame type is in bits [6:4] (3 bits)
		frameType := (b0 >> 4) & 0x07
		return frameType == 1 // 1 = keyframe
	}

	// Legacy FLV: frame type is in bits [7:4] (4 bits)
	frameType := (b0 >> 4) & 0x0F
	return frameType == 1 // 1 = keyframe
}

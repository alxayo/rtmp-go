package mkv

// This file implements a streaming Matroska/WebM demuxer that processes EBML
// elements incrementally. It is designed for live streaming over SRT, where
// data arrives in arbitrary-sized chunks.
//
// # How it works
//
// The demuxer is a state machine with four states:
//
//   - stateEBMLHeader: Waiting for the EBML header (document type declaration)
//     and Segment element. Every Matroska stream starts with these.
//
//   - stateTopLevel: Parsing Segment-level children like Info (which contains
//     TimecodeScale) and Tracks (which describes the audio/video codecs).
//     Transitions to stateStreaming when the first Cluster is found.
//
//   - stateStreaming: The hot path. Parses Cluster children — Timecode elements
//     (base timestamp for the cluster) and SimpleBlock/BlockGroup elements
//     (actual media frames). Each SimpleBlock is decoded to extract the track
//     number, timestamp offset, keyframe flag, and raw codec data.
//
//   - stateSkipping: Consumes bytes for elements we don't need (SeekHead,
//     Cues, Tags, etc.). Handles elements that span multiple Feed() calls.
//
// # Feed-based API
//
// Data is fed via Feed(data) in chunks of any size. The demuxer maintains an
// internal buffer and handles element boundaries that fall between Feed calls.
// Parsed frames are delivered via a FrameHandler callback.
//
// # Timestamp calculation
//
// Matroska timestamps work in two layers:
//   - Each Cluster has a base timecode (in TimecodeScale units)
//   - Each SimpleBlock has a signed 16-bit offset from the Cluster timecode
//   - The final millisecond timestamp = (clusterTC + offset) * timecodeScale / 1,000,000
//
// The default TimecodeScale is 1,000,000 ns (= 1 ms per unit), so cluster
// timecodes are typically already in milliseconds.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
)

// ─── Errors ─────────────────────────────────────────────────────────────────

// errNeedMoreData is an internal sentinel returned by state handlers when the
// buffer doesn't contain enough data to parse the next element. Feed() catches
// this and returns nil to the caller, waiting for the next Feed() call.
var errNeedMoreData = errors.New("need more data")

// errBufferOverflow is returned when the internal buffer exceeds the maximum
// allowed size (2 MB). This protects against malformed streams that never
// produce valid elements, which would cause unbounded memory growth.
var errBufferOverflow = errors.New("mkv: buffer overflow (>2MB)")

// maxBufferSize is the hard cap on the internal buffer. If the buffer grows
// beyond this without the demuxer making progress, something is wrong.
const maxBufferSize = 2 * 1024 * 1024

// idReferenceBlock is the EBML element ID for ReferenceBlock (0xFB), used
// inside BlockGroup to indicate that a block references another block.
// If no ReferenceBlock is present, the block is a keyframe.
const idReferenceBlock uint32 = 0xFB

// ─── Codec mapping ──────────────────────────────────────────────────────────

// codecMap translates Matroska CodecID strings (from TrackEntry elements)
// to internal codec constants used throughout the server. These constants
// match the ones in internal/rtmp/media/video.go and audio.go.
//
// Matroska CodecID format: "V_" prefix for video, "A_" prefix for audio.
var codecMap = map[string]string{
	"V_VP8":            "VP8",
	"V_VP9":            "VP9",
	"V_AV1":            "AV1",
	"V_MPEG4/ISO/AVC":  "H264",
	"V_MPEGH/ISO/HEVC": "H265",
	"V_MPEGH/ISO/VVC":  "VVC",
	"A_OPUS":           "Opus",
	"A_FLAC":           "FLAC",
	"A_AC3":            "AC3",
	"A_EAC3":           "EAC3",
	"A_AAC":            "AAC",
	"A_MP3":            "MP3",
	"A_VORBIS":         "Vorbis",
}

// ─── Frame ──────────────────────────────────────────────────────────────────

// Frame represents a single media frame extracted from a Matroska stream.
// It is the MKV equivalent of ts.MediaFrame but uses millisecond timestamps
// (after TimecodeScale conversion) instead of 90kHz clock units.
type Frame struct {
	// Codec is the internal codec constant (e.g., "VP9", "Opus", "H264").
	// Mapped from the Matroska CodecID string (e.g., "V_VP9" → "VP9").
	Codec string

	// IsVideo is true for video frames, false for audio frames.
	IsVideo bool

	// Timestamp is the presentation timestamp in milliseconds.
	// Calculated as: (clusterTimecode + blockTimecodeOffset) * timecodeScale / 1_000_000
	Timestamp int64

	// Data contains the raw codec frame data.
	// For H.264/H.265: length-prefixed NALUs (NOT Annex B — different from MPEG-TS!)
	// For VP8/VP9/AV1: raw codec frame
	// For AAC: raw AAC frame (NOT ADTS — different from MPEG-TS!)
	// For Opus/FLAC/AC-3/E-AC-3: raw codec frame
	Data []byte

	// IsKey is true for keyframes (from SimpleBlock flags byte, bit 7).
	IsKey bool

	// CodecPrivate is the codec initialization data from the track definition.
	// Non-nil only on the FIRST frame for each track — the bridge uses this
	// to build the Enhanced RTMP sequence header. Nil for subsequent frames.
	CodecPrivate []byte
}

// FrameHandler is the callback for delivering parsed frames.
type FrameHandler func(frame *Frame)

// ─── Track info ─────────────────────────────────────────────────────────────

// trackInfo holds parsed metadata for a single track from the Tracks element.
// The demuxer populates one of these for each TrackEntry it finds, then
// selects the first video and first audio track for frame extraction.
type trackInfo struct {
	number       uint8   // Track number (1-based, from SimpleBlock VINT)
	trackType    uint8   // TrackTypeVideo=1 or TrackTypeAudio=2
	codecID      string  // Matroska CodecID string (e.g., "V_VP9", "A_OPUS")
	codec        string  // Internal codec constant (e.g., "VP9", "Opus")
	codecPrivate []byte  // Decoder init data (may be nil for VP8/VP9)
	codecDelay   uint64  // Nanoseconds (for Opus pre-skip)
	seekPreRoll  uint64  // Nanoseconds
	configSent   bool    // True after we've emitted the first frame with CodecPrivate
	sampleRate   float64 // Audio sampling frequency in Hz
	channels     uint8   // Number of audio channels
	bitDepth     uint8   // Bits per audio sample
}

// ─── Demuxer state machine ──────────────────────────────────────────────────

// demuxerState represents which phase of parsing the demuxer is in.
// The state machine progresses linearly: EBMLHeader → TopLevel → Streaming,
// with Skipping as a temporary state that returns to the previous state.
type demuxerState int

const (
	stateEBMLHeader demuxerState = iota // Waiting for EBML header element
	stateTopLevel                       // Parsing Segment top-level children
	stateStreaming                      // Parsing Clusters and SimpleBlocks
	stateSkipping                       // Skipping an unknown/unneeded element body
)

// ─── Demuxer ────────────────────────────────────────────────────────────────

// Demuxer processes a Matroska/WebM byte stream and emits media frames.
// It is designed for streaming: data is fed in chunks of any size via Feed(),
// and the demuxer handles element boundary alignment internally.
//
// Unlike a file-based parser, this demuxer handles:
//   - Unknown-size Segment elements (streaming mode)
//   - Partial elements split across Feed() calls
//   - Incremental parsing of top-level elements (skips SeekHead, Cues, Tags, etc.)
type Demuxer struct {
	// buf holds accumulated data from Feed() calls.
	// We parse from buf[cursor:] and compact periodically.
	buf    []byte
	cursor int

	// state tracks where we are in the parsing state machine.
	state demuxerState

	// handler is called for each complete media frame.
	handler FrameHandler

	// timecodeScale is nanoseconds per Matroska timecode unit.
	// Default is 1,000,000 (meaning each timecode unit = 1 ms).
	// Parsed from Info > TimecodeScale element.
	timecodeScale uint64

	// tracks maps track number → track info, populated from the Tracks element.
	tracks map[uint8]*trackInfo

	// videoTrack and audioTrack are the selected tracks (first of each type).
	// We only support one video + one audio track; extras are skipped.
	videoTrack *trackInfo
	audioTrack *trackInfo

	// clusterTimecode is the base timestamp for the current Cluster
	// (in timecodeScale units).
	clusterTimecode uint64

	// skipRemaining is the number of bytes left to skip in the current
	// element when in stateSkipping.
	skipRemaining int64

	// skipReturnState is the state to return to after finishing a skip.
	skipReturnState demuxerState

	// ebmlParsed is true after the EBML header has been successfully parsed.
	// Used to track progress within stateEBMLHeader (which handles both
	// the EBML header and the Segment header).
	ebmlParsed bool

	// segmentFound is true after the Segment element header has been read.
	segmentFound bool

	// tracksFound is true after the Tracks element has been parsed.
	tracksFound bool

	// log for debug output.
	log *slog.Logger
}

// NewDemuxer creates a new Matroska demuxer that calls handler for each
// complete media frame. Pass a slog.Logger for debug output (or slog.Default()).
func NewDemuxer(handler FrameHandler, log *slog.Logger) *Demuxer {
	return &Demuxer{
		handler:       handler,
		timecodeScale: 1_000_000, // Default: 1ms per timecode unit
		tracks:        make(map[uint8]*trackInfo),
		log:           log,
	}
}

// ─── Feed ───────────────────────────────────────────────────────────────────

// Feed processes raw data from the SRT connection.
// Data doesn't need to be aligned — the demuxer handles partial elements
// across calls. Returns an error for unrecoverable parse failures.
func (d *Demuxer) Feed(data []byte) error {
	// Append incoming data to the internal buffer.
	d.buf = append(d.buf, data...)

	// Enforce hard buffer cap to protect against malformed streams.
	if len(d.buf) > maxBufferSize {
		return errBufferOverflow
	}

	// Main parse loop: keep parsing elements until we need more data.
	// Each iteration of this loop processes one element and advances the
	// cursor. The loop exits when a state handler returns errNeedMoreData
	// (not enough bytes for the next element) or a real error.
	for {
		var err error

		switch d.state {
		case stateEBMLHeader:
			err = d.parseEBMLHeader()
		case stateTopLevel:
			err = d.parseTopLevel()
		case stateStreaming:
			err = d.parseStreaming()
		case stateSkipping:
			err = d.processSkipping()
		}

		if err == errNeedMoreData {
			d.compact()
			return nil
		}
		if err != nil {
			return err
		}
		// Successfully parsed something — loop to try the next element.
	}
}

// ─── State: EBML Header ────────────────────────────────────────────────────

// parseEBMLHeader handles two sequential parse steps:
//  1. The EBML header element (validates DocType is "matroska" or "webm")
//  2. The Segment element header (the root container for all media data)
//
// After both are parsed, the state transitions to stateTopLevel.
func (d *Demuxer) parseEBMLHeader() error {
	// Step 1: Parse the EBML header if we haven't yet.
	if !d.ebmlParsed {
		data := d.buf[d.cursor:]

		// Try to read the element header (ID + size).
		id, size, hdrLen, err := ReadElementHeader(data)
		if err == ErrBufferTooShort {
			return errNeedMoreData
		}
		if err != nil {
			return fmt.Errorf("mkv: failed to read EBML header: %w", err)
		}

		// The very first element must be the EBML header.
		if id != IDEBMLHeader {
			return fmt.Errorf("mkv: expected EBML header (0x%X), got 0x%X (%s)",
				IDEBMLHeader, id, ElementName(id))
		}

		// EBML header must have a known size (it's a small element).
		if size == UnknownSize {
			return fmt.Errorf("mkv: EBML header has unknown size")
		}

		// Wait until we have the complete EBML header body.
		totalLen := hdrLen + int(size)
		if len(data) < totalLen {
			return errNeedMoreData
		}

		// Parse children to extract and validate DocType.
		body := data[hdrLen:totalLen]
		docType, err := d.parseEBMLHeaderBody(body)
		if err != nil {
			return err
		}

		if docType != "matroska" && docType != "webm" {
			return fmt.Errorf("mkv: unsupported DocType %q (expected \"matroska\" or \"webm\")", docType)
		}

		d.log.Debug("EBML header parsed", "docType", docType)
		d.cursor += totalLen
		d.ebmlParsed = true
	}

	// Step 2: Parse the Segment element header.
	data := d.buf[d.cursor:]

	id, size, hdrLen, err := ReadElementHeader(data)
	if err == ErrBufferTooShort {
		return errNeedMoreData
	}
	if err != nil {
		return fmt.Errorf("mkv: failed to read Segment header: %w", err)
	}

	if id != IDSegment {
		return fmt.Errorf("mkv: expected Segment (0x%X), got 0x%X (%s)",
			IDSegment, id, ElementName(id))
	}

	d.log.Debug("Segment found", "size", size)

	// Advance past the Segment header only — the body contains all media
	// data and will be parsed element-by-element in subsequent states.
	d.cursor += hdrLen
	d.segmentFound = true
	d.state = stateTopLevel

	return nil
}

// parseEBMLHeaderBody scans the children of the EBML header element to
// find the DocType string. Other children (EBMLVersion, etc.) are skipped.
func (d *Demuxer) parseEBMLHeaderBody(body []byte) (string, error) {
	docType := ""
	pos := 0

	for pos < len(body) {
		id, size, hdrLen, err := ReadElementHeader(body[pos:])
		if err != nil {
			// Truncated child — stop parsing but don't fail.
			break
		}
		if size == UnknownSize {
			break
		}

		childEnd := pos + hdrLen + int(size)
		if childEnd > len(body) {
			break
		}

		if id == IDDocType {
			docType = ReadString(body[pos+hdrLen:], int(size))
		}

		pos = childEnd
	}

	if docType == "" {
		return "", fmt.Errorf("mkv: EBML header missing DocType element")
	}

	return docType, nil
}

// ─── State: Top Level ──────────────────────────────────────────────────────

// parseTopLevel processes Segment-level children: Info (for TimecodeScale),
// Tracks (for codec discovery), and transitions to streaming when the first
// Cluster is found. All other elements (SeekHead, Cues, Tags, etc.) are skipped.
func (d *Demuxer) parseTopLevel() error {
	data := d.buf[d.cursor:]

	id, size, hdrLen, err := ReadElementHeader(data)
	if err == ErrBufferTooShort {
		return errNeedMoreData
	}
	if err != nil {
		return fmt.Errorf("mkv: parse error at top level: %w", err)
	}

	switch id {
	case IDInfo:
		// Info contains TimecodeScale and optionally Duration.
		// It always has a known size — read the full body.
		if size == UnknownSize {
			return fmt.Errorf("mkv: Info element has unknown size")
		}
		totalLen := hdrLen + int(size)
		if len(data) < totalLen {
			return errNeedMoreData
		}
		d.parseInfoBody(data[hdrLen:totalLen])
		d.cursor += totalLen
		return nil

	case IDTracks:
		// Tracks contains TrackEntry children that describe each stream.
		// Must have a known size — we need the full body.
		if size == UnknownSize {
			return fmt.Errorf("mkv: Tracks element has unknown size")
		}
		totalLen := hdrLen + int(size)
		if len(data) < totalLen {
			return errNeedMoreData
		}
		d.parseTracksBody(data[hdrLen:totalLen])
		d.tracksFound = true
		d.cursor += totalLen
		d.log.Debug("tracks parsed",
			"count", len(d.tracks),
			"video", d.videoTrack != nil,
			"audio", d.audioTrack != nil)
		return nil

	case IDCluster:
		// First Cluster found — transition to streaming mode.
		// Advance past just the Cluster header (ID + size); the children
		// (Timecode, SimpleBlock, etc.) will be parsed in stateStreaming.
		d.cursor += hdrLen
		d.state = stateStreaming
		d.log.Debug("first Cluster found, entering streaming mode")
		return nil

	default:
		// Skip everything else: SeekHead, Cues, Tags, Attachments, Chapters,
		// Void, CRC-32, and any unknown elements.
		if size == UnknownSize {
			return fmt.Errorf("mkv: unknown-size element 0x%X (%s) at top level",
				id, ElementName(id))
		}
		d.log.Debug("skipping top-level element",
			"id", fmt.Sprintf("0x%X", id),
			"name", ElementName(id),
			"size", size)
		d.skipRemaining = size
		d.skipReturnState = stateTopLevel
		d.cursor += hdrLen
		d.state = stateSkipping
		return nil
	}
}

// parseInfoBody scans the children of the Info element to extract the
// TimecodeScale value. Other Info children (Duration, MuxingApp, etc.)
// are skipped.
func (d *Demuxer) parseInfoBody(body []byte) {
	pos := 0
	for pos < len(body) {
		id, size, hdrLen, err := ReadElementHeader(body[pos:])
		if err != nil {
			break
		}
		if size == UnknownSize {
			break
		}
		childEnd := pos + hdrLen + int(size)
		if childEnd > len(body) {
			break
		}

		if id == IDTimecodeScale {
			d.timecodeScale = ReadUint(body[pos+hdrLen:], int(size))
			d.log.Debug("TimecodeScale", "ns_per_unit", d.timecodeScale)
		}

		pos = childEnd
	}
}

// ─── Track parsing ──────────────────────────────────────────────────────────

// parseTracksBody scans the children of the Tracks element for TrackEntry
// elements, parses each one, and registers it with the demuxer.
func (d *Demuxer) parseTracksBody(body []byte) {
	pos := 0
	for pos < len(body) {
		id, size, hdrLen, err := ReadElementHeader(body[pos:])
		if err != nil {
			break
		}
		if size == UnknownSize {
			break
		}
		childEnd := pos + hdrLen + int(size)
		if childEnd > len(body) {
			break
		}

		if id == IDTrackEntry {
			track := d.parseTrackEntryBody(body[pos+hdrLen : childEnd])
			if track != nil {
				d.registerTrack(track)
			}
		}

		pos = childEnd
	}
}

// parseTrackEntryBody parses a single TrackEntry element body to extract
// track number, type, codec, and optional CodecPrivate data.
func (d *Demuxer) parseTrackEntryBody(body []byte) *trackInfo {
	track := &trackInfo{}
	pos := 0

	for pos < len(body) {
		id, size, hdrLen, err := ReadElementHeader(body[pos:])
		if err != nil {
			break
		}
		if size == UnknownSize {
			break
		}
		childEnd := pos + hdrLen + int(size)
		if childEnd > len(body) {
			break
		}

		elemData := body[pos+hdrLen : childEnd]

		switch id {
		case IDTrackNumber:
			track.number = uint8(ReadUint(elemData, int(size)))
		case IDTrackType:
			track.trackType = uint8(ReadUint(elemData, int(size)))
		case IDCodecID:
			track.codecID = ReadString(elemData, int(size))
		case IDCodecPrivate:
			track.codecPrivate = make([]byte, size)
			copy(track.codecPrivate, elemData)
		case IDCodecDelay:
			track.codecDelay = ReadUint(elemData, int(size))
		case IDSeekPreRoll:
			track.seekPreRoll = ReadUint(elemData, int(size))
		case IDAudio:
			d.parseAudioBody(track, elemData)
		}

		pos = childEnd
	}

	// Map the Matroska CodecID to our internal codec name.
	if mapped, ok := codecMap[track.codecID]; ok {
		track.codec = mapped
	} else if track.codecID != "" {
		d.log.Debug("unknown codec", "codecID", track.codecID)
	}

	return track
}

// parseAudioBody extracts audio-specific parameters (sample rate, channels,
// bit depth) from an Audio sub-element inside a TrackEntry.
func (d *Demuxer) parseAudioBody(track *trackInfo, body []byte) {
	pos := 0
	for pos < len(body) {
		id, size, hdrLen, err := ReadElementHeader(body[pos:])
		if err != nil {
			break
		}
		if size == UnknownSize {
			break
		}
		childEnd := pos + hdrLen + int(size)
		if childEnd > len(body) {
			break
		}

		elemData := body[pos+hdrLen : childEnd]

		switch id {
		case IDSamplingFreq:
			track.sampleRate = ReadFloat(elemData, int(size))
		case IDChannels:
			track.channels = uint8(ReadUint(elemData, int(size)))
		case IDBitDepth:
			track.bitDepth = uint8(ReadUint(elemData, int(size)))
		}

		pos = childEnd
	}
}

// registerTrack adds a parsed track to the demuxer's track map and selects
// the first video and first audio track for frame extraction. Extra tracks
// of the same type are logged and skipped.
func (d *Demuxer) registerTrack(track *trackInfo) {
	d.tracks[track.number] = track

	switch track.trackType {
	case TrackTypeVideo:
		if d.videoTrack == nil {
			d.videoTrack = track
			d.log.Debug("video track selected",
				"track", track.number,
				"codec", track.codecID,
				"mapped", track.codec)
		} else {
			d.log.Debug("extra video track skipped",
				"track", track.number,
				"codec", track.codecID)
		}
	case TrackTypeAudio:
		if d.audioTrack == nil {
			d.audioTrack = track
			d.log.Debug("audio track selected",
				"track", track.number,
				"codec", track.codecID,
				"mapped", track.codec)
		} else {
			d.log.Debug("extra audio track skipped",
				"track", track.number,
				"codec", track.codecID)
		}
	default:
		d.log.Debug("non-media track skipped",
			"track", track.number,
			"type", track.trackType)
	}
}

// ─── State: Streaming ──────────────────────────────────────────────────────

// parseStreaming is the hot-path state handler. It processes elements inside
// Clusters: Timecode (base timestamp), SimpleBlock (media frames), and
// BlockGroup (media frames with reference info). New Cluster elements are
// handled by advancing past their header.
func (d *Demuxer) parseStreaming() error {
	data := d.buf[d.cursor:]

	id, size, hdrLen, err := ReadElementHeader(data)
	if err == ErrBufferTooShort {
		return errNeedMoreData
	}
	if err != nil {
		return fmt.Errorf("mkv: parse error in streaming: %w", err)
	}

	switch id {
	case IDCluster:
		// New Cluster — advance past its header. The Cluster's children
		// (Timecode, SimpleBlocks) will be parsed in subsequent iterations.
		d.cursor += hdrLen
		d.log.Debug("new Cluster")
		return nil

	case IDTimecode:
		// Cluster base timestamp. All SimpleBlock offsets in this Cluster
		// are relative to this value.
		if size == UnknownSize || int64(len(data)-hdrLen) < size {
			return errNeedMoreData
		}
		d.clusterTimecode = ReadUint(data[hdrLen:], int(size))
		d.cursor += hdrLen + int(size)
		return nil

	case IDSimpleBlock:
		// The most common media frame element. Contains track number,
		// timestamp offset, flags (keyframe), and raw codec data.
		if size == UnknownSize || int64(len(data)-hdrLen) < size {
			return errNeedMoreData
		}
		body := data[hdrLen : hdrLen+int(size)]
		if err := d.parseBlockData(body, true, false); err != nil {
			return err
		}
		d.cursor += hdrLen + int(size)
		return nil

	case IDBlockGroup:
		// Less common frame container that includes reference info.
		// Must have a known size — we parse its children to find the Block
		// element and check for ReferenceBlock (keyframe detection).
		if size == UnknownSize || int64(len(data)-hdrLen) < size {
			return errNeedMoreData
		}
		body := data[hdrLen : hdrLen+int(size)]
		if err := d.parseBlockGroup(body); err != nil {
			return err
		}
		d.cursor += hdrLen + int(size)
		return nil

	default:
		// Skip Void, CRC-32, and any other unknown elements.
		if size == UnknownSize {
			return fmt.Errorf("mkv: unknown-size element 0x%X (%s) in stream",
				id, ElementName(id))
		}
		d.skipRemaining = size
		d.skipReturnState = stateStreaming
		d.cursor += hdrLen
		d.state = stateSkipping
		return nil
	}
}

// ─── Block parsing ──────────────────────────────────────────────────────────

// parseBlockData parses the body of a SimpleBlock or Block element. Both have
// the same binary layout:
//
//	[track number VINT] [timecode offset int16 BE] [flags byte] [frame data...]
//
// The difference is that for SimpleBlock, the keyframe flag is in the flags
// byte (bit 7), while for Block (inside BlockGroup), keyframe is determined
// externally by the absence of ReferenceBlock elements.
//
// The isSimpleBlock parameter controls which keyframe detection method is used.
// The blockGroupKeyframe parameter is only used when isSimpleBlock is false.
func (d *Demuxer) parseBlockData(body []byte, isSimpleBlock bool, blockGroupKeyframe bool) error {
	// Minimum: 1 byte track VINT + 2 bytes offset + 1 byte flags = 4 bytes
	if len(body) < 4 {
		return fmt.Errorf("mkv: block too short (%d bytes)", len(body))
	}

	// Read track number as a VINT value (marker bit masked out).
	// For track 1: 0x81 → value 1. For track 2: 0x82 → value 2.
	trackNumVal, vintWidth, err := ReadVINTValue(body)
	if err != nil {
		return fmt.Errorf("mkv: block track number: %w", err)
	}
	if trackNumVal == UnknownSize || trackNumVal <= 0 || trackNumVal > 255 {
		return fmt.Errorf("mkv: invalid track number %d", trackNumVal)
	}
	trackNum := uint8(trackNumVal)

	pos := vintWidth

	// Need at least 3 more bytes: 2 for timecode offset + 1 for flags.
	if len(body) < pos+3 {
		return fmt.Errorf("mkv: block too short after track number")
	}

	// Read the signed 16-bit timecode offset (big-endian).
	// This is relative to the Cluster's base timecode. Can be negative
	// for B-frames or audio frames that precede the Cluster's keyframe.
	timecodeOffset := int16(binary.BigEndian.Uint16(body[pos : pos+2]))
	pos += 2

	// Read the flags byte:
	//   Bit 7 (0x80): Keyframe (SimpleBlock only)
	//   Bit 3 (0x08): Invisible
	//   Bits 2-1:     Lacing type (0=none, 1=Xiph, 2=fixed-size, 3=EBML)
	//   Bit 0 (0x01): Discardable
	flags := body[pos]
	pos++

	// Determine keyframe status.
	keyframe := false
	if isSimpleBlock {
		keyframe = (flags & 0x80) != 0
	} else {
		keyframe = blockGroupKeyframe
	}

	// Check lacing type. We support no-lacing (0) and fixed-size lacing (2).
	// Xiph lacing (1) and EBML lacing (3) are rarely used and not supported.
	lacingType := (flags >> 1) & 0x03

	// Look up the track. Skip if unknown or not a selected track.
	track := d.tracks[trackNum]
	if track == nil {
		return nil
	}
	if track != d.videoTrack && track != d.audioTrack {
		return nil
	}

	frameData := body[pos:]

	switch lacingType {
	case 0:
		// No lacing — the remaining data is a single frame.
		d.emitFrame(track, timecodeOffset, keyframe, frameData)

	case 2:
		// Fixed-size lacing: all frames have equal size.
		// First byte = frame count - 1 (so 0x01 means 2 frames).
		if len(frameData) < 1 {
			return fmt.Errorf("mkv: fixed-size lacing missing frame count")
		}
		frameCount := int(frameData[0]) + 1
		frameData = frameData[1:]

		if frameCount == 0 || len(frameData) == 0 || len(frameData)%frameCount != 0 {
			return fmt.Errorf("mkv: fixed-size lacing: %d bytes not divisible by %d frames",
				len(frameData), frameCount)
		}
		frameSize := len(frameData) / frameCount

		for i := 0; i < frameCount; i++ {
			// Only the first frame in a laced block inherits the keyframe flag.
			isKey := keyframe && i == 0
			d.emitFrame(track, timecodeOffset, isKey, frameData[i*frameSize:(i+1)*frameSize])
		}

	case 1, 3:
		// Xiph lacing (1) and EBML lacing (3) are not supported.
		return fmt.Errorf("mkv: unsupported lacing mode %d", lacingType)
	}

	return nil
}

// parseBlockGroup parses a BlockGroup element body. A BlockGroup contains:
//   - Block (0xA1): the media data (same format as SimpleBlock, minus keyframe flag)
//   - ReferenceBlock (0xFB): if present, indicates this is NOT a keyframe
//
// If no ReferenceBlock is found, the block is treated as a keyframe.
func (d *Demuxer) parseBlockGroup(body []byte) error {
	var blockBody []byte
	isKey := true // Assume keyframe unless ReferenceBlock says otherwise.

	pos := 0
	for pos < len(body) {
		id, size, hdrLen, err := ReadElementHeader(body[pos:])
		if err != nil {
			break
		}
		if size == UnknownSize {
			break
		}
		childEnd := pos + hdrLen + int(size)
		if childEnd > len(body) {
			break
		}

		switch id {
		case IDBlock:
			blockBody = body[pos+hdrLen : childEnd]
		case idReferenceBlock:
			isKey = false
		}

		pos = childEnd
	}

	if blockBody == nil {
		return nil
	}

	return d.parseBlockData(blockBody, false, isKey)
}

// ─── Frame emission ─────────────────────────────────────────────────────────

// emitFrame creates a Frame from parsed block data and calls the handler.
// On the first frame for each track, CodecPrivate is included so the
// downstream bridge can construct codec initialization headers.
func (d *Demuxer) emitFrame(track *trackInfo, timecodeOffset int16, keyframe bool, data []byte) {
	if d.handler == nil || len(data) == 0 {
		return
	}

	// Calculate the final timestamp in milliseconds.
	// Formula: ms = (clusterTimecode + blockOffset) * timecodeScale / 1,000,000
	//
	// The cluster timecode is unsigned, but the block offset is signed (int16),
	// so we convert to int64 for the arithmetic. With the default timecodeScale
	// of 1,000,000 ns, this simplifies to ms = clusterTimecode + blockOffset.
	rawTC := int64(d.clusterTimecode) + int64(timecodeOffset)
	timestampMs := rawTC * int64(d.timecodeScale) / 1_000_000

	frame := &Frame{
		Codec:     track.codec,
		IsVideo:   track.trackType == TrackTypeVideo,
		Timestamp: timestampMs,
		Data:      make([]byte, len(data)),
		IsKey:     keyframe,
	}
	copy(frame.Data, data)

	// Include CodecPrivate on the first frame for this track.
	if !track.configSent {
		if len(track.codecPrivate) > 0 {
			frame.CodecPrivate = make([]byte, len(track.codecPrivate))
			copy(frame.CodecPrivate, track.codecPrivate)
		}
		track.configSent = true
	}

	d.handler(frame)
}

// ─── State: Skipping ────────────────────────────────────────────────────────

// processSkipping consumes bytes for an element we don't need. This handles
// elements whose body spans multiple Feed() calls — we consume what's
// available and track the remainder.
func (d *Demuxer) processSkipping() error {
	avail := int64(len(d.buf) - d.cursor)

	if avail == 0 {
		return errNeedMoreData
	}

	if avail >= d.skipRemaining {
		// All remaining bytes are available — finish the skip.
		d.cursor += int(d.skipRemaining)
		d.skipRemaining = 0
		d.state = d.skipReturnState
		return nil
	}

	// Consume what we have and wait for more data.
	d.skipRemaining -= avail
	d.cursor += int(avail)
	return errNeedMoreData
}

// ─── Buffer management ──────────────────────────────────────────────────────

// compact shifts unprocessed data to the front of the buffer to reclaim
// memory. This is called when we're waiting for more data and the cursor
// has advanced past a significant portion of the buffer.
func (d *Demuxer) compact() {
	if d.cursor > len(d.buf)/2 && d.cursor > 4096 {
		remaining := len(d.buf) - d.cursor
		copy(d.buf, d.buf[d.cursor:])
		d.buf = d.buf[:remaining]
		d.cursor = 0
	}
}

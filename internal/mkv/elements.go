package mkv

import "fmt"

// This file defines the EBML element IDs used when parsing Matroska/WebM
// streams. Each element in a Matroska file is identified by a unique
// variable-length integer ID. These constants represent the IDs we need
// to recognize during streaming demux.
//
// The values come from the Matroska specification:
// https://www.matroska.org/technical/elements.html
//
// Note: EBML element IDs include their width marker bit. For example,
// the SimpleBlock ID is 0xA3 — the leading '1' bit in 10100011 is the
// VINT width marker for a 1-byte ID, but it's kept as part of the value.

// EBML Header elements — these appear at the very beginning of every
// Matroska/WebM file and declare the document format and version.
const (
	// IDEBMLHeader is the root element of the EBML header (master element).
	IDEBMLHeader uint32 = 0x1A45DFA3

	// IDEBMLVersion is the EBML version used to create the file.
	IDEBMLVersion uint32 = 0x4286

	// IDEBMLReadVersion is the minimum EBML version a reader needs to support.
	IDEBMLReadVersion uint32 = 0x42F7

	// IDEBMLMaxIDLength is the maximum length (in bytes) of element IDs in this file.
	IDEBMLMaxIDLength uint32 = 0x42F2

	// IDEBMLMaxSizeLength is the maximum length (in bytes) of element sizes in this file.
	IDEBMLMaxSizeLength uint32 = 0x42F3

	// IDDocType is a string identifying the document type (e.g., "matroska" or "webm").
	IDDocType uint32 = 0x4282

	// IDDocTypeVersion is the version of the document type format used.
	IDDocTypeVersion uint32 = 0x4287

	// IDDocTypeReadVersion is the minimum document type version a reader must support.
	IDDocTypeReadVersion uint32 = 0x4285
)

// Segment-level elements — the Segment is the root container for all media
// data. It holds track definitions, timing info, and the actual media frames.
const (
	// IDSegment is the root container for all media data in the file.
	// In streaming mode, its size is often unknown (indeterminate).
	IDSegment uint32 = 0x18538067

	// IDSeekHead contains seek entries that point to other top-level elements.
	// Useful for random access but often absent in streaming scenarios.
	IDSeekHead uint32 = 0x114D9B74

	// IDInfo holds general information about the segment (timecode scale, duration, etc.).
	IDInfo uint32 = 0x1549A966

	// IDTimecodeScale is the number of nanoseconds per Cluster/Block timecode tick.
	// Default is 1,000,000 (meaning timecodes are in milliseconds).
	IDTimecodeScale uint32 = 0x2AD7B1

	// IDDuration is the total duration of the segment, in timecode-scale units.
	// Usually absent in live streams.
	IDDuration uint32 = 0x4489
)

// Track elements — these describe the audio and video tracks in the stream.
// The Tracks element is a master element containing one TrackEntry per track.
const (
	// IDTracks is the master element containing all track descriptions.
	IDTracks uint32 = 0x1654AE6B

	// IDTrackEntry describes a single track (video, audio, subtitle, etc.).
	IDTrackEntry uint32 = 0xAE

	// IDTrackNumber is the track's number, used to identify it in Block headers.
	IDTrackNumber uint32 = 0xD7

	// IDTrackUID is a globally unique ID for the track (random 64-bit integer).
	IDTrackUID uint32 = 0x73C5

	// IDTrackType indicates what kind of track this is (video, audio, etc.).
	// See the TrackType* constants below.
	IDTrackType uint32 = 0x83

	// IDFlagLacing indicates whether this track uses lacing (multiple frames
	// packed into a single Block). 1 = lacing allowed, 0 = no lacing.
	IDFlagLacing uint32 = 0x9C

	// IDCodecID is a string identifying the codec (e.g., "V_VP9", "A_OPUS").
	IDCodecID uint32 = 0x86

	// IDCodecPrivate contains codec-specific initialization data.
	// For example, for Opus this holds the OpusHead structure, and for
	// H.264 this holds the AVCDecoderConfigurationRecord.
	IDCodecPrivate uint32 = 0x63A2

	// IDCodecDelay is a delay (in nanoseconds) that the codec introduces.
	// Used primarily by Opus to signal the encoder delay.
	IDCodecDelay uint32 = 0x56AA

	// IDSeekPreRoll is the duration (in nanoseconds) of data the decoder
	// must decode before the decoded data is valid. Used by Opus.
	IDSeekPreRoll uint32 = 0x56BB

	// IDDefaultDuration is the default duration of each frame in nanoseconds.
	// For example, 33333333 ns ≈ 30 fps for video.
	IDDefaultDuration uint32 = 0x23E383
)

// Video and Audio track detail elements — nested inside TrackEntry to
// describe codec-specific parameters like resolution and sample rate.
const (
	// IDVideo is the master element for video-specific track settings.
	IDVideo uint32 = 0xE0

	// IDPixelWidth is the width of the video frames in pixels.
	IDPixelWidth uint32 = 0xB0

	// IDPixelHeight is the height of the video frames in pixels.
	IDPixelHeight uint32 = 0xBA

	// IDAudio is the master element for audio-specific track settings.
	IDAudio uint32 = 0xE1

	// IDSamplingFreq is the audio sampling frequency in Hz (stored as a float).
	IDSamplingFreq uint32 = 0xB5

	// IDChannels is the number of audio channels.
	IDChannels uint32 = 0x9F

	// IDBitDepth is the number of bits per audio sample.
	IDBitDepth uint32 = 0x6264
)

// Cluster and Block elements — Clusters are the main containers for
// time-ordered media data. Each Cluster holds a base timestamp and one
// or more Blocks/SimpleBlocks containing the actual codec frames.
const (
	// IDCluster is a master element grouping media frames by time.
	// In streaming mode, Clusters often have unknown (indeterminate) size.
	IDCluster uint32 = 0x1F43B675

	// IDTimecode is the absolute timestamp of the Cluster, in timecode-scale units.
	IDTimecode uint32 = 0xE7

	// IDSimpleBlock is the most common way to carry a single media frame.
	// The first bytes of a SimpleBlock contain the track number, relative
	// timestamp, and flags (keyframe, discardable, etc.).
	IDSimpleBlock uint32 = 0xA3

	// IDBlockGroup is an alternative to SimpleBlock that can carry additional
	// metadata like reference frames and block duration.
	IDBlockGroup uint32 = 0xA0

	// IDBlock is the actual media data inside a BlockGroup.
	IDBlock uint32 = 0xA1
)

// Miscellaneous elements — these are top-level or utility elements that
// we need to recognize so we can skip past them during streaming demux.
const (
	// IDVoid is a padding element used to reserve space in the file.
	// Its content has no meaning and should be skipped.
	IDVoid uint32 = 0xEC

	// IDCRC32 contains a CRC-32 checksum for the parent element.
	IDCRC32 uint32 = 0xBF

	// IDTags holds metadata tags (title, artist, etc.) for the segment.
	IDTags uint32 = 0x1254C367

	// IDCues is an index of Cluster positions for seeking.
	IDCues uint32 = 0x1C53BB6B

	// IDAttachments holds attached files (album art, fonts, etc.).
	IDAttachments uint32 = 0x1941A469

	// IDChapters holds chapter/edition information for navigation.
	IDChapters uint32 = 0x1043A770
)

// Track type values from the Matroska spec. These appear in the TrackType
// element (ID 0x83) and tell us whether a track carries video, audio, etc.
const (
	TrackTypeVideo    uint8 = 1
	TrackTypeAudio    uint8 = 2
	TrackTypeComplex  uint8 = 3
	TrackTypeSubtitle uint8 = 17
)

// ElementName returns a human-readable name for the given EBML element ID.
// This is useful for debug logging — instead of printing "element 0x1F43B675",
// you get "Cluster". Returns "Unknown(0x...)" for unrecognized IDs.
func ElementName(id uint32) string {
	switch id {
	// EBML Header
	case IDEBMLHeader:
		return "EBMLHeader"
	case IDEBMLVersion:
		return "EBMLVersion"
	case IDEBMLReadVersion:
		return "EBMLReadVersion"
	case IDEBMLMaxIDLength:
		return "EBMLMaxIDLength"
	case IDEBMLMaxSizeLength:
		return "EBMLMaxSizeLength"
	case IDDocType:
		return "DocType"
	case IDDocTypeVersion:
		return "DocTypeVersion"
	case IDDocTypeReadVersion:
		return "DocTypeReadVersion"

	// Segment
	case IDSegment:
		return "Segment"
	case IDSeekHead:
		return "SeekHead"
	case IDInfo:
		return "Info"
	case IDTimecodeScale:
		return "TimecodeScale"
	case IDDuration:
		return "Duration"

	// Tracks
	case IDTracks:
		return "Tracks"
	case IDTrackEntry:
		return "TrackEntry"
	case IDTrackNumber:
		return "TrackNumber"
	case IDTrackUID:
		return "TrackUID"
	case IDTrackType:
		return "TrackType"
	case IDFlagLacing:
		return "FlagLacing"
	case IDCodecID:
		return "CodecID"
	case IDCodecPrivate:
		return "CodecPrivate"
	case IDCodecDelay:
		return "CodecDelay"
	case IDSeekPreRoll:
		return "SeekPreRoll"
	case IDDefaultDuration:
		return "DefaultDuration"

	// Video/Audio details
	case IDVideo:
		return "Video"
	case IDPixelWidth:
		return "PixelWidth"
	case IDPixelHeight:
		return "PixelHeight"
	case IDAudio:
		return "Audio"
	case IDSamplingFreq:
		return "SamplingFrequency"
	case IDChannels:
		return "Channels"
	case IDBitDepth:
		return "BitDepth"

	// Clusters
	case IDCluster:
		return "Cluster"
	case IDTimecode:
		return "Timecode"
	case IDSimpleBlock:
		return "SimpleBlock"
	case IDBlockGroup:
		return "BlockGroup"
	case IDBlock:
		return "Block"

	// Miscellaneous
	case IDVoid:
		return "Void"
	case IDCRC32:
		return "CRC-32"
	case IDTags:
		return "Tags"
	case IDCues:
		return "Cues"
	case IDAttachments:
		return "Attachments"
	case IDChapters:
		return "Chapters"

	default:
		return fmt.Sprintf("Unknown(0x%X)", id)
	}
}

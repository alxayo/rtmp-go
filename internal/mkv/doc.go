// Package mkv implements a streaming Matroska/WebM demuxer.
//
// # What is Matroska?
//
// Matroska (.mkv) is an open, flexible multimedia container format based on
// EBML (Extensible Binary Meta Language). WebM is a subset of Matroska that
// restricts codecs to VP8/VP9/AV1 for video and Vorbis/Opus for audio.
//
// The key design idea behind Matroska is extensibility: the container is built
// from nested EBML elements, each identified by a variable-length integer ID.
// This makes it easy to add new element types without breaking existing parsers
// — unknown elements can simply be skipped by reading their size and advancing
// past their data.
//
// # How Matroska is structured
//
// A Matroska file contains several layers:
//
//   - EBML Header: Declares the document type ("matroska" or "webm") and
//     the EBML version. Every valid Matroska file starts with this header.
//
//   - Segment: The root container that holds all media data. In streaming
//     mode, the Segment often has an unknown (indeterminate) size, meaning
//     it continues until the stream ends.
//
//   - Tracks: Metadata describing each audio/video track — codec ID,
//     dimensions, sample rate, codec-specific initialization data
//     (CodecPrivate), etc.
//
//   - Clusters: Time-ordered groups of media frames. Each Cluster starts
//     with a timestamp, and the individual frames (SimpleBlock or
//     BlockGroup elements) carry relative timestamps within the Cluster.
//     In live streaming, Clusters arrive continuously and may also have
//     unknown sizes.
//
// # What this demuxer does
//
// This package parses EBML elements from a byte stream, reads the track
// metadata to discover which audio and video codecs are present, and then
// extracts individual media frames from Clusters. It is designed to work
// with the SRT ingest pipeline, converting incoming Matroska/WebM data
// into individual codec frames suitable for re-muxing into RTMP or other
// formats.
//
// # EBML Variable-Length Integers (VINTs)
//
// EBML encodes both element IDs and element sizes using variable-length
// integers (VINTs). The width of the integer is determined by counting
// the leading zero bits in the first byte:
//
//   - 1xxxxxxx  → 1 byte  (7 value bits)
//   - 01xxxxxx  → 2 bytes (14 value bits)
//   - 001xxxxx  → 3 bytes (21 value bits)
//   - 0001xxxx  → 4 bytes (28 value bits)
//
// For element IDs, the leading marker bit is kept as part of the value.
// For element sizes, the marker bit is masked out to extract the pure
// numeric value. A size where all value bits are set to 1 indicates an
// unknown/indeterminate length — common for Segment and Cluster elements
// in live streams.
package mkv

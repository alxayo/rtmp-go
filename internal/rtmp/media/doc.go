// Package media handles audio/video message parsing, codec detection,
// FLV file recording, and local subscriber relay.
//
// # Audio & Video Parsing
//
// RTMP audio messages (TypeID 8) and video messages (TypeID 9) carry codec-
// specific headers in the first few payload bytes.
//
//   - [AudioMessage]: Parses codec (AAC, MP3, Speex) and packet type
//     (sequence header vs raw data).
//   - [VideoMessage]: Parses codec (H.264/AVC, H.265/HEVC), frame type
//     (keyframe vs inter-frame), and packet type.
//
// # Codec Detection
//
// [CodecDetector] identifies the audio and video codecs on first contact
// by parsing the initial media message headers. Results are stored via the
// [CodecStore] interface so any type (Stream, test mock) can receive them.
//
// # FLV Recording
//
// [Recorder] writes incoming audio/video messages to an FLV file with proper
// FLV header, tag headers, and PreviousTagSize fields. It gracefully disables
// itself on the first write error.
//
// # Local Relay
//
// [Stream] manages the publisher and subscriber list for a single stream key.
// [BroadcastMessage] forwards each audio/video message to all subscribers
// using a non-blocking send pattern to prevent slow subscribers from blocking
// the publisher.
package media

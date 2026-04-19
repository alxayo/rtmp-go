// Package codec provides audio/video codec conversion utilities
// for bridging between transport formats used in live streaming.
//
// The primary conversions are:
//
//   - H.264 Annex B ↔ AVCC: MPEG-TS (used by SRT) carries H.264 in "Annex B"
//     format where NAL units are delimited by start codes (0x00000001). RTMP
//     expects "AVCC" format where each NAL unit is prefixed with its length.
//
//   - AAC ADTS ↔ Raw: MPEG-TS wraps each AAC frame in an ADTS header (7-9 bytes).
//     RTMP expects raw AAC frames with a separate AudioSpecificConfig header.
//
//   - AC-3/E-AC-3 → Enhanced RTMP: MPEG-TS carries AC-3 and E-AC-3 syncframes.
//     Enhanced RTMP wraps them in FourCC-tagged audio tags ('ac-3' / 'ec-3').
//
// These conversions are essential for the SRT-to-RTMP bridge, which ingests
// MPEG-TS streams over SRT and re-publishes them as RTMP streams.
package codec

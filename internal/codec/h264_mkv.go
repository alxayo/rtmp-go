package codec

// This file implements H.264/AVC helper functions specific to Matroska containers.
//
// In Matroska (MKV/WebM), H.264 video is stored differently from MPEG-TS:
//
//   - NALUs are length-prefixed (AVCC format), NOT Annex B with start codes.
//     The length field size (1, 2, 3, or 4 bytes) is specified in the
//     AVCDecoderConfigurationRecord stored as MKV CodecPrivate.
//
//   - The AVCDecoderConfigurationRecord is stored as the track's CodecPrivate
//     data. It contains SPS and PPS parameter sets, plus the NALU length size.
//
// This means we CANNOT reuse the Annex B parsing functions (SplitAnnexB,
// ExtractSPSPPS) for MKV H.264 data. Instead, we need to:
//   1. Parse the AVCDecoderConfigurationRecord to extract SPS, PPS, and
//      the NALU length field size.
//   2. Split frame data using length-prefixed NALU boundaries.
//
// The RTMP output always uses 4-byte NALU lengths, so after parsing we
// convert any non-4-byte lengths to 4-byte format via the existing ToAVCC().
//
// AVCDecoderConfigurationRecord layout (ISO/IEC 14496-15 §5.3.3.1):
//
//	Byte 0:    configurationVersion (always 1)
//	Byte 1:    AVCProfileIndication (from SPS[1])
//	Byte 2:    profile_compatibility (from SPS[2])
//	Byte 3:    AVCLevelIndication (from SPS[3])
//	Byte 4:    [111111][lengthSizeMinusOne:2] — bottom 2 bits = NALU length size - 1
//	           Typically 3, meaning 4-byte lengths (the most common).
//	Byte 5:    [111][numOfSequenceParameterSets:5] — bottom 5 bits = SPS count
//	Then:      For each SPS: [uint16 length][SPS data]
//	Next byte: numOfPictureParameterSets (uint8)
//	Then:      For each PPS: [uint16 length][PPS data]

import (
	"encoding/binary"
	"fmt"
)

// AVCDecoderConfig holds the parsed contents of an AVCDecoderConfigurationRecord.
// This is extracted from MKV CodecPrivate data for H.264 tracks.
type AVCDecoderConfig struct {
	// SPS is the first Sequence Parameter Set from the config record.
	// Contains profile, level, resolution, and other encoding parameters.
	SPS []byte

	// PPS is the first Picture Parameter Set from the config record.
	// Contains picture-level encoding parameters.
	PPS []byte

	// NALULengthSize is the number of bytes used for NALU length fields
	// in frame data (1, 2, 3, or 4 bytes). Usually 4.
	NALULengthSize int
}

// ParseAVCDecoderConfig parses an AVCDecoderConfigurationRecord (typically from
// MKV CodecPrivate data) and extracts the SPS, PPS, and NALU length field size.
//
// The returned SPS and PPS are copies (safe to keep after the input is freed).
// The NALULengthSize tells you how many bytes each NALU length prefix uses
// in the frame data — you need this for SplitLengthPrefixed().
//
// Returns an error if the data is too short or malformed.
func ParseAVCDecoderConfig(data []byte) (*AVCDecoderConfig, error) {
	// Minimum size: 6 bytes fixed header + at least 1 SPS entry (3 bytes min)
	if len(data) < 7 {
		return nil, fmt.Errorf("AVC config too short: %d bytes, need at least 7", len(data))
	}

	// Byte 0: configurationVersion — must be 1
	if data[0] != 1 {
		return nil, fmt.Errorf("unsupported AVC config version: %d", data[0])
	}

	// Byte 4: bottom 2 bits = lengthSizeMinusOne (0, 1, 2, or 3)
	naluLengthSize := int(data[4]&0x03) + 1 // Convert from "minus one" encoding

	// Byte 5: bottom 5 bits = number of SPS entries
	numSPS := int(data[5] & 0x1F)
	if numSPS == 0 {
		return nil, fmt.Errorf("AVC config has no SPS entries")
	}

	config := &AVCDecoderConfig{
		NALULengthSize: naluLengthSize,
	}

	// Parse SPS entries — we only keep the first one
	offset := 6
	for i := 0; i < numSPS; i++ {
		// Each SPS is preceded by a 2-byte big-endian length
		if offset+2 > len(data) {
			return nil, fmt.Errorf("AVC config truncated reading SPS %d length", i)
		}
		spsLen := int(binary.BigEndian.Uint16(data[offset:]))
		offset += 2

		if offset+spsLen > len(data) {
			return nil, fmt.Errorf("AVC config truncated reading SPS %d data (need %d, have %d)",
				i, spsLen, len(data)-offset)
		}

		// Keep only the first SPS — multiple SPS is rare in practice
		if i == 0 {
			config.SPS = make([]byte, spsLen)
			copy(config.SPS, data[offset:offset+spsLen])
		}
		offset += spsLen
	}

	// Next byte: number of PPS entries
	if offset >= len(data) {
		return nil, fmt.Errorf("AVC config truncated before PPS count")
	}
	numPPS := int(data[offset])
	offset++

	if numPPS == 0 {
		return nil, fmt.Errorf("AVC config has no PPS entries")
	}

	// Parse PPS entries — we only keep the first one
	for i := 0; i < numPPS; i++ {
		if offset+2 > len(data) {
			return nil, fmt.Errorf("AVC config truncated reading PPS %d length", i)
		}
		ppsLen := int(binary.BigEndian.Uint16(data[offset:]))
		offset += 2

		if offset+ppsLen > len(data) {
			return nil, fmt.Errorf("AVC config truncated reading PPS %d data (need %d, have %d)",
				i, ppsLen, len(data)-offset)
		}

		// Keep only the first PPS
		if i == 0 {
			config.PPS = make([]byte, ppsLen)
			copy(config.PPS, data[offset:offset+ppsLen])
		}
		offset += ppsLen
	}

	return config, nil
}

// SplitLengthPrefixed splits frame data that uses length-prefixed NALUs
// (AVCC format) into individual NALUs. This is how H.264 and H.265 data
// is stored in Matroska containers.
//
// Unlike Annex B (used in MPEG-TS), where NALUs are separated by start codes
// (0x000001), AVCC format prefixes each NALU with a fixed-size length field.
// The length field size (1, 2, 3, or 4 bytes) comes from the decoder config.
//
// Parameters:
//   - data: the frame payload from MKV (one or more length-prefixed NALUs)
//   - naluLenSize: number of bytes per length prefix (from AVCDecoderConfig)
//
// Returns a slice of NALU byte slices. The returned slices point into the
// original data (no copies), so they are only valid while data is alive.
func SplitLengthPrefixed(data []byte, naluLenSize int) [][]byte {
	var nalus [][]byte
	offset := 0

	for offset < len(data) {
		// Read the NALU length prefix
		if offset+naluLenSize > len(data) {
			break // Not enough bytes for another length prefix
		}

		// Read the length field (big-endian, variable size)
		var naluLen int
		switch naluLenSize {
		case 1:
			naluLen = int(data[offset])
		case 2:
			naluLen = int(binary.BigEndian.Uint16(data[offset:]))
		case 3:
			naluLen = int(data[offset])<<16 | int(data[offset+1])<<8 | int(data[offset+2])
		case 4:
			naluLen = int(binary.BigEndian.Uint32(data[offset:]))
		default:
			break // Invalid NALU length size
		}
		offset += naluLenSize

		// Validate the NALU length
		if naluLen <= 0 || offset+naluLen > len(data) {
			break // Malformed data or truncated
		}

		nalus = append(nalus, data[offset:offset+naluLen])
		offset += naluLen
	}

	return nalus
}

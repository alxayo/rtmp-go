package codec

// This file implements H.265/HEVC helper functions specific to Matroska containers.
//
// In Matroska (MKV), H.265 video stores its decoder configuration as the
// HEVCDecoderConfigurationRecord in the track's CodecPrivate data. This record
// contains VPS, SPS, and PPS parameter sets, plus the NALU length field size.
//
// Just like H.264 in MKV, H.265 frame data uses length-prefixed NALUs (not
// Annex B start codes). The SplitLengthPrefixed() function from h264_mkv.go
// works for both codecs.
//
// HEVCDecoderConfigurationRecord layout (ISO/IEC 14496-15 §8.3.3.1):
//
//	Bytes 0-22:  23 bytes of fixed header (version, profile, tier, level, etc.)
//	  Byte 0:    configurationVersion (always 1)
//	  Byte 21:   [111111][lengthSizeMinusOne:2] — bottom 2 bits = NALU length size - 1
//	  Byte 22:   numOfArrays — number of NALU arrays that follow
//
//	For each array:
//	  Byte 0:    [array_completeness:1][reserved:1][NAL_unit_type:6]
//	  Bytes 1-2: numNalus (uint16 big-endian)
//	  For each NALU:
//	    Bytes 0-1: naluLength (uint16 big-endian)
//	    Data:      NALU data
//
// NAL unit type values we look for:
//   - 32 (0x20) = VPS (Video Parameter Set)
//   - 33 (0x21) = SPS (Sequence Parameter Set)
//   - 34 (0x22) = PPS (Picture Parameter Set)

import (
	"encoding/binary"
	"fmt"
)

// HEVCDecoderConfig holds the parsed contents of an HEVCDecoderConfigurationRecord.
// This is extracted from MKV CodecPrivate data for H.265 tracks.
type HEVCDecoderConfig struct {
	// VPS is the first Video Parameter Set from the config record.
	// Contains encoding options, profile, tier, and level information.
	VPS []byte

	// SPS is the first Sequence Parameter Set from the config record.
	// Contains resolution, frame rate, and other sequence-level parameters.
	SPS []byte

	// PPS is the first Picture Parameter Set from the config record.
	// Contains picture-level encoding parameters.
	PPS []byte

	// NALULengthSize is the number of bytes used for NALU length fields
	// in frame data (1, 2, 3, or 4 bytes). Usually 4.
	NALULengthSize int
}

// ParseHEVCDecoderConfig parses an HEVCDecoderConfigurationRecord (typically
// from MKV CodecPrivate data) and extracts VPS, SPS, PPS, and the NALU
// length field size.
//
// The returned parameter sets are copies (safe to keep after the input is freed).
// Returns an error if the data is too short or malformed.
func ParseHEVCDecoderConfig(data []byte) (*HEVCDecoderConfig, error) {
	// Minimum size: 23 bytes fixed header
	if len(data) < 23 {
		return nil, fmt.Errorf("HEVC config too short: %d bytes, need at least 23", len(data))
	}

	// Byte 0: configurationVersion — must be 1
	if data[0] != 1 {
		return nil, fmt.Errorf("unsupported HEVC config version: %d", data[0])
	}

	// Byte 21: bottom 2 bits = lengthSizeMinusOne (0, 1, 2, or 3)
	naluLengthSize := int(data[21]&0x03) + 1

	// Byte 22: number of NALU arrays
	numArrays := int(data[22])

	config := &HEVCDecoderConfig{
		NALULengthSize: naluLengthSize,
	}

	// Walk through each NALU array looking for VPS (32), SPS (33), PPS (34)
	offset := 23
	for i := 0; i < numArrays; i++ {
		// Each array starts with: [1 byte type/flags] [2 bytes numNalus]
		if offset+3 > len(data) {
			return nil, fmt.Errorf("HEVC config truncated at array %d header", i)
		}

		// Extract NAL unit type from bottom 6 bits of the first byte
		naluType := data[offset] & 0x3F
		offset++

		// Number of NALUs in this array
		numNalus := int(binary.BigEndian.Uint16(data[offset:]))
		offset += 2

		// Read each NALU in the array
		for j := 0; j < numNalus; j++ {
			if offset+2 > len(data) {
				return nil, fmt.Errorf("HEVC config truncated at array %d NALU %d length", i, j)
			}
			naluLen := int(binary.BigEndian.Uint16(data[offset:]))
			offset += 2

			if offset+naluLen > len(data) {
				return nil, fmt.Errorf("HEVC config truncated at array %d NALU %d data (need %d, have %d)",
					i, j, naluLen, len(data)-offset)
			}

			// Copy the first NALU of each type we care about
			naluData := make([]byte, naluLen)
			copy(naluData, data[offset:offset+naluLen])

			switch naluType {
			case 32: // VPS
				if config.VPS == nil {
					config.VPS = naluData
				}
			case 33: // SPS
				if config.SPS == nil {
					config.SPS = naluData
				}
			case 34: // PPS
				if config.PPS == nil {
					config.PPS = naluData
				}
			}

			offset += naluLen
		}
	}

	// Validate that we found all three required parameter sets
	if config.VPS == nil {
		return nil, fmt.Errorf("HEVC config missing VPS")
	}
	if config.SPS == nil {
		return nil, fmt.Errorf("HEVC config missing SPS")
	}
	if config.PPS == nil {
		return nil, fmt.Errorf("HEVC config missing PPS")
	}

	return config, nil
}

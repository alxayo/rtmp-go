package codec

// This file implements MP3 audio helpers for the SRT-to-RTMP bridge.
//
// MP3 is self-describing: every frame carries its own sync word and header
// with sample rate, bitrate, channel mode, etc. There is no separate
// "sequence header" or codec config — unlike AAC (which needs an
// AudioSpecificConfig) or AC-3/Opus/FLAC (which use Enhanced RTMP FourCC
// tags with SequenceStart).
//
// MP3 uses the legacy FLV audio tag format (SoundFormat=2), NOT Enhanced
// RTMP. The RTMP audio tag is simply:
//
//	Byte 0: [SoundFormat:4][SoundRate:2][SoundSize:1][SoundType:1]
//	  - SoundFormat = 2 (MP3)
//	  - SoundRate   = 3 (44kHz hint — actual rate is in the MP3 frame header)
//	  - SoundSize   = 1 (16-bit)
//	  - SoundType   = 1 (stereo)
//	  Result: (2 << 4) | (3 << 2) | (1 << 1) | 1 = 0x2F
//
//	Bytes 1+: Raw MP3 frame data (complete frame including sync word)
//
// The sound byte values are hints only. All RTMP players parse the MP3
// frame header for the actual sample rate and channel count.

import (
	"fmt"
)

// mp3SoundByte is the legacy FLV audio tag format byte for MP3.
//
//	SoundFormat = 2 (MP3)     → bits 7-4: 0010
//	SoundRate   = 3 (44kHz)   → bits 3-2: 11   (hint only)
//	SoundSize   = 1 (16-bit)  → bit 1:    1
//	SoundType   = 1 (stereo)  → bit 0:    1
//	= 0b0010_1111 = 0x2F
const mp3SoundByte = 0x2F

// mp3BitratesMPEG1Layer3 maps the 4-bit bitrate index to kbps for MPEG1 Layer III.
// Index 0 = free format, index 15 = bad/reserved.
var mp3BitratesMPEG1Layer3 = [16]int{
	0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0,
}

// mp3BitratesMPEG2Layer3 maps the 4-bit bitrate index to kbps for MPEG2/2.5 Layer III.
// Index 0 = free format, index 15 = bad/reserved.
var mp3BitratesMPEG2Layer3 = [16]int{
	0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0,
}

// mp3BitratesMPEG1Layer2 maps the 4-bit bitrate index to kbps for MPEG1 Layer II.
var mp3BitratesMPEG1Layer2 = [16]int{
	0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384, 0,
}

// mp3BitratesMPEG1Layer1 maps the 4-bit bitrate index to kbps for MPEG1 Layer I.
var mp3BitratesMPEG1Layer1 = [16]int{
	0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448, 0,
}

// mp3BitratesMPEG2Layer1 maps the 4-bit bitrate index to kbps for MPEG2/2.5 Layer I.
var mp3BitratesMPEG2Layer1 = [16]int{
	0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256, 0,
}

// mp3SampleRatesMPEG1 maps the 2-bit sample rate index for MPEG1.
// Index 3 is reserved.
var mp3SampleRatesMPEG1 = [4]uint32{44100, 48000, 32000, 0}

// mp3SampleRatesMPEG2 maps the 2-bit sample rate index for MPEG2.
// Index 3 is reserved.
var mp3SampleRatesMPEG2 = [4]uint32{22050, 24000, 16000, 0}

// mp3SampleRatesMPEG25 maps the 2-bit sample rate index for MPEG2.5.
// Index 3 is reserved.
var mp3SampleRatesMPEG25 = [4]uint32{11025, 12000, 8000, 0}

// MP3FrameInfo holds metadata parsed from an MP3 frame header.
// Used for logging and diagnostics when the bridge first encounters MP3 audio.
type MP3FrameInfo struct {
	// SampleRate in Hz, e.g., 44100, 48000, 32000.
	SampleRate uint32

	// Channels is 1 (mono) or 2 (stereo/joint-stereo/dual-channel).
	Channels int

	// Bitrate in kbps, e.g., 128, 192, 320.
	Bitrate int

	// Layer is the MPEG audio layer: 1, 2, or 3.
	Layer int

	// Version is the MPEG audio version: 1 = MPEG1, 2 = MPEG2, 25 = MPEG2.5.
	Version int
}

// ParseMP3FrameHeader parses the 4-byte MP3 frame header to extract
// audio parameters. This is used for logging on first frame only.
//
// MP3 frame header format (32 bits):
//
//	Bits 31-21: Frame sync (all 1s — 11 bits)
//	Bits 20-19: MPEG Audio version (00=2.5, 01=reserved, 10=2, 11=1)
//	Bits 18-17: Layer (00=reserved, 01=III, 10=II, 11=I)
//	Bit 16:     Protection (0=CRC present, 1=no CRC)
//	Bits 15-12: Bitrate index
//	Bits 11-10: Sample rate index
//	Bit 9:      Padding
//	Bit 8:      Private
//	Bits 7-6:   Channel mode (00=stereo, 01=joint stereo, 10=dual channel, 11=mono)
//	Bits 5-4:   Mode extension (used only with joint stereo)
//	Bit 3:      Copyright
//	Bit 2:      Original
//	Bits 1-0:   Emphasis
func ParseMP3FrameHeader(data []byte) (*MP3FrameInfo, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("MP3 frame header too short: need at least 4 bytes, got %d", len(data))
	}

	// Check sync word: bits 31-21 must all be 1s.
	// That means the first byte is 0xFF and bits 7-5 of the second byte are 111.
	// Mask: data[0] == 0xFF && (data[1] & 0xE0) == 0xE0
	if data[0] != 0xFF || (data[1]&0xE0) != 0xE0 {
		return nil, fmt.Errorf("invalid MP3 sync word: 0x%02X%02X (expected 0xFFE0 mask)", data[0], data[1])
	}

	info := &MP3FrameInfo{}

	// Bits 20-19: MPEG version (2 bits)
	//   00 = MPEG2.5 (unofficial extension)
	//   01 = reserved
	//   10 = MPEG2 (ISO 13818-3)
	//   11 = MPEG1 (ISO 11172-3)
	versionBits := (data[1] >> 3) & 0x03
	switch versionBits {
	case 3:
		info.Version = 1
	case 2:
		info.Version = 2
	case 0:
		info.Version = 25
	default:
		return nil, fmt.Errorf("MP3 reserved MPEG version bits: %d", versionBits)
	}

	// Bits 18-17: Layer (2 bits)
	//   00 = reserved
	//   01 = Layer III
	//   10 = Layer II
	//   11 = Layer I
	layerBits := (data[1] >> 1) & 0x03
	switch layerBits {
	case 3:
		info.Layer = 1
	case 2:
		info.Layer = 2
	case 1:
		info.Layer = 3
	default:
		return nil, fmt.Errorf("MP3 reserved layer bits: %d", layerBits)
	}

	// Bits 15-12: Bitrate index (4 bits)
	bitrateIdx := (data[2] >> 4) & 0x0F

	// Look up bitrate from the appropriate table based on version and layer
	var bitrate int
	switch {
	case info.Version == 1 && info.Layer == 1:
		bitrate = mp3BitratesMPEG1Layer1[bitrateIdx]
	case info.Version == 1 && info.Layer == 2:
		bitrate = mp3BitratesMPEG1Layer2[bitrateIdx]
	case info.Version == 1 && info.Layer == 3:
		bitrate = mp3BitratesMPEG1Layer3[bitrateIdx]
	case (info.Version == 2 || info.Version == 25) && info.Layer == 1:
		bitrate = mp3BitratesMPEG2Layer1[bitrateIdx]
	case (info.Version == 2 || info.Version == 25) && (info.Layer == 2 || info.Layer == 3):
		bitrate = mp3BitratesMPEG2Layer3[bitrateIdx]
	}

	if bitrateIdx == 0 {
		return nil, fmt.Errorf("MP3 free format bitrate not supported")
	}
	if bitrateIdx == 15 || bitrate == 0 {
		return nil, fmt.Errorf("MP3 invalid bitrate index: %d", bitrateIdx)
	}
	info.Bitrate = bitrate

	// Bits 11-10: Sample rate index (2 bits)
	sampleRateIdx := (data[2] >> 2) & 0x03
	if sampleRateIdx == 3 {
		return nil, fmt.Errorf("MP3 reserved sample rate index: 3")
	}

	switch info.Version {
	case 1:
		info.SampleRate = mp3SampleRatesMPEG1[sampleRateIdx]
	case 2:
		info.SampleRate = mp3SampleRatesMPEG2[sampleRateIdx]
	case 25:
		info.SampleRate = mp3SampleRatesMPEG25[sampleRateIdx]
	}

	// Bits 7-6: Channel mode (2 bits)
	//   00 = Stereo
	//   01 = Joint Stereo
	//   10 = Dual Channel
	//   11 = Mono
	channelMode := (data[3] >> 6) & 0x03
	if channelMode == 3 {
		info.Channels = 1
	} else {
		info.Channels = 2
	}

	return info, nil
}

// BuildMP3AudioTag wraps a raw MP3 frame in the legacy RTMP audio tag format.
// Returns: [0x2F][raw_mp3_frame_data]
//
// The 0x2F byte encodes:
//
//	SoundFormat = 2 (MP3)     → bits 7-4
//	SoundRate   = 3 (44kHz)   → bits 3-2 (hint only, actual rate is in MP3 header)
//	SoundSize   = 1 (16-bit)  → bit 1
//	SoundType   = 1 (stereo)  → bit 0
//
// Note: The sound byte is a fixed hint. The actual sample rate and channel
// count come from the MP3 frame header itself. All RTMP players parse the
// MP3 frame header for the real values.
func BuildMP3AudioTag(rawFrame []byte) []byte {
	// 1 byte format header + raw MP3 frame data
	buf := make([]byte, 1+len(rawFrame))

	// Byte 0: Legacy FLV audio format byte
	buf[0] = mp3SoundByte

	// Bytes 1+: Raw MP3 frame data (complete frame including sync word)
	copy(buf[1:], rawFrame)

	return buf
}

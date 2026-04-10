package codec

// This file implements AAC audio conversion for RTMP.
//
// When AAC audio travels in MPEG-TS (over SRT), each frame has an ADTS header
// (Audio Data Transport Stream) — a 7 or 9 byte header that describes the
// audio format for every single frame. This is redundant but robust.
//
// RTMP does it differently: it sends the audio format description once as an
// "AudioSpecificConfig" sequence header, then sends raw AAC frames without
// any per-frame headers.
//
// The RTMP audio tag format (FLV-style) is:
//
//	Byte 0: [SoundFormat:4][SoundRate:2][SoundSize:1][SoundType:1]
//	  - SoundFormat: 10 = AAC
//	  - SoundRate:   3 = 44kHz (always 3 for AAC, actual rate is in config)
//	  - SoundSize:   1 = 16-bit (always 1 for AAC)
//	  - SoundType:   1 = stereo (always 1 for AAC, actual channels in config)
//	  Result: 0xAF
//
//	Byte 1: AACPacketType
//	  - 0 = Sequence header (AudioSpecificConfig)
//	  - 1 = Raw AAC frame data
//
//	Remaining: Either AudioSpecificConfig or raw AAC frame

import (
	"errors"
	"fmt"
)

// ADTSHeader represents a parsed ADTS (Audio Data Transport Stream) header.
// ADTS wraps each AAC frame with format information so the frame can be
// decoded independently.
type ADTSHeader struct {
	// Profile is the AAC audio profile minus 1.
	// 0 = AAC Main, 1 = AAC-LC (Low Complexity), 2 = AAC SSR
	// AAC-LC (1) is by far the most common for live streaming.
	Profile uint8

	// SamplingFreqIdx is an index into the standard AAC sample rate table:
	//   0=96000, 1=88200, 2=64000, 3=48000, 4=44100, 5=32000,
	//   6=24000, 7=22050, 8=16000, 9=12000, 10=11025, 11=8000
	SamplingFreqIdx uint8

	// ChannelConfig is the number of audio channels:
	//   1=mono, 2=stereo, 3=center+L+R, 6=5.1, 7=7.1
	ChannelConfig uint8

	// FrameLength is the total ADTS frame length including the header.
	// The raw AAC data length = FrameLength - HeaderSize.
	FrameLength uint16

	// HeaderSize is 7 bytes (no CRC) or 9 bytes (with CRC).
	// Determined by the protection_absent bit in the ADTS header.
	HeaderSize int
}

// ParseADTSHeader parses the 7-byte ADTS fixed header from the beginning
// of an AAC frame.
//
// ADTS header bit layout (7 bytes = 56 bits):
//
//	Bits 0-11:  Sync word (0xFFF) — identifies this as an ADTS frame
//	Bit 12:     ID (0=MPEG-4, 1=MPEG-2)
//	Bits 13-14: Layer (always 0)
//	Bit 15:     Protection absent (1=no CRC, 0=CRC present → 9-byte header)
//	Bits 16-17: Profile (0=Main, 1=LC, 2=SSR)
//	Bits 18-21: Sampling frequency index (0-11)
//	Bit 22:     Private bit
//	Bits 23-25: Channel configuration (1-7)
//	Bits 26-29: Various flags
//	Bits 30-42: Frame length (including header)
//	Bits 43-53: Buffer fullness
//	Bits 54-55: Number of AAC frames minus 1
func ParseADTSHeader(data []byte) (*ADTSHeader, error) {
	if len(data) < 7 {
		return nil, errors.New("ADTS header too short: need at least 7 bytes")
	}

	// Check sync word: first 12 bits must be 0xFFF
	if data[0] != 0xFF || (data[1]&0xF0) != 0xF0 {
		return nil, fmt.Errorf("invalid ADTS sync word: 0x%02X%02X", data[0], data[1])
	}

	h := &ADTSHeader{}

	// Bit 15: protection_absent. 1 = no CRC (7-byte header), 0 = CRC (9-byte header)
	protectionAbsent := data[1] & 0x01
	if protectionAbsent == 1 {
		h.HeaderSize = 7
	} else {
		h.HeaderSize = 9
	}

	// Bits 16-17: Profile (stored as profile - 1 in ADTS)
	h.Profile = (data[2] >> 6) & 0x03

	// Bits 18-21: Sampling frequency index
	h.SamplingFreqIdx = (data[2] >> 2) & 0x0F

	// Bits 23-25: Channel configuration (split across bytes 2 and 3)
	h.ChannelConfig = ((data[2] & 0x01) << 2) | ((data[3] >> 6) & 0x03)

	// Bits 30-42: Frame length (13 bits, split across bytes 3, 4, 5)
	h.FrameLength = (uint16(data[3]&0x03) << 11) |
		(uint16(data[4]) << 3) |
		(uint16(data[5]) >> 5)

	if h.FrameLength < uint16(h.HeaderSize) {
		return nil, fmt.Errorf("ADTS frame length %d is smaller than header %d", h.FrameLength, h.HeaderSize)
	}

	return h, nil
}

// BuildAudioSpecificConfig creates the 2-byte AudioSpecificConfig from
// ADTS header information.
//
// AudioSpecificConfig is the AAC decoder's initialization data.
// For AAC-LC (which is almost always what live streams use), it's just
// 2 bytes that encode:
//
//	Bits [4:0] = audioObjectType (profile + 1, so AAC-LC = 2)
//	Bits [3:0] = samplingFrequencyIndex
//	Bits [3:0] = channelConfiguration
//	Remaining  = 0 (frame length flag + depends on core coder + extension flag)
//
// Example for 44.1kHz stereo AAC-LC:
//
//	audioObjectType = 2 (AAC-LC) → 5 bits: 00010
//	sampFreqIdx = 4 (44100)      → 4 bits: 0100
//	channelConfig = 2 (stereo)   → 4 bits: 0010
//	remaining = 0                → 3 bits: 000
//	Result: 0x12 0x10
func BuildAudioSpecificConfig(h *ADTSHeader) []byte {
	// audioObjectType = profile + 1 (ADTS stores profile-1, ASC stores profile)
	objectType := h.Profile + 1

	// Pack into 2 bytes:
	//   Byte 0: [objectType:5][freqIdx_high:3]
	//   Byte 1: [freqIdx_low:1][channelConfig:4][0:3]
	config := make([]byte, 2)
	config[0] = (objectType << 3) | (h.SamplingFreqIdx >> 1)
	config[1] = (h.SamplingFreqIdx << 7) | (h.ChannelConfig << 3)

	return config
}

// BuildAACSequenceHeader wraps an AudioSpecificConfig in the RTMP audio
// tag format. This is sent once before any audio frames to tell the
// subscriber's decoder how to decode the AAC data.
func BuildAACSequenceHeader(h *ADTSHeader) []byte {
	config := BuildAudioSpecificConfig(h)

	// 2 bytes RTMP header + AudioSpecificConfig
	buf := make([]byte, 2+len(config))

	// Byte 0: SoundFormat=10(AAC)<<4 | SoundRate=3<<2 | SoundSize=1<<1 | SoundType=1
	// = 0xA0 | 0x0C | 0x02 | 0x01 = 0xAF
	buf[0] = 0xAF

	// Byte 1: AACPacketType = 0 (sequence header)
	buf[1] = 0x00

	// Copy the AudioSpecificConfig
	copy(buf[2:], config)

	return buf
}

// BuildAACFrame wraps raw AAC frame data (ADTS header already stripped)
// in the RTMP audio tag format for transmission.
func BuildAACFrame(rawFrame []byte) []byte {
	// 2 bytes RTMP header + raw frame
	buf := make([]byte, 2+len(rawFrame))

	// Byte 0: Same format byte as sequence header
	buf[0] = 0xAF

	// Byte 1: AACPacketType = 1 (raw AAC frame)
	buf[1] = 0x01

	// Copy the raw AAC data
	copy(buf[2:], rawFrame)

	return buf
}

// StripADTS removes the ADTS header from an AAC frame, returning the
// raw AAC data and the parsed header. The header is needed to build
// the AudioSpecificConfig on the first frame.
func StripADTS(data []byte) ([]byte, *ADTSHeader, error) {
	h, err := ParseADTSHeader(data)
	if err != nil {
		return nil, nil, err
	}

	if len(data) < int(h.FrameLength) {
		return nil, nil, fmt.Errorf("ADTS frame truncated: have %d bytes, need %d", len(data), h.FrameLength)
	}

	// The raw AAC data starts after the header
	rawData := data[h.HeaderSize:h.FrameLength]
	return rawData, h, nil
}

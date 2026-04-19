package media

import (
	"encoding/binary"
	"fmt"
)

// Audio codec identifiers. Legacy codecs use the 4-bit SoundFormat in the FLV spec.
// Enhanced RTMP codecs use 4-byte FourCC identifiers (see E-RTMP v2 spec).
const (
	AudioCodecMP3   = "MP3"
	AudioCodecAAC   = "AAC"
	AudioCodecSpeex = "Speex"
	AudioCodecOpus  = "Opus"
	AudioCodecFLAC  = "FLAC"
	AudioCodecAC3   = "AC3"
	AudioCodecEAC3  = "EAC3"
)

// AAC packet types distinguish between codec configuration and actual audio data.
// The sequence header must be sent before any raw audio frames.
const (
	AACPacketTypeSequenceHeader = "sequence_header" // Contains AudioSpecificConfig (codec init data)
	AACPacketTypeRaw            = "raw"             // Contains actual compressed audio samples
)

// Enhanced RTMP AudioPacketType values (E-RTMP v2 spec).
const (
	AudioPacketTypeSequenceStart    = "sequence_start"    // Codec configuration record
	AudioPacketTypeCodedFrames      = "coded_frames"      // Compressed audio data
	AudioPacketTypeSequenceEnd      = "sequence_end"      // End of audio stream signal
	AudioPacketTypeMultichannelConfig = "multichannel_config" // Multichannel audio layout
	AudioPacketTypeMultitrack       = "multitrack"         // Multiple audio tracks
	AudioPacketTypeModEx            = "modex"              // Modifier extension wrapper
)

// Enhanced RTMP AudioPacketType numeric values on the wire.
const (
	audioPacketTypeSequenceStart      uint8 = 0
	audioPacketTypeCodedFrames        uint8 = 1
	audioPacketTypeSequenceEnd        uint8 = 4 // End of audio stream
	audioPacketTypeMultichannelConfig uint8 = 5 // Multichannel audio layout configuration
	audioPacketTypeMultitrack         uint8 = 6 // Multitrack audio (multiple audio tracks)
	audioPacketTypeModEx              uint8 = 7 // Modifier Extension
)

// SoundFormat value that signals Enhanced RTMP audio (ExAudioTagHeader).
const soundFormatExHeader uint8 = 9

// audioFourCCMap maps well-known audio FourCC values (as big-endian uint32)
// to their canonical codec constant. See fourCC() in codec.go.
var audioFourCCMap = map[uint32]string{
	fourCC("mp4a"): AudioCodecAAC,
	fourCC("Opus"): AudioCodecOpus,
	fourCC("fLaC"): AudioCodecFLAC,
	fourCC(".mp3"): AudioCodecMP3,
	fourCC("ac-3"): AudioCodecAC3,
	fourCC("ec-3"): AudioCodecEAC3,
}

// AudioMessage is a lightweight parsed representation of an RTMP audio (message type 8) tag.
// It supports both legacy FLV tags (4-bit SoundFormat) and Enhanced RTMP tags
// (SoundFormat=9 ExAudioTagHeader + FourCC).
//
// Legacy tag structure: [AudioHeader(1B)][AACPacketType?][Payload...]
// Enhanced tag structure: [ExAudioHeader(1B)][FourCC(4B)][Payload...]
type AudioMessage struct {
	Codec      string // One of AudioCodec* constants
	PacketType string // sequence_header / raw / sequence_start / coded_frames
	Payload    []byte // Raw payload after parsed header bytes
	Enhanced   bool   // True if parsed via Enhanced RTMP (ExAudioTagHeader) path
	FourCC     string // Raw FourCC string (e.g. "Opus"), empty for legacy

	// NanosecondOffset is the sub-millisecond offset from ModEx, if present.
	// When non-zero, the full nanosecond timestamp is:
	//   (chunk.Message.Timestamp * 1_000_000) + NanosecondOffset
	// This provides microsecond/nanosecond A/V sync precision.
	// Only populated when PacketType is "modex" and the ModEx carries a
	// TimestampOffsetNano modifier.
	NanosecondOffset uint32
}

// ParseAudioMessage parses a raw RTMP audio message payload (the FLV/RTMP tag data for
// message type 8) and returns an AudioMessage with codec metadata.
// Supports both legacy SoundFormat and Enhanced RTMP ExAudioTagHeader.
func ParseAudioMessage(data []byte) (*AudioMessage, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("audio.parse: empty payload")
	}
	header := data[0]
	soundFormat := (header >> 4) & 0x0F

	if soundFormat == soundFormatExHeader {
		return parseEnhancedAudio(data)
	}
	return parseLegacyAudio(data, soundFormat)
}

// parseEnhancedAudio handles the Enhanced RTMP (E-RTMP) audio tag format.
//
// Wire format (first byte):
//
//	bits [7:4]: SoundFormat = 9 (ExHeader signal)
//	bits [3:0]: AudioPacketType (4 bits)
//
// Followed by 4-byte FourCC (big-endian), then codec-specific payload.
func parseEnhancedAudio(data []byte) (*AudioMessage, error) {
	// Minimum: 1 byte header + 4 bytes FourCC = 5 bytes
	if len(data) < 5 {
		return nil, fmt.Errorf("audio.parse: enhanced packet truncated (need 5 bytes, got %d)", len(data))
	}

	pktType := data[0] & 0x0F
	fourCCVal := binary.BigEndian.Uint32(data[1:5])
	fourCCStr := string(data[1:5])

	msg := &AudioMessage{
		Enhanced: true,
		FourCC:   fourCCStr,
	}

	// Map FourCC to codec.
	codec, ok := audioFourCCMap[fourCCVal]
	if !ok {
		return nil, fmt.Errorf("audio.parse: unsupported enhanced fourcc %q (0x%08x)", fourCCStr, fourCCVal)
	}
	msg.Codec = codec

	// Map AudioPacketType.
	switch pktType {
	case audioPacketTypeSequenceStart:
		msg.PacketType = AudioPacketTypeSequenceStart
	case audioPacketTypeCodedFrames:
		msg.PacketType = AudioPacketTypeCodedFrames
	case audioPacketTypeSequenceEnd:
		// End-of-stream signal — no payload expected after this.
		msg.PacketType = AudioPacketTypeSequenceEnd
	case audioPacketTypeMultichannelConfig:
		// Multichannel audio layout configuration (speaker positions, channel count).
		msg.PacketType = AudioPacketTypeMultichannelConfig
	case audioPacketTypeMultitrack:
		// Multitrack audio — multiple audio tracks in one RTMP message.
		// Use ParseMultitrack() on msg.Payload to extract individual tracks.
		msg.PacketType = AudioPacketTypeMultitrack
	case audioPacketTypeModEx:
		// ModEx (Modifier Extension) — wraps another audio packet with modifiers
		// like nanosecond timestamps. Parse the ModEx wrapper to extract
		// modifiers and unwrap the inner payload automatically.
		msg.PacketType = AudioPacketTypeModEx
		modex, modexErr := ParseModEx(data[5:])
		if modexErr == nil {
			// Successfully parsed: extract nanosecond offset and unwrapped payload.
			msg.NanosecondOffset = modex.NanosecondOffset
			msg.Payload = modex.WrappedPayload
			return msg, nil
		}
		// Parse failed: fall through to default payload assignment below.
	default:
		msg.PacketType = fmt.Sprintf("enhanced_%d", pktType)
	}

	msg.Payload = data[5:]
	return msg, nil
}

// parseLegacyAudio handles the traditional FLV audio tag format.
func parseLegacyAudio(data []byte, soundFormat uint8) (*AudioMessage, error) {
	msg := &AudioMessage{}

	switch soundFormat {
	case 2:
		msg.Codec = AudioCodecMP3
		msg.Payload = data[1:]
	case 10:
		msg.Codec = AudioCodecAAC
		if len(data) < 2 {
			return nil, fmt.Errorf("audio.parse: aac packet truncated (need packet type)")
		}
		pt := data[1]
		if pt == 0x00 {
			msg.PacketType = AACPacketTypeSequenceHeader
		} else if pt == 0x01 {
			msg.PacketType = AACPacketTypeRaw
		} else {
			msg.PacketType = fmt.Sprintf("unknown_%d", pt)
		}
		msg.Payload = data[2:]
	case 11:
		msg.Codec = AudioCodecSpeex
		msg.Payload = data[1:]
	default:
		return nil, fmt.Errorf("audio.parse: unsupported sound format id=%d", soundFormat)
	}
	return msg, nil
}

// IsAudioMultitrack checks whether raw audio tag data is an Enhanced RTMP
// multitrack message (AudioPacketType = 6). Used by the stream registry to
// detect multitrack containers and extract per-track sequence headers.
func IsAudioMultitrack(data []byte) bool {
	// Need at least 6 bytes: 1 header + 4 FourCC + 1 multitrack header byte.
	if len(data) < 6 {
		return false
	}
	soundFormat := (data[0] >> 4) & 0x0F
	if soundFormat != soundFormatExHeader {
		return false // Multitrack only exists in Enhanced RTMP
	}
	pktType := data[0] & 0x0F
	return pktType == audioPacketTypeMultitrack
}

// BuildAudioSeqStartPayload constructs a complete Enhanced RTMP audio sequence
// start payload for a single track. This is used to wrap per-track codec config
// data into a standalone RTMP audio message for late-join delivery.
//
// Wire format: [0x90 (soundFormat=9 | seqStart)][FourCC(4B)][configData...]
func BuildAudioSeqStartPayload(fourCC string, configData []byte) []byte {
	// byte 0: soundFormat=9 (bits 7:4) | pktType=seqStart=0 (bits 3:0)
	// = 0b1001_0000 = 0x90
	payload := make([]byte, 5+len(configData))
	payload[0] = 0x90
	copy(payload[1:5], []byte(fourCC))
	copy(payload[5:], configData)
	return payload
}

// IsAudioSequenceHeader checks whether raw audio tag data represents a sequence header
// (codec configuration record). This works for both legacy and Enhanced RTMP formats.
// Used by the stream registry to cache sequence headers for late-joining subscribers.
func IsAudioSequenceHeader(data []byte) bool {
	if len(data) < 2 {
		return false
	}

	header := data[0]
	soundFormat := (header >> 4) & 0x0F

	if soundFormat == soundFormatExHeader {
		// Enhanced RTMP: AudioPacketType is in bits [3:0]
		pktType := header & 0x0F
		return pktType == audioPacketTypeSequenceStart
	}

	// Legacy AAC: SoundFormat=10 and AACPacketType=0
	if soundFormat == 10 {
		return data[1] == 0x00
	}
	return false
}

package media

import (
	"fmt"
)

// Audio codecs of interest for basic detection.
const (
	AudioCodecMP3   = "MP3"
	AudioCodecAAC   = "AAC"
	AudioCodecSpeex = "Speex"
)

// AAC packet types.
const (
	AACPacketTypeSequenceHeader = "sequence_header"
	AACPacketTypeRaw            = "raw"
)

// AudioMessage is a lightweight parsed representation of an RTMP audio (message type 8) tag.
// It only extracts enough information for codec detection / routing and leaves the raw
// payload bytes untouched for transparent relay.
//
// For AAC the tag structure is: [AudioHeader][AACPacketType][AACPayload...]
// For other codecs:             [AudioHeader][Payload...]
//
// AudioHeader (first byte) bits:
//
//	7-4: SoundFormat
//	3-2: SoundRate   (ignored here)
//	1:   SoundSize   (ignored)
//	0:   SoundType   (ignored)
//
// We support codec detection for: MP3 (2), AAC (10), Speex (11)
// Anything else returns an error so callers can decide how to handle.
type AudioMessage struct {
	Codec      string // One of AudioCodec* constants
	PacketType string // AAC only (sequence_header/raw), empty otherwise
	Payload    []byte // Raw payload (excludes header + AACPacketType if AAC)
}

// ParseAudioMessage parses a raw RTMP audio message payload (the FLV/RTMP tag data for
// message type 8) and returns an AudioMessage with codec metadata.
//
// Error cases:
//   - len(data) < 1: insufficient data
//   - Unsupported sound format (not MP3/AAC/Speex)
//   - AAC but len(data) < 2 (needs AACPacketType)
func ParseAudioMessage(data []byte) (*AudioMessage, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("audio.parse: empty payload")
	}
	header := data[0]
	soundFormat := (header >> 4) & 0x0F
	msg := &AudioMessage{}

	switch soundFormat {
	case 2:
		msg.Codec = AudioCodecMP3
		// MP3: rest of data after header
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
			// Non-standard packet type; keep numeric string for debugging but not fatal.
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

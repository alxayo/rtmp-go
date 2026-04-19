package media

// Multichannel Audio Configuration — E-RTMP v2
//
// AudioPacketType 5 carries multichannel audio layout configuration,
// telling the player how to map decoded audio samples to speaker positions.
//
// Wire format (after FourCC):
//
//	[AudioChannelOrder(4 bits)][AudioChannelCount(4 bits)]
//	[ChannelMapping... (variable, depends on AudioChannelOrder)]
//
// AudioChannelOrder values:
//
//	0 = Unspecified — channels are in codec-native order
//	1 = Native — codec-specific default layout (e.g., AAC channel config)
//	2 = Custom — explicit per-channel speaker assignment follows
//
// This configuration is cached and delivered to late-joining subscribers
// alongside the audio sequence header.

import "fmt"

// AudioChannelOrder constants specify how audio channels are arranged
// in a multichannel audio stream. These values occupy the high nibble
// (bits 7-4) of the first byte after the FourCC in a MultichannelConfig packet.
const (
	// ChannelOrderUnspecified means channel positions are unknown.
	// The player should use its default layout for the given channel count.
	ChannelOrderUnspecified uint8 = 0

	// ChannelOrderNative means channels follow the codec's native ordering.
	// For AAC, this would be center/left/right/surround etc. per ISO 14496-3.
	ChannelOrderNative uint8 = 1

	// ChannelOrderCustom means an explicit channel-to-speaker mapping follows
	// in the configuration data. Each channel is assigned a specific speaker
	// position from the AudioChannelFlags enumeration.
	ChannelOrderCustom uint8 = 2
)

// MultichannelConfig represents a parsed multichannel audio configuration
// from an AudioPacketType.MultichannelConfig (type 5) packet.
//
// Example channel counts:
//   - 1 = mono
//   - 2 = stereo
//   - 6 = 5.1 surround (front L/R, center, LFE, surround L/R)
//   - 8 = 7.1 surround (5.1 + back L/R)
type MultichannelConfig struct {
	// ChannelOrder indicates how channels are arranged (unspecified, native, custom).
	// See ChannelOrder* constants above.
	ChannelOrder uint8

	// ChannelCount is the number of audio channels (1=mono, 2=stereo, 6=5.1, 8=7.1, etc.).
	// Stored in the low nibble (bits 3-0) of the first byte, so max value is 15.
	ChannelCount uint8

	// ChannelMapping contains per-channel speaker assignments when ChannelOrder is Custom.
	// Each byte is an AudioChannelFlags value identifying the speaker position
	// (e.g., front-left, front-right, center, LFE, etc.).
	// Empty when ChannelOrder is Unspecified or Native.
	ChannelMapping []uint8
}

// ParseMultichannelConfig parses an AudioPacketType.MultichannelConfig payload.
// The input should be the data AFTER the FourCC bytes (i.e., AudioMessage.Payload
// when PacketType is "multichannel_config").
//
// Wire format:
//
//	byte 0: [AudioChannelOrder:4 bits][AudioChannelCount:4 bits]
//	bytes 1..N: per-channel speaker assignment (only when ChannelOrder == Custom)
func ParseMultichannelConfig(data []byte) (*MultichannelConfig, error) {
	// We need at least 1 byte for the channel order + channel count header.
	if len(data) < 1 {
		return nil, fmt.Errorf("multichannel.parse: payload too short (need >= 1 byte, got %d)", len(data))
	}

	// First byte layout:
	//   high nibble (bits 7-4) = AudioChannelOrder
	//   low nibble  (bits 3-0) = AudioChannelCount
	channelOrder := (data[0] >> 4) & 0x0F
	channelCount := data[0] & 0x0F

	cfg := &MultichannelConfig{
		ChannelOrder: channelOrder,
		ChannelCount: channelCount,
	}

	// For Custom channel order, the remaining bytes after the header specify
	// per-channel speaker positions. We read up to channelCount bytes.
	if channelOrder == ChannelOrderCustom && len(data) > 1 {
		// Allocate a slice to hold speaker assignments for each channel.
		mapping := make([]uint8, 0, channelCount)
		for i := 1; i < len(data) && uint8(len(mapping)) < channelCount; i++ {
			mapping = append(mapping, data[i])
		}
		cfg.ChannelMapping = mapping
	}

	return cfg, nil
}

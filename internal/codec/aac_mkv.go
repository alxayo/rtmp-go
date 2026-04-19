package codec

// This file implements AAC helper functions specific to Matroska containers.
//
// In Matroska (MKV), AAC audio is stored differently from MPEG-TS:
//
//   - MPEG-TS: Each AAC frame has an ADTS header (7 or 9 bytes) that describes
//     the audio format. We strip this header and use it to build the RTMP
//     AudioSpecificConfig sequence header.
//
//   - Matroska: There is NO per-frame ADTS header. Instead, the AudioSpecificConfig
//     is stored once as the track's CodecPrivate data (typically 2 bytes).
//     Frame data is raw AAC (already stripped of any headers).
//
// So for MKV AAC, we need BuildAACSequenceHeaderFromConfig() which wraps the
// raw AudioSpecificConfig in the RTMP audio tag format, instead of
// BuildAACSequenceHeader() which expects an ADTSHeader to build the config.
//
// The AudioSpecificConfig format (ISO/IEC 14496-3 §1.6.2.1):
//
//	Bits [4:0] = audioObjectType (e.g., 2 = AAC-LC)
//	Bits [3:0] = samplingFrequencyIndex (0-11 for standard rates)
//	Bits [3:0] = channelConfiguration (1-7)
//	Remaining  = 0 (for simple AAC-LC)
//
// This is typically just 2 bytes for AAC-LC, the most common profile.

// BuildAACSequenceHeaderFromConfig wraps a raw AudioSpecificConfig (from MKV
// CodecPrivate) in the RTMP audio tag format for use as a sequence header.
//
// This is the MKV equivalent of BuildAACSequenceHeader() — the difference is
// that BuildAACSequenceHeader() takes an ADTSHeader and constructs the config,
// while this function takes the pre-built config directly.
//
// Wire format:
//
//	Byte 0:    0xAF = [SoundFormat=10(AAC):4][SoundRate=3:2][SoundSize=1:1][SoundType=1:1]
//	Byte 1:    0x00 = AACPacketType (0 = sequence header)
//	Bytes 2+:  AudioSpecificConfig data (typically 2 bytes)
//
// Parameters:
//   - audioSpecificConfig: the raw AudioSpecificConfig from MKV CodecPrivate
//     (typically 2 bytes for AAC-LC)
func BuildAACSequenceHeaderFromConfig(audioSpecificConfig []byte) []byte {
	// 2 bytes RTMP header + AudioSpecificConfig data
	buf := make([]byte, 2+len(audioSpecificConfig))

	// Byte 0: SoundFormat=10(AAC)<<4 | SoundRate=3<<2 | SoundSize=1<<1 | SoundType=1
	// = 0xA0 | 0x0C | 0x02 | 0x01 = 0xAF
	buf[0] = 0xAF

	// Byte 1: AACPacketType = 0 (sequence header)
	buf[1] = 0x00

	// Copy the AudioSpecificConfig directly — no ADTS parsing needed
	copy(buf[2:], audioSpecificConfig)

	return buf
}

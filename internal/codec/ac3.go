package codec

// This file implements AC-3 (Dolby Digital) audio conversion for Enhanced RTMP.
//
// When AC-3 audio travels in MPEG-TS (over SRT), each frame is a "syncframe"
// that starts with the syncword 0x0B77. The syncframe header contains all the
// information needed to decode the frame: sample rate, bit rate, channel layout.
//
// Enhanced RTMP uses a FourCC-based tag format for AC-3:
//
//   Byte 0: [SoundFormat:4bits=9][AudioPacketType:4bits]
//     - SoundFormat 9 means "Enhanced RTMP, use FourCC"
//     - AudioPacketType 0 = SequenceStart (codec config)
//     - AudioPacketType 1 = CodedFrames (compressed audio)
//
//   Bytes 1-4: FourCC identifier ('ac-3' for AC-3)
//
//   Bytes 5+: Either AudioSpecificConfig (for sequence start) or raw syncframe data
//
// The AC-3 AudioSpecificConfig is a 3-byte structure derived from the
// dac3 (AC3SpecificBox) format used in MP4/ISOBMFF containers.

import (
	"fmt"
)

// ac3SyncWord is the 16-bit sync word that marks the start of every AC-3 frame.
// Every valid AC-3 syncframe begins with these two bytes: 0x0B 0x77.
const ac3SyncWord uint16 = 0x0B77

// ac3FourCC is the 4-byte FourCC identifier for AC-3 in Enhanced RTMP.
// This is placed in bytes 1-4 of the Enhanced RTMP audio tag.
var ac3FourCC = [4]byte{'a', 'c', '-', '3'}

// ac3SampleRates maps the 2-bit fscod field to sample rates in Hz.
// fscod is found in bits [7:6] of byte 4 of the syncframe.
//   - 0 = 48000 Hz (most common for broadcast)
//   - 1 = 44100 Hz
//   - 2 = 32000 Hz
//   - 3 = reserved (invalid)
var ac3SampleRates = [4]uint32{48000, 44100, 32000, 0}

// ac3ChannelCounts maps the 3-bit acmod (audio coding mode) field to
// the number of output channels (not counting LFE/subwoofer).
//   - 0 = 2 channels (dual mono, Ch1 + Ch2)
//   - 1 = 1 channel  (center only, mono)
//   - 2 = 2 channels (left + right, stereo)
//   - 3 = 3 channels (L, C, R)
//   - 4 = 3 channels (L, R, surround)
//   - 5 = 4 channels (L, C, R, surround)
//   - 6 = 4 channels (L, R, SL, SR)
//   - 7 = 5 channels (L, C, R, SL, SR) — used in 5.1
var ac3ChannelCounts = [8]uint8{2, 1, 2, 3, 3, 4, 4, 5}

// ac3FrameSizeTable maps (frmsizecod, fscod) to frame size in 16-bit words.
// Each row is a frmsizecod value (0-37), each column is an fscod value (0-2).
// The frame size in bytes = words * 2.
//
// This table is defined in ATSC A/52 Table 5.18.
// frmsizecod values 0-1 = 32 kbps, 2-3 = 40 kbps, etc.
var ac3FrameSizeTable = [38][3]uint16{
	{64, 69, 96},       // frmsizecod 0:  32 kbps
	{64, 70, 96},       // frmsizecod 1:  32 kbps
	{80, 87, 120},      // frmsizecod 2:  40 kbps
	{80, 88, 120},      // frmsizecod 3:  40 kbps
	{96, 104, 144},     // frmsizecod 4:  48 kbps
	{96, 105, 144},     // frmsizecod 5:  48 kbps
	{112, 121, 168},    // frmsizecod 6:  56 kbps
	{112, 122, 168},    // frmsizecod 7:  56 kbps
	{128, 139, 192},    // frmsizecod 8:  64 kbps
	{128, 140, 192},    // frmsizecod 9:  64 kbps
	{160, 174, 240},    // frmsizecod 10: 80 kbps
	{160, 175, 240},    // frmsizecod 11: 80 kbps
	{192, 208, 288},    // frmsizecod 12: 96 kbps
	{192, 209, 288},    // frmsizecod 13: 96 kbps
	{224, 243, 336},    // frmsizecod 14: 112 kbps
	{224, 244, 336},    // frmsizecod 15: 112 kbps
	{256, 278, 384},    // frmsizecod 16: 128 kbps
	{256, 279, 384},    // frmsizecod 17: 128 kbps
	{320, 348, 480},    // frmsizecod 18: 160 kbps
	{320, 349, 480},    // frmsizecod 19: 160 kbps
	{384, 417, 576},    // frmsizecod 20: 192 kbps
	{384, 418, 576},    // frmsizecod 21: 192 kbps
	{448, 487, 672},    // frmsizecod 22: 224 kbps
	{448, 488, 672},    // frmsizecod 23: 224 kbps
	{512, 557, 768},    // frmsizecod 24: 256 kbps
	{512, 558, 768},    // frmsizecod 25: 256 kbps
	{640, 696, 960},    // frmsizecod 26: 320 kbps
	{640, 697, 960},    // frmsizecod 27: 320 kbps
	{768, 835, 1152},   // frmsizecod 28: 384 kbps
	{768, 836, 1152},   // frmsizecod 29: 384 kbps
	{896, 975, 1344},   // frmsizecod 30: 448 kbps
	{896, 976, 1344},   // frmsizecod 31: 448 kbps
	{1024, 1114, 1536}, // frmsizecod 32: 512 kbps
	{1024, 1115, 1536}, // frmsizecod 33: 512 kbps
	{1152, 1253, 1728}, // frmsizecod 34: 576 kbps
	{1152, 1254, 1728}, // frmsizecod 35: 576 kbps
	{1280, 1393, 1920}, // frmsizecod 36: 640 kbps
	{1280, 1394, 1920}, // frmsizecod 37: 640 kbps
}

// AC3SyncInfo holds the parsed metadata from an AC-3 syncframe header.
// This information is extracted from the first few bytes of the syncframe
// and is needed to build the RTMP sequence header (AudioSpecificConfig).
type AC3SyncInfo struct {
	// SampleRate is the audio sample rate in Hz (48000, 44100, or 32000).
	SampleRate uint32

	// Channels is the number of audio channels (not counting LFE).
	// For example, 5.1 surround has Channels=5 (the ".1" LFE is separate).
	Channels uint8

	// Fscod is the 2-bit sample rate code from the syncframe header.
	// 0=48kHz, 1=44.1kHz, 2=32kHz. Stored for building AudioSpecificConfig.
	Fscod uint8

	// Frmsizecod is the 6-bit frame size code from the syncframe header.
	// It determines both the bit rate and frame size. Even values and the
	// next odd value share the same bit rate.
	Frmsizecod uint8

	// Bsid is the 5-bit bitstream identification field.
	// For standard AC-3, this must be ≤ 10. Values > 10 indicate E-AC-3.
	Bsid uint8

	// Bsmod is the 3-bit bitstream mode field.
	// 0 = main audio (complete main), which is the most common value.
	Bsmod uint8

	// Acmod is the 3-bit audio coding mode (channel layout).
	// Determines the speaker configuration (mono, stereo, 5.1, etc.).
	Acmod uint8
}

// ParseAC3SyncFrame parses the header of an AC-3 syncframe to extract
// audio format metadata. This is called on the first AC-3 frame received
// from the MPEG-TS demuxer to determine the stream's audio characteristics.
//
// AC-3 syncframe header layout (first 8 bytes):
//
//	Bytes 0-1: Syncword (0x0B77) — identifies this as an AC-3 frame
//	Bytes 2-3: CRC1 — error detection for the first 5/8 of the frame
//	Byte 4:    [fscod:2][frmsizecod:6] — sample rate and frame size codes
//	Byte 5:    [bsid:5][bsmod:3] — bitstream ID and mode
//	Byte 6:    [acmod:3][...] — audio coding mode (channel layout)
func ParseAC3SyncFrame(data []byte) (*AC3SyncInfo, error) {
	// Need at least 8 bytes to read all header fields we need:
	// 2 (syncword) + 2 (CRC1) + 1 (fscod+frmsizecod) + 1 (bsid+bsmod) + 1 (acmod) = 7
	// We require 8 for safe access to byte 6 which also has bits after acmod.
	if len(data) < 8 {
		return nil, fmt.Errorf("AC-3 syncframe too short: need at least 8 bytes, got %d", len(data))
	}

	// Verify the syncword (first 2 bytes must be 0x0B77)
	syncword := uint16(data[0])<<8 | uint16(data[1])
	if syncword != ac3SyncWord {
		return nil, fmt.Errorf("invalid AC-3 syncword: 0x%04X, expected 0x0B77", syncword)
	}

	info := &AC3SyncInfo{}

	// Byte 4: [fscod:2 bits][frmsizecod:6 bits]
	// fscod (bits 7-6) = sample rate code
	info.Fscod = (data[4] >> 6) & 0x03
	// frmsizecod (bits 5-0) = frame size code (index into frame size table)
	info.Frmsizecod = data[4] & 0x3F

	// Validate fscod — value 3 is reserved and not valid
	if info.Fscod >= 3 {
		return nil, fmt.Errorf("AC-3 reserved fscod value: %d", info.Fscod)
	}

	// Validate frmsizecod — valid range is 0-37
	if info.Frmsizecod >= 38 {
		return nil, fmt.Errorf("AC-3 invalid frmsizecod: %d (max 37)", info.Frmsizecod)
	}

	// Look up sample rate from the fscod code
	info.SampleRate = ac3SampleRates[info.Fscod]

	// Byte 5: [bsid:5 bits][bsmod:3 bits]
	// bsid (bits 7-3) = bitstream identification
	info.Bsid = (data[5] >> 3) & 0x1F
	// bsmod (bits 2-0) = bitstream mode
	info.Bsmod = data[5] & 0x07

	// AC-3 uses bsid values 0-10. Values > 10 indicate E-AC-3 (bsid=16).
	if info.Bsid > 10 {
		return nil, fmt.Errorf("AC-3 bsid %d indicates E-AC-3, not AC-3 (expected ≤ 10)", info.Bsid)
	}

	// Byte 6: [acmod:3 bits][cmixlev or surmixlev or other bits...]
	// acmod (bits 7-5) = audio coding mode (channel layout)
	info.Acmod = (data[6] >> 5) & 0x07

	// Look up channel count from acmod
	info.Channels = ac3ChannelCounts[info.Acmod]

	return info, nil
}

// BuildAC3SequenceHeader builds the Enhanced RTMP audio sequence header for AC-3.
// This is sent once before any audio frames to tell subscribers' decoders how
// to decode the AC-3 data.
//
// Wire format:
//
//	Byte 0:   0x90 = [SoundFormat=9:4bits][AudioPacketType=0(SequenceStart):4bits]
//	Bytes 1-4: FourCC 'ac-3'
//	Bytes 5-7: AC-3 AudioSpecificConfig (dac3 box content, 3 bytes)
//
// The AudioSpecificConfig follows the dac3 (AC3SpecificBox) format:
//
//	Byte 0: [fscod:2][bsid:5][bsmod_high:1]
//	Byte 1: [bsmod_low:2][acmod:3][lfeon:1][bit_rate_code_high:2]
//	Byte 2: [bit_rate_code_low:3][reserved:5]
func BuildAC3SequenceHeader(info *AC3SyncInfo) []byte {
	// Total size: 1 byte header + 4 bytes FourCC + 3 bytes config = 8 bytes
	buf := make([]byte, 8)

	// Byte 0: Enhanced RTMP header
	// SoundFormat=9 (upper nibble) | AudioPacketType=0 SequenceStart (lower nibble)
	// = 0b1001_0000 = 0x90
	buf[0] = 0x90

	// Bytes 1-4: FourCC 'ac-3'
	copy(buf[1:5], ac3FourCC[:])

	// Bytes 5-7: dac3 AudioSpecificConfig (3 bytes)
	// bit_rate_code is the upper 5 bits of frmsizecod divided by 2
	// (even and odd frmsizecod share the same bit rate)
	bitRateCode := info.Frmsizecod / 2

	// Byte 5: [fscod:2][bsid:5][bsmod_high_bit:1]
	buf[5] = (info.Fscod << 6) | (info.Bsid << 1) | (info.Bsmod >> 2)

	// Byte 6: [bsmod_low_2bits:2][acmod:3][lfeon:1][bit_rate_code_high_2bits:2]
	// lfeon = 0 (we don't detect LFE from the basic header fields)
	buf[6] = (info.Bsmod << 6) | (info.Acmod << 3) | (0 << 2) | ((bitRateCode >> 3) & 0x03)

	// Byte 7: [bit_rate_code_low_3bits:3][reserved:5]
	buf[7] = (bitRateCode & 0x07) << 5

	return buf
}

// BuildAC3AudioFrame wraps raw AC-3 syncframe data in the Enhanced RTMP
// audio tag format for transmission to subscribers.
//
// Wire format:
//
//	Byte 0:   0x91 = [SoundFormat=9:4bits][AudioPacketType=1(CodedFrames):4bits]
//	Bytes 1-4: FourCC 'ac-3'
//	Bytes 5+:  Raw AC-3 syncframe data (complete frame including 0x0B77 syncword)
func BuildAC3AudioFrame(data []byte) []byte {
	// Total size: 1 byte header + 4 bytes FourCC + raw frame data
	buf := make([]byte, 5+len(data))

	// Byte 0: Enhanced RTMP header
	// SoundFormat=9 (upper nibble) | AudioPacketType=1 CodedFrames (lower nibble)
	// = 0b1001_0001 = 0x91
	buf[0] = 0x91

	// Bytes 1-4: FourCC 'ac-3'
	copy(buf[1:5], ac3FourCC[:])

	// Bytes 5+: Raw AC-3 syncframe data
	copy(buf[5:], data)

	return buf
}

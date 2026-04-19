package codec

// This file implements E-AC-3 (Dolby Digital Plus) audio conversion for Enhanced RTMP.
//
// E-AC-3 is an extension of AC-3 that supports higher bit rates and more
// channels. Like AC-3, each frame starts with syncword 0x0B77, but E-AC-3
// frames are distinguished by having bsid > 10 (typically 16).
//
// Key differences from AC-3:
//   - Frame size is encoded directly in bytes 2-3 (not via a lookup table)
//   - bsid is always > 10 (typically 16)
//   - Supports more channel configurations and higher bit rates
//   - Uses FourCC 'ec-3' instead of 'ac-3' in Enhanced RTMP
//
// Enhanced RTMP uses the same tag structure as AC-3 but with FourCC 'ec-3':
//
//   Byte 0: [SoundFormat:4bits=9][AudioPacketType:4bits]
//   Bytes 1-4: FourCC identifier ('ec-3' for E-AC-3)
//   Bytes 5+: Either AudioSpecificConfig (for sequence start) or raw syncframe data
//
// The E-AC-3 AudioSpecificConfig is a 3-byte structure derived from the
// dec3 (EC3SpecificBox) format used in MP4/ISOBMFF containers.

import (
	"fmt"
)

// eac3FourCC is the 4-byte FourCC identifier for E-AC-3 in Enhanced RTMP.
// This is placed in bytes 1-4 of the Enhanced RTMP audio tag.
var eac3FourCC = [4]byte{'e', 'c', '-', '3'}

// eac3SampleRates maps the 2-bit fscod field to sample rates in Hz.
// E-AC-3 uses the same sample rate codes as AC-3:
//   - 0 = 48000 Hz (most common)
//   - 1 = 44100 Hz
//   - 2 = 32000 Hz
//   - 3 = reserved (but E-AC-3 can use fscod2 for half rates)
var eac3SampleRates = [4]uint32{48000, 44100, 32000, 0}

// eac3ChannelCounts maps the 3-bit acmod (audio coding mode) field to
// the number of output channels (not counting LFE/subwoofer).
// Same mapping as AC-3 — the acmod field has identical meaning.
var eac3ChannelCounts = [8]uint8{2, 1, 2, 3, 3, 4, 4, 5}

// EAC3SyncInfo holds the parsed metadata from an E-AC-3 syncframe header.
// This information is extracted from the first few bytes of the syncframe
// and is needed to build the RTMP sequence header (AudioSpecificConfig).
type EAC3SyncInfo struct {
	// SampleRate is the audio sample rate in Hz (48000, 44100, or 32000).
	SampleRate uint32

	// Channels is the number of audio channels (not counting LFE).
	Channels uint8

	// Fscod is the 2-bit sample rate code from the syncframe header.
	// 0=48kHz, 1=44.1kHz, 2=32kHz.
	Fscod uint8

	// FrameSize is the total frame size in bytes, derived from the frmsiz field.
	// frmsiz is an 11-bit field, and FrameSize = (frmsiz + 1) * 2.
	FrameSize uint16

	// Bsid is the 5-bit bitstream identification field.
	// For E-AC-3, this is > 10 (typically 16).
	Bsid uint8

	// Bsmod is the 3-bit bitstream mode field.
	// 0 = main audio (complete main), which is the most common value.
	Bsmod uint8

	// Acmod is the 3-bit audio coding mode (channel layout).
	// Same meaning as in AC-3.
	Acmod uint8

	// Lfeon indicates whether the Low Frequency Effects (LFE) channel
	// is present (the ".1" in "5.1"). true = LFE present.
	Lfeon bool
}

// ParseEAC3SyncFrame parses the header of an E-AC-3 syncframe to extract
// audio format metadata. This is called on the first E-AC-3 frame received
// from the MPEG-TS demuxer to determine the stream's audio characteristics.
//
// E-AC-3 syncframe header layout (first 6+ bytes):
//
//	Bytes 0-1: Syncword (0x0B77) — same as AC-3
//	Byte 2:    [strmtyp:2][substreamid:3][frmsiz_high:3]
//	Byte 3:    [frmsiz_low:8] — together with byte 2 gives 11-bit frmsiz
//	Byte 4:    [fscod:2][numblkscod:2][acmod:3][lfeon:1]
//	Byte 5:    [bsid:5][...] — bitstream ID (must be > 10 for E-AC-3)
//
// Note: The bit layout differs from AC-3. In E-AC-3, bsid is in byte 5
// (bits 7-3), while in AC-3 it's in byte 5 as well but the surrounding
// fields are different.
func ParseEAC3SyncFrame(data []byte) (*EAC3SyncInfo, error) {
	// Need at least 6 bytes to read all the header fields we need:
	// 2 (syncword) + 1 (strmtyp+substreamid+frmsiz_high) + 1 (frmsiz_low)
	// + 1 (fscod+numblkscod+acmod+lfeon) + 1 (bsid) = 6
	if len(data) < 6 {
		return nil, fmt.Errorf("E-AC-3 syncframe too short: need at least 6 bytes, got %d", len(data))
	}

	// Verify the syncword (first 2 bytes must be 0x0B77)
	syncword := uint16(data[0])<<8 | uint16(data[1])
	if syncword != ac3SyncWord {
		return nil, fmt.Errorf("invalid E-AC-3 syncword: 0x%04X, expected 0x0B77", syncword)
	}

	info := &EAC3SyncInfo{}

	// Byte 2: [strmtyp:2][substreamid:3][frmsiz_high:3]
	// Byte 3: [frmsiz_low:8]
	// frmsiz is 11 bits: 3 bits from byte 2 (bits 2-0) + 8 bits from byte 3.
	// Frame size in bytes = (frmsiz + 1) * 2
	frmsiz := (uint16(data[2]&0x07) << 8) | uint16(data[3])
	info.FrameSize = (frmsiz + 1) * 2

	// Byte 4: [fscod:2][numblkscod:2][acmod:3][lfeon:1]
	// fscod (bits 7-6) = sample rate code
	info.Fscod = (data[4] >> 6) & 0x03

	// For fscod=3, E-AC-3 uses a secondary fscod2 field (reduced sample rates).
	// For simplicity, we treat fscod=3 as 0 (48kHz) since it's rare in practice.
	if info.Fscod < 3 {
		info.SampleRate = eac3SampleRates[info.Fscod]
	} else {
		// fscod=3 indicates a reduced sample rate. Default to 48kHz.
		info.SampleRate = 48000
	}

	// acmod (bits 3-1) = audio coding mode
	info.Acmod = (data[4] >> 1) & 0x07

	// lfeon (bit 0) = LFE channel present
	info.Lfeon = (data[4] & 0x01) == 1

	// Look up channel count from acmod
	info.Channels = eac3ChannelCounts[info.Acmod]

	// Byte 5: [bsid:5][dialnorm:5 or bsmod:3 depending on version]
	// bsid (bits 7-3) = bitstream identification
	info.Bsid = (data[5] >> 3) & 0x1F

	// E-AC-3 must have bsid > 10 (typically 16)
	if info.Bsid <= 10 {
		return nil, fmt.Errorf("E-AC-3 bsid %d indicates AC-3, not E-AC-3 (expected > 10)", info.Bsid)
	}

	// bsmod — in E-AC-3, bsmod is not always at the same position as AC-3.
	// For simplicity we default to 0 (main audio) which is correct for most streams.
	info.Bsmod = 0

	return info, nil
}

// BuildEAC3SequenceHeader builds the Enhanced RTMP audio sequence header for E-AC-3.
// This is sent once before any audio frames to tell subscribers' decoders how
// to decode the E-AC-3 data.
//
// Wire format:
//
//	Byte 0:   0x90 = [SoundFormat=9:4bits][AudioPacketType=0(SequenceStart):4bits]
//	Bytes 1-4: FourCC 'ec-3'
//	Bytes 5-7: E-AC-3 AudioSpecificConfig (dec3 box content, 3 bytes)
//
// The AudioSpecificConfig follows the dec3 (EC3SpecificBox) format:
//
//	Bits [15:13]: num_ind_sub (number of independent substreams - 1, usually 0)
//	Bits [12:11]: fscod
//	Bits [10:6]:  bsid
//	Bits [5:3]:   bsmod
//	Bits [2:0]:   acmod
//	Byte 2:       [lfeon:1][reserved:3][num_dep_sub:4]
func BuildEAC3SequenceHeader(info *EAC3SyncInfo) []byte {
	// Total size: 1 byte header + 4 bytes FourCC + 3 bytes config = 8 bytes
	buf := make([]byte, 8)

	// Byte 0: Enhanced RTMP header
	// SoundFormat=9 (upper nibble) | AudioPacketType=0 SequenceStart (lower nibble)
	buf[0] = 0x90

	// Bytes 1-4: FourCC 'ec-3'
	copy(buf[1:5], eac3FourCC[:])

	// Bytes 5-7: dec3 AudioSpecificConfig (3 bytes)
	// Build the 16-bit first word:
	// [num_ind_sub:3][fscod:2][bsid:5][bsmod:3][acmod:3]
	word := uint16(0)                               // num_ind_sub = 0 (1 independent substream)
	word |= uint16(info.Fscod&0x03) << 11           // fscod in bits [12:11]
	word |= uint16(info.Bsid&0x1F) << 6             // bsid in bits [10:6]
	word |= uint16(info.Bsmod&0x07) << 3            // bsmod in bits [5:3]
	word |= uint16(info.Acmod & 0x07)               // acmod in bits [2:0]

	buf[5] = byte(word >> 8)
	buf[6] = byte(word & 0xFF)

	// Byte 7: [lfeon:1][reserved:3][num_dep_sub:4]
	var lfeonBit byte
	if info.Lfeon {
		lfeonBit = 0x80 // bit 7 set
	}
	buf[7] = lfeonBit // reserved=0, num_dep_sub=0

	return buf
}

// BuildEAC3AudioFrame wraps raw E-AC-3 syncframe data in the Enhanced RTMP
// audio tag format for transmission to subscribers.
//
// Wire format:
//
//	Byte 0:   0x91 = [SoundFormat=9:4bits][AudioPacketType=1(CodedFrames):4bits]
//	Bytes 1-4: FourCC 'ec-3'
//	Bytes 5+:  Raw E-AC-3 syncframe data (complete frame including 0x0B77 syncword)
func BuildEAC3AudioFrame(data []byte) []byte {
	// Total size: 1 byte header + 4 bytes FourCC + raw frame data
	buf := make([]byte, 5+len(data))

	// Byte 0: Enhanced RTMP header
	// SoundFormat=9 (upper nibble) | AudioPacketType=1 CodedFrames (lower nibble)
	buf[0] = 0x91

	// Bytes 1-4: FourCC 'ec-3'
	copy(buf[1:5], eac3FourCC[:])

	// Bytes 5+: Raw E-AC-3 syncframe data
	copy(buf[5:], data)

	return buf
}

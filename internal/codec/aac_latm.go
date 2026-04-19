package codec

// This file implements LATM/LOAS AAC de-encapsulation for the SRT bridge.
//
// LATM (Low-overhead Audio Transport Multiplex) is an alternative framing
// for AAC in MPEG-TS streams (stream type 0x11). It is used by some broadcast
// encoders (Japanese ISDB, some DVB systems) instead of the more common ADTS
// framing (stream type 0x0F).
//
// LOAS (Low Overhead Audio Stream) adds a sync layer on top of LATM:
//
//   LOAS frame: [SyncWord(11b)][FrameLength(13b)][AudioMuxElement...]
//   SyncWord = 0x2B7 → first two bytes are 0x56, 0xE0-0xFF
//
// The AudioMuxElement contains:
//   - useSameStreamMux bit: if 0, StreamMuxConfig follows (contains AudioSpecificConfig)
//   - PayloadLengthInfo: variable-length encoded payload size
//   - PayloadMux: raw AAC frame data
//
// This parser handles the common case:
//   - audioMuxVersion = 0
//   - Single stream (numProgram=0, numLayer=0)
//   - AAC-LC profile
//
// The output (AudioSpecificConfig + raw AAC) is identical to what StripADTS()
// produces, so the bridge can reuse BuildAACSequenceHeaderFromConfig() and
// BuildAACFrame() for the RTMP output.

import (
	"errors"
	"fmt"
)

// LATMFrame represents a parsed LATM/LOAS frame containing raw AAC data
// and the AudioSpecificConfig needed for decoder initialization.
type LATMFrame struct {
	// AudioSpecificConfig is the AAC decoder configuration (typically 2 bytes).
	// Only populated when StreamMuxConfig is present in this frame.
	AudioSpecificConfig []byte

	// RawAAC is the raw AAC frame data with all LATM framing removed.
	RawAAC []byte
}

// bitReader provides bit-level reading from a byte slice.
// LATM requires bit-level parsing since fields are not byte-aligned.
type bitReader struct {
	data []byte
	pos  int // current bit position
}

func newBitReader(data []byte) *bitReader {
	return &bitReader{data: data}
}

func (br *bitReader) bitsLeft() int {
	return len(br.data)*8 - br.pos
}

// readBits reads n bits (1-32) and returns them as a uint32.
func (br *bitReader) readBits(n int) (uint32, error) {
	if n <= 0 || n > 32 {
		return 0, fmt.Errorf("latm: invalid bit count %d", n)
	}
	if br.bitsLeft() < n {
		return 0, errors.New("latm: not enough bits")
	}

	var result uint32
	for i := 0; i < n; i++ {
		byteIdx := br.pos / 8
		bitIdx := 7 - (br.pos % 8)
		bit := (br.data[byteIdx] >> uint(bitIdx)) & 1
		result = (result << 1) | uint32(bit)
		br.pos++
	}
	return result, nil
}

// readBit reads a single bit.
func (br *bitReader) readBit() (uint32, error) {
	return br.readBits(1)
}

// alignToByte advances position to the next byte boundary.
func (br *bitReader) alignToByte() {
	if br.pos%8 != 0 {
		br.pos += 8 - (br.pos%8)
	}
}

// remainingBytes returns the remaining data from current position (byte-aligned).
func (br *bitReader) remainingBytes() []byte {
	br.alignToByte()
	bytePos := br.pos / 8
	if bytePos >= len(br.data) {
		return nil
	}
	return br.data[bytePos:]
}

// ParseLATMFrame parses a LATM/LOAS frame and extracts the raw AAC data
// and AudioSpecificConfig.
//
// The input should be raw PES payload data from an MPEG-TS stream with
// stream type 0x11 (AAC-LATM). It may or may not have the LOAS sync layer.
//
// Parameters:
//   - data: raw bytes from the TS PES payload
//   - cachedASC: previously extracted AudioSpecificConfig (used when the
//     current frame doesn't contain StreamMuxConfig). Pass nil on first call.
//
// Returns the parsed frame with raw AAC data. If AudioSpecificConfig is nil
// in the result, use the cached value from a previous frame.
func ParseLATMFrame(data []byte, cachedASC []byte) (*LATMFrame, error) {
	if len(data) < 3 {
		return nil, errors.New("latm: frame too short")
	}

	// Check for LOAS sync word (0x2B7 in top 11 bits)
	// Byte pattern: 0x56 0xE0-0xFF ...
	payload := data
	if len(data) >= 3 && data[0] == 0x56 && (data[1]&0xE0) == 0xE0 {
		// LOAS sync present — extract frame length and skip header
		frameLen := (int(data[1]&0x1F) << 8) | int(data[2])
		if frameLen+3 > len(data) {
			return nil, fmt.Errorf("latm: LOAS frame truncated (need %d, have %d)", frameLen+3, len(data))
		}
		payload = data[3 : 3+frameLen]
	}

	return parseAudioMuxElement(payload, cachedASC)
}

// parseAudioMuxElement parses the AudioMuxElement which is the LATM payload
// after stripping the optional LOAS sync header.
func parseAudioMuxElement(data []byte, cachedASC []byte) (*LATMFrame, error) {
	if len(data) == 0 {
		return nil, errors.New("latm: empty AudioMuxElement")
	}

	br := newBitReader(data)
	frame := &LATMFrame{}

	// useSameStreamMux: 1 bit
	// 0 = StreamMuxConfig follows (contains AudioSpecificConfig)
	// 1 = reuse previous config (must use cachedASC)
	useSame, err := br.readBit()
	if err != nil {
		return nil, fmt.Errorf("latm: read useSameStreamMux: %w", err)
	}

	if useSame == 0 {
		// StreamMuxConfig present — parse it to extract AudioSpecificConfig
		asc, err := parseStreamMuxConfig(br)
		if err != nil {
			return nil, fmt.Errorf("latm: parse StreamMuxConfig: %w", err)
		}
		frame.AudioSpecificConfig = asc
	} else {
		// Reuse cached config
		frame.AudioSpecificConfig = cachedASC
	}

	// PayloadLengthInfo: variable-length encoded
	// Read bytes until a byte != 0xFF is found; sum all bytes for total length
	payloadLen, err := readPayloadLengthInfo(br)
	if err != nil {
		return nil, fmt.Errorf("latm: read PayloadLengthInfo: %w", err)
	}

	// PayloadMux: raw AAC frame data
	// After PayloadLengthInfo, the remaining data is byte-aligned AAC
	br.alignToByte()
	bytePos := br.pos / 8
	if bytePos+payloadLen > len(data) {
		// If calculated payload extends beyond data, use all remaining bytes
		// (some encoders don't include padding after the AAC data)
		payloadLen = len(data) - bytePos
	}
	if payloadLen <= 0 {
		return nil, errors.New("latm: no AAC payload data")
	}

	frame.RawAAC = data[bytePos : bytePos+payloadLen]
	return frame, nil
}

// parseStreamMuxConfig parses the StreamMuxConfig to extract AudioSpecificConfig.
//
// Simplified for the common live streaming case:
//   - audioMuxVersion = 0
//   - allStreamsSameTimeFraming = 1
//   - numSubFrames = 0 (one frame per LOAS packet)
//   - numProgram = 0, numLayer = 0
//   - frameLengthType = 0 (variable frame length)
func parseStreamMuxConfig(br *bitReader) ([]byte, error) {
	// audioMuxVersion: 1 bit (we only support version 0)
	version, err := br.readBit()
	if err != nil {
		return nil, err
	}
	if version != 0 {
		return nil, fmt.Errorf("unsupported audioMuxVersion %d (only 0 supported)", version)
	}

	// allStreamsSameTimeFraming: 1 bit
	if _, err := br.readBit(); err != nil {
		return nil, err
	}

	// numSubFrames: 6 bits (number of PayloadMux chunks per AudioMuxElement - 1)
	if _, err := br.readBits(6); err != nil {
		return nil, err
	}

	// numProgram: 4 bits (number of programs - 1)
	numProgram, err := br.readBits(4)
	if err != nil {
		return nil, err
	}
	if numProgram != 0 {
		return nil, fmt.Errorf("unsupported numProgram %d (only single program supported)", numProgram)
	}

	// numLayer: 3 bits (number of layers per program - 1)
	numLayer, err := br.readBits(3)
	if err != nil {
		return nil, err
	}
	if numLayer != 0 {
		return nil, fmt.Errorf("unsupported numLayer %d (only single layer supported)", numLayer)
	}

	// AudioSpecificConfig follows inline (variable length)
	// We need to parse it to know its length, then extract the raw bytes.
	ascStartBit := br.pos
	asc, err := parseAndExtractASC(br)
	if err != nil {
		// Fallback: try to extract a minimal 2-byte ASC
		br.pos = ascStartBit
		return extractMinimalASC(br)
	}
	_ = ascStartBit // used in error path
	return asc, nil
}

// parseAndExtractASC parses AudioSpecificConfig and returns its raw bytes.
//
// AudioSpecificConfig structure (ISO/IEC 14496-3 §1.6.2.1):
//   bits [4:0] = audioObjectType (5 bits, or 5+6 if extended)
//   bits [3:0] = samplingFrequencyIndex (4 bits, +24 if index==0xF)
//   bits [3:0] = channelConfiguration (4 bits)
//   remaining  = depends on audioObjectType (usually 0 for AAC-LC)
func parseAndExtractASC(br *bitReader) ([]byte, error) {
	startBit := br.pos

	// audioObjectType: 5 bits (extended: if 31, read 6 more bits)
	objType, err := br.readBits(5)
	if err != nil {
		return nil, err
	}
	if objType == 31 {
		ext, err := br.readBits(6)
		if err != nil {
			return nil, err
		}
		objType = 32 + ext
	}

	// samplingFrequencyIndex: 4 bits (if 0xF, read 24-bit explicit frequency)
	freqIdx, err := br.readBits(4)
	if err != nil {
		return nil, err
	}
	if freqIdx == 0x0F {
		// 24-bit explicit sampling frequency
		if _, err := br.readBits(24); err != nil {
			return nil, err
		}
	}

	// channelConfiguration: 4 bits
	if _, err := br.readBits(4); err != nil {
		return nil, err
	}

	// For SBR (5) and PS (29), there's extended frequency info
	if objType == 5 || objType == 29 {
		// extensionSamplingFrequencyIndex: 4 bits
		extFreqIdx, err := br.readBits(4)
		if err != nil {
			return nil, err
		}
		if extFreqIdx == 0x0F {
			if _, err := br.readBits(24); err != nil {
				return nil, err
			}
		}
		// extensionAudioObjectType: 5 bits
		extObjType, err := br.readBits(5)
		if err != nil {
			return nil, err
		}
		if extObjType == 31 {
			if _, err := br.readBits(6); err != nil {
				return nil, err
			}
		}
	}

	// frameLengthType for StreamMuxConfig: 3 bits
	flt, err := br.readBits(3)
	if err != nil {
		return nil, err
	}
	if flt == 0 {
		// latmBufferFullness: 8 bits
		if _, err := br.readBits(8); err != nil {
			return nil, err
		}
	}

	// otherDataPresent: 1 bit
	otherData, err := br.readBit()
	if err != nil {
		return nil, err
	}
	if otherData == 1 {
		// otherDataLenBits: variable (read 8-bit chunks until escape bit is 0)
		for {
			escape, err := br.readBit()
			if err != nil {
				return nil, err
			}
			if _, err := br.readBits(8); err != nil {
				return nil, err
			}
			if escape == 0 {
				break
			}
		}
	}

	// crcCheckPresent: 1 bit
	crcPresent, err := br.readBit()
	if err != nil {
		return nil, err
	}
	if crcPresent == 1 {
		// crcCheckSum: 8 bits
		if _, err := br.readBits(8); err != nil {
			return nil, err
		}
	}

	// The AudioSpecificConfig is the portion we parsed for objType, freqIdx, channels
	// Reconstruct it as 2 bytes (standard AAC-LC case)
	_ = startBit
	return buildASCFromParsed(objType, freqIdx), nil
}

// extractMinimalASC extracts a minimal 2-byte AudioSpecificConfig by reading
// just the essential fields (audioObjectType, freqIndex, channelConfig).
func extractMinimalASC(br *bitReader) ([]byte, error) {
	// audioObjectType: 5 bits
	objType, err := br.readBits(5)
	if err != nil {
		return nil, err
	}
	if objType == 31 {
		ext, err := br.readBits(6)
		if err != nil {
			return nil, err
		}
		objType = 32 + ext
	}

	// samplingFrequencyIndex: 4 bits
	freqIdx, err := br.readBits(4)
	if err != nil {
		return nil, err
	}
	if freqIdx == 0x0F {
		// Skip 24-bit explicit frequency — use index 4 (44100) as fallback
		if _, err := br.readBits(24); err != nil {
			return nil, err
		}
		freqIdx = 4
	}

	// channelConfiguration: 4 bits
	chanConfig, err := br.readBits(4)
	if err != nil {
		return nil, err
	}

	// Build 2-byte AudioSpecificConfig
	// Byte 0: [objectType:5][freqIdx_high:3]
	// Byte 1: [freqIdx_low:1][channelConfig:4][0:3]
	asc := make([]byte, 2)
	asc[0] = byte(objType<<3) | byte(freqIdx>>1)
	asc[1] = byte(freqIdx<<7) | byte(chanConfig<<3)
	return asc, nil
}

// buildASCFromParsed constructs a 2-byte AudioSpecificConfig from parsed fields.
func buildASCFromParsed(objType, freqIdx uint32) []byte {
	// For extended object types, cap to fit in 5 bits for the basic ASC
	if objType > 31 {
		objType = 2 // Fall back to AAC-LC
	}
	if freqIdx > 12 {
		freqIdx = 4 // Fall back to 44100 Hz
	}

	asc := make([]byte, 2)
	asc[0] = byte(objType<<3) | byte(freqIdx>>1)
	asc[1] = byte(freqIdx<<7) | byte(2<<3) // default stereo (channelConfig=2)
	return asc
}

// StripLATM is a high-level helper that strips LATM framing and returns
// raw AAC data plus the AudioSpecificConfig. This is the LATM equivalent
// of StripADTS().
//
// Parameters:
//   - data: raw PES payload from TS stream type 0x11
//   - cachedASC: previously extracted AudioSpecificConfig (nil on first call)
//
// Returns:
//   - rawAAC: raw AAC frame data ready for BuildAACFrame()
//   - asc: AudioSpecificConfig (may be nil if not in this frame — use cached)
//   - err: parsing error
func StripLATM(data []byte, cachedASC []byte) (rawAAC []byte, asc []byte, err error) {
	frame, err := ParseLATMFrame(data, cachedASC)
	if err != nil {
		return nil, nil, err
	}
	return frame.RawAAC, frame.AudioSpecificConfig, nil
}

// readPayloadLengthInfo reads the variable-length PayloadLengthInfo field.
// Format: read 8-bit values, accumulating sum; stop when a value != 255 is read.
func readPayloadLengthInfo(br *bitReader) (int, error) {
	length := 0
	for {
		b, err := br.readBits(8)
		if err != nil {
			return 0, err
		}
		length += int(b)
		if b != 255 {
			break
		}
	}
	return length, nil
}

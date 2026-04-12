package media

// FLVMetadata holds extracted properties for the FLV onMetaData script tag.
type FLVMetadata struct {
	Width           int
	Height          int
	VideoCodecID    float64 // FLV video codec ID: 7=AVC, 12=HEVC
	AudioCodecID    float64 // FLV audio codec ID: 10=AAC, 2=MP3
	AudioSampleRate float64
	AudioChannels   int
	Stereo          bool
}

// bitReader reads individual bits from a byte slice.
type bitReader struct {
	data []byte
	pos  int // bit position
}

func (br *bitReader) readBits(n int) (uint32, error) {
	if n <= 0 || n > 32 {
		return 0, errBitReader
	}
	var result uint32
	for i := 0; i < n; i++ {
		byteIdx := br.pos / 8
		if byteIdx >= len(br.data) {
			return 0, errBitReader
		}
		bitIdx := 7 - (br.pos % 8)
		result = (result << 1) | uint32((br.data[byteIdx]>>bitIdx)&1)
		br.pos++
	}
	return result, nil
}

// readExpGolomb reads an unsigned Exp-Golomb coded value (ue(v)).
func (br *bitReader) readExpGolomb() (uint32, error) {
	leadingZeros := 0
	for {
		byteIdx := br.pos / 8
		if byteIdx >= len(br.data) {
			return 0, errBitReader
		}
		bitIdx := 7 - (br.pos % 8)
		bit := (br.data[byteIdx] >> bitIdx) & 1
		br.pos++
		if bit == 1 {
			break
		}
		leadingZeros++
		if leadingZeros > 31 {
			return 0, errBitReader
		}
	}
	if leadingZeros == 0 {
		return 0, nil
	}
	suffix, err := br.readBits(leadingZeros)
	if err != nil {
		return 0, err
	}
	return (1 << leadingZeros) - 1 + suffix, nil
}

func (br *bitReader) skipBits(n int) error {
	if br.pos+n > len(br.data)*8 {
		return errBitReader
	}
	br.pos += n
	return nil
}

var errBitReader = errorf("bit reader: out of bounds")

type errorString struct{ s string }

func (e *errorString) Error() string { return e.s }
func errorf(s string) error          { return &errorString{s} }

// High profiles that require chroma_format_idc parsing in SPS.
var highProfiles = map[uint32]bool{
	100: true, 110: true, 122: true, 244: true, 44: true,
	83: true, 86: true, 118: true, 128: true, 138: true, 139: true, 134: true,
}

// ExtractVideoMetadata extracts width and height from an RTMP video sequence header.
// For H.264, this parses the AVCDecoderConfigurationRecord to find the SPS NALU,
// then decodes the SPS using Exp-Golomb to extract dimensions.
// Returns (0, 0) if parsing fails (graceful degradation).
func ExtractVideoMetadata(payload []byte) (width, height int) {
	// Need at least: 5 (FLV header) + 6 (AVCC up to numSPS) + 2 (spsLength) = 13 bytes
	if len(payload) < 13 {
		return 0, 0
	}

	// Verify this is a keyframe AVC sequence header
	if payload[0] != 0x17 || payload[1] != 0x00 {
		return 0, 0
	}

	avcc := payload[5:] // skip FLV video tag header (5 bytes)

	// Parse AVCDecoderConfigurationRecord
	numSPS := int(avcc[5] & 0x1F)
	if numSPS < 1 {
		return 0, 0
	}

	if len(avcc) < 8 {
		return 0, 0
	}
	spsLength := int(avcc[6])<<8 | int(avcc[7])
	if spsLength < 2 || len(avcc) < 8+spsLength {
		return 0, 0
	}

	spsNALU := avcc[8 : 8+spsLength]

	// Skip NAL header byte, parse SPS body
	return parseSPS(spsNALU[1:])
}

func parseSPS(sps []byte) (width, height int) {
	if len(sps) < 3 {
		return 0, 0
	}

	br := &bitReader{data: sps}

	profileIDC, err := br.readBits(8)
	if err != nil {
		return 0, 0
	}

	// Skip constraint flags + reserved
	if err = br.skipBits(8); err != nil {
		return 0, 0
	}
	// Skip level_idc
	if err = br.skipBits(8); err != nil {
		return 0, 0
	}
	// seq_parameter_set_id
	if _, err = br.readExpGolomb(); err != nil {
		return 0, 0
	}

	var chromaFormatIDC uint32 = 1 // default 4:2:0 per H.264 spec
	var separateColourPlaneFlag uint32

	if highProfiles[profileIDC] {
		chromaFormatIDC, err = br.readExpGolomb()
		if err != nil {
			return 0, 0
		}
		if chromaFormatIDC == 3 {
			val, err2 := br.readBits(1)
			if err2 != nil {
				return 0, 0
			}
			separateColourPlaneFlag = val
		}
		// bit_depth_luma_minus8
		if _, err = br.readExpGolomb(); err != nil {
			return 0, 0
		}
		// bit_depth_chroma_minus8
		if _, err = br.readExpGolomb(); err != nil {
			return 0, 0
		}
		// qpprime_y_zero_transform_bypass_flag
		if err = br.skipBits(1); err != nil {
			return 0, 0
		}
		// seq_scaling_matrix_present_flag
		scalingPresent, err2 := br.readBits(1)
		if err2 != nil {
			return 0, 0
		}
		if scalingPresent != 0 {
			// Scaling lists are complex; bail out gracefully
			return 0, 0
		}
	}

	// log2_max_frame_num_minus4
	if _, err = br.readExpGolomb(); err != nil {
		return 0, 0
	}

	picOrderCntType, err := br.readExpGolomb()
	if err != nil {
		return 0, 0
	}
	switch picOrderCntType {
	case 0:
		// log2_max_pic_order_cnt_lsb_minus4
		if _, err = br.readExpGolomb(); err != nil {
			return 0, 0
		}
	case 1:
		// Complex to parse; bail out
		return 0, 0
	case 2:
		// Nothing to read
	default:
		return 0, 0
	}

	// max_num_ref_frames
	if _, err = br.readExpGolomb(); err != nil {
		return 0, 0
	}
	// gaps_in_frame_num_value_allowed_flag
	if err = br.skipBits(1); err != nil {
		return 0, 0
	}

	picWidthInMbsMinus1, err := br.readExpGolomb()
	if err != nil {
		return 0, 0
	}
	picHeightInMapUnitsMinus1, err := br.readExpGolomb()
	if err != nil {
		return 0, 0
	}

	frameMbsOnlyFlag, err := br.readBits(1)
	if err != nil {
		return 0, 0
	}
	if frameMbsOnlyFlag == 0 {
		// mb_adaptive_frame_field_flag
		if err = br.skipBits(1); err != nil {
			return 0, 0
		}
	}

	// direct_8x8_inference_flag
	if err = br.skipBits(1); err != nil {
		return 0, 0
	}

	var cropLeft, cropRight, cropTop, cropBottom uint32
	frameCroppingFlag, err := br.readBits(1)
	if err != nil {
		return 0, 0
	}
	if frameCroppingFlag != 0 {
		cropLeft, err = br.readExpGolomb()
		if err != nil {
			return 0, 0
		}
		cropRight, err = br.readExpGolomb()
		if err != nil {
			return 0, 0
		}
		cropTop, err = br.readExpGolomb()
		if err != nil {
			return 0, 0
		}
		cropBottom, err = br.readExpGolomb()
		if err != nil {
			return 0, 0
		}
	}

	// Calculate dimensions
	chromaArrayType := chromaFormatIDC
	if separateColourPlaneFlag != 0 {
		chromaArrayType = 0
	}

	subWidthC := 1
	subHeightC := 1
	if chromaArrayType == 1 || chromaArrayType == 2 {
		subWidthC = 2
	}
	if chromaArrayType == 1 {
		subHeightC = 2
	}

	width = int(picWidthInMbsMinus1+1)*16 -
		subWidthC*int(cropLeft+cropRight)
	height = int(2-frameMbsOnlyFlag)*int(picHeightInMapUnitsMinus1+1)*16 -
		subHeightC*int(cropTop+cropBottom)

	if width <= 0 || height <= 0 {
		return 0, 0
	}
	return width, height
}

// AAC sample rates indexed by frequencyIndex (ISO 14496-3).
var aacSampleRates = []int{
	96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050,
	16000, 12000, 11025, 8000, 7350,
}

// aacChannelCounts indexed by channelConfiguration (ISO 14496-3).
var aacChannelCounts = []int{
	0, 1, 2, 3, 4, 5, 6, 8,
}

// ExtractAudioMetadata extracts sample rate and channel info from an RTMP audio sequence header.
// For AAC, parses AudioSpecificConfig. Returns zero values if parsing fails.
func ExtractAudioMetadata(payload []byte) (sampleRate int, channels int, stereo bool) {
	// Need at least: 1 (audio tag header) + 1 (AACPacketType) + 2 (AudioSpecificConfig)
	if len(payload) < 4 {
		return 0, 0, false
	}

	// Verify AAC sequence header
	soundFormat := (payload[0] >> 4) & 0x0F
	if soundFormat != 0x0A { // AAC
		return 0, 0, false
	}
	if payload[1] != 0x00 { // sequence header
		return 0, 0, false
	}

	asc := payload[2:]
	if len(asc) < 2 {
		return 0, 0, false
	}

	br := &bitReader{data: asc}

	// audioObjectType (5 bits)
	aot, err := br.readBits(5)
	if err != nil {
		return 0, 0, false
	}
	// Extended AOT
	if aot == 31 {
		ext, err2 := br.readBits(6)
		if err2 != nil {
			return 0, 0, false
		}
		aot = 32 + ext
	}
	_ = aot

	// frequencyIndex (4 bits)
	freqIdx, err := br.readBits(4)
	if err != nil {
		return 0, 0, false
	}
	if freqIdx == 0x0F {
		// Explicit 24-bit sample rate
		sr, err2 := br.readBits(24)
		if err2 != nil {
			return 0, 0, false
		}
		sampleRate = int(sr)
	} else if int(freqIdx) < len(aacSampleRates) {
		sampleRate = aacSampleRates[freqIdx]
	} else {
		return 0, 0, false
	}

	// channelConfiguration (4 bits)
	chanCfg, err := br.readBits(4)
	if err != nil {
		return 0, 0, false
	}
	if int(chanCfg) < len(aacChannelCounts) {
		channels = aacChannelCounts[chanCfg]
	}

	stereo = channels == 2
	return sampleRate, channels, stereo
}

// VideoCodecFLVID returns the FLV numeric codec ID for a video codec string.
func VideoCodecFLVID(codec string) float64 {
	switch codec {
	case VideoCodecAVC:
		return 7
	case VideoCodecHEVC:
		return 12
	default:
		return 0
	}
}

// AudioCodecFLVID returns the FLV numeric codec ID for an audio codec string.
func AudioCodecFLVID(codec string) float64 {
	switch codec {
	case AudioCodecAAC:
		return 10
	case AudioCodecMP3:
		return 2
	case AudioCodecSpeex:
		return 11
	default:
		return 0
	}
}

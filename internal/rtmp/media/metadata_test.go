package media

import (
	"testing"
)

// buildAVCC wraps an SPS NALU in an RTMP video sequence header (keyframe + AVC + AVCC record).
func buildAVCC(spsNALU []byte) []byte {
	buf := []byte{
		0x17, 0x00, 0x00, 0x00, 0x00, // FLV: keyframe + AVC, seqhdr, CTS=0
		0x01,        // configurationVersion
		spsNALU[1],  // AVCProfileIndication
		spsNALU[2],  // profile_compatibility
		spsNALU[3],  // AVCLevelIndication
		0xFF,        // lengthSizeMinusOne=3
		0xE1,        // numSPS=1
		byte(len(spsNALU) >> 8), byte(len(spsNALU)), // SPS length (BE)
	}
	return append(buf, spsNALU...)
}

func TestExtractVideoMetadata_H264_640x360(t *testing.T) {
	// Manually constructed SPS for 640x360, Baseline profile (66), level 3.0.
	// pic_width_in_mbs_minus1=39, pic_height_in_map_units_minus1=22,
	// frame_mbs_only=1, crop_bottom=4 (360 = 23*16 - 2*4).
	spsNALU := []byte{
		0x67, // NAL header: nal_ref_idc=3, nal_unit_type=7 (SPS)
		0x42, 0xC0, 0x1E, // profile=66, compat=0xC0, level=30
		0xF4, 0x05, 0x01, 0x7B, 0xCA,
	}
	payload := buildAVCC(spsNALU)

	w, h := ExtractVideoMetadata(payload)
	if w != 640 || h != 360 {
		t.Fatalf("expected 640x360, got %dx%d", w, h)
	}
}

func TestExtractVideoMetadata_H264_1920x1080(t *testing.T) {
	// Manually constructed SPS for 1920x1080, High profile (100), level 4.0.
	// chroma_format_idc=1, pic_width_in_mbs_minus1=119,
	// pic_height_in_map_units_minus1=67, frame_mbs_only=1, crop_bottom=4.
	spsNALU := []byte{
		0x67, // NAL header
		0x64, 0x00, 0x28, // profile=100, compat=0x00, level=40
		0xAC, 0xE5, 0x01, 0xE0, 0x08, 0x9F, 0x94,
	}
	payload := buildAVCC(spsNALU)

	w, h := ExtractVideoMetadata(payload)
	if w != 1920 || h != 1080 {
		t.Fatalf("expected 1920x1080, got %dx%d", w, h)
	}
}

func TestExtractVideoMetadata_Empty(t *testing.T) {
	w, h := ExtractVideoMetadata(nil)
	if w != 0 || h != 0 {
		t.Fatalf("expected 0x0 for nil, got %dx%d", w, h)
	}
	w, h = ExtractVideoMetadata([]byte{})
	if w != 0 || h != 0 {
		t.Fatalf("expected 0x0 for empty, got %dx%d", w, h)
	}
}

func TestExtractVideoMetadata_TooShort(t *testing.T) {
	cases := [][]byte{
		{0x17},
		{0x17, 0x00, 0x00},
		{0x17, 0x00, 0x00, 0x00, 0x00, 0x01, 0x42}, // truncated AVCC
	}
	for _, payload := range cases {
		w, h := ExtractVideoMetadata(payload)
		if w != 0 || h != 0 {
			t.Fatalf("expected 0x0 for truncated input len=%d, got %dx%d", len(payload), w, h)
		}
	}
}

func TestExtractVideoMetadata_WrongHeader(t *testing.T) {
	// Not a keyframe sequence header (wrong first byte)
	payload := make([]byte, 20)
	payload[0] = 0x27 // interframe, not keyframe
	payload[1] = 0x00
	w, h := ExtractVideoMetadata(payload)
	if w != 0 || h != 0 {
		t.Fatalf("expected 0x0 for wrong header, got %dx%d", w, h)
	}
}

func TestExtractAudioMetadata_AAC_44100_Stereo(t *testing.T) {
	// AAC-LC (objectType=2), frequencyIndex=4 (44100), channels=2
	// AudioSpecificConfig: 00010 0100 0010 0... = 0x12 0x10
	payload := []byte{0xAF, 0x00, 0x12, 0x10}

	sr, ch, stereo := ExtractAudioMetadata(payload)
	if sr != 44100 {
		t.Fatalf("expected sample rate 44100, got %d", sr)
	}
	if ch != 2 {
		t.Fatalf("expected 2 channels, got %d", ch)
	}
	if !stereo {
		t.Fatal("expected stereo=true")
	}
}

func TestExtractAudioMetadata_AAC_48000_Mono(t *testing.T) {
	// AAC-LC (objectType=2), frequencyIndex=3 (48000), channels=1
	// AudioSpecificConfig: 00010 0011 0001 0... = 0x11 0x88
	payload := []byte{0xAF, 0x00, 0x11, 0x88}

	sr, ch, stereo := ExtractAudioMetadata(payload)
	if sr != 48000 {
		t.Fatalf("expected sample rate 48000, got %d", sr)
	}
	if ch != 1 {
		t.Fatalf("expected 1 channel, got %d", ch)
	}
	if stereo {
		t.Fatal("expected stereo=false")
	}
}

func TestExtractAudioMetadata_Empty(t *testing.T) {
	sr, ch, stereo := ExtractAudioMetadata(nil)
	if sr != 0 || ch != 0 || stereo {
		t.Fatalf("expected zeros for nil, got sr=%d ch=%d stereo=%v", sr, ch, stereo)
	}
	sr, ch, stereo = ExtractAudioMetadata([]byte{})
	if sr != 0 || ch != 0 || stereo {
		t.Fatalf("expected zeros for empty, got sr=%d ch=%d stereo=%v", sr, ch, stereo)
	}
}

func TestExtractAudioMetadata_NotAAC(t *testing.T) {
	// SoundFormat != 0xA (not AAC)
	payload := []byte{0x2F, 0x00, 0x12, 0x10}
	sr, ch, stereo := ExtractAudioMetadata(payload)
	if sr != 0 || ch != 0 || stereo {
		t.Fatalf("expected zeros for non-AAC, got sr=%d ch=%d stereo=%v", sr, ch, stereo)
	}
}

func TestBitReader(t *testing.T) {
	t.Run("readBits", func(t *testing.T) {
		br := &bitReader{data: []byte{0xAB, 0xCD}} // 10101011 11001101
		v, err := br.readBits(4)
		if err != nil || v != 0x0A { // 1010
			t.Fatalf("expected 0x0A, got 0x%X err=%v", v, err)
		}
		v, err = br.readBits(8)
		if err != nil || v != 0xBC { // 1011 1100
			t.Fatalf("expected 0xBC, got 0x%X err=%v", v, err)
		}
		v, err = br.readBits(4)
		if err != nil || v != 0x0D { // 1101
			t.Fatalf("expected 0x0D, got 0x%X err=%v", v, err)
		}
	})

	t.Run("readBits_overflow", func(t *testing.T) {
		br := &bitReader{data: []byte{0xFF}}
		_, err := br.readBits(9)
		if err == nil {
			t.Fatal("expected error for reading beyond buffer")
		}
	})

	t.Run("readExpGolomb", func(t *testing.T) {
		tests := []struct {
			name string
			data []byte
			want uint32
		}{
			{"zero", []byte{0x80}, 0},          // 1... → 0
			{"one", []byte{0x40}, 1},            // 010... → 1
			{"two", []byte{0x60}, 2},            // 011... → 2
			{"three", []byte{0x20}, 3},          // 00100... → 3
			{"seven", []byte{0x10}, 7},          // 0001000... → 7
			{"eight", []byte{0x12}, 8},          // 0001001... → 8 (00010010)
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				br := &bitReader{data: tc.data}
				got, err := br.readExpGolomb()
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got != tc.want {
					t.Fatalf("expected %d, got %d", tc.want, got)
				}
			})
		}
	})

	t.Run("skipBits", func(t *testing.T) {
		br := &bitReader{data: []byte{0xFF, 0x00}}
		if err := br.skipBits(12); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		v, err := br.readBits(4)
		if err != nil || v != 0x00 {
			t.Fatalf("expected 0x00 after skip, got 0x%X err=%v", v, err)
		}
	})

	t.Run("skipBits_overflow", func(t *testing.T) {
		br := &bitReader{data: []byte{0xFF}}
		if err := br.skipBits(9); err == nil {
			t.Fatal("expected error for skipping beyond buffer")
		}
	})
}

func TestVideoCodecFLVID(t *testing.T) {
	tests := []struct {
		codec string
		want  float64
	}{
		{VideoCodecAVC, 7},
		{VideoCodecHEVC, 12},
		{"unknown", 0},
		{"", 0},
	}
	for _, tc := range tests {
		got := VideoCodecFLVID(tc.codec)
		if got != tc.want {
			t.Errorf("VideoCodecFLVID(%q) = %v, want %v", tc.codec, got, tc.want)
		}
	}
}

func TestAudioCodecFLVID(t *testing.T) {
	tests := []struct {
		codec string
		want  float64
	}{
		{AudioCodecAAC, 10},
		{AudioCodecMP3, 2},
		{AudioCodecSpeex, 11},
		{"unknown", 0},
		{"", 0},
	}
	for _, tc := range tests {
		got := AudioCodecFLVID(tc.codec)
		if got != tc.want {
			t.Errorf("AudioCodecFLVID(%q) = %v, want %v", tc.codec, got, tc.want)
		}
	}
}

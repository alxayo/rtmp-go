package codec

import (
	"testing"
)

// TestParseMP3FrameHeader tests MP3 frame header parsing with various valid configurations.
func TestParseMP3FrameHeader(t *testing.T) {
	tests := []struct {
		name       string
		data       []byte
		wantVersion    int
		wantLayer      int
		wantBitrate    int
		wantSampleRate uint32
		wantChannels   int
	}{
		{
			name:           "MPEG1 Layer III 128kbps 44100Hz stereo",
			data:           []byte{0xFF, 0xFB, 0x90, 0x00},
			wantVersion:    1,
			wantLayer:      3,
			wantBitrate:    128,
			wantSampleRate: 44100,
			wantChannels:   2,
		},
		{
			name:           "MPEG1 Layer III 320kbps 48000Hz mono",
			data:           []byte{0xFF, 0xFB, 0xE4, 0xC0},
			wantVersion:    1,
			wantLayer:      3,
			wantBitrate:    320,
			wantSampleRate: 48000,
			wantChannels:   1,
		},
		{
			name:           "MPEG2 Layer III 40kbps 22050Hz joint stereo",
			data:           []byte{0xFF, 0xF3, 0x50, 0x40},
			wantVersion:    2,
			wantLayer:      3,
			wantBitrate:    40,
			wantSampleRate: 22050,
			wantChannels:   2,
		},
		{
			name:           "MPEG2.5 Layer III 32kbps 11025Hz stereo",
			data:           []byte{0xFF, 0xE3, 0x40, 0x00},
			wantVersion:    25,
			wantLayer:      3,
			wantBitrate:    32,
			wantSampleRate: 11025,
			wantChannels:   2,
		},
		{
			name:           "MPEG1 Layer I 128kbps 44100Hz stereo",
			data:           []byte{0xFF, 0xFF, 0x40, 0x00},
			wantVersion:    1,
			wantLayer:      1,
			wantBitrate:    128,
			wantSampleRate: 44100,
			wantChannels:   2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := ParseMP3FrameHeader(tt.data)
			if err != nil {
				t.Fatalf("ParseMP3FrameHeader: %v", err)
			}

			if info.Version != tt.wantVersion {
				t.Errorf("Version: got %d, want %d", info.Version, tt.wantVersion)
			}
			if info.Layer != tt.wantLayer {
				t.Errorf("Layer: got %d, want %d", info.Layer, tt.wantLayer)
			}
			if info.Bitrate != tt.wantBitrate {
				t.Errorf("Bitrate: got %d, want %d", info.Bitrate, tt.wantBitrate)
			}
			if info.SampleRate != tt.wantSampleRate {
				t.Errorf("SampleRate: got %d, want %d", info.SampleRate, tt.wantSampleRate)
			}
			if info.Channels != tt.wantChannels {
				t.Errorf("Channels: got %d, want %d", info.Channels, tt.wantChannels)
			}
		})
	}
}

// TestParseMP3FrameHeader_Errors tests error handling for invalid MP3 frame headers.
func TestParseMP3FrameHeader_Errors(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "invalid sync word",
			data: []byte{0xFE, 0xFB, 0x90, 0x00},
		},
		{
			name: "reserved version bits",
			// byte[1] = 0b111_01_01_1 = 0xEB (version=01=reserved)
			data: []byte{0xFF, 0xEB, 0x90, 0x00},
		},
		{
			name: "reserved layer bits",
			// byte[1] = 0b111_11_00_1 = 0xF9 (layer=00=reserved)
			data: []byte{0xFF, 0xF9, 0x90, 0x00},
		},
		{
			name: "too short data",
			data: []byte{0xFF, 0xFB, 0x90},
		},
		{
			name: "free format bitrate",
			// byte[2] = 0b0000_00_0_0 = 0x00 (bitrate_idx=0=free format)
			data: []byte{0xFF, 0xFB, 0x00, 0x00},
		},
		{
			name: "bad bitrate index 15",
			// byte[2] = 0b1111_00_0_0 = 0xF0 (bitrate_idx=15=bad)
			data: []byte{0xFF, 0xFB, 0xF0, 0x00},
		},
		{
			name: "reserved sample rate",
			// byte[2] = 0b1001_11_0_0 = 0x9C (sample_rate_idx=3=reserved)
			data: []byte{0xFF, 0xFB, 0x9C, 0x00},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseMP3FrameHeader(tt.data)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

// TestBuildMP3AudioTag tests the legacy RTMP audio tag construction for MP3.
func TestBuildMP3AudioTag(t *testing.T) {
	tests := []struct {
		name     string
		rawFrame []byte
		want     []byte
	}{
		{
			name:     "normal frame",
			rawFrame: []byte{0xFF, 0xFB, 0x90, 0x00, 0x01, 0x02},
			want:     []byte{0x2F, 0xFF, 0xFB, 0x90, 0x00, 0x01, 0x02},
		},
		{
			name:     "empty frame",
			rawFrame: []byte{},
			want:     []byte{0x2F},
		},
		{
			name:     "single byte",
			rawFrame: []byte{0xAB},
			want:     []byte{0x2F, 0xAB},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildMP3AudioTag(tt.rawFrame)

			if len(got) != len(tt.want) {
				t.Fatalf("length: got %d, want %d", len(got), len(tt.want))
			}

			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("byte %d: got 0x%02X, want 0x%02X", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestBuildMP3AudioTag_FormatByte verifies byte 0 is always 0x2F
// (SoundFormat=2, SoundRate=3, SoundSize=1, SoundType=1).
func TestBuildMP3AudioTag_FormatByte(t *testing.T) {
	inputs := [][]byte{
		{},
		{0x00},
		{0xFF, 0xFB, 0x90, 0x00},
		{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF},
	}

	for _, input := range inputs {
		tag := BuildMP3AudioTag(input)
		if tag[0] != 0x2F {
			t.Errorf("format byte: got 0x%02X, want 0x2F for input len %d", tag[0], len(input))
		}
	}
}

// TestBuildMP3AudioTag_OutputLength verifies output length is always input length + 1.
func TestBuildMP3AudioTag_OutputLength(t *testing.T) {
	for _, length := range []int{0, 1, 4, 128, 1024} {
		input := make([]byte, length)
		tag := BuildMP3AudioTag(input)
		if len(tag) != length+1 {
			t.Errorf("input len %d: output len got %d, want %d", length, len(tag), length+1)
		}
	}
}

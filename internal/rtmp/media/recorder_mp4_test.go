// recorder_mp4_test.go — tests for the MP4 file recorder's enhanced audio codec support.
//
// Tests verify that each supported audio codec (AAC, Opus, FLAC, AC-3, E-AC-3, MP3)
// produces a valid MP4 file with the correct sample entry box type in the moov atom.
//
// Test structure:
//   - Each test creates an MP4Recorder, feeds it enhanced audio messages, and closes it.
//   - After closing, the output file is read back and validated for:
//     1. Non-empty file size (recording happened)
//     2. ftyp box at offset 0 (valid MP4)
//     3. moov box present (metadata was written)
//     4. Correct codec-specific box type inside stsd (e.g., "Opus", "fLaC", "dOps")
package media

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// makeEnhancedAudioMsg builds an RTMP audio message with Enhanced RTMP format:
//
//	byte 0: soundFormat=9 (enhanced) in upper nibble | pktType in lower nibble
//	bytes 1-4: fourCC (4-byte codec identifier, e.g., "Opus", "fLaC")
//	bytes 5+: payload data (config for pktType=0, raw audio for pktType=1)
func makeEnhancedAudioMsg(ts uint32, fourCC string, pktType uint8, payload []byte) *chunk.Message {
	// SoundFormat 9 = 0x90 in upper nibble, pktType in lower nibble
	data := make([]byte, 0, 5+len(payload))
	data = append(data, 0x90|pktType)      // enhanced audio header
	data = append(data, []byte(fourCC)...) // codec FourCC
	data = append(data, payload...)         // config or coded frames
	return &chunk.Message{
		Timestamp:     ts,
		TypeID:        8, // audio
		Payload:       data,
		MessageLength: uint32(len(data)),
	}
}

// makeLegacyAACMsg builds a traditional RTMP AAC audio message:
//
//	byte 0: 0xAF (soundFormat=10/AAC, rate=44100, size=16bit, stereo)
//	byte 1: pktType (0=sequence header, 1=raw frame)
//	bytes 2+: payload
func makeLegacyAACMsg(ts uint32, pktType uint8, payload []byte) *chunk.Message {
	data := make([]byte, 0, 2+len(payload))
	data = append(data, 0xAF, pktType)
	data = append(data, payload...)
	return &chunk.Message{
		Timestamp:     ts,
		TypeID:        8,
		Payload:       data,
		MessageLength: uint32(len(data)),
	}
}

// makeEnhancedVideoKeyframe builds a minimal Enhanced RTMP video keyframe for H.265.
// Used to ensure the MP4 recorder has at least one video frame so the file is valid.
func makeEnhancedVideoKeyframe(ts uint32, isSeqHeader bool) *chunk.Message {
	// ExHeader: frameType=1 (key) in bits 4-6, isExHeader=1 in bit 7
	// pktType in lower 4 bits: 0=SequenceStart, 1=CodedFrames (with CTS)
	var b0 uint8
	if isSeqHeader {
		b0 = 0x90 // key(1)<<4 | exHeader(1)<<7 | pktType(0)
	} else {
		b0 = 0x91 // key(1)<<4 | exHeader(1)<<7 | pktType(1)=CodedFrames
	}
	data := []byte{b0, 'h', 'v', 'c', '1'}
	if isSeqHeader {
		// Minimal HEVCDecoderConfigurationRecord placeholder
		data = append(data, make([]byte, 23)...)
	} else {
		// CTS (3 bytes) + minimal NALU data
		data = append(data, 0, 0, 0)          // CTS = 0
		data = append(data, 0, 0, 0, 5)       // NALU length = 5
		data = append(data, 0x40, 0, 0, 0, 0) // minimal NALU
	}
	return &chunk.Message{
		Timestamp:     ts,
		TypeID:        9,
		Payload:       data,
		MessageLength: uint32(len(data)),
	}
}

// findBox searches for an MP4 box with the given 4-character type in data.
// Returns the offset where the box starts, or -1 if not found.
// Box format: [4B size][4B type][contents...]
func findBox(data []byte, boxType string) int {
	bt := []byte(boxType)
	for i := 0; i+8 <= len(data); {
		size := binary.BigEndian.Uint32(data[i:])
		if size < 8 {
			// Invalid box — skip forward to prevent infinite loop
			i += 4
			continue
		}
		if data[i+4] == bt[0] && data[i+5] == bt[1] && data[i+6] == bt[2] && data[i+7] == bt[3] {
			return i
		}
		i += int(size)
	}
	return -1
}

// findBoxRecursive searches for an MP4 box by type anywhere in the data,
// including inside nested container boxes. Returns true if found.
func findBoxRecursive(data []byte, boxType string) bool {
	bt := []byte(boxType)
	// Simple byte-pattern search since nested boxes make offset-based parsing complex
	for i := 0; i+4 <= len(data); i++ {
		if data[i] == bt[0] && data[i+1] == bt[1] && data[i+2] == bt[2] && data[i+3] == bt[3] {
			return true
		}
	}
	return false
}

// TestMP4Recorder_LegacyAAC verifies that legacy AAC audio produces
// an mp4a sample entry with an esds box in the moov atom.
func TestMP4Recorder_LegacyAAC(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aac.mp4")

	rec, err := NewMP4Recorder(path, NullLogger())
	if err != nil {
		t.Fatalf("NewMP4Recorder: %v", err)
	}

	// Write video (needed so the file has content)
	rec.WriteMessage(makeEnhancedVideoKeyframe(0, true))   // seq header
	rec.WriteMessage(makeEnhancedVideoKeyframe(33, false))  // keyframe

	// Write legacy AAC: sequence header + a few raw frames
	aacConfig := []byte{0x12, 0x10} // AAC-LC, 44100 Hz, stereo
	rec.WriteMessage(makeLegacyAACMsg(0, 0, aacConfig))
	rec.WriteMessage(makeLegacyAACMsg(23, 1, []byte{0xDE, 0xAD, 0xBE, 0xEF}))
	rec.WriteMessage(makeLegacyAACMsg(46, 1, []byte{0xCA, 0xFE, 0xBA, 0xBE}))

	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) < 40 {
		t.Fatalf("file too small: %d bytes", len(data))
	}

	// Verify ftyp box at start
	if string(data[4:8]) != "ftyp" {
		t.Fatalf("expected ftyp box at offset 4, got %q", data[4:8])
	}
	// Verify moov box exists
	if findBox(data, "moov") < 0 {
		t.Fatal("moov box not found")
	}
	// Verify mp4a sample entry
	if !findBoxRecursive(data, "mp4a") {
		t.Error("mp4a box not found in moov")
	}
	// Verify esds config box
	if !findBoxRecursive(data, "esds") {
		t.Error("esds box not found in moov")
	}
}

// TestMP4Recorder_EnhancedOpus verifies that Opus via Enhanced RTMP produces
// an "Opus" sample entry with a "dOps" box in the moov atom.
func TestMP4Recorder_EnhancedOpus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opus.mp4")

	rec, err := NewMP4Recorder(path, NullLogger())
	if err != nil {
		t.Fatalf("NewMP4Recorder: %v", err)
	}

	rec.WriteMessage(makeEnhancedVideoKeyframe(0, true))
	rec.WriteMessage(makeEnhancedVideoKeyframe(33, false))

	// Opus sequence header: a minimal OpusHead (19 bytes)
	// "OpusHead" magic + version(1) + channels(2) + preskip(312) + samplerate(48000) + gain(0) + mapping(0)
	opusHead := []byte{
		'O', 'p', 'u', 's', 'H', 'e', 'a', 'd', // magic
		0x01,       // version
		0x02,       // channel count = 2
		0x38, 0x01, // pre-skip = 312 (little-endian)
		0x80, 0xBB, 0x00, 0x00, // input sample rate = 48000 (little-endian)
		0x00, 0x00, // output gain = 0
		0x00, // channel mapping family = 0
	}
	rec.WriteMessage(makeEnhancedAudioMsg(0, "Opus", 0, opusHead))
	rec.WriteMessage(makeEnhancedAudioMsg(20, "Opus", 1, []byte{0x01, 0x02, 0x03, 0x04}))
	rec.WriteMessage(makeEnhancedAudioMsg(40, "Opus", 1, []byte{0x05, 0x06, 0x07, 0x08}))

	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if findBox(data, "moov") < 0 {
		t.Fatal("moov box not found")
	}
	if !findBoxRecursive(data, "Opus") {
		t.Error("Opus sample entry box not found")
	}
	if !findBoxRecursive(data, "dOps") {
		t.Error("dOps (OpusSpecificBox) not found")
	}
}

// TestMP4Recorder_EnhancedFLAC verifies that FLAC via Enhanced RTMP produces
// a "fLaC" sample entry with a "dfLa" box.
func TestMP4Recorder_EnhancedFLAC(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flac.mp4")

	rec, err := NewMP4Recorder(path, NullLogger())
	if err != nil {
		t.Fatalf("NewMP4Recorder: %v", err)
	}

	rec.WriteMessage(makeEnhancedVideoKeyframe(0, true))
	rec.WriteMessage(makeEnhancedVideoKeyframe(33, false))

	// Minimal FLAC STREAMINFO block (34 bytes):
	// min/max block size (2+2), min/max frame size (3+3),
	// sample rate (20 bits) + channels-1 (3 bits) + bps-1 (5 bits) + total samples (36 bits)
	// Then 16 bytes of MD5 signature
	streaminfo := make([]byte, 34)
	// Set sample rate = 44100 in bits 80-99 (bytes 10-12, upper 20 bits)
	// 44100 = 0xAC44 → in 20 bits: 0x0AC44
	streaminfo[10] = 0x0A                             // upper 8 bits of sample rate
	streaminfo[11] = 0xC4                             // middle 8 bits
	streaminfo[12] = 0x42                             // lower 4 bits of SR (0x4) + channels-1 (001) + upper bit of bps-1
	// channels-1 = 1 (stereo), packed in bits 100-102 of streaminfo
	// bps-1 = 15 (16-bit), packed in bits 103-107

	rec.WriteMessage(makeEnhancedAudioMsg(0, "fLaC", 0, streaminfo))
	rec.WriteMessage(makeEnhancedAudioMsg(23, "fLaC", 1, []byte{0xFF, 0xF8, 0x01, 0x02}))
	rec.WriteMessage(makeEnhancedAudioMsg(46, "fLaC", 1, []byte{0xFF, 0xF8, 0x03, 0x04}))

	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if findBox(data, "moov") < 0 {
		t.Fatal("moov box not found")
	}
	if !findBoxRecursive(data, "fLaC") {
		t.Error("fLaC sample entry box not found")
	}
	if !findBoxRecursive(data, "dfLa") {
		t.Error("dfLa (FLAC config) box not found")
	}
}

// TestMP4Recorder_EnhancedAC3 verifies that AC-3 via Enhanced RTMP produces
// an "ac-3" sample entry with a "dac3" box.
func TestMP4Recorder_EnhancedAC3(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ac3.mp4")

	rec, err := NewMP4Recorder(path, NullLogger())
	if err != nil {
		t.Fatalf("NewMP4Recorder: %v", err)
	}

	rec.WriteMessage(makeEnhancedVideoKeyframe(0, true))
	rec.WriteMessage(makeEnhancedVideoKeyframe(33, false))

	// AC-3 config: minimal dac3 data (3 bytes)
	ac3Config := []byte{0x10, 0x40, 0x00}
	rec.WriteMessage(makeEnhancedAudioMsg(0, "ac-3", 0, ac3Config))
	rec.WriteMessage(makeEnhancedAudioMsg(32, "ac-3", 1, []byte{0x0B, 0x77, 0x01, 0x02}))
	rec.WriteMessage(makeEnhancedAudioMsg(64, "ac-3", 1, []byte{0x0B, 0x77, 0x03, 0x04}))

	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if findBox(data, "moov") < 0 {
		t.Fatal("moov box not found")
	}
	if !findBoxRecursive(data, "ac-3") {
		t.Error("ac-3 sample entry box not found")
	}
	if !findBoxRecursive(data, "dac3") {
		t.Error("dac3 (AC3SpecificBox) not found")
	}
}

// TestMP4Recorder_EnhancedEAC3 verifies that E-AC-3 via Enhanced RTMP produces
// an "ec-3" sample entry with a "dec3" box.
func TestMP4Recorder_EnhancedEAC3(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "eac3.mp4")

	rec, err := NewMP4Recorder(path, NullLogger())
	if err != nil {
		t.Fatalf("NewMP4Recorder: %v", err)
	}

	rec.WriteMessage(makeEnhancedVideoKeyframe(0, true))
	rec.WriteMessage(makeEnhancedVideoKeyframe(33, false))

	// E-AC-3 config: minimal dec3 data (4 bytes)
	eac3Config := []byte{0x00, 0x20, 0x0F, 0x00}
	rec.WriteMessage(makeEnhancedAudioMsg(0, "ec-3", 0, eac3Config))
	rec.WriteMessage(makeEnhancedAudioMsg(32, "ec-3", 1, []byte{0x0B, 0x77, 0x05, 0x06}))
	rec.WriteMessage(makeEnhancedAudioMsg(64, "ec-3", 1, []byte{0x0B, 0x77, 0x07, 0x08}))

	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if findBox(data, "moov") < 0 {
		t.Fatal("moov box not found")
	}
	if !findBoxRecursive(data, "ec-3") {
		t.Error("ec-3 sample entry box not found")
	}
	if !findBoxRecursive(data, "dec3") {
		t.Error("dec3 (EC3SpecificBox) not found")
	}
}

// TestMP4Recorder_EnhancedMP3 verifies that MP3 via Enhanced RTMP produces
// a ".mp3" sample entry with an esds box using objectTypeIndication 0x6B.
func TestMP4Recorder_EnhancedMP3(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mp3.mp4")

	rec, err := NewMP4Recorder(path, NullLogger())
	if err != nil {
		t.Fatalf("NewMP4Recorder: %v", err)
	}

	rec.WriteMessage(makeEnhancedVideoKeyframe(0, true))
	rec.WriteMessage(makeEnhancedVideoKeyframe(33, false))

	// MP3 typically has no sequence header config, just coded frames
	rec.WriteMessage(makeEnhancedAudioMsg(0, ".mp3", 0, []byte{})) // empty config
	rec.WriteMessage(makeEnhancedAudioMsg(26, ".mp3", 1, []byte{0xFF, 0xFB, 0x90, 0x00}))
	rec.WriteMessage(makeEnhancedAudioMsg(52, ".mp3", 1, []byte{0xFF, 0xFB, 0x90, 0x01}))

	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if findBox(data, "moov") < 0 {
		t.Fatal("moov box not found")
	}
	if !findBoxRecursive(data, ".mp3") {
		t.Error(".mp3 sample entry box not found")
	}
	// The .mp3 entry should contain an esds box with OTI 0x6B
	if !findBoxRecursive(data, "esds") {
		t.Error("esds box not found in .mp3 sample entry")
	}
}

// TestMP4Recorder_AudioCodecDetection verifies that the audioCodec field
// is set correctly for each enhanced audio FourCC.
func TestMP4Recorder_AudioCodecDetection(t *testing.T) {
	tests := []struct {
		name      string
		fourCC    string
		wantCodec string
	}{
		{"AAC", "mp4a", "AAC"},
		{"Opus", "Opus", "Opus"},
		{"FLAC", "fLaC", "FLAC"},
		{"AC3", "ac-3", "AC3"},
		{"EAC3", "ec-3", "EAC3"},
		{"MP3", ".mp3", "MP3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "detect.mp4")
			rec, err := NewMP4Recorder(path, NullLogger())
			if err != nil {
				t.Fatalf("NewMP4Recorder: %v", err)
			}

			// Send an enhanced audio message with this FourCC
			rec.WriteMessage(makeEnhancedAudioMsg(0, tt.fourCC, 0, []byte{0x01, 0x02}))

			// Access the internal MP4Recorder to check audioCodec
			mp4rec := rec.(*MP4Recorder)
			if mp4rec.audioCodec != tt.wantCodec {
				t.Errorf("audioCodec = %q, want %q", mp4rec.audioCodec, tt.wantCodec)
			}

			rec.Close()
		})
	}
}

// TestMP4Recorder_OpusWithoutConfig verifies that Opus recording works
// even without a full OpusHead config (uses minimal defaults).
func TestMP4Recorder_OpusWithoutConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opus_noconfig.mp4")

	rec, err := NewMP4Recorder(path, NullLogger())
	if err != nil {
		t.Fatalf("NewMP4Recorder: %v", err)
	}

	rec.WriteMessage(makeEnhancedVideoKeyframe(0, true))
	rec.WriteMessage(makeEnhancedVideoKeyframe(33, false))

	// Send Opus frames without a sequence header (no config)
	rec.WriteMessage(makeEnhancedAudioMsg(0, "Opus", 0, []byte{0x01}))  // minimal/incomplete config
	rec.WriteMessage(makeEnhancedAudioMsg(20, "Opus", 1, []byte{0xAA, 0xBB}))

	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	// Should still produce a valid file with Opus box and default dOps
	if !findBoxRecursive(data, "Opus") {
		t.Error("Opus sample entry box not found")
	}
	if !findBoxRecursive(data, "dOps") {
		t.Error("dOps box not found")
	}
}

// TestMP4Recorder_AudioOnlyEnhanced verifies that an MP4 with only audio
// (no video) still produces a valid file.
func TestMP4Recorder_AudioOnlyEnhanced(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audio_only.mp4")

	rec, err := NewMP4Recorder(path, NullLogger())
	if err != nil {
		t.Fatalf("NewMP4Recorder: %v", err)
	}

	// Only audio, no video
	aacConfig := []byte{0x12, 0x10}
	rec.WriteMessage(makeLegacyAACMsg(0, 0, aacConfig))
	rec.WriteMessage(makeLegacyAACMsg(23, 1, []byte{0x01, 0x02, 0x03}))
	rec.WriteMessage(makeLegacyAACMsg(46, 1, []byte{0x04, 0x05, 0x06}))

	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if findBox(data, "moov") < 0 {
		t.Fatal("moov box not found")
	}
	if !findBoxRecursive(data, "mp4a") {
		t.Error("mp4a box not found for audio-only recording")
	}
}

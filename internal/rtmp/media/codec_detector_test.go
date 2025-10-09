package media

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// fakeStream implements CodecStore for tests.
type fakeStream struct {
	key        string
	audioCodec string
	videoCodec string
}

func (f *fakeStream) SetAudioCodec(c string) { f.audioCodec = c }
func (f *fakeStream) SetVideoCodec(c string) { f.videoCodec = c }
func (f *fakeStream) GetAudioCodec() string  { return f.audioCodec }
func (f *fakeStream) GetVideoCodec() string  { return f.videoCodec }
func (f *fakeStream) StreamKey() string      { return f.key }

func newLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

func TestCodecDetector_H264_AAC(t *testing.T) {
	var logBuf bytes.Buffer
	logger := newLogger(&logBuf)
	stream := &fakeStream{key: "app/stream"}
	cd := &CodecDetector{}

	// Video: H.264 sequence header (frameType=1 keyframe, codecID=7 AVC)
	videoPayload := []byte{0x17, 0x00, 0x01} // 0x17 = 0001 0111
	cd.Process(9, videoPayload, stream, logger)

	// Audio: AAC sequence header (soundFormat=10)
	audioPayload := []byte{0xA0, 0x00, 0x01} // 0xA0 = 1010 0000
	cd.Process(8, audioPayload, stream, logger)

	if stream.videoCodec != VideoCodecAVC {
		// we expect H264
		t.Fatalf("expected video codec H264, got %s", stream.videoCodec)
	}
	if stream.audioCodec != AudioCodecAAC {
		t.Fatalf("expected audio codec AAC, got %s", stream.audioCodec)
	}

	logStr := logBuf.String()
	if !strings.Contains(logStr, "H264") || !strings.Contains(logStr, "AAC") {
		t.Errorf("log does not contain both codecs: %s", logStr)
	}
}

func TestCodecDetector_MP3_Only(t *testing.T) {
	var logBuf bytes.Buffer
	logger := newLogger(&logBuf)
	stream := &fakeStream{key: "app/music"}
	cd := &CodecDetector{}

	// Audio: MP3 frame (soundFormat=2)
	audioPayload := []byte{0x20, 0xFF, 0xFB} // simplified MP3 frame start (not parsed beyond header)
	cd.Process(8, audioPayload, stream, logger)

	if stream.audioCodec != AudioCodecMP3 {
		t.Fatalf("expected audio codec MP3, got %s", stream.audioCodec)
	}
	if stream.videoCodec != "" {
		t.Fatalf("expected no video codec, got %s", stream.videoCodec)
	}

	logStr := logBuf.String()
	if !strings.Contains(logStr, "MP3") {
		t.Errorf("log does not contain MP3: %s", logStr)
	}
}

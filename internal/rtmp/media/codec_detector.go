package media

import (
	"log/slog"
)

// CodecStore is an interface satisfied by the future Stream entity (see data-model.md)
// and by test fakes. It lets the codec detector store discovered audio/video codecs
// without depending on the concrete Stream implementation (which may be added later
// in a different task phase).
type CodecStore interface {
	SetAudioCodec(string)
	SetVideoCodec(string)
	GetAudioCodec() string
	GetVideoCodec() string
	StreamKey() string
}

// CodecDetector performs one-shot detection of audio and video codecs based on the
// first audio (type 8) and video (type 9) messages received on a stream.
// It is concurrency-safe for single goroutine usage (called from a media relay loop)
// and keeps no internal state; state lives in the CodecStore implementation.
type CodecDetector struct{}

// Process inspects an incoming RTMP message (by its type ID and raw payload) and
// updates the codec store if this is the first occurrence of that media type.
//
// msgType: RTMP message type ID (8 = audio, 9 = video)
// payload: Raw tag data (FLV tag body) for that media message
// store:   Stream or other structure where detected codecs are persisted
// logger:  Structured logger (required for observability)
func (d *CodecDetector) Process(msgType uint8, payload []byte, store CodecStore, logger *slog.Logger) {
	if store == nil || logger == nil {
		return
	}

	var updated bool

	switch msgType {
	case 8: // Audio
		if store.GetAudioCodec() == "" {
			if am, err := ParseAudioMessage(payload); err == nil {
				store.SetAudioCodec(am.Codec)
				updated = true
			}
		}
	case 9: // Video
		if store.GetVideoCodec() == "" {
			if vm, err := ParseVideoMessage(payload); err == nil {
				store.SetVideoCodec(vm.Codec)
				updated = true
			}
		}
	}

	if updated {
		logger.Info("Codecs detected", "stream_key", store.StreamKey(), "videoCodec", store.GetVideoCodec(), "audioCodec", store.GetAudioCodec())
	}
}

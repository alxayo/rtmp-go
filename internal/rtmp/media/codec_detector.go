package media

import (
	"log/slog"
)

// CodecStore is the interface for storing detected codec information.
// Both server.Stream and test fakes implement this, allowing the codec
// detector to work independently of the concrete stream implementation.
type CodecStore interface {
	SetAudioCodec(string)  // Store the detected audio codec name (e.g. "AAC")
	SetVideoCodec(string)  // Store the detected video codec name (e.g. "H264")
	GetAudioCodec() string // Retrieve current audio codec (empty if not yet detected)
	GetVideoCodec() string // Retrieve current video codec (empty if not yet detected)
	StreamKey() string     // Return the stream's key for logging (e.g. "live/mystream")
}

// CodecDetector identifies audio and video codecs by examining the first
// media message on each stream. It is stateless — detection results are
// stored in the CodecStore, and detection only runs when the store's codec
// field is still empty (one-shot detection).
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

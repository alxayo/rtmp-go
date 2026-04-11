package server

// Media dispatch routes incoming audio/video messages to recording, local
// subscriber broadcast, and external multi-destination relay. It is called
// from the per-connection message handler installed by attachCommandHandling.

import (
	"log/slog"

	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
	"github.com/alxayo/go-rtmp/internal/rtmp/relay"
)

// dispatchMedia handles a single audio (TypeID 8) or video (TypeID 9)
// message: logging, codec detection, recording, local broadcast, and external relay.
//
// The ordering is important: codec detection (via BroadcastMessage) runs first
// so that ensureRecorder can select the correct container format (FLV for H.264,
// MP4 for H.265+). The recorder is lazily initialized on the first frame after
// codec detection, ensuring no format mismatch.
func dispatchMedia(
	m *chunk.Message,
	st *commandState,
	reg *Registry,
	destMgr *relay.DestinationManager,
	log *slog.Logger,
) {
	st.mediaLogger.ProcessMessage(m)

	if st.streamKey == "" {
		return
	}
	stream := reg.GetStream(st.streamKey)
	if stream == nil {
		return
	}

	// 1. Codec detection + subscriber broadcast first.
	// BroadcastMessage performs one-shot codec detection (setting stream.VideoCodec
	// and stream.AudioCodec) and fans out the frame to all subscribers.
	stream.BroadcastMessage(st.codecDetector, m, log)

	// 2. Lazy recorder initialization — creates the recorder once the video codec
	// is known, selecting the correct container format automatically.
	ensureRecorder(stream, log)

	// 3. Write to recorder (snapshot under lock to avoid race with teardown).
	if rec := stream.GetRecorder(); rec != nil {
		rec.WriteMessage(m)
	}

	// 4. Forward to external relay destinations.
	if destMgr != nil {
		destMgr.RelayMessage(m)
	}
}

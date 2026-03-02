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
// message: logging, recording, local broadcast, and external relay.
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

	// Write to FLV recorder if recording is active.
	if stream.Recorder != nil {
		stream.Recorder.WriteMessage(m)
	}

	// Broadcast to all local subscribers (play clients).
	stream.BroadcastMessage(st.codecDetector, m, log)

	// Forward to external relay destinations.
	if destMgr != nil {
		destMgr.RelayMessage(m)
	}
}

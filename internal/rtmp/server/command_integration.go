package server

// Command Integration (Incremental Wiring)
// ---------------------------------------
// This file bridges the lower-level connection (handshake + control +
// chunking read/write loops) with the existing RPC command parsing and
// handlers so that real RTMP clients (OBS / ffmpeg) can complete the
// connect → createStream → publish sequence.
//
// Scope (minimal, pragmatic):
//   * Per-connection state: application name (from connect), stream id
//     allocator for createStream responses.
//   * Dispatch handling for: connect, createStream, publish.
//   * Play is left for later tasks; unknown commands ignored by dispatcher.
//   * Errors are logged; fatal protocol errors currently just logged (a
//     future enhancement can close the connection or send _error responses).
//
// This unlocks basic interoperability with standard broadcasters which
// expect the canonical responses:
//   - _result for connect (NetConnection.Connect.Success)
//   - _result for createStream returning stream id (1)
//   - onStatus NetStream.Publish.Start after publish
//
// NOTE: Media forwarding is still unimplemented; after publish OBS will
// start sending audio/video messages which we currently just read and drop.
// That is acceptable for the user goal of validating stream key handling.

import (
	"log/slog"
	"time"

	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
	iconn "github.com/alxayo/go-rtmp/internal/rtmp/conn"
	"github.com/alxayo/go-rtmp/internal/rtmp/control"
	"github.com/alxayo/go-rtmp/internal/rtmp/rpc"
)

// commandState holds mutable per-connection fields needed by handlers.
type commandState struct {
	app         string
	allocator   *rpc.StreamIDAllocator
	mediaLogger *MediaLogger
}

// attachCommandHandling installs a dispatcher-backed message handler on the
// provided connection. Safe to call immediately after Accept returns.
func attachCommandHandling(c *iconn.Connection, reg *Registry, log *slog.Logger) {
	if c == nil || reg == nil {
		return
	}
	st := &commandState{
		allocator:   rpc.NewStreamIDAllocator(),
		mediaLogger: NewMediaLogger(c.ID(), log, 30*time.Second),
	}

	d := rpc.NewDispatcher(func() string { return st.app })

	d.OnConnect = func(cc *rpc.ConnectCommand, msg *chunk.Message) error {
		log.Debug("OnConnect handler invoked", "app", cc.App, "tcUrl", cc.TcURL, "txn_id", cc.TransactionID)
		// Persist app for subsequent publish/play parsing.
		st.app = cc.App
		log.Debug("building connect response", "txn_id", cc.TransactionID)
		resp, err := rpc.BuildConnectResponse(cc.TransactionID, "Connection succeeded.")
		if err != nil {
			log.Error("connect response build failed", "error", err)
			return nil // swallow errors to keep connection alive for now
		}
		// Debug: log first 64 bytes of response payload
		previewLen := 64
		if len(resp.Payload) < previewLen {
			previewLen = len(resp.Payload)
		}
		log.Debug("connect response payload preview", "bytes", resp.Payload[:previewLen])
		log.Debug("sending connect response", "txn_id", cc.TransactionID, "payload_len", len(resp.Payload))
		if err := c.SendMessage(resp); err != nil {
			log.Error("connect response send failed", "error", err)
		} else {
			log.Info("connect response sent successfully", "app", cc.App)
		}
		return nil // swallow errors to keep connection alive for now
	}

	d.OnCreateStream = func(cs *rpc.CreateStreamCommand, msg *chunk.Message) error {
		log.Debug("OnCreateStream handler invoked", "txn_id", cs.TransactionID)
		resp, streamID, err := rpc.BuildCreateStreamResponse(cs.TransactionID, st.allocator)
		if err != nil {
			log.Error("createStream response build failed", "error", err)
			return nil
		}
		log.Debug("createStream response built", "stream_id", streamID, "payload_len", len(resp.Payload))
		if err := c.SendMessage(resp); err != nil {
			log.Error("createStream response send failed", "error", err)
		} else {
			log.Info("createStream response sent successfully", "stream_id", streamID, "txn_id", cs.TransactionID)
		}

		// Send UserControl StreamBegin to signal stream is ready
		streamBegin := control.EncodeUserControlStreamBegin(streamID)
		if err := c.SendMessage(streamBegin); err != nil {
			log.Error("StreamBegin send failed", "error", err, "stream_id", streamID)
		} else {
			log.Info("StreamBegin sent", "stream_id", streamID)
		}
		return nil
	}

	d.OnPublish = func(pc *rpc.PublishCommand, msg *chunk.Message) error {
		// Delegate to existing publish handler (sends onStatus internally).
		if _, err := HandlePublish(reg, c, st.app, msg); err != nil {
			log.Error("publish handle", "error", err)
		}
		return nil
	}

	c.SetMessageHandler(func(m *chunk.Message) {
		if m == nil {
			return
		}

		log.Debug("message handler invoked", "type_id", m.TypeID, "msid", m.MessageStreamID, "len", len(m.Payload))

		// Process media packets (audio/video) through MediaLogger
		if m.TypeID == 8 || m.TypeID == 9 {
			st.mediaLogger.ProcessMessage(m)
			return // Media packets don't need command dispatch
		}

		if m.TypeID != rpc.CommandMessageAMF0TypeIDForTest() {
			log.Debug("skipping non-command message", "type_id", m.TypeID)
			return
		}
		log.Debug("dispatching command message", "type_id", m.TypeID)
		if err := d.Dispatch(m); err != nil {
			log.Error("dispatch error", "error", err)
		}
	})
}

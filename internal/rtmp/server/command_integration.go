package server

// Command Integration (Incremental Wiring)
// ---------------------------------------
// Bridges the connection layer with RPC command parsing and handlers so that
// real RTMP clients (OBS / ffmpeg) can complete connect → createStream →
// publish / play sequences. Media dispatch (recording, relay, broadcast)
// is delegated to media_dispatch.go.
//
// Authentication is enforced here at the publish/play command level via
// the auth.Validator interface configured in server.Config.

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
	iconn "github.com/alxayo/go-rtmp/internal/rtmp/conn"
	"github.com/alxayo/go-rtmp/internal/rtmp/control"
	"github.com/alxayo/go-rtmp/internal/rtmp/media"
	"github.com/alxayo/go-rtmp/internal/rtmp/relay"
	"github.com/alxayo/go-rtmp/internal/rtmp/rpc"
	"github.com/alxayo/go-rtmp/internal/rtmp/server/auth"
	"github.com/alxayo/go-rtmp/internal/rtmp/server/hooks"
)

// commandState holds mutable per-connection state needed by the command handlers.
// Each accepted connection gets its own commandState instance.
type commandState struct {
	app           string                 // application name from the connect command (e.g. "live")
	streamKey     string                 // current stream key (e.g. "live/mystream")
	connectParams map[string]interface{} // extra fields from connect command object (for auth context)
	allocator     *rpc.StreamIDAllocator // assigns unique message stream IDs for createStream
	mediaLogger   *MediaLogger           // tracks audio/video packet statistics
	codecDetector *media.CodecDetector   // identifies audio/video codecs on first packets
	role          string                 // "publisher" or "subscriber" — set by OnPublish/OnPlay handlers
	enhancedRTMP  bool                   // true if client advertised fourCcList in connect
	fourCcList    []string               // Enhanced RTMP FourCC codecs supported by client
}

// attachCommandHandling installs a dispatcher-backed message handler on the
// provided connection. Safe to call immediately after Accept returns.
func attachCommandHandling(c *iconn.Connection, reg *Registry, cfg *Config, log *slog.Logger, destMgr *relay.DestinationManager, srv *Server) {
	if c == nil || reg == nil || cfg == nil {
		return
	}
	st := &commandState{
		allocator:     rpc.NewStreamIDAllocator(),
		mediaLogger:   NewMediaLogger(c.ID(), log, 30*time.Second),
		codecDetector: &media.CodecDetector{},
	}
	// Install disconnect handler — fires when readLoop exits for any reason.
	c.SetDisconnectHandler(func() {
		// 1. Stop media logger (prevents goroutine + ticker leak)
		st.mediaLogger.Stop()

		// Compute session duration for hook data.
		durationSec := time.Since(c.AcceptedAt()).Seconds()

		// 2. Publisher cleanup: close recorder, unregister publisher, fire hook
		if st.streamKey != "" && st.role == "publisher" {
			stream := reg.GetStream(st.streamKey)
			if stream != nil {
				// Close recorder under lock (concurrent with cleanupAllRecorders)
				stream.mu.Lock()
				if stream.Recorder != nil {
					if err := stream.Recorder.Close(); err != nil {
						log.Error("recorder close error on disconnect", "error", err, "stream_key", st.streamKey)
					}
					stream.Recorder = nil
				}
				stream.mu.Unlock()
				// Unregister publisher (allows stream key reuse by new publisher)
				PublisherDisconnected(reg, st.streamKey, c)
			}
			audioPkts, videoPkts, totalBytes, audioCodec, videoCodec := st.mediaLogger.GetStats()
			srv.triggerHookEvent(hooks.EventPublishStop, c.ID(), st.streamKey, map[string]interface{}{
				"audio_packets": audioPkts,
				"video_packets": videoPkts,
				"total_bytes":   totalBytes,
				"audio_codec":   audioCodec,
				"video_codec":   videoCodec,
				"duration_sec":  durationSec,
			})
		}

		// 3. Subscriber cleanup: unregister subscriber, fire hook
		if st.streamKey != "" && st.role == "subscriber" {
			SubscriberDisconnected(reg, st.streamKey, c)
			srv.triggerHookEvent(hooks.EventPlayStop, c.ID(), st.streamKey, map[string]interface{}{
				"duration_sec": durationSec,
			})
			// Fire subscriber count change after removal
			stream := reg.GetStream(st.streamKey)
			if stream != nil {
				srv.triggerHookEvent(hooks.EventSubscriberCount, c.ID(), st.streamKey, map[string]interface{}{
					"count": stream.SubscriberCount(),
				})
			}
		}

		// 4. Remove from server connection tracking (fixes memory leak)
		srv.RemoveConnection(c.ID())

		// 5. Fire connection close hook
		srv.triggerHookEvent(hooks.EventConnectionClose, c.ID(), st.streamKey, map[string]interface{}{
			"role":         st.role,
			"duration_sec": durationSec,
		})

		log.Info("connection disconnected", "conn_id", c.ID(), "stream_key", st.streamKey, "role", st.role)
	})
	d := rpc.NewDispatcher(func() string { return st.app })

	d.OnConnect = func(cc *rpc.ConnectCommand, msg *chunk.Message) error {
		log.Debug("OnConnect handler invoked", "app", cc.App, "tcUrl", cc.TcURL, "txn_id", cc.TransactionID)
		st.app = cc.App
		st.connectParams = cc.Extra // preserve extra connect fields for auth context

		// Track Enhanced RTMP capabilities from client's fourCcList.
		if len(cc.FourCcList) > 0 {
			st.enhancedRTMP = true
			st.fourCcList = cc.FourCcList
			log.Info("Enhanced RTMP client detected", "fourCcList", cc.FourCcList)
		}

		resp, err := rpc.BuildConnectResponse(cc.TransactionID, "Connection succeeded.", cc.FourCcList)
		if err != nil {
			log.Error("connect response build failed", "error", err)
			return nil
		}
		if err := c.SendMessage(resp); err != nil {
			log.Error("connect response send failed", "error", err)
		} else {
			log.Info("connect response sent", "app", cc.App)
		}
		return nil
	}

	d.OnCreateStream = func(cs *rpc.CreateStreamCommand, msg *chunk.Message) error {
		resp, streamID, err := rpc.BuildCreateStreamResponse(cs.TransactionID, st.allocator)
		if err != nil {
			log.Error("createStream response build failed", "error", err)
			return nil
		}
		if err := c.SendMessage(resp); err != nil {
			log.Error("createStream response send failed", "error", err)
		} else {
			log.Info("createStream response sent", "stream_id", streamID, "txn_id", cs.TransactionID)
		}

		// Send UserControl StreamBegin to signal stream is ready.
		streamBegin := control.EncodeUserControlStreamBegin(streamID)
		if err := c.SendMessage(streamBegin); err != nil {
			log.Error("StreamBegin send failed", "error", err, "stream_id", streamID)
		}
		return nil
	}

	d.OnPublish = func(pc *rpc.PublishCommand, msg *chunk.Message) error {
		// Validate auth token before allowing publish.
		if rejected := authenticateRequest(cfg, c, st, msg, "publish", pc.PublishingName, pc.StreamKey, pc.QueryParams, log, srv); rejected {
			return nil
		}

		// Delegate to existing publish handler (sends onStatus internally).
		if _, err := HandlePublish(reg, c, st.app, msg); err != nil {
			log.Error("publish handle", "error", err)
			return nil
		}

		// Track stream key for this connection
		st.streamKey = pc.StreamKey
		st.role = "publisher"

		// Trigger publish start hook event
		srv.triggerHookEvent(hooks.EventPublishStart, c.ID(), pc.StreamKey, map[string]interface{}{
			"app":             st.app,
			"publishing_name": pc.PublishingName,
		})

		// Initialize recorder if recording is enabled
		if cfg.RecordAll {
			stream := reg.GetStream(pc.StreamKey)
			if stream != nil {
				if err := initRecorder(stream, cfg.RecordDir, log); err != nil {
					log.Error("failed to create recorder", "error", err, "stream_key", pc.StreamKey)
				} else {
					log.Info("recording started", "stream_key", pc.StreamKey, "record_dir", cfg.RecordDir)
				}
			}
		}

		return nil
	}

	d.OnPlay = func(pl *rpc.PlayCommand, msg *chunk.Message) error {
		// Validate auth token before allowing play.
		if rejected := authenticateRequest(cfg, c, st, msg, "play", pl.StreamName, pl.StreamKey, pl.QueryParams, log, srv); rejected {
			return nil
		}

		// Delegate to existing play handler (sends onStatus internally).
		if _, err := HandlePlay(reg, c, st.app, msg); err != nil {
			log.Error("play handle", "error", err)
			return nil
		}

		// Track stream key for this connection
		st.streamKey = pl.StreamKey
		st.role = "subscriber"

		// Trigger play start hook event
		srv.triggerHookEvent(hooks.EventPlayStart, c.ID(), pl.StreamKey, map[string]interface{}{
			"app": st.app,
		})
		// Fire subscriber count change after addition
		stream := reg.GetStream(pl.StreamKey)
		if stream != nil {
			srv.triggerHookEvent(hooks.EventSubscriberCount, c.ID(), pl.StreamKey, map[string]interface{}{
				"count": stream.SubscriberCount(),
			})
		}

		return nil
	}

	c.SetMessageHandler(func(m *chunk.Message) {
		if m == nil {
			return
		}

		// Route audio/video messages to media dispatch (recording + relay + broadcast).
		if m.TypeID == 8 || m.TypeID == 9 {
			dispatchMedia(m, st, reg, destMgr, log)
			return
		}

		if m.TypeID != rpc.CommandMessageAMF0TypeIDForTest() {
			return
		}
		if err := d.Dispatch(m); err != nil {
			log.Error("dispatch error", "error", err)
		}
	})
}

// authenticateRequest validates an auth token for a publish or play request.
// Returns true if the request was rejected (caller should return nil).
// Returns false if auth passed or no auth is configured (caller should proceed).
func authenticateRequest(
	cfg *Config,
	c *iconn.Connection,
	st *commandState,
	msg *chunk.Message,
	action string, // "publish" or "play"
	streamName string,
	streamKey string,
	queryParams map[string]string,
	log *slog.Logger,
	srv *Server,
) bool {
	if cfg.AuthValidator == nil {
		return false // no auth configured — allow
	}

	authReq := &auth.Request{
		App:           st.app,
		StreamName:    streamName,
		StreamKey:     streamKey,
		QueryParams:   queryParams,
		ConnectParams: st.connectParams,
		RemoteAddr:    c.NetConn().RemoteAddr().String(),
	}

	var err error
	if action == "publish" {
		err = cfg.AuthValidator.ValidatePublish(context.Background(), authReq)
	} else {
		err = cfg.AuthValidator.ValidatePlay(context.Background(), authReq)
	}

	if err == nil {
		log.Info(action+" authenticated", "stream_key", streamKey)
		return false // auth passed
	}

	// Auth failed — send error, emit hook, close connection.
	log.Warn(action+" authentication failed",
		"stream_key", streamKey,
		"remote_addr", authReq.RemoteAddr,
		"error", err)

	statusCode := "NetStream." + strings.ToUpper(action[:1]) + action[1:] + ".Unauthorized"
	errStatus, _ := buildOnStatus(msg.MessageStreamID, streamKey, statusCode, "Authentication failed.")
	_ = c.SendMessage(errStatus)

	srv.triggerHookEvent(hooks.EventAuthFailed, c.ID(), streamKey, map[string]interface{}{
		"action": action,
		"error":  err.Error(),
	})

	_ = c.Close()
	return true // rejected
}

// initRecorder creates and initializes a recorder for the given stream.
// It generates a timestamped filename based on the stream key and stores
// the recorder in the stream's Recorder field.
func initRecorder(stream *Stream, recordDir string, log *slog.Logger) error {
	if stream == nil {
		return fmt.Errorf("nil stream")
	}

	// Ensure record directory exists
	if err := os.MkdirAll(recordDir, 0755); err != nil {
		return fmt.Errorf("create record dir: %w", err)
	}

	// Generate filename: streamkey_timestamp.flv
	// Replace slashes in stream key with underscores for filesystem safety
	safeKey := strings.ReplaceAll(stream.Key, "/", "_")
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("%s_%s.flv", safeKey, timestamp)
	filepath := filepath.Join(recordDir, filename)

	// Create recorder
	recorder, err := media.NewRecorder(filepath, log)
	if err != nil {
		return fmt.Errorf("create recorder: %w", err)
	}

	// Store recorder in stream
	stream.mu.Lock()
	stream.Recorder = recorder
	stream.mu.Unlock()

	log.Info("recorder initialized", "stream_key", stream.Key, "file", filepath)
	return nil
}

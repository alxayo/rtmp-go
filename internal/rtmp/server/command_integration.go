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
		_, err := HandlePublish(reg, c, st.app, msg)

		// If publish failed because another publisher already occupies this
		// stream key, evict the stale publisher and retry. This handles the
		// common scenario where a streamer's app crashes or loses network,
		// then reconnects on a new TCP connection while the old zombie
		// connection hasn't timed out yet. Without eviction, the new
		// connection would be rejected with "publisher already registered".
		if err == ErrPublisherExists {
			log.Warn("evicting stale publisher",
				"stream_key", pc.StreamKey,
				"new_conn_id", c.ID())

			stream := reg.GetStream(pc.StreamKey)
			if stream != nil {
				// EvictPublisher atomically swaps the publisher and returns
				// the old one. The old publisher's disconnect handler will
				// fire when we close it below, but the identity check in
				// PublisherDisconnected (s.Publisher == pub) will see the
				// publisher has changed and safely skip cleanup.
				oldPub := stream.EvictPublisher(c)

				// Close the old connection to free resources. This runs in a
				// goroutine so we don't block the new publisher's setup.
				// The old connection's disconnect handler will fire and
				// handle its own cleanup (media logger stop, hook events,
				// server tracking removal).
				if closer, ok := oldPub.(interface{ Close() error }); ok {
					go func() {
						if err := closer.Close(); err != nil {
							log.Debug("error closing evicted publisher", "error", err)
						}
					}()
				}

				// Send onStatus to the new publisher since HandlePublish
				// didn't get to send it (it failed with ErrPublisherExists).
				onStatus, buildErr := buildOnStatus(
					msg.MessageStreamID,
					pc.StreamKey,
					"NetStream.Publish.Start",
					fmt.Sprintf("Publishing %s.", pc.StreamKey),
				)
				if buildErr == nil {
					_ = c.SendMessage(onStatus)
				}

				// Reset stream codec/header state so the new publisher's
				// sequence headers are properly cached (the old publisher's
				// codec config is stale and must not be sent to subscribers).
				stream.mu.Lock()
				stream.AudioSequenceHeader = nil
				stream.VideoSequenceHeader = nil
				stream.AudioCodec = ""
				stream.VideoCodec = ""
				stream.mu.Unlock()

				// Clear the error so we proceed with normal publish setup below.
				err = nil
			}
		}

		if err != nil {
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

	// handleStreamTeardown is a shared helper used by both the deleteStream and
	// closeStream handlers below. When an RTMP client ends a session, it sends
	// one of these commands to tell the server to release the stream. Without
	// this cleanup, the publisher stays registered in the registry even after
	// the client disconnects, which blocks any new publisher from reusing the
	// same stream key (they get "publisher already registered" errors).
	//
	// This function performs three things:
	//   1. Clears the publisher or subscriber from the stream registry
	//   2. Closes any active FLV recorder for the stream
	//   3. Resets the connection's role and stream key so the disconnect handler
	//      (which fires later when the TCP connection closes) doesn't try to
	//      clean up the same state a second time
	handleStreamTeardown := func(commandName string) {
		// If no stream was ever published or played on this connection, there
		// is nothing to clean up. This can happen if the client sends
		// deleteStream before completing a publish or play handshake.
		if st.streamKey == "" {
			log.Debug("stream teardown: no active stream", "command", commandName, "conn_id", c.ID())
			return
		}

		log.Info("stream teardown", "command", commandName, "conn_id", c.ID(),
			"stream_key", st.streamKey, "role", st.role)

		if st.role == "publisher" {
			// Publisher cleanup: close the recorder and unregister from the
			// registry so another client can publish to the same stream key.
			stream := reg.GetStream(st.streamKey)
			if stream != nil {
				// Close the FLV recorder (if active) under lock to avoid races
				// with the media dispatch goroutine that writes to it.
				stream.mu.Lock()
				if stream.Recorder != nil {
					if err := stream.Recorder.Close(); err != nil {
						log.Error("recorder close error on stream teardown",
							"error", err, "stream_key", st.streamKey)
					}
					stream.Recorder = nil
				}
				stream.mu.Unlock()
			}

			// Remove this connection as the publisher. After this call, a new
			// client can successfully publish to the same stream key.
			PublisherDisconnected(reg, st.streamKey, c)

			// Fire the publish-stop hook so external systems (webhooks, scripts)
			// know the stream has ended.
			audioPkts, videoPkts, totalBytes, audioCodec, videoCodec := st.mediaLogger.GetStats()
			durationSec := time.Since(c.AcceptedAt()).Seconds()
			srv.triggerHookEvent(hooks.EventPublishStop, c.ID(), st.streamKey, map[string]interface{}{
				"audio_packets": audioPkts,
				"video_packets": videoPkts,
				"total_bytes":   totalBytes,
				"audio_codec":   audioCodec,
				"video_codec":   videoCodec,
				"duration_sec":  durationSec,
			})
		} else if st.role == "subscriber" {
			// Subscriber cleanup: remove from the stream's subscriber list.
			SubscriberDisconnected(reg, st.streamKey, c)

			durationSec := time.Since(c.AcceptedAt()).Seconds()
			srv.triggerHookEvent(hooks.EventPlayStop, c.ID(), st.streamKey, map[string]interface{}{
				"duration_sec": durationSec,
			})
			// Notify external systems about the updated subscriber count.
			stream := reg.GetStream(st.streamKey)
			if stream != nil {
				srv.triggerHookEvent(hooks.EventSubscriberCount, c.ID(), st.streamKey, map[string]interface{}{
					"count": stream.SubscriberCount(),
				})
			}
		}

		// Clear the role and stream key so the disconnect handler (which fires
		// when the TCP connection finally closes) knows there is nothing left
		// to clean up. Without this, we would double-free the publisher or
		// subscriber slot.
		st.role = ""
		st.streamKey = ""
	}

	// deleteStream handler: called when the client sends the standard RTMP
	// "deleteStream" command to release a previously created stream. This is
	// the primary teardown command defined in the RTMP specification.
	d.OnDeleteStream = func(values []interface{}, msg *chunk.Message) error {
		handleStreamTeardown("deleteStream")
		return nil
	}

	// closeStream handler: called when the client sends "closeStream" instead
	// of (or in addition to) "deleteStream". Some RTMP clients like OBS and
	// certain mobile streaming apps use this non-standard command. It serves
	// the same purpose as deleteStream so we perform identical cleanup.
	d.OnCloseStream = func(values []interface{}, msg *chunk.Message) error {
		handleStreamTeardown("closeStream")
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

	// Generate filename: streamkey_timestamp.flv (or .mp4 for H.265+)
	// Replace slashes in stream key with underscores for filesystem safety
	safeKey := strings.ReplaceAll(stream.Key, "/", "_")
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("%s_%s.flv", safeKey, timestamp)
	filepath := filepath.Join(recordDir, filename)

	// Create recorder with detected codec (auto-selects FLV or MP4)
	codec := stream.GetVideoCodec()
	recorder, err := media.NewRecorder(filepath, codec, log)
	if err != nil {
		return fmt.Errorf("create recorder: %w", err)
	}

	// Store recorder in stream
	stream.mu.Lock()
	stream.Recorder = recorder
	stream.mu.Unlock()

	log.Info("recorder initialized", "stream_key", stream.Key, "file", filepath, "codec", codec)
	return nil
}

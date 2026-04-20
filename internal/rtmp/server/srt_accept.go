package server

// SRT Accept Loop
// ===============
// This file wires SRT ingest into the RTMP server. When an SRT publisher
// connects, we:
//   1. Parse the SRT Stream ID to determine the stream key and publish mode
//   2. Accept or reject the connection
//   3. Create a virtual "publisher" that satisfies the ingress.Publisher interface
//   4. Start the SRT→RTMP bridge which converts MPEG-TS to RTMP chunk.Messages
//
// From the RTMP server's perspective, SRT streams are indistinguishable from
// native RTMP publishes. The same stream registry, subscriber system, recording,
// and relay infrastructure handles both protocols transparently.

import (
	"fmt"
	"net"
	"strings"

	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
	"github.com/alxayo/go-rtmp/internal/rtmp/media"
	"github.com/alxayo/go-rtmp/internal/rtmp/metrics"
	"github.com/alxayo/go-rtmp/internal/rtmp/server/hooks"
	"github.com/alxayo/go-rtmp/internal/srt"
	srtconn "github.com/alxayo/go-rtmp/internal/srt/conn"
)

// startSRTListener creates and starts the SRT UDP listener.
// It's called from Server.Start() when SRTListenAddr is configured.
func (s *Server) startSRTListener() error {
	// Build SRT configuration from server config.
	// PassphraseResolver and Passphrase are mutually exclusive at the CLI
	// level, but both are passed here because the srt.Config layer handles
	// the precedence: resolver wins if set, static passphrase is the fallback.
	cfg := srt.Config{
		ListenAddr:         s.cfg.SRTListenAddr,
		Latency:            s.cfg.SRTLatency,
		Passphrase:         s.cfg.SRTPassphrase,
		PbKeyLen:           s.cfg.SRTPbKeyLen,
		PassphraseResolver: s.cfg.SRTPassphraseResolver,
	}

	s.log.Debug("SRT listener starting",
		"requested_addr", cfg.ListenAddr,
		"latency_ms", cfg.Latency,
		"encryption", cfg.Passphrase != "" || cfg.PassphraseResolver != nil,
		"pb_key_len", cfg.PbKeyLen,
		"per_stream", cfg.PassphraseResolver != nil, // true when using -srt-passphrase-file
	)

	// Log the active encryption mode at Info level so operators can confirm
	// the config at startup. Three modes: per-stream resolver, single static
	// passphrase, or no encryption at all.
	if cfg.PassphraseResolver != nil {
		aesLabel := map[int]string{16: "AES-128", 24: "AES-192", 32: "AES-256"}[cfg.PbKeyLen]
		s.log.Info("SRT per-stream encryption enabled", "key_length", aesLabel)
	} else if cfg.Passphrase != "" {
		aesLabel := map[int]string{16: "AES-128", 24: "AES-192", 32: "AES-256"}[cfg.PbKeyLen]
		s.log.Info("SRT encryption enabled", "key_length", aesLabel)
	} else {
		s.log.Info("SRT encryption disabled (no passphrase configured)")
	}

	// Start the SRT listener (binds a UDP socket and starts the read loop)
	ln, err := srt.Listen(cfg.ListenAddr, cfg)
	if err != nil {
		return fmt.Errorf("srt listen %s: %w", cfg.ListenAddr, err)
	}

	// Store the listener so we can shut it down later
	s.mu.Lock()
	s.srtListener = ln
	s.mu.Unlock()

	// Log details about the bound UDP socket
	addr := ln.Addr().String()
	netAddr, _ := ln.Addr().(*net.UDPAddr)

	s.log.Debug("SRT UDP socket bound",
		"requested", s.cfg.SRTListenAddr,
		"actual", addr,
		"network", "udp",
		"port", netAddr.Port,
		"is_wildcard", netAddr.IP.IsUnspecified(),
	)

	// Resolve all reachable addresses when listening on wildcard
	if netAddr != nil && netAddr.IP.IsUnspecified() {
		var ipv4Addrs []string
		var ipv6Addrs []string

		ifaces, err := net.Interfaces()
		if err == nil {
			for _, iface := range ifaces {
				if iface.Flags&net.FlagUp == 0 {
					continue
				}
				addrs, err := iface.Addrs()
				if err != nil {
					continue
				}
				for _, ifaceAddr := range addrs {
					ipNet, ok := ifaceAddr.(*net.IPNet)
					if !ok {
						continue
					}
					ip := ipNet.IP
					isLoopback := iface.Flags&net.FlagLoopback != 0

					if ip.To4() != nil {
						label := fmt.Sprintf("%s:%d", ip, netAddr.Port)
						if isLoopback {
							label += " (loopback)"
						}
						ipv4Addrs = append(ipv4Addrs, label)
						s.log.Debug("SRT reachable address",
							"interface", iface.Name,
							"ip_version", "IPv4",
							"address", fmt.Sprintf("%s:%d", ip, netAddr.Port),
							"loopback", isLoopback,
						)
					} else if ip.To16() != nil && !ip.IsLinkLocalUnicast() {
						label := fmt.Sprintf("[%s]:%d", ip, netAddr.Port)
						if isLoopback {
							label += " (loopback)"
						}
						ipv6Addrs = append(ipv6Addrs, label)
						s.log.Debug("SRT reachable address",
							"interface", iface.Name,
							"ip_version", "IPv6",
							"address", fmt.Sprintf("[%s]:%d", ip, netAddr.Port),
							"loopback", isLoopback,
						)
					}
				}
			}
		}

		var accessible []string
		if len(ipv4Addrs) > 0 {
			accessible = append(accessible, "IPv4: "+strings.Join(ipv4Addrs, ", "))
		}
		if len(ipv6Addrs) > 0 {
			accessible = append(accessible, "IPv6: "+strings.Join(ipv6Addrs, ", "))
		}

		s.log.Info("SRT server listening",
			"listen_addr", addr,
			"port", netAddr.Port,
			"protocol", "UDP",
			"accessible_at", strings.Join(accessible, " | "))
	} else {
		s.log.Info("SRT server listening", "addr", addr, "protocol", "UDP")
	}

	// Start the accept loop in a background goroutine.
	// acceptingWg ensures the server waits for this goroutine during shutdown.
	s.acceptingWg.Add(1)
	go s.srtAcceptLoop()

	return nil
}

// srtAcceptLoop accepts incoming SRT connections and wires each one
// to the media pipeline via the SRT→RTMP bridge.
//
// This is the SRT equivalent of acceptLoop() for RTMP. It runs in its
// own goroutine and processes connections sequentially (each connection
// spawns its own goroutine for actual media processing).
func (s *Server) srtAcceptLoop() {
	defer s.acceptingWg.Done()
	s.log.Debug("SRT accept loop started", "listener_addr", s.srtListener.Addr().String())

	for {
		// Block until a new SRT connection completes its handshake
		req, err := s.srtListener.Accept()
		if err != nil {
			// Check if we're shutting down
			s.mu.RLock()
			closing := s.closing
			s.mu.RUnlock()
			if closing {
				s.log.Debug("SRT accept loop exiting (listener closed)")
				return
			}
			// Check for listener closed error
			if err == net.ErrClosed {
				s.log.Debug("SRT accept loop exiting (net.ErrClosed)")
				return
			}
			s.log.Warn("SRT accept error", "error", err)
			continue
		}

		// Log every incoming SRT connection attempt at DEBUG — fires after
		// the handshake completes but before the server accepts/rejects.
		s.log.Debug("SRT incoming connection request",
			"remote", req.PeerAddr().String(),
			"stream_id", req.StreamID(),
			"stage", "pre-accept",
		)

		// Handle each connection in its own goroutine so we can
		// immediately go back to accepting the next one.
		go s.handleSRTConnection(req)
	}
}

// handleSRTConnection processes a single SRT ingest connection.
//
// The full lifecycle is:
//   1. Parse the Stream ID to get the stream key and mode (publish/subscribe)
//   2. Reject non-publish connections (SRT playback not supported in MVP)
//   3. Accept the SRT connection to get a conn.Conn handle
//   4. Register as a publisher in the ingress manager
//   5. Start the bridge (SRT → TS demux → codec convert → RTMP messages)
//   6. When the connection closes, clean up the publish session
func (s *Server) handleSRTConnection(req *srt.ConnRequest) {
	// Parse the SRT Stream ID to determine what the client wants to do.
	// The Stream ID format supports structured ("#!::r=live/test,m=publish")
	// and simple ("live/test" or "publish:live/test") formats.
	info := srt.ParseStreamID(req.StreamID())

	s.log.Debug("SRT stream ID parsed",
		"raw_stream_id", req.StreamID(),
		"stream_key", info.StreamKey(),
		"mode", info.Mode,
		"is_publish", info.IsPublish(),
		"remote", req.PeerAddr().String(),
	)

	// Only accept publish connections for now.
	// SRT playback (subscribing) is not supported in this version.
	if !info.IsPublish() {
		s.log.Warn("SRT connection rejected: not a publish request",
			"stream_id", req.StreamID(),
			"mode", info.Mode,
			"remote", req.PeerAddr().String(),
			"stage", "rejected",
		)
		req.Reject(srt.RejectBadRequest)
		return
	}

	// Accept the SRT connection — this completes the handshake.
	s.log.Debug("SRT accepting connection",
		"remote", req.PeerAddr().String(),
		"stream_key", info.StreamKey(),
		"stage", "accepting",
	)
	conn, err := req.Accept()
	if err != nil {
		s.log.Error("SRT accept failed",
			"error", err,
			"remote", req.PeerAddr().String(),
			"stream_key", info.StreamKey(),
			"stage", "accept-failed",
		)
		return
	}

	// Start the SRT reliability loop immediately after accepting.
	// This spawns a goroutine that sends ACK, NAK, and keepalive packets
	// back to the sender at regular intervals. Without this, the sender
	// will timeout after ~5 seconds because it never receives any ACKs.
	conn.StartReliability()

	// Install disconnect handler to clean up the listener's connection
	// registry when the SRT connection closes. Without this, closed
	// connections leak in the listener's conns map forever.
	// Capture the listener pointer now — s.srtListener may be nilled
	// during server shutdown before this handler fires.
	ln := s.srtListener
	conn.SetDisconnectHandler(func() {
		if ln != nil {
			ln.RemoveConn(conn.LocalSocketID())
		}
	})

	// Generate a unique connection ID for logging and tracking.
	connID := fmt.Sprintf("srt-%d", conn.LocalSocketID())

	// Update metrics
	metrics.SRTConnectionsActive.Add(1)
	metrics.SRTConnectionsTotal.Add(1)

	s.log.Info("SRT connection accepted",
		"conn_id", connID,
		"remote", conn.PeerAddr().String(),
		"stream_key", info.StreamKey(),
		"stream_id", req.StreamID(),
		"stage", "connected",
	)
	s.log.Debug("SRT connection details",
		"conn_id", connID,
		"remote", conn.PeerAddr().String(),
		"stream_key", info.StreamKey(),
		"socket_id", conn.LocalSocketID(),
		"active_srt_connections", metrics.SRTConnectionsActive.Value(),
		"total_srt_connections", metrics.SRTConnectionsTotal.Value(),
	)

	// Fire the connection accept hook event so external systems are notified.
	s.triggerHookEvent(hooks.EventConnectionAccept, connID, info.StreamKey(), map[string]interface{}{
		"remote_addr": conn.PeerAddr().String(),
		"protocol":    "srt",
	})

	// Create a virtual publisher that wraps the SRT connection.
	// This satisfies the ingress.Publisher interface so the ingress
	// manager can track it alongside RTMP publishers.
	pub := &srtPublisher{
		id:        connID,
		conn:      conn,
		streamKey: info.StreamKey(),
	}

	// Register the publisher with the ingress manager.
	// This ensures uniqueness (only one publisher per stream key).
	s.log.Debug("SRT registering publisher",
		"conn_id", connID,
		"stream_key", info.StreamKey(),
		"stage", "registering",
	)
	session, err := s.ingressManager.BeginPublish(pub)
	if err != nil {
		// Stream key in use — evict the stale session and retry.
		// This handles the common case where a streamer disconnects
		// (OBS crash, network loss) and reconnects before the old
		// session's cleanup has finished running EndPublish().
		s.log.Warn("SRT evicting stale publish session",
			"stream_key", info.StreamKey(),
			"conn_id", connID,
		)
		s.ingressManager.EndPublish(info.StreamKey())
		session, err = s.ingressManager.BeginPublish(pub)
		if err != nil {
			s.log.Error("SRT publish rejected after eviction attempt",
				"error", err,
				"stream_key", info.StreamKey(),
				"conn_id", connID,
				"stage", "publish-rejected",
			)
			conn.Close()
			metrics.SRTConnectionsActive.Add(-1)
			return
		}
	}

	// Create or get the stream in the registry. This is the same structure
	// used by RTMP publishers — it holds subscribers, codec info, sequence
	// headers, and the recorder. Creating it here makes SRT streams visible
	// to RTMP play clients and the recording system.
	stream, _ := s.reg.CreateStream(info.StreamKey())
	if stream == nil {
		s.log.Error("SRT failed to create stream in registry",
			"stream_key", info.StreamKey(),
			"conn_id", connID,
		)
		session.EndPublish()
		conn.Close()
		metrics.SRTConnectionsActive.Add(-1)
		return
	}

	// Register this SRT connection as the stream's publisher.
	// This enforces single-publisher-per-stream and allows RTMP play
	// clients to detect that a publisher is active.
	if err := stream.SetPublisher(pub); err != nil {
		// Publisher already exists — evict the stale one. This mirrors
		// the RTMP eviction pattern in command_integration.go and handles
		// reconnection after unclean disconnect (zombie connection).
		s.log.Warn("SRT evicting stale publisher",
			"stream_key", info.StreamKey(),
			"conn_id", connID,
		)
		oldPub := stream.EvictPublisher(pub)
		if closer, ok := oldPub.(interface{ Close() error }); ok {
			go func() {
				if err := closer.Close(); err != nil {
					s.log.Debug("error closing evicted SRT publisher", "error", err)
				}
			}()
		}
	}

	// Mark stream for recording — actual recorder creation is deferred to the
	// first media frame (in the MediaHandler below) so that the video codec is
	// known and the correct container format (FLV for H.264, MP4 for H.265+)
	// is selected automatically.
	if s.cfg.RecordAll {
		stream.mu.Lock()
		stream.RecordDir = s.cfg.RecordDir
		stream.SegmentDuration = s.cfg.SegmentDuration // propagate segment config
		stream.SegmentPattern = s.cfg.SegmentPattern   // propagate segment config
		stream.mu.Unlock()
		s.log.Info("recording requested",
			"stream_key", info.StreamKey(),
			"record_dir", s.cfg.RecordDir,
			"conn_id", connID,
		)
	}

	// Wire the media handler: when the bridge pushes a chunk.Message
	// (audio TypeID=8 or video TypeID=9), it flows through this callback
	// to the recording system and subscriber broadcast — exactly the same
	// path as native RTMP media dispatch.
	//
	// Ordering: codec detection runs first (via BroadcastMessage), then the
	// recorder is lazily initialized with the correct format, then the frame
	// is written. This ensures H.265 streams get MP4 containers.
	detector := &media.CodecDetector{}
	connLog := s.log.With("conn_id", connID)
	session.MediaHandler = func(msg *chunk.Message) {
		// 1. Codec detection + subscriber broadcast first
		stream.BroadcastMessage(detector, msg, connLog)

		// 2. Lazy recorder init (creates recorder once codec is known)
		ensureRecorder(stream, connLog, nil)

		// 3. Write to recorder (snapshot under lock to avoid race with teardown)
		if rec := stream.GetRecorder(); rec != nil {
			rec.WriteMessage(msg)
		}
	}

	// Start the bridge — this blocks until the SRT connection closes.
	// The bridge reads MPEG-TS from SRT, converts to RTMP, and pushes
	// through the publish session.
	s.log.Debug("SRT bridge starting",
		"conn_id", connID,
		"stream_key", info.StreamKey(),
		"stage", "bridge-starting",
	)
	bridge := srt.NewBridge(conn, session, s.log.With("conn_id", connID))
	bridgeErr := bridge.Run()

	if bridgeErr != nil {
		s.log.Warn("SRT bridge exited with error",
			"conn_id", connID,
			"error", bridgeErr,
			"stage", "bridge-error",
		)
	} else {
		s.log.Debug("SRT bridge exited cleanly",
			"conn_id", connID,
			"stream_key", info.StreamKey(),
			"stage", "bridge-done",
		)
	}

	// Clean up: close recorder, clear publisher, end session, close connection.
	// This mirrors the RTMP publisher disconnect cleanup in command_integration.go.
	//
	// Guard all cleanup with a publisher identity check: if we were evicted
	// by a new publisher (via EvictPublisher), the identity check fails and
	// we skip recorder/publisher cleanup to avoid interfering with the new
	// publisher's state.
	if stream != nil {
		stream.mu.Lock()
		if stream.Publisher == pub {
			// We're still the active publisher — clean up fully.
			if stream.Recorder != nil {
				if err := stream.Recorder.Close(); err != nil {
					s.log.Error("recorder close error on SRT disconnect",
						"error", err,
						"stream_key", info.StreamKey(),
						"conn_id", connID,
					)
					metrics.RecordingErrorsTotal.Add(1)
				} else {
					s.log.Info("recording stopped",
						"stream_key", info.StreamKey(),
						"conn_id", connID,
					)
				}
				stream.Recorder = nil
				metrics.RecordingsActive.Add(-1)
			}
			stream.Publisher = nil
			metrics.PublishersActive.Add(-1)
		}
		stream.mu.Unlock()
	}

	session.EndPublish()
	conn.Close()
	metrics.SRTConnectionsActive.Add(-1)

	s.log.Info("SRT connection closed",
		"conn_id", connID,
		"stream_key", info.StreamKey(),
		"stage", "disconnected",
	)
	s.log.Debug("SRT connection cleanup done",
		"conn_id", connID,
		"active_srt_connections", metrics.SRTConnectionsActive.Value(),
	)
}

// srtPublisher implements the ingress.Publisher interface for SRT connections.
// It wraps an SRT conn.Conn and provides the identity information needed
// for the publish lifecycle.
type srtPublisher struct {
	// id is the unique connection identifier (e.g., "srt-12345").
	id string

	// conn is the underlying SRT connection.
	conn *srtconn.Conn

	// streamKey is the RTMP-style stream key derived from the SRT Stream ID.
	streamKey string
}

// ID returns the unique identifier for this SRT publisher.
func (p *srtPublisher) ID() string { return p.id }

// Protocol returns "srt" to identify this as an SRT connection.
func (p *srtPublisher) Protocol() string { return "srt" }

// RemoteAddr returns the remote UDP address as a string.
func (p *srtPublisher) RemoteAddr() string {
	if p.conn.PeerAddr() != nil {
		return p.conn.PeerAddr().String()
	}
	return "unknown"
}

// StreamKey returns the stream key for routing in the server.
func (p *srtPublisher) StreamKey() string { return p.streamKey }

// Close disconnects the SRT publisher by closing the underlying connection.
func (p *srtPublisher) Close() error { return p.conn.Close() }

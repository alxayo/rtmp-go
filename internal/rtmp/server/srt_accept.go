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

	"github.com/alxayo/go-rtmp/internal/rtmp/metrics"
	"github.com/alxayo/go-rtmp/internal/rtmp/server/hooks"
	"github.com/alxayo/go-rtmp/internal/srt"
	srtconn "github.com/alxayo/go-rtmp/internal/srt/conn"
)

// startSRTListener creates and starts the SRT UDP listener.
// It's called from Server.Start() when SRTListenAddr is configured.
func (s *Server) startSRTListener() error {
	// Build SRT configuration from server config
	cfg := srt.Config{
		ListenAddr: s.cfg.SRTListenAddr,
		Latency:    s.cfg.SRTLatency,
		Passphrase: s.cfg.SRTPassphrase,
		PbKeyLen:   s.cfg.SRTPbKeyLen,
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

	s.log.Info("SRT server listening", "addr", ln.Addr().String())

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

	for {
		// Block until a new SRT connection completes its handshake
		req, err := s.srtListener.Accept()
		if err != nil {
			// Check if we're shutting down
			s.mu.RLock()
			closing := s.closing
			s.mu.RUnlock()
			if closing {
				return
			}
			// Check for listener closed error
			if err == net.ErrClosed {
				return
			}
			s.log.Warn("SRT accept error", "error", err)
			continue
		}

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

	// Only accept publish connections for now.
	// SRT playback (subscribing) is not supported in this version.
	if !info.IsPublish() {
		s.log.Warn("SRT connection rejected: not a publish request",
			"stream_id", req.StreamID(),
			"mode", info.Mode,
			"remote", req.PeerAddr().String(),
		)
		req.Reject(srt.RejectBadRequest)
		return
	}

	// Accept the SRT connection — this completes the handshake.
	conn, err := req.Accept()
	if err != nil {
		s.log.Error("SRT accept failed", "error", err)
		return
	}

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
	session, err := s.ingressManager.BeginPublish(pub)
	if err != nil {
		s.log.Error("SRT publish rejected",
			"error", err,
			"stream_key", info.StreamKey(),
			"conn_id", connID,
		)
		conn.Close()
		metrics.SRTConnectionsActive.Add(-1)
		return
	}

	// Wire the media handler: when the bridge pushes a chunk.Message,
	// it goes through the session's MediaHandler to the stream registry
	// and out to all subscribers.
	// TODO: Wire session.MediaHandler to the RTMP stream registry
	// session.MediaHandler = func(msg *chunk.Message) { s.reg.Broadcast(info.StreamKey(), msg) }

	// Start the bridge — this blocks until the SRT connection closes.
	// The bridge reads MPEG-TS from SRT, converts to RTMP, and pushes
	// through the publish session.
	bridge := srt.NewBridge(conn, session, s.log.With("conn_id", connID))
	bridgeErr := bridge.Run()

	if bridgeErr != nil {
		s.log.Warn("SRT bridge exited with error",
			"conn_id", connID,
			"error", bridgeErr,
		)
	}

	// Clean up: end the publish session and close the connection.
	session.EndPublish()
	conn.Close()
	metrics.SRTConnectionsActive.Add(-1)

	s.log.Info("SRT connection closed",
		"conn_id", connID,
		"stream_key", info.StreamKey(),
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

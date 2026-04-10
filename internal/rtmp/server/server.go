package server

// RTMP Server
// ===========
// TCP listener + connection manager with stream registry, pub/sub
// coordination, media recording, relay, and event hooks.

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/alxayo/go-rtmp/internal/ingress"
	"github.com/alxayo/go-rtmp/internal/logger"
	"github.com/alxayo/go-rtmp/internal/rtmp/client"
	iconn "github.com/alxayo/go-rtmp/internal/rtmp/conn"
	"github.com/alxayo/go-rtmp/internal/rtmp/metrics"
	"github.com/alxayo/go-rtmp/internal/rtmp/relay"
	"github.com/alxayo/go-rtmp/internal/rtmp/server/auth"
	"github.com/alxayo/go-rtmp/internal/rtmp/server/hooks"
	"github.com/alxayo/go-rtmp/internal/srt"
)

// Config holds all settings for the RTMP server.
type Config struct {
	ListenAddr        string   // TCP address to listen on (default ":1935")
	ChunkSize         uint32   // outbound chunk payload size in bytes (default 4096)
	WindowAckSize     uint32   // flow control: bytes before client must acknowledge (default 2,500,000)
	RecordAll         bool     // if true, automatically record all published streams to FLV files
	RecordDir         string   // directory for FLV recordings (default "recordings")
	LogLevel          string   // log verbosity: "debug", "info", "warn", "error" (default "info")
	RelayDestinations []string // RTMP URLs to forward published streams to (e.g. rtmp://cdn/live/key)

	// TLS configuration (all optional). When TLSListenAddr is non-empty, the server
	// starts a second listener for RTMPS (RTMP over TLS) alongside the plain RTMP listener.
	TLSListenAddr string // RTMPS listen address (e.g. ":443"). Empty = disabled
	TLSCertFile   string // Path to PEM-encoded TLS certificate file
	TLSKeyFile    string // Path to PEM-encoded TLS private key file

	// Event hook configuration (all optional)
	HookScripts     []string // Shell hooks: "event_type=/path/to/script" pairs
	HookWebhooks    []string // Webhook hooks: "event_type=https://url" pairs
	HookStdioFormat string   // Stdio output format: "json", "env", or "" (disabled)
	HookTimeout     string   // Hook execution timeout (default "30s")
	HookConcurrency int      // Max concurrent hook executions (default 10)

	// Authentication (optional). When nil, all publish/play requests are allowed.
	// Set to an auth.Validator implementation to enforce token-based access control.
	AuthValidator auth.Validator

	// SRT configuration (all optional). When SRTListenAddr is non-empty,
	// the server starts a UDP listener for SRT ingest alongside RTMP.
	SRTListenAddr string // SRT UDP listen address (e.g. ":10080"). Empty = disabled
	SRTLatency    int    // SRT buffer latency in milliseconds (default 120)
	SRTPassphrase string // SRT encryption passphrase (empty = plaintext)
	SRTPbKeyLen   int    // AES key length: 16, 24, or 32 (default 16)
}

// applyDefaults fills zero values with sensible defaults.
func (c *Config) applyDefaults() {
	if c.ListenAddr == "" {
		c.ListenAddr = ":1935"
	}
	if c.ChunkSize == 0 {
		c.ChunkSize = 4096
	} // matches control burst constant
	if c.WindowAckSize == 0 {
		c.WindowAckSize = 2_500_000
	} // matches control burst
	if c.RecordDir == "" {
		c.RecordDir = "recordings"
	}
	if c.LogLevel == "" {
		c.LogLevel = "info"
	}
	if c.SRTLatency == 0 {
		c.SRTLatency = 120
	}
	if c.SRTPbKeyLen == 0 {
		c.SRTPbKeyLen = 16
	}
}

// Server encapsulates listener + active connection tracking.
type Server struct {
	cfg                Config
	l                  net.Listener
	tlsListener        net.Listener // optional RTMPS listener (nil when TLS disabled)
	srtListener        *srt.Listener // optional SRT listener (nil when SRT disabled)
	log                *slog.Logger
	reg                *Registry
	destinationManager *relay.DestinationManager
	hookManager        *hooks.HookManager
	ingressManager     *ingress.Manager // protocol-agnostic publish manager

	mu          sync.RWMutex
	conns       map[string]*iconn.Connection
	acceptingWg sync.WaitGroup
	closing     bool
}

// New creates a new, unstarted Server instance.
func New(cfg Config) *Server {
	cfg.applyDefaults()

	// Initialize destination manager if destinations are provided
	var destMgr *relay.DestinationManager
	if len(cfg.RelayDestinations) > 0 {
		var err error
		// Create a client factory that wraps the client.New function
		clientFactory := func(url string) (relay.RTMPClient, error) {
			return client.New(url)
		}
		destMgr, err = relay.NewDestinationManager(cfg.RelayDestinations, logger.Logger(), clientFactory)
		if err != nil {
			logger.Logger().Error("Failed to initialize destination manager", "error", err)
			// Continue without relay functionality
		}
	}

	// Initialize hook manager
	hookMgr := initializeHookManager(cfg, logger.Logger())

	return &Server{
		cfg:                cfg,
		reg:                NewRegistry(),
		conns:              make(map[string]*iconn.Connection),
		log:                logger.Logger().With("component", "rtmp_server"),
		destinationManager: destMgr,
		hookManager:        hookMgr,
		ingressManager:     ingress.NewManager(logger.Logger()),
	}
}

// Start begins listening and launches the accept loop. It's safe to call
// only once; repeated calls return an error.
func (s *Server) Start() error {
	if s == nil {
		return errors.New("nil server")
	}

	s.log.Debug("starting server",
		"listen_addr", s.cfg.ListenAddr,
		"chunk_size", s.cfg.ChunkSize,
		"window_ack_size", s.cfg.WindowAckSize,
		"record_all", s.cfg.RecordAll,
		"record_dir", s.cfg.RecordDir,
		"log_level", s.cfg.LogLevel,
		"srt_listen", s.cfg.SRTListenAddr,
		"srt_latency_ms", s.cfg.SRTLatency,
		"tls_listen", s.cfg.TLSListenAddr,
	)

	s.mu.Lock()
	if s.l != nil {
		s.mu.Unlock()
		return errors.New("server already started")
	}
	ln, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("listen %s: %w", s.cfg.ListenAddr, err)
	}
	s.l = ln
	s.mu.Unlock()

	// Log the listening address and resolved IPs
	s.logListenerInfo("RTMP", ln)
	s.acceptingWg.Add(1)
	go s.acceptLoop(ln)

	// Start optional RTMPS (TLS) listener
	if s.cfg.TLSListenAddr != "" {
		tlsLn, err := s.startTLSListener()
		if err != nil {
			// TLS listener failure is fatal — stop the plain listener and return error
			_ = ln.Close()
			s.mu.Lock()
			s.l = nil
			s.mu.Unlock()
			return fmt.Errorf("tls listen: %w", err)
		}
		s.mu.Lock()
		s.tlsListener = tlsLn
		s.mu.Unlock()
		s.logListenerInfo("RTMPS", tlsLn)
		s.acceptingWg.Add(1)
		go s.acceptLoop(tlsLn)
	}

	// Start optional SRT (UDP) listener for SRT ingest
	if s.cfg.SRTListenAddr != "" {
		if err := s.startSRTListener(); err != nil {
			// SRT listener failure is not fatal — RTMP still works
			s.log.Error("SRT listener failed to start", "error", err)
		}
	}

	return nil
}

// startTLSListener creates and returns a TLS-wrapped net.Listener.
func (s *Server) startTLSListener() (net.Listener, error) {
	cert, err := tls.LoadX509KeyPair(s.cfg.TLSCertFile, s.cfg.TLSKeyFile)
	if err != nil {
		return nil, fmt.Errorf("load TLS certificate: %w", err)
	}
	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
	tcpLn, err := net.Listen("tcp", s.cfg.TLSListenAddr)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", s.cfg.TLSListenAddr, err)
	}
	return tls.NewListener(tcpLn, tlsCfg), nil
}

// logListenerInfo logs the listening address and resolves all reachable IPs.
// For wildcard addresses ([::]  or 0.0.0.0), it enumerates every network interface
// so the operator can see exactly which IPs the server is reachable at.
func (s *Server) logListenerInfo(protocol string, listener net.Listener) {
	addr := listener.Addr().String()

	// Try to resolve which IPs this listener is actually bound to
	netAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok || netAddr == nil {
		s.log.Info(protocol+" server listening", "addr", addr)
		return
	}

	// Log the raw bind result at DEBUG so operators can see network/address details
	s.log.Debug(protocol+" TCP socket bound",
		"requested", s.cfg.ListenAddr,
		"actual", addr,
		"network", netAddr.Network(),
		"ip", netAddr.IP.String(),
		"port", netAddr.Port,
		"is_ipv4", netAddr.IP.To4() != nil,
		"is_ipv6", netAddr.IP.To4() == nil,
		"is_wildcard", netAddr.IP.IsUnspecified(),
	)

	// If listening on a specific IP (not wildcard), just log that address
	if !netAddr.IP.IsUnspecified() {
		s.log.Info(protocol+" server listening", "addr", addr)
		return
	}

	// Wildcard address — resolve every reachable IP from all interfaces
	var ipv4Addrs []string
	var ipv6Addrs []string

	ifaces, err := net.Interfaces()
	if err != nil {
		s.log.Debug("failed to list network interfaces", "error", err)
		s.log.Info(protocol+" server listening", "addr", addr)
		return
	}

	for _, iface := range ifaces {
		// Skip interfaces that are down or loopback
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
				s.log.Debug(protocol+" reachable address",
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
				s.log.Debug(protocol+" reachable address",
					"interface", iface.Name,
					"ip_version", "IPv6",
					"address", fmt.Sprintf("[%s]:%d", ip, netAddr.Port),
					"loopback", isLoopback,
				)
			}
		}
	}

	// Build a human-readable summary for the INFO line
	var accessible []string
	if len(ipv4Addrs) > 0 {
		accessible = append(accessible, "IPv4: "+strings.Join(ipv4Addrs, ", "))
	}
	if len(ipv6Addrs) > 0 {
		accessible = append(accessible, "IPv6: "+strings.Join(ipv6Addrs, ", "))
	}

	s.log.Info(protocol+" server listening",
		"listen_addr", addr,
		"port", netAddr.Port,
		"accessible_at", strings.Join(accessible, " | "))
}

// acceptLoop runs until listener close. Each successful accept performs the
// RTMP handshake via conn.Accept which internally sends the control burst.
func (s *Server) acceptLoop(l net.Listener) {
	defer s.acceptingWg.Done()
	s.log.Debug("RTMP accept loop started", "listener_addr", l.Addr().String())

	for {
		raw, err := l.Accept()
		if err != nil {
			// If we are shutting down, Accept will return an error (use closing flag to suppress noise).
			s.mu.RLock()
			closing := s.closing
			s.mu.RUnlock()
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				s.log.Debug("RTMP accept timeout (retrying)", "error", err)
				continue
			}
			if closing || errors.Is(err, net.ErrClosed) {
				s.log.Debug("RTMP accept loop exiting (listener closed)")
				return
			}
			s.log.Warn("RTMP accept error (loop terminating)", "error", err)
			return
		}

		// Log every incoming TCP connection at DEBUG — this fires BEFORE the
		// RTMP handshake, so you can see connection attempts even if they fail.
		remoteAddr := raw.RemoteAddr().String()
		localAddr := raw.LocalAddr().String()
		s.log.Debug("RTMP incoming TCP connection",
			"remote", remoteAddr,
			"local", localAddr,
			"stage", "pre-handshake",
		)

		// Detect whether this connection arrived over TLS
		_, isTLS := raw.(*tls.Conn)

		// Handshake + control burst integration lives in conn.Accept.
		// We temporarily wrap the raw listener to reuse existing function.
		// Trick: create a one-off fake listener returning this raw conn.
		single := &singleConnListener{conn: raw}
		c, err := iconn.Accept(single)
		if err != nil {
			// Handshake failed — log at WARN so operators can diagnose
			s.log.Warn("RTMP handshake failed",
				"remote", remoteAddr,
				"local", localAddr,
				"tls", isTLS,
				"error", err,
				"stage", "handshake",
			)
			continue
		}

		s.mu.Lock()
		s.conns[c.ID()] = c
		s.mu.Unlock()
		metrics.ConnectionsActive.Add(1)
		metrics.ConnectionsTotal.Add(1)

		s.log.Info("RTMP connection registered",
			"conn_id", c.ID(),
			"remote", remoteAddr,
			"local", localAddr,
			"tls", isTLS,
			"stage", "connected",
		)
		s.log.Debug("RTMP connection details",
			"conn_id", c.ID(),
			"remote", remoteAddr,
			"local", localAddr,
			"tls", isTLS,
			"active_connections", metrics.ConnectionsActive.Value(),
			"total_connections", metrics.ConnectionsTotal.Value(),
		)

		// Trigger connection accept hook event
		s.triggerHookEvent(hooks.EventConnectionAccept, c.ID(), "", map[string]interface{}{
			"remote_addr": raw.RemoteAddr().String(),
			"tls":         isTLS,
		})

		// Wire command handling so real clients (OBS/ffmpeg) can complete
		// connect/createStream/publish. (Incremental integration step.)
		attachCommandHandling(c, s.reg, &s.cfg, s.log, s.destinationManager, s)
		// Start readLoop AFTER message handler is attached to avoid race condition
		c.Start()
	}
}

// Stop gracefully shuts down the server: stops accepting new connections,
// closes all active ones, waits for accept loop completion.
func (s *Server) Stop() error {
	if s == nil {
		return errors.New("nil server")
	}
	s.mu.Lock()
	if s.l == nil {
		s.mu.Unlock()
		return nil
	}
	s.closing = true
	l := s.l
	s.l = nil
	tlsLn := s.tlsListener
	s.tlsListener = nil
	srtLn := s.srtListener
	s.srtListener = nil
	s.mu.Unlock()
	_ = l.Close()
	if tlsLn != nil {
		_ = tlsLn.Close()
	}
	if srtLn != nil {
		_ = srtLn.Close()
	}

	// Close all connections and clean up recorders.
	s.mu.Lock()
	connsToClose := make([]*iconn.Connection, 0, len(s.conns))
	for _, c := range s.conns {
		connsToClose = append(connsToClose, c)
	}
	clear(s.conns)
	s.mu.Unlock()

	// Close connections outside the lock to avoid deadlock with
	// disconnect handler's RemoveConnection call.
	for _, c := range connsToClose {
		_ = c.Close()
	}

	// Clean up all active recorders
	s.cleanupAllRecorders()

	// Close destination manager
	if s.destinationManager != nil {
		if err := s.destinationManager.Close(); err != nil {
			s.log.Error("Error closing destination manager", "error", err)
		}
	}

	// Close hook manager
	if s.hookManager != nil {
		if err := s.hookManager.Close(); err != nil {
			s.log.Error("Error closing hook manager", "error", err)
		}
	}

	s.acceptingWg.Wait()
	s.log.Info("RTMP server stopped")
	return nil
}

// Addr returns the bound listener address (nil if not started).
func (s *Server) Addr() net.Addr {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.l == nil {
		return nil
	}
	return s.l.Addr()
}

// TLSAddr returns the bound TLS listener address (nil if TLS not enabled).
func (s *Server) TLSAddr() net.Addr {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.tlsListener == nil {
		return nil
	}
	return s.tlsListener.Addr()
}

// SRTAddr returns the bound SRT listener address (nil if SRT not enabled).
func (s *Server) SRTAddr() net.Addr {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.srtListener == nil {
		return nil
	}
	return s.srtListener.Addr()
}

// ConnectionCount returns current number of tracked active connections.
func (s *Server) ConnectionCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.conns)
}

// RemoveConnection removes a single connection from the tracking map.
// Called by the disconnect handler when a connection's readLoop exits.
func (s *Server) RemoveConnection(id string) {
	s.mu.Lock()
	delete(s.conns, id)
	s.mu.Unlock()
	metrics.ConnectionsActive.Add(-1)
}

// singleConnListener wraps a single pre-accepted net.Conn as a net.Listener.
// This adapter exists because conn.Accept() expects a net.Listener (for the
// handshake flow), but the server's accept loop already called l.Accept() to
// get the raw TCP connection. Rather than duplicating the handshake logic,
// we wrap the raw conn in this one-shot listener so conn.Accept() can reuse it.
type singleConnListener struct{ conn net.Conn }

func (s *singleConnListener) Accept() (net.Conn, error) {
	if s.conn == nil {
		return nil, errors.New("no conn")
	}
	c := s.conn
	s.conn = nil
	return c, nil
}
func (s *singleConnListener) Close() error {
	if s.conn != nil {
		_ = s.conn.Close()
	}
	return nil
}
func (s *singleConnListener) Addr() net.Addr {
	if s.conn != nil {
		return s.conn.LocalAddr()
	}
	return &net.TCPAddr{}
}

// cleanupAllRecorders closes all active recorders in the registry.
// This is called during server shutdown to ensure all FLV files are properly closed.
func (s *Server) cleanupAllRecorders() {
	if s == nil || s.reg == nil {
		return
	}

	s.reg.mu.RLock()
	streams := make([]*Stream, 0, len(s.reg.streams))
	for _, stream := range s.reg.streams {
		streams = append(streams, stream)
	}
	s.reg.mu.RUnlock()

	for _, stream := range streams {
		if stream == nil {
			continue
		}

		stream.mu.Lock()
		if stream.Recorder != nil {
			if err := stream.Recorder.Close(); err != nil {
				s.log.Error("recorder close error", "error", err, "stream_key", stream.Key)
			} else {
				s.log.Info("recorder closed", "stream_key", stream.Key)
			}
			stream.Recorder = nil
		}
		stream.mu.Unlock()
	}
}

// triggerHookEvent dispatches an event to all registered hooks for the given event type.
// Safe to call even if the hook manager is nil (hooks disabled).
func (s *Server) triggerHookEvent(eventType hooks.EventType, connID, streamKey string, data map[string]interface{}) {
	if s == nil || s.hookManager == nil {
		return
	}
	event := hooks.NewEvent(eventType).
		WithConnID(connID).
		WithStreamKey(streamKey)
	for k, v := range data {
		event.WithData(k, v)
	}
	s.hookManager.TriggerEvent(context.Background(), *event)
}

// initializeHookManager creates and configures the hook manager from server config.
func initializeHookManager(cfg Config, logger *slog.Logger) *hooks.HookManager {
	hookConfig := hooks.HookConfig{
		Timeout:     cfg.HookTimeout,
		Concurrency: cfg.HookConcurrency,
		StdioFormat: cfg.HookStdioFormat,
	}
	if hookConfig.Timeout == "" {
		hookConfig.Timeout = "30s"
	}
	if hookConfig.Concurrency == 0 {
		hookConfig.Concurrency = 10
	}

	hookManager := hooks.NewHookManager(hookConfig, logger)

	// Register shell hooks from configuration (format: "event_type=/path/to/script")
	for i, script := range cfg.HookScripts {
		parts := strings.SplitN(script, "=", 2)
		if len(parts) != 2 {
			logger.Error("Invalid shell hook format (expected event_type=script_path)", "hook", script)
			continue
		}
		eventType := hooks.EventType(parts[0])
		shellHook := hooks.NewShellHook(fmt.Sprintf("shell_%d", i), parts[1], 30*time.Second)
		if err := hookManager.RegisterHook(eventType, shellHook); err != nil {
			logger.Error("Failed to register shell hook", "hook", script, "error", err)
		}
	}

	// Register webhook hooks from configuration (format: "event_type=https://url")
	for i, webhook := range cfg.HookWebhooks {
		parts := strings.SplitN(webhook, "=", 2)
		if len(parts) != 2 {
			logger.Error("Invalid webhook hook format (expected event_type=url)", "hook", webhook)
			continue
		}
		eventType := hooks.EventType(parts[0])
		webhookHook := hooks.NewWebhookHook(fmt.Sprintf("webhook_%d", i), parts[1], 30*time.Second)
		if err := hookManager.RegisterHook(eventType, webhookHook); err != nil {
			logger.Error("Failed to register webhook hook", "hook", webhook, "error", err)
		}
	}

	return hookManager
}

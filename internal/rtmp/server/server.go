package server

// RTMP Server
// ===========
// TCP listener + connection manager with stream registry, pub/sub
// coordination, media recording, relay, and event hooks.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/alxayo/go-rtmp/internal/logger"
	"github.com/alxayo/go-rtmp/internal/rtmp/client"
	iconn "github.com/alxayo/go-rtmp/internal/rtmp/conn"
	"github.com/alxayo/go-rtmp/internal/rtmp/metrics"
	"github.com/alxayo/go-rtmp/internal/rtmp/relay"
	"github.com/alxayo/go-rtmp/internal/rtmp/server/auth"
	"github.com/alxayo/go-rtmp/internal/rtmp/server/hooks"
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

	// Event hook configuration (all optional)
	HookScripts     []string // Shell hooks: "event_type=/path/to/script" pairs
	HookWebhooks    []string // Webhook hooks: "event_type=https://url" pairs
	HookStdioFormat string   // Stdio output format: "json", "env", or "" (disabled)
	HookTimeout     string   // Hook execution timeout (default "30s")
	HookConcurrency int      // Max concurrent hook executions (default 10)

	// Authentication (optional). When nil, all publish/play requests are allowed.
	// Set to an auth.Validator implementation to enforce token-based access control.
	AuthValidator auth.Validator
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
}

// Server encapsulates listener + active connection tracking.
type Server struct {
	cfg                Config
	l                  net.Listener
	log                *slog.Logger
	reg                *Registry
	destinationManager *relay.DestinationManager
	hookManager        *hooks.HookManager

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
	}
}

// Start begins listening and launches the accept loop. It's safe to call
// only once; repeated calls return an error.
func (s *Server) Start() error {
	if s == nil {
		return errors.New("nil server")
	}
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

	s.log.Info("RTMP server listening", "addr", ln.Addr().String())
	s.acceptingWg.Add(1)
	go s.acceptLoop()
	return nil
}

// acceptLoop runs until listener close. Each successful accept performs the
// RTMP handshake via conn.Accept which internally sends the control burst.
func (s *Server) acceptLoop() {
	defer s.acceptingWg.Done()
	for {
		s.mu.RLock()
		l := s.l
		s.mu.RUnlock()
		if l == nil {
			return
		}
		raw, err := l.Accept()
		if err != nil {
			// If we are shutting down, Accept will return an error (use closing flag to suppress noise).
			s.mu.RLock()
			closing := s.closing
			s.mu.RUnlock()
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			if closing || errors.Is(err, net.ErrClosed) {
				return
			}
			s.log.Warn("accept error", "error", err)
			return
		}
		// Handshake + control burst integration lives in conn.Accept.
		// We temporarily wrap the raw listener to reuse existing function.
		// Trick: create a one-off fake listener returning this raw conn.
		single := &singleConnListener{conn: raw}
		c, err := iconn.Accept(single)
		if err != nil { // handshake failure already logged; continue accepting.
			continue
		}
		s.mu.Lock()
		s.conns[c.ID()] = c
		s.mu.Unlock()
		metrics.ConnectionsActive.Add(1)
		metrics.ConnectionsTotal.Add(1)
		s.log.Info("connection registered", "conn_id", c.ID(), "remote", raw.RemoteAddr().String())

		// Trigger connection accept hook event
		s.triggerHookEvent(hooks.EventConnectionAccept, c.ID(), "", map[string]interface{}{
			"remote_addr": raw.RemoteAddr().String(),
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
	s.mu.Unlock()
	_ = l.Close()

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

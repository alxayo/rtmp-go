package server

// RTMP Server Listener (Task T051)
// --------------------------------
// Provides a minimal TCP listener + connection manager integrating the
// existing handshake + control burst + connection lifecycle implemented in
// the conn package. Scope intentionally small – advanced routing/dispatcher
// wiring will be layered in later tasks. This satisfies the requirements:
//   * Listen on configured address (default :1935)
//   * Accept loop spawning a goroutine per connection (via conn.Accept)
//   * Track active connections in a concurrent-safe map
//   * Graceful shutdown: stop accepting, close all connections, wait
//   * Configuration options (chunk/window sizes, recording placeholders)
//   * Exposed methods for tests: Start, Stop, Addr, ConnectionCount

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
	"github.com/alxayo/go-rtmp/internal/rtmp/relay"
	"github.com/alxayo/go-rtmp/internal/rtmp/server/hooks"
)

// Config holds server configuration knobs. Future tasks may extend with
// validation / functional options. For now we keep a plain struct.
type Config struct {
	ListenAddr        string
	ChunkSize         uint32 // initial outbound chunk size (after control burst peer will update)
	WindowAckSize     uint32 // advertised window acknowledgement size
	RecordAll         bool
	RecordDir         string
	LogLevel          string
	RelayDestinations []string // NEW: List of destination URLs for relay
	// Hook configuration (all optional for backward compatibility)
	HookScripts     []string // event_type=script_path pairs
	HookWebhooks    []string // event_type=webhook_url pairs
	HookStdioFormat string   // "json", "env", or "" (disabled)
	HookTimeout     string   // timeout duration
	HookConcurrency int      // max concurrent hook executions
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
	destinationManager *relay.DestinationManager // NEW: Multi-destination relay manager
	hookManager        *hooks.HookManager        // NEW: Event hook manager

	mu          sync.RWMutex
	conns       map[string]*iconn.Connection
	acceptingWg sync.WaitGroup // waits for accept loop exit
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

	// Initialize hook manager (always safe, even with empty config)
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
		s.log.Info("connection registered", "conn_id", c.ID(), "remote", raw.RemoteAddr().String())

		// Trigger connection accept hook event
		clientAddr := raw.RemoteAddr().(*net.TCPAddr)
		serverAddr := s.l.Addr().(*net.TCPAddr)
		s.triggerHookEvent(hooks.EventConnectionAccept, c.ID(), "", map[string]interface{}{
			"client_ip":   clientAddr.IP.String(),
			"client_port": clientAddr.Port,
			"server_ip":   serverAddr.IP.String(),
			"server_port": serverAddr.Port,
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
	s.mu.RLock()
	for id, c := range s.conns {
		// Trigger connection close event before closing
		s.triggerHookEvent(hooks.EventConnectionClose, c.ID(), "", map[string]interface{}{
			"reason": "server_shutdown",
		})
		_ = c.Close()
		delete(s.conns, id)
	}
	s.mu.RUnlock()

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

// singleConnListener is a tiny adapter implementing net.Listener for a single
// pre-accepted net.Conn. It returns the conn once then permanently errors.
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

// initializeHookManager creates and configures the hook manager based on server config
func initializeHookManager(cfg Config, logger *slog.Logger) *hooks.HookManager {
	// Create hook config from server config
	hookConfig := hooks.HookConfig{
		Timeout:     cfg.HookTimeout,
		Concurrency: cfg.HookConcurrency,
		StdioFormat: cfg.HookStdioFormat,
	}

	// Apply defaults if not specified
	if hookConfig.Timeout == "" {
		hookConfig.Timeout = "30s"
	}
	if hookConfig.Concurrency == 0 {
		hookConfig.Concurrency = 10
	}

	// Create hook manager
	hookManager := hooks.NewHookManager(hookConfig, logger)

	// Register shell hooks from configuration
	if err := registerShellHooks(hookManager, cfg.HookScripts, logger); err != nil {
		logger.Error("Failed to register shell hooks", "error", err)
	}

	// Register webhook hooks from configuration
	if err := registerWebhookHooks(hookManager, cfg.HookWebhooks, logger); err != nil {
		logger.Error("Failed to register webhook hooks", "error", err)
	}

	return hookManager
}

// triggerHookEvent is a helper method to trigger hook events safely
func (s *Server) triggerHookEvent(eventType hooks.EventType, connID, streamKey string, data map[string]interface{}) {
	if s == nil || s.hookManager == nil {
		return // Hooks disabled or server not initialized
	}

	event := hooks.NewEvent(eventType).
		WithConnID(connID).
		WithStreamKey(streamKey)

	// Add data fields if provided
	for key, value := range data {
		event.WithData(key, value)
	}

	s.hookManager.TriggerEvent(context.Background(), *event)
}

// registerShellHooks parses and registers shell hooks from configuration
func registerShellHooks(hookManager *hooks.HookManager, scripts []string, logger *slog.Logger) error {
	for i, script := range scripts {
		parts := strings.SplitN(script, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid shell hook format: %s", script)
		}

		eventType := hooks.EventType(parts[0])
		scriptPath := parts[1]

		// Create shell hook with default timeout (will be overridden by manager's config)
		shellHook := hooks.NewShellHook(
			fmt.Sprintf("shell_%d", i),
			scriptPath,
			30*time.Second, // Default timeout, actual timeout controlled by manager
		)

		if err := hookManager.RegisterHook(eventType, shellHook); err != nil {
			return fmt.Errorf("failed to register shell hook %s: %w", script, err)
		}

		logger.Info("Registered shell hook", "event_type", eventType, "script_path", scriptPath)
	}

	return nil
}

// registerWebhookHooks parses and registers webhook hooks from configuration
func registerWebhookHooks(hookManager *hooks.HookManager, webhooks []string, logger *slog.Logger) error {
	for i, webhook := range webhooks {
		parts := strings.SplitN(webhook, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid webhook hook format: %s", webhook)
		}

		eventType := hooks.EventType(parts[0])
		webhookURL := parts[1]

		// Create webhook hook with default timeout
		webhookHook := hooks.NewWebhookHook(
			fmt.Sprintf("webhook_%d", i),
			webhookURL,
			30*time.Second, // Default timeout
		)

		if err := hookManager.RegisterHook(eventType, webhookHook); err != nil {
			return fmt.Errorf("failed to register webhook hook %s: %w", webhook, err)
		}

		logger.Info("Registered webhook hook", "event_type", eventType, "webhook_url", webhookURL)
	}

	return nil
}

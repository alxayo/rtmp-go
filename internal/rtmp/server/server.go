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
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"github.com/alxayo/go-rtmp/internal/logger"
	iconn "github.com/alxayo/go-rtmp/internal/rtmp/conn"
)

// Config holds server configuration knobs. Future tasks may extend with
// validation / functional options. For now we keep a plain struct.
type Config struct {
	ListenAddr    string
	ChunkSize     uint32 // initial outbound chunk size (after control burst peer will update)
	WindowAckSize uint32 // advertised window acknowledgement size
	RecordAll     bool
	RecordDir     string
	LogLevel      string
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
	cfg Config
	l   net.Listener
	log *slog.Logger
	reg *Registry

	mu          sync.RWMutex
	conns       map[string]*iconn.Connection
	acceptingWg sync.WaitGroup // waits for accept loop exit
	closing     bool
}

// New creates a new, unstarted Server instance.
func New(cfg Config) *Server {
	cfg.applyDefaults()
	return &Server{cfg: cfg, reg: NewRegistry(), conns: make(map[string]*iconn.Connection), log: logger.Logger().With("component", "rtmp_server")}
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
		// Wire command handling so real clients (OBS/ffmpeg) can complete
		// connect/createStream/publish. (Incremental integration step.)
		attachCommandHandling(c, s.reg, s.log)
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

	// Close all connections.
	s.mu.RLock()
	for id, c := range s.conns {
		_ = c.Close()
		delete(s.conns, id)
	}
	s.mu.RUnlock()
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

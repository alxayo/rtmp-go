package main

import (
	"context"
	_ "expvar" // Register /debug/vars handler on DefaultServeMux
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/alxayo/go-rtmp/internal/logger"
	_ "github.com/alxayo/go-rtmp/internal/rtmp/metrics" // Register expvar RTMP counters
	srv "github.com/alxayo/go-rtmp/internal/rtmp/server"
	"github.com/alxayo/go-rtmp/internal/rtmp/server/auth"
	"github.com/alxayo/go-rtmp/internal/srt"
	srtauth "github.com/alxayo/go-rtmp/internal/srt/auth" // Per-stream SRT passphrase resolution
)

func main() {
	cfg, err := parseFlags(os.Args[1:])
	if err != nil {
		// flag package already printed usage/error
		os.Exit(2)
	}
	if cfg.showVersion {
		fmt.Println(version)
		return
	}

	// Initialize global logger and set level based on flag
	logger.Init()
	if err := logger.SetLevel(cfg.logLevel); err != nil {
		fmt.Printf("Warning: invalid log level %q, using default\n", cfg.logLevel)
	}
	log := logger.Logger().With("component", "cli")
	log.Debug("logger initialized", "level", cfg.logLevel)

	// Build authentication validator from CLI flags
	authValidator, err := buildAuthValidator(cfg, log)
	if err != nil {
		log.Error("failed to initialize authentication", "error", err)
		os.Exit(2)
	}

	// Build SRT passphrase resolver from CLI flags.
	// srtResolver is the function the server calls during each SRT handshake.
	// srtFileResolver is non-nil only in file mode — we keep a reference to it
	// so the SIGHUP handler below can call Reload() to pick up file changes.
	srtResolver, srtFileResolver, err := buildSRTResolver(cfg)
	if err != nil {
		log.Error("failed to initialize SRT passphrase resolver", "error", err)
		os.Exit(2)
	}

	// Parse the segment duration string into a time.Duration.
	// The string was already validated in parseFlags(), so we can safely ignore the error.
	var segmentDur time.Duration
	if cfg.segmentDuration != "" {
		segmentDur, _ = time.ParseDuration(cfg.segmentDuration) // already validated in parseFlags
	}

	server := srv.New(srv.Config{
		ListenAddr:            cfg.listenAddr,
		ChunkSize:             uint32(cfg.chunkSize),
		WindowAckSize:         2_500_000,
		RecordAll:             cfg.recordAll,
		RecordDir:             cfg.recordDir,
		SegmentDuration:       segmentDur,
		SegmentPattern:        cfg.segmentPattern,
		LogLevel:              cfg.logLevel,
		RelayDestinations:     cfg.relayDestinations,
		HookScripts:           cfg.hookScripts,
		HookWebhooks:          cfg.hookWebhooks,
		HookStdioFormat:       cfg.hookStdioFormat,
		HookTimeout:           cfg.hookTimeout,
		HookConcurrency:       cfg.hookConcurrency,
		AuthValidator:         authValidator,
		TLSListenAddr:         cfg.tlsListenAddr,
		TLSCertFile:           cfg.tlsCertFile,
		TLSKeyFile:            cfg.tlsKeyFile,
		SRTListenAddr:         cfg.srtListenAddr,
		SRTLatency:            cfg.srtLatency,
		SRTPassphrase:         cfg.srtPassphrase,
		SRTPbKeyLen:            cfg.srtPbKeyLen,
		SRTPassphraseFile:     cfg.srtPassphraseFile,
		SRTPassphraseResolver: srtResolver,
	})

	if err := server.Start(); err != nil {
		log.Error("failed to start server", "error", err)
		os.Exit(1)
	}

	log.Info("server started", "addr", server.Addr().String(), "version", version, "auth_mode", cfg.authMode, "log_level", cfg.logLevel)
	if server.TLSAddr() != nil {
		log.Info("RTMPS enabled", "tls_addr", server.TLSAddr().String())
	}
	if server.SRTAddr() != nil {
		log.Info("SRT ingest enabled", "srt_addr", server.SRTAddr().String())
	}

	// Start HTTP metrics server if configured
	if cfg.metricsAddr != "" {
		go func() {
			log.Info("metrics HTTP server listening", "addr", cfg.metricsAddr)
			if err := http.ListenAndServe(cfg.metricsAddr, nil); err != nil && err != http.ErrServerClosed {
				log.Error("metrics HTTP server error", "error", err)
			}
		}()
	}

	// Register a SIGHUP handler for live configuration reload without restart.
	// A single handler covers both features because they share the same reload
	// pattern: re-read a JSON file from disk and atomically swap the in-memory
	// map. Each reload is independent — if the auth file reload fails, the SRT
	// passphrase file reload still runs (and vice versa).
	needSighup := cfg.authMode == "file" || srtFileResolver != nil
	if needSighup {
		sighup := make(chan os.Signal, 1)
		signal.Notify(sighup, syscall.SIGHUP)
		go func() {
			for range sighup {
				// Reload RTMP auth tokens (if using file-based auth)
				if cfg.authMode == "file" {
					if fv, ok := authValidator.(*auth.FileValidator); ok {
						if err := fv.Reload(); err != nil {
							log.Error("auth file reload failed", "error", err)
						} else {
							log.Info("auth file reloaded")
						}
					}
				}
				// Reload SRT per-stream passphrases (if using file-based resolver).
				// The resolver's Reload() re-reads the JSON file and validates all
				// passphrases; on failure, the previous valid map is preserved.
				if srtFileResolver != nil {
					if err := srtFileResolver.Reload(); err != nil {
						log.Error("SRT passphrase file reload failed", "error", err)
					} else {
						log.Info("SRT passphrase file reloaded")
					}
				}
			}
		}()
	}

	// Register SIGUSR1 handler for E-RTMP v2 reconnect-all.
	// Sending SIGUSR1 to the process asks ALL connected clients to gracefully
	// disconnect and reconnect. If -reconnect-url is set, clients are redirected
	// to that URL; otherwise, they reconnect to the same server.
	// Usage: kill -USR1 <pid>
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGUSR1)
		for range sigCh {
			desc := "Server maintenance — please reconnect"
			if cfg.reconnectURL != "" {
				desc = fmt.Sprintf("Server maintenance — please reconnect to %s", cfg.reconnectURL)
			}
			count := server.RequestReconnectAll(cfg.reconnectURL, desc)
			log.Info("SIGUSR1: reconnect request sent", "count", count, "redirect", cfg.reconnectURL)
		}
	}()

	// Set up signal handling for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()
	log.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Perform shutdown in a separate goroutine in case it blocks; we just wait or force exit on timeout.
	done := make(chan struct{})
	go func() {
		if err := server.Stop(); err != nil {
			log.Error("server stop error", "error", err)
		}
		close(done)
	}()

	select {
	case <-done:
		log.Info("server stopped cleanly")
	case <-shutdownCtx.Done():
		log.Error("forced exit after timeout")
		os.Exit(1)
	}
}

// buildAuthValidator creates the appropriate auth.Validator based on CLI flags.
func buildAuthValidator(cfg *cliConfig, log interface{ Info(string, ...any) }) (auth.Validator, error) {
	switch cfg.authMode {
	case "token":
		tokens := make(map[string]string, len(cfg.authTokens))
		for _, t := range cfg.authTokens {
			parts := strings.SplitN(t, "=", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid -auth-token format %q (expected streamKey=token)", t)
			}
			tokens[parts[0]] = parts[1]
		}
		return &auth.TokenValidator{Tokens: tokens}, nil
	case "file":
		return auth.NewFileValidator(cfg.authFile)
	case "callback":
		timeout, _ := time.ParseDuration(cfg.authCallbackTimeout)
		if timeout == 0 {
			timeout = 5 * time.Second
		}
		return auth.NewCallbackValidator(cfg.authCallbackURL, timeout), nil
	default: // "none"
		return &auth.AllowAllValidator{}, nil
	}
}

// buildSRTResolver creates the SRT passphrase resolver from CLI flags.
//
// The resolver is the bridge between the CLI configuration layer and the SRT
// handshake engine. It translates the user's chosen encryption mode into a
// function that the handshake calls during the Conclusion phase to look up
// the passphrase for each incoming connection.
//
// Three modes are supported (mutually exclusive, enforced in parseFlags):
//
//  1. File mode (-srt-passphrase-file): loads a JSON map of stream key →
//     passphrase. Returns both a resolver function and a *FileResolver handle
//     so the SIGHUP handler can call Reload() for live updates.
//
//  2. Static mode (-srt-passphrase): a single passphrase for all streams.
//     No resolver is needed because the handshake layer uses the static
//     passphrase from srt.Config.Passphrase directly.
//
//  3. No encryption (neither flag): returns nil for both values.
//
// Returns:
//   - resolver: the function to pass to srv.Config.SRTPassphraseResolver (nil if static or none)
//   - fileResolver: the FileResolver handle for SIGHUP reload (nil unless file mode)
//   - err: any configuration error (e.g., bad JSON, invalid passphrase)
func buildSRTResolver(cfg *cliConfig) (func(string) (string, error), *srtauth.FileResolver, error) {
	switch {
	case cfg.srtPassphraseFile != "":
		// Per-stream encryption from JSON file.
		// NewFileResolver reads the file, parses JSON, and validates every
		// passphrase against SRT spec constraints (10–79 chars).
		fr, err := srtauth.NewFileResolver(cfg.srtPassphraseFile)
		if err != nil {
			return nil, nil, fmt.Errorf("load SRT passphrase file: %w", err)
		}
		// Wrap the FileResolver into a closure that normalizes the raw Stream ID
		// before looking up the passphrase. This is necessary because the handshake
		// layer passes the raw Stream ID string (e.g., "#!::r=live/test,m=publish"),
		// but the JSON file uses normalized stream keys (e.g., "live/test").
		// ParseStreamID handles all supported formats (structured and simple).
		resolverFunc := func(rawStreamID string) (string, error) {
			info := srt.ParseStreamID(rawStreamID)
			streamKey := info.StreamKey()
			return fr.ResolvePassphrase(streamKey)
		}
		return resolverFunc, fr, nil

	case cfg.srtPassphrase != "":
		// Single passphrase for all streams (backward compatible).
		// No resolver needed — the static passphrase is passed via
		// srv.Config.SRTPassphrase and used by the handshake listener
		// directly without any per-stream lookup.
		return nil, nil, nil

	default:
		// No SRT encryption configured.
		return nil, nil, nil
	}
}

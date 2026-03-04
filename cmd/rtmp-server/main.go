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

	// Build authentication validator from CLI flags
	authValidator, err := buildAuthValidator(cfg, log)
	if err != nil {
		log.Error("failed to initialize authentication", "error", err)
		os.Exit(2)
	}

	server := srv.New(srv.Config{
		ListenAddr:        cfg.listenAddr,
		ChunkSize:         uint32(cfg.chunkSize),
		WindowAckSize:     2_500_000,
		RecordAll:         cfg.recordAll,
		RecordDir:         cfg.recordDir,
		LogLevel:          cfg.logLevel,
		RelayDestinations: cfg.relayDestinations,
		HookScripts:       cfg.hookScripts,
		HookWebhooks:      cfg.hookWebhooks,
		HookStdioFormat:   cfg.hookStdioFormat,
		HookTimeout:       cfg.hookTimeout,
		HookConcurrency:   cfg.hookConcurrency,
		AuthValidator:     authValidator,
	})

	if err := server.Start(); err != nil {
		log.Error("failed to start server", "error", err)
		os.Exit(1)
	}

	log.Info("server started", "addr", server.Addr().String(), "version", version, "auth_mode", cfg.authMode)

	// Start HTTP metrics server if configured
	if cfg.metricsAddr != "" {
		go func() {
			log.Info("metrics HTTP server listening", "addr", cfg.metricsAddr)
			if err := http.ListenAndServe(cfg.metricsAddr, nil); err != nil && err != http.ErrServerClosed {
				log.Error("metrics HTTP server error", "error", err)
			}
		}()
	}

	// If using file-based auth, listen for SIGHUP to reload the token file
	if cfg.authMode == "file" {
		if fv, ok := authValidator.(*auth.FileValidator); ok {
			sighup := make(chan os.Signal, 1)
			signal.Notify(sighup, syscall.SIGHUP)
			go func() {
				for range sighup {
					if err := fv.Reload(); err != nil {
						log.Error("auth file reload failed", "error", err)
					} else {
						log.Info("auth file reloaded")
					}
				}
			}()
		}
	}

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

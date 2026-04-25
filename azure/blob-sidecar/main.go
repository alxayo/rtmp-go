package main

// Azure Blob Storage Sidecar for rtmp-go
// ========================================
// Watches rtmp-go's recording directory for completed segment files and uploads
// them to Azure Blob Storage. Supports multi-tenant routing: each stream key
// can be directed to a different storage account/container.
//
// Three operating modes:
//   - watch:   filesystem monitoring via fsnotify (no rtmp-go changes needed)
//   - events:  reads hook events from stdin (piped from rtmp-go stderr)
//   - webhook: HTTP server receiving hook events from rtmp-go's webhook hooks
//
// Usage:
//   blob-sidecar -mode watch -watch-dir ./recordings -config tenants.json
//   blob-sidecar -mode webhook -listen-addr :8080 -config tenants.json

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// Flags
	mode := flag.String("mode", "watch", "Operating mode: watch (fsnotify), events (stdin hook events), or webhook (HTTP listener)")
	listenAddr := flag.String("listen-addr", ":8080", "HTTP listen address for webhook mode")
	watchDir := flag.String("watch-dir", "recordings", "Directory to watch for segment files (watch mode only)")
	configPath := flag.String("config", "tenants.json", "Path to tenant configuration file")
	workers := flag.Int("workers", 4, "Number of concurrent upload workers")
	cleanup := flag.Bool("cleanup", false, "Delete local files after successful upload")
	stabilizeDur := flag.Duration("stabilize-duration", 2*time.Second, "Wait time after last write before uploading (watch mode only)")
	logLevel := flag.String("log-level", "info", "Log level: debug, info, warn, error")

	// Ingest HTTP server flags
	ingestAddr := flag.String("ingest-addr", ":8081", "HTTP listen address for ingest endpoint")
	ingestStorage := flag.String("ingest-storage", "blob", "Storage backend for ingest: blob or local")
	ingestLocalDir := flag.String("ingest-local-dir", "", "Root directory for local storage backend (required if -ingest-storage=local)")
	ingestToken := flag.String("ingest-token", "", "Optional bearer token for ingest authentication (empty = auth disabled)")
	ingestMaxBody := flag.Int64("ingest-max-body", 50*1024*1024, "Maximum request body size in bytes")

	flag.Parse()

	// Logger setup
	var level slog.Level
	switch *logLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	// Load tenant configuration
	cfg, err := LoadConfig(*configPath)
	if err != nil {
		logger.Error("failed to load config", "path", *configPath, "error", err)
		os.Exit(1)
	}
	logger.Info("config loaded", "tenants", len(cfg.Get().Tenants), "path", *configPath)

	// Build resolvers
	fileResolver := NewFileResolver(cfg)
	var apiResolver *APIResolver
	if apiCfg := cfg.Get().APIFallback; apiCfg != nil && apiCfg.Enabled {
		apiResolver = NewAPIResolver(apiCfg, logger)
		logger.Info("API resolver enabled", "url", apiCfg.URL)
	}

	router := NewRouter(fileResolver, apiResolver, cfg, logger)

	// Build uploader
	uploader := NewUploader(*workers, *cleanup, logger)

	// Initialize ingest HTTP server and storage backend
	ingestBackend, err := NewStorageBackend(*ingestStorage, uploader, router, *ingestLocalDir, logger)
	if err != nil {
		logger.Error("failed to initialize ingest storage backend", "backend", *ingestStorage, "error", err)
		os.Exit(1)
	}
	logger.Info("ingest storage backend initialized", "backend", *ingestStorage)

	ingestHandler := NewIngestHandler(ingestBackend, *ingestMaxBody, *ingestToken, logger)
	ingestMux := http.NewServeMux()
	ingestMux.Handle("/ingest/", ingestHandler)
	ingestMux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	})

	ingestServer := &http.Server{
		Addr:              *ingestAddr,
		Handler:           ingestMux,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       30 * time.Second,
		// No ReadTimeout: FFmpeg uses chunked transfer encoding with unknown body size.
		// ReadTimeout covers the entire request including body, which would kill large
		// segment uploads. ReadHeaderTimeout protects against slowloris attacks instead.
	}
	// Disable keep-alives to force one connection per PUT request.
	// Prevents stale connection issues with Envoy proxy and FFmpeg's HTTP client.
	ingestServer.SetKeepAlivesEnabled(false)

	// Start ingest HTTP server in goroutine
	go func() {
		logger.Info("starting ingest HTTP server", "addr", *ingestAddr)
		if err := ingestServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("ingest server error", "error", err)
		}
	}()

	// Signal handling
	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)

	go func() {
		for sig := range sigCh {
			switch sig {
			case syscall.SIGHUP:
				logger.Info("SIGHUP received, reloading config")
				if err := cfg.Reload(); err != nil {
					logger.Error("config reload failed", "error", err)
				} else {
					logger.Info("config reloaded", "tenants", len(cfg.Get().Tenants))
				}
			case syscall.SIGTERM, syscall.SIGINT:
				logger.Info("shutdown signal received", "signal", sig)
				cancel()
				return
			}
		}
	}()

	// Start uploader
	uploader.Start(ctx)

	switch *mode {
	case "events":
		logger.Info("starting in events mode (reading hook events from stdin)")
		listener := NewStdinListener(os.Stdin, logger, func(event HookEvent) {
			path, _ := event.Data["path"].(string)
			if path == "" {
				logger.Warn("segment_complete event missing path", "stream_key", event.StreamKey)
				return
			}
			tenant, err := router.ResolveByStreamKey(event.StreamKey)
			if err != nil {
				logger.Error("tenant resolution failed", "stream_key", event.StreamKey, "error", err)
				return
			}
			uploader.Submit(UploadJob{
				FilePath:  path,
				Tenant:    tenant,
				StreamKey: event.StreamKey,
			})
		})
		if err := listener.Run(ctx); err != nil && err != context.Canceled {
			logger.Error("listener error", "error", err)
		}

	case "watch":
		logger.Info("starting in watch mode", "dir", *watchDir, "stabilize", *stabilizeDur)
		watcher, err := NewWatcher(*watchDir, *stabilizeDur, logger, func(path string) {
			tenant, err := router.Resolve(path)
			if err != nil {
				logger.Error("tenant resolution failed", "path", path, "error", err)
				return
			}
			uploader.Submit(UploadJob{
				FilePath:  path,
				Tenant:    tenant,
				StreamKey: router.ExtractStreamKey(path),
			})
		})
		if err != nil {
			logger.Error("failed to create watcher", "dir", *watchDir, "error", err)
			os.Exit(1)
		}
		if err := watcher.Start(ctx); err != nil {
			logger.Error("watcher failed", "error", err)
			os.Exit(1)
		}
		// Wait for shutdown
		<-ctx.Done()
		watcher.Stop()

	case "webhook":
		logger.Info("starting in webhook mode (HTTP listener)", "addr", *listenAddr)
		listener := NewWebhookListener(*listenAddr, logger, func(event HookEvent) {
			path, _ := event.Data["path"].(string)
			if path == "" {
				logger.Warn("segment_complete event missing path", "stream_key", event.StreamKey)
				return
			}
			tenant, err := router.ResolveByStreamKey(event.StreamKey)
			if err != nil {
				logger.Error("tenant resolution failed", "stream_key", event.StreamKey, "error", err)
				return
			}
			uploader.Submit(UploadJob{
				FilePath:  path,
				Tenant:    tenant,
				StreamKey: event.StreamKey,
			})
		})
		if err := listener.Run(ctx); err != nil && err != context.Canceled {
			logger.Error("webhook listener error", "error", err)
		}

	default:
		logger.Error("unknown mode", "mode", *mode)
		fmt.Fprintf(os.Stderr, "Usage: -mode must be 'watch', 'events', or 'webhook'\n")
		os.Exit(1)
	}

	logger.Info("shutting down, waiting for in-progress uploads...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Shutdown ingest server
	if err := ingestServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("ingest server shutdown error", "error", err)
	}

	uploader.Shutdown(shutdownCtx)
	logger.Info("shutdown complete")
	fmt.Fprintln(os.Stderr, "blob-sidecar stopped")
}

package main

// Azure Blob Storage Sidecar for rtmp-go
// ========================================
// Watches rtmp-go's recording directory for completed segment files and uploads
// them to Azure Blob Storage. Supports multi-tenant routing: each stream key
// can be directed to a different storage account/container.
//
// Zero modifications to rtmp-go required — uses filesystem watching (fsnotify).
//
// Usage:
//   blob-sidecar -watch-dir ./recordings -config tenants.json -workers 4

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// Flags
	mode := flag.String("mode", "watch", "Operating mode: watch (fsnotify) or events (stdin hook events)")
	watchDir := flag.String("watch-dir", "recordings", "Directory to watch for segment files (watch mode only)")
	configPath := flag.String("config", "tenants.json", "Path to tenant configuration file")
	workers := flag.Int("workers", 4, "Number of concurrent upload workers")
	cleanup := flag.Bool("cleanup", false, "Delete local files after successful upload")
	stabilizeDur := flag.Duration("stabilize-duration", 2*time.Second, "Wait time after last write before uploading (watch mode only)")
	logLevel := flag.String("log-level", "info", "Log level: debug, info, warn, error")

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

	default:
		logger.Error("unknown mode", "mode", *mode)
		fmt.Fprintf(os.Stderr, "Usage: -mode must be 'watch' or 'events'\n")
		os.Exit(1)
	}

	logger.Info("shutting down, waiting for in-progress uploads...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	uploader.Shutdown(shutdownCtx)
	logger.Info("shutdown complete")
	fmt.Fprintln(os.Stderr, "blob-sidecar stopped")
}

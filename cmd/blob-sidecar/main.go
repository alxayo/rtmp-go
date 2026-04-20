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
	watchDir := flag.String("watch-dir", "recordings", "Directory to watch for segment files")
	configPath := flag.String("config", "tenants.json", "Path to tenant configuration file")
	workers := flag.Int("workers", 4, "Number of concurrent upload workers")
	cleanup := flag.Bool("cleanup", false, "Delete local files after successful upload")
	stabilizeDur := flag.Duration("stabilize-duration", 2*time.Second, "Wait time after last write before uploading")
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

	// Build watcher
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

	// Start components
	uploader.Start(ctx)
	if err := watcher.Start(ctx); err != nil {
		logger.Error("watcher failed", "error", err)
		os.Exit(1)
	}

	// Wait for shutdown
	<-ctx.Done()
	logger.Info("shutting down, waiting for in-progress uploads...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	uploader.Shutdown(shutdownCtx)
	watcher.Stop()
	logger.Info("shutdown complete")
	fmt.Fprintln(os.Stderr, "blob-sidecar stopped")
}

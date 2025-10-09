package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alxayo/go-rtmp/internal/logger"
	srv "github.com/alxayo/go-rtmp/internal/rtmp/server"
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

	// Initialize global logger level based on flag (simple approach: environment override or logger package extension later)
	base := logger.Logger()
	// For now we rely on slog default; future enhancement: dynamic level filter. We'll attach level field.
	log := base.With("log_level", cfg.logLevel, "component", "cli")

	server := srv.New(srv.Config{
		ListenAddr:    cfg.listenAddr,
		ChunkSize:     uint32(cfg.chunkSize),
		WindowAckSize: 2_500_000, // matches control burst constant
		RecordAll:     cfg.recordAll,
		RecordDir:     cfg.recordDir,
		LogLevel:      cfg.logLevel,
	})

	if err := server.Start(); err != nil {
		log.Error("failed to start server", "error", err)
		os.Exit(1)
	}

	log.Info("server started", "addr", server.Addr().String(), "version", version)

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
	}
}

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

	// Initialize global logger and set level based on flag
	logger.Init()
	if err := logger.SetLevel(cfg.logLevel); err != nil {
		fmt.Printf("Warning: invalid log level %q, using default\n", cfg.logLevel)
	}
	log := logger.Logger().With("component", "cli")

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

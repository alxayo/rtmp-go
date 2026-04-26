package main

// HLS Transcoder Service for rtmp-go
// ====================================
// Webhook-driven service that converts live RTMP streams into multi-bitrate
// adaptive HLS output using FFmpeg. Receives publish_start/publish_stop
// events from rtmp-server's webhook hook system and manages FFmpeg process
// lifecycles accordingly.
//
// Two transcoding modes:
//   - abr:  multi-bitrate adaptive streaming (1080p/720p/480p) via single
//           FFmpeg process with -var_stream_map (requires 4 vCPU / 8 GiB)
//   - copy: remux-only passthrough (-c copy) for cost-sensitive deployments
//           (requires 0.5 vCPU / 1 GiB)
//
// Usage:
//   hls-transcoder -listen-addr :8090 -hls-dir /hls-output -rtmp-host rtmp-server -mode abr
//   hls-transcoder -listen-addr :8090 -hls-dir /hls-output -rtmp-host rtmp-server -mode copy

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
	// CLI flags
	listenAddr := flag.String("listen-addr", ":8090", "HTTP listen address for webhook events")
	hlsDir := flag.String("hls-dir", "/hls-output", "Root directory for HLS output files")
	rtmpHost := flag.String("rtmp-host", "localhost", "RTMP server hostname (internal FQDN in Azure)")
	rtmpPort := flag.Int("rtmp-port", 1935, "RTMP server port")
	rtmpToken := flag.String("rtmp-token", "", "Auth token for RTMP subscribe access")
	mode := flag.String("mode", "abr", "Transcoding mode: abr (multi-bitrate) or copy (remux)")
	blobWebhookURL := flag.String("blob-webhook-url", "", "Webhook URL for blob-sidecar segment upload (empty = no blob upload)")
	logLevel := flag.String("log-level", "info", "Log level: debug, info, warn, error")

	// HTTP output mode flags (Phase 2)
	// -output-mode: "file" (default, writes to local directory) or "http" (streams to blob-sidecar HTTP endpoint)
	outputMode := flag.String("output-mode", "file", "Output mode: file (local filesystem) or http (blob-sidecar HTTP ingest)")
	// -ingest-url: Base URL for blob-sidecar HTTP ingest endpoint (required when output-mode=http)
	// Example: http://blob-sidecar:8081/ingest/
	ingestURL := flag.String("ingest-url", "", "HTTP ingest base URL for blob-sidecar (required if output-mode=http)")
	// -ingest-token: Bearer token for authentication to blob-sidecar HTTP ingest (optional, for secure deployments)
	ingestToken := flag.String("ingest-token", "", "Bearer token for HTTP ingest endpoint authentication (optional)")

	// Platform API flags — for fetching per-event stream configuration (Phase 4)
	// -platform-url: Base URL of the StreamGate Platform API (required)
	platformURL := flag.String("platform-url", "", "Platform API base URL for stream config fetch (required)")
	// -platform-api-key: Internal API key for authentication to Platform API (required)
	platformAPIKey := flag.String("platform-api-key", "", "Internal API key for Platform API authentication (required)")
	// -codec: This transcoder's codec identity — used for self-filtering (only start if event has this codec enabled)
	codec := flag.String("codec", "h264", "Codec this transcoder handles (h264, av1, vp9)")
	// -config-cache-ttl: How long cached configs stay valid before refresh (applies to both event and system caches)
	configCacheTTL := flag.Duration("config-cache-ttl", 10*time.Minute, "Config cache TTL (e.g., 10m, 5m)")
	// -config-fetch-timeout: HTTP timeout for each config fetch request
	configFetchTimeout := flag.Duration("config-fetch-timeout", 2*time.Second, "Config fetch HTTP timeout (e.g., 2s)")

	flag.Parse()

	// Logger setup — JSON output for structured log aggregation in Azure
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

	// Validate mode
	if *mode != "abr" && *mode != "copy" {
		logger.Error("invalid mode", "mode", *mode)
		fmt.Fprintf(os.Stderr, "Usage: -mode must be 'abr' or 'copy'\n")
		os.Exit(1)
	}

	// Validate output mode
	if *outputMode != "file" && *outputMode != "http" {
		logger.Error("invalid output-mode", "output_mode", *outputMode)
		fmt.Fprintf(os.Stderr, "Usage: -output-mode must be 'file' or 'http'\n")
		os.Exit(1)
	}

	// Validate HTTP ingest requirements
	if *outputMode == "http" && *ingestURL == "" {
		logger.Error("http output mode requires -ingest-url", "output_mode", *outputMode)
		fmt.Fprintf(os.Stderr, "Usage: -output-mode http requires -ingest-url\n")
		os.Exit(1)
	}

	// Validate Platform API requirements — transcoder cannot operate without config fetch.
	// The four-tier fallback chain (§4.1) assumes the fetcher is configured.
	if *platformURL == "" {
		logger.Error("missing required flag: -platform-url")
		fmt.Fprintf(os.Stderr, "Usage: -platform-url is required for config fetch\n")
		os.Exit(1)
	}
	if *platformAPIKey == "" {
		logger.Error("missing required flag: -platform-api-key")
		fmt.Fprintf(os.Stderr, "Usage: -platform-api-key is required for config fetch\n")
		os.Exit(1)
	}

	// Build transcoder configuration
	cfg := TranscoderConfig{
		HLSDir:         *hlsDir,
		RTMPHost:       *rtmpHost,
		RTMPPort:       *rtmpPort,
		RTMPToken:      *rtmpToken,
		Mode:           *mode,
		BlobWebhookURL: *blobWebhookURL,
		// HTTP output mode (Phase 2)
		OutputMode: *outputMode,
		IngestURL:  *ingestURL,
		IngestToken: *ingestToken,
	}

	// Create the config fetcher — fetches per-event and system-wide config from Platform API.
	// Starts a background goroutine to periodically refresh system defaults.
	configFetcher := NewConfigFetcher(ConfigFetcherConfig{
		PlatformURL:  *platformURL,
		APIKey:       *platformAPIKey,
		CacheTTL:     *configCacheTTL,
		FetchTimeout: *configFetchTimeout,
	}, logger)

	transcoder := NewTranscoder(cfg, *codec, configFetcher, logger)

	// Signal handling — SIGTERM/SIGINT trigger graceful shutdown which kills
	// all running FFmpeg child processes via context cancellation.
	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		sig := <-sigCh
		logger.Info("shutdown signal received", "signal", sig)
		cancel()
	}()

	// Start webhook listener
	handler := NewHandler(transcoder, logger)
	listener := NewWebhookListener(*listenAddr, handler, logger)

	logger.Info("hls-transcoder starting",
		"addr", *listenAddr,
		"mode", *mode,
		"hls_dir", *hlsDir,
		"rtmp_host", *rtmpHost,
		"rtmp_port", *rtmpPort,
		"blob_upload", *blobWebhookURL != "",
		"output_mode", *outputMode,
		"ingest_url", *ingestURL != "",
		"codec", *codec,
		"platform_url", *platformURL,
		"config_cache_ttl", configCacheTTL.String(),
	)

	if err := listener.Run(ctx); err != nil && err != context.Canceled {
		logger.Error("webhook listener error", "error", err)
	}

	// Graceful shutdown — stop all FFmpeg processes and config fetcher
	logger.Info("shutting down, stopping all active transcoders...")
	transcoder.StopAll()
	configFetcher.Stop()
	logger.Info("shutdown complete")
	fmt.Fprintln(os.Stderr, "hls-transcoder stopped")
}

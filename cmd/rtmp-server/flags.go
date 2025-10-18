package main

import (
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// version is injected at build time with -ldflags "-X main.version=...". Defaults to dev.
var version = "dev"

// cliConfig holds user supplied flag values prior to translation into server.Config
// so main.go can validate and map.
type cliConfig struct {
	listenAddr        string
	logLevel          string
	recordAll         bool
	recordDir         string
	chunkSize         uint
	showVersion       bool
	relayDestinations []string // NEW: Multiple destination URLs for relay
	// Hook configuration (backward compatible - all optional)
	hookScripts     []string // event_type=script_path pairs
	hookWebhooks    []string // event_type=webhook_url pairs
	hookStdioFormat string   // "json", "env", or "" (disabled)
	hookTimeout     string   // timeout duration (e.g. "30s")
	hookConcurrency int      // max concurrent hook executions
}

func parseFlags(args []string) (*cliConfig, error) {
	fs := flag.NewFlagSet("rtmp-server", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)

	cfg := &cliConfig{}
	var relayDests stringSliceFlag
	var hookScripts stringSliceFlag
	var hookWebhooks stringSliceFlag

	fs.StringVar(&cfg.listenAddr, "listen", ":1935", "TCP listen address (e.g. :1935 or 0.0.0.0:1935)")
	fs.StringVar(&cfg.logLevel, "log-level", "info", "Log level: debug|info|warn|error")
	fs.BoolVar(&cfg.recordAll, "record-all", false, "Enable recording of all streams to -record-dir")
	fs.StringVar(&cfg.recordDir, "record-dir", "recordings", "Directory to write FLV recordings")
	fs.UintVar(&cfg.chunkSize, "chunk-size", 4096, "Initial outbound chunk size")
	fs.BoolVar(&cfg.showVersion, "version", false, "Print version and exit")
	fs.Var(&relayDests, "relay-to", "RTMP destination URL (can be specified multiple times)")

	// Hook configuration flags (all optional for backward compatibility)
	fs.Var(&hookScripts, "hook-script", "Hook script in format event_type=script_path (can be specified multiple times)")
	fs.Var(&hookWebhooks, "hook-webhook", "Hook webhook in format event_type=webhook_url (can be specified multiple times)")
	fs.StringVar(&cfg.hookStdioFormat, "hook-stdio-format", "", "Enable structured stdio output: json|env (empty=disabled)")
	fs.StringVar(&cfg.hookTimeout, "hook-timeout", "30s", "Timeout for hook execution")
	fs.IntVar(&cfg.hookConcurrency, "hook-concurrency", 10, "Maximum concurrent hook executions")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	cfg.relayDestinations = relayDests
	cfg.hookScripts = hookScripts
	cfg.hookWebhooks = hookWebhooks

	if cfg.chunkSize == 0 || cfg.chunkSize > 65536 {
		return nil, errors.New("chunk-size must be between 1 and 65536")
	}

	switch cfg.logLevel {
	case "debug", "info", "warn", "error":
	default:
		return nil, fmt.Errorf("invalid log-level %q", cfg.logLevel)
	}

	// Validate hook configuration
	if err := validateHookConfig(cfg); err != nil {
		return nil, err
	}

	// Validate relay destinations
	for _, dest := range cfg.relayDestinations {
		if err := validateRelayDestination(dest); err != nil {
			return nil, fmt.Errorf("invalid relay destination %q: %w", dest, err)
		}
	}

	return cfg, nil
}

// stringSliceFlag implements flag.Value for multiple string values
type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return strings.Join(*s, ", ")
}

func (s *stringSliceFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

// validateRelayDestination validates an RTMP URL
func validateRelayDestination(rawURL string) error {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	if parsedURL.Scheme != "rtmp" {
		return fmt.Errorf("URL must use rtmp:// scheme, got %s", parsedURL.Scheme)
	}

	if parsedURL.Host == "" {
		return fmt.Errorf("URL must have a host")
	}

	return nil
}

// validateHookConfig validates hook configuration settings
func validateHookConfig(cfg *cliConfig) error {
	// Validate stdio format
	if cfg.hookStdioFormat != "" && cfg.hookStdioFormat != "json" && cfg.hookStdioFormat != "env" {
		return fmt.Errorf("invalid hook-stdio-format %q, must be 'json' or 'env'", cfg.hookStdioFormat)
	}

	// Validate timeout
	if cfg.hookTimeout != "" {
		if _, err := parseTimeDuration(cfg.hookTimeout); err != nil {
			return fmt.Errorf("invalid hook-timeout %q: %w", cfg.hookTimeout, err)
		}
	}

	// Validate concurrency
	if cfg.hookConcurrency < 1 || cfg.hookConcurrency > 100 {
		return fmt.Errorf("hook-concurrency must be between 1 and 100, got %d", cfg.hookConcurrency)
	}

	// Validate hook scripts format (event_type=script_path)
	for _, script := range cfg.hookScripts {
		if err := validateHookAssignment("hook-script", script); err != nil {
			return err
		}
	}

	// Validate hook webhooks format (event_type=webhook_url)
	for _, webhook := range cfg.hookWebhooks {
		if err := validateHookAssignment("hook-webhook", webhook); err != nil {
			return err
		}
	}

	return nil
}

// parseTimeDuration parses a duration string (handles common formats)
func parseTimeDuration(s string) (string, error) {
	// Simple validation - just check if it looks like a duration
	if len(s) < 2 {
		return "", fmt.Errorf("duration too short")
	}

	// Check suffix
	suffix := s[len(s)-1:]
	if suffix != "s" && suffix != "m" && suffix != "h" {
		return "", fmt.Errorf("duration must end with s, m, or h")
	}

	return s, nil
}

// validateHookAssignment validates event_type=value format
func validateHookAssignment(flagName, assignment string) error {
	parts := strings.SplitN(assignment, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid %s format %q, expected event_type=value", flagName, assignment)
	}

	eventType, value := parts[0], parts[1]

	if eventType == "" {
		return fmt.Errorf("invalid %s: event type cannot be empty", flagName)
	}

	if value == "" {
		return fmt.Errorf("invalid %s: value cannot be empty", flagName)
	}

	// Validate event type (basic validation - hook manager will validate against known types)
	validEventTypes := map[string]bool{
		"connection_accept":  true,
		"connection_close":   true,
		"handshake_complete": true,
		"stream_create":      true,
		"stream_delete":      true,
		"publish_start":      true,
		"publish_stop":       true,
		"play_start":         true,
		"play_stop":          true,
		"codec_detected":     true,
	}

	if !validEventTypes[eventType] {
		return fmt.Errorf("invalid %s: unknown event type %q", flagName, eventType)
	}

	return nil
}

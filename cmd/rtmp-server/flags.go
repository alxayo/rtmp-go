package main

import (
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// version is set at build time using: go build -ldflags "-X main.version=v0.1.0"
// When building without ldflags, it defaults to the value below.
var version = "v0.1.0"

// cliConfig holds the parsed command-line flag values.
// These are validated in parseFlags() before being mapped to server.Config.
type cliConfig struct {
	listenAddr        string   // TCP address to listen on (e.g. ":1935")
	logLevel          string   // log verbosity level (debug/info/warn/error)
	recordAll         bool     // whether to record all published streams
	recordDir         string   // directory for FLV recording files
	chunkSize         uint     // outbound chunk size (1-65536 bytes)
	showVersion       bool     // print version and exit
	relayDestinations []string // RTMP URLs to relay published streams to

	// Event hooks
	hookScripts     []string // shell hooks: "event_type=/path/to/script"
	hookWebhooks    []string // webhook hooks: "event_type=https://url"
	hookStdioFormat string   // stdio output: "json", "env", or ""
	hookTimeout     string   // hook execution timeout (e.g. "30s")
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
	fs.Var(&hookScripts, "hook-script", "Shell hook: event_type=/path/to/script (repeatable)")
	fs.Var(&hookWebhooks, "hook-webhook", "Webhook hook: event_type=https://url (repeatable)")
	fs.StringVar(&cfg.hookStdioFormat, "hook-stdio-format", "", "Stdio hook output format: json|env (empty=disabled)")
	fs.StringVar(&cfg.hookTimeout, "hook-timeout", "30s", "Hook execution timeout")
	fs.IntVar(&cfg.hookConcurrency, "hook-concurrency", 10, "Max concurrent hook executions")

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

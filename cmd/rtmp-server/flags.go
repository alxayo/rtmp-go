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
}

func parseFlags(args []string) (*cliConfig, error) {
	fs := flag.NewFlagSet("rtmp-server", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)

	cfg := &cliConfig{}
	var relayDests stringSliceFlag

	fs.StringVar(&cfg.listenAddr, "listen", ":1935", "TCP listen address (e.g. :1935 or 0.0.0.0:1935)")
	fs.StringVar(&cfg.logLevel, "log-level", "info", "Log level: debug|info|warn|error")
	fs.BoolVar(&cfg.recordAll, "record-all", false, "Enable recording of all streams to -record-dir")
	fs.StringVar(&cfg.recordDir, "record-dir", "recordings", "Directory to write FLV recordings")
	fs.UintVar(&cfg.chunkSize, "chunk-size", 4096, "Initial outbound chunk size")
	fs.BoolVar(&cfg.showVersion, "version", false, "Print version and exit")
	fs.Var(&relayDests, "relay-to", "RTMP destination URL (can be specified multiple times)")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	cfg.relayDestinations = relayDests

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

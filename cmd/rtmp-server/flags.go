package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
)

// version is injected at build time with -ldflags "-X main.version=...". Defaults to dev.
var version = "dev"

// cliConfig holds user supplied flag values prior to translation into server.Config
// so main.go can validate and map.
type cliConfig struct {
	listenAddr  string
	logLevel    string
	recordAll   bool
	recordDir   string
	chunkSize   uint
	showVersion bool
}

func parseFlags(args []string) (*cliConfig, error) {
	fs := flag.NewFlagSet("rtmp-server", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)

	cfg := &cliConfig{}
	fs.StringVar(&cfg.listenAddr, "listen", ":1935", "TCP listen address (e.g. :1935 or 0.0.0.0:1935)")
	fs.StringVar(&cfg.logLevel, "log-level", "info", "Log level: debug|info|warn|error")
	fs.BoolVar(&cfg.recordAll, "record-all", false, "Enable recording of all streams to -record-dir")
	fs.StringVar(&cfg.recordDir, "record-dir", "recordings", "Directory to write FLV recordings")
	fs.UintVar(&cfg.chunkSize, "chunk-size", 4096, "Initial outbound chunk size")
	fs.BoolVar(&cfg.showVersion, "version", false, "Print version and exit")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	if cfg.chunkSize == 0 || cfg.chunkSize > 65536 {
		return nil, errors.New("chunk-size must be between 1 and 65536")
	}

	switch cfg.logLevel {
	case "debug", "info", "warn", "error":
	default:
		return nil, fmt.Errorf("invalid log-level %q", cfg.logLevel)
	}

	return cfg, nil
}

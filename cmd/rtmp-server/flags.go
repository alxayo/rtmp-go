package main

import (
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// version is set at build time using: go build -ldflags "-X main.version=v0.4.0"
// When building without ldflags, it defaults to the value below.
var version = "v0.4.0"

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

	// TLS (RTMPS) configuration
	tlsListenAddr string // optional RTMPS listen address (e.g. ":443")
	tlsCertFile   string // path to PEM-encoded TLS certificate
	tlsKeyFile    string // path to PEM-encoded TLS private key

	// Event hooks
	hookScripts     []string // shell hooks: "event_type=/path/to/script"
	hookWebhooks    []string // webhook hooks: "event_type=https://url"
	hookStdioFormat string   // stdio output: "json", "env", or ""
	hookTimeout     string   // hook execution timeout (e.g. "30s")
	hookConcurrency int      // max concurrent hook executions

	// Metrics
	metricsAddr string // HTTP address for expvar metrics (e.g. ":8080"); empty = disabled

	// Authentication
	authMode            string   // "none", "token", "file", "callback"
	authTokens          []string // "streamKey=token" pairs (for mode=token)
	authFile            string   // path to JSON token file (for mode=file)
	authCallbackURL     string   // webhook URL (for mode=callback)
	authCallbackTimeout string   // callback HTTP timeout (default "5s")

	// SRT configuration
	srtListenAddr     string // SRT UDP listen address (e.g. ":10080"). Empty = disabled
	srtLatency        int    // SRT buffer latency in milliseconds (default 120)
	srtPassphrase     string // SRT encryption passphrase (empty = no encryption)
	srtPbKeyLen       int    // AES key length: 16, 24, or 32 (default 16)
	srtPassphraseFile string // path to JSON file mapping stream keys → passphrases (per-stream encryption; mutually exclusive with srtPassphrase)

	// Reconnect
	reconnectURL string // URL to redirect clients to when SIGUSR1 triggers a reconnect-all request
}

func parseFlags(args []string) (*cliConfig, error) {
	fs := flag.NewFlagSet("rtmp-server", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)

	cfg := &cliConfig{}
	var relayDests stringSliceFlag
	var hookScripts stringSliceFlag
	var hookWebhooks stringSliceFlag
	var authTokens stringSliceFlag

	fs.StringVar(&cfg.listenAddr, "listen", ":1935", "TCP listen address (e.g. :1935 or 0.0.0.0:1935)")
	fs.StringVar(&cfg.logLevel, "log-level", "info", "Log level: debug|info|warn|error")
	fs.Var(&explicitBool{&cfg.recordAll}, "record-all", "Enable recording of all streams to -record-dir (true/false)")
	fs.StringVar(&cfg.recordDir, "record-dir", "recordings", "Directory to write FLV recordings")
	fs.UintVar(&cfg.chunkSize, "chunk-size", 4096, "Initial outbound chunk size")
	fs.BoolVar(&cfg.showVersion, "version", false, "Print version and exit")
	fs.Var(&relayDests, "relay-to", "RTMP destination URL (can be specified multiple times)")

	// TLS (RTMPS) flags
	fs.StringVar(&cfg.tlsListenAddr, "tls-listen", "", "RTMPS listen address (e.g. :443). Requires -tls-cert and -tls-key")
	fs.StringVar(&cfg.tlsCertFile, "tls-cert", "", "Path to PEM-encoded TLS certificate file")
	fs.StringVar(&cfg.tlsKeyFile, "tls-key", "", "Path to PEM-encoded TLS private key file")

	fs.Var(&hookScripts, "hook-script", "Shell hook: event_type=/path/to/script (repeatable)")
	fs.Var(&hookWebhooks, "hook-webhook", "Webhook hook: event_type=https://url (repeatable)")
	fs.StringVar(&cfg.hookStdioFormat, "hook-stdio-format", "", "Stdio hook output format: json|env (empty=disabled)")
	fs.StringVar(&cfg.hookTimeout, "hook-timeout", "30s", "Hook execution timeout")
	fs.IntVar(&cfg.hookConcurrency, "hook-concurrency", 10, "Max concurrent hook executions")

	// Metrics
	fs.StringVar(&cfg.metricsAddr, "metrics-addr", "", "HTTP address for metrics endpoint (e.g. :8080 or 127.0.0.1:8080). Empty = disabled")

	// Authentication flags
	fs.StringVar(&cfg.authMode, "auth-mode", "none", "Authentication mode: none|token|file|callback")
	fs.Var(&authTokens, "auth-token", `Stream token: "streamKey=token" (repeatable, for -auth-mode=token)`)
	fs.StringVar(&cfg.authFile, "auth-file", "", "Path to JSON token file (for -auth-mode=file)")
	fs.StringVar(&cfg.authCallbackURL, "auth-callback", "", "Webhook URL for auth validation (for -auth-mode=callback)")
	fs.StringVar(&cfg.authCallbackTimeout, "auth-callback-timeout", "5s", "Auth callback HTTP timeout")

	// SRT flags
	fs.StringVar(&cfg.srtListenAddr, "srt-listen", "", "SRT UDP listen address (e.g. :10080). Empty = disabled")
	fs.IntVar(&cfg.srtLatency, "srt-latency", 120, "SRT buffer latency in milliseconds")
	fs.StringVar(&cfg.srtPassphrase, "srt-passphrase", "", "SRT encryption passphrase (empty = no encryption)")
	fs.IntVar(&cfg.srtPbKeyLen, "srt-pbkeylen", 16, "SRT AES key length: 16, 24, or 32")
	fs.StringVar(&cfg.srtPassphraseFile, "srt-passphrase-file", "", "Path to JSON file mapping stream keys to passphrases (per-stream encryption)")

	// Reconnect (E-RTMP v2)
	fs.StringVar(&cfg.reconnectURL, "reconnect-url", "", "URL to redirect clients to on SIGUSR1 reconnect request")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	cfg.relayDestinations = relayDests
	cfg.hookScripts = hookScripts
	cfg.hookWebhooks = hookWebhooks
	cfg.authTokens = authTokens

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

	// Validate TLS configuration
	if cfg.tlsListenAddr != "" {
		if cfg.tlsCertFile == "" || cfg.tlsKeyFile == "" {
			return nil, errors.New("-tls-listen requires both -tls-cert and -tls-key")
		}
	}
	if (cfg.tlsCertFile != "" || cfg.tlsKeyFile != "") && cfg.tlsListenAddr == "" {
		return nil, errors.New("-tls-cert and -tls-key require -tls-listen")
	}

	// Validate authentication configuration
	switch cfg.authMode {
	case "none":
		// No validation needed
	case "token":
		if len(cfg.authTokens) == 0 {
			return nil, errors.New("-auth-mode=token requires at least one -auth-token flag")
		}
		for _, t := range cfg.authTokens {
			if !strings.Contains(t, "=") {
				return nil, fmt.Errorf("invalid -auth-token format %q (expected streamKey=token)", t)
			}
		}
	case "file":
		if cfg.authFile == "" {
			return nil, errors.New("-auth-mode=file requires -auth-file flag")
		}
	case "callback":
		if cfg.authCallbackURL == "" {
			return nil, errors.New("-auth-mode=callback requires -auth-callback flag")
		}
	default:
		return nil, fmt.Errorf("invalid -auth-mode %q (expected none|token|file|callback)", cfg.authMode)
	}

	// Validate SRT configuration
	if cfg.srtPbKeyLen != 0 && cfg.srtPbKeyLen != 16 && cfg.srtPbKeyLen != 24 && cfg.srtPbKeyLen != 32 {
		return nil, fmt.Errorf("-srt-pbkeylen must be 16, 24, or 32, got %d", cfg.srtPbKeyLen)
	}
	// -srt-passphrase sets one passphrase for ALL streams; -srt-passphrase-file
	// sets per-stream passphrases from a JSON file. Allowing both would be
	// ambiguous (which passphrase wins for a given stream?), so we reject it.
	if cfg.srtPassphrase != "" && cfg.srtPassphraseFile != "" {
		return nil, errors.New("-srt-passphrase and -srt-passphrase-file are mutually exclusive")
	}
	// libsrt enforces passphrase length of 10–79 characters (derived from
	// the SRT spec). We validate early here to give a clear CLI error rather
	// than a cryptic handshake failure at runtime.
	if cfg.srtPassphrase != "" {
		if len(cfg.srtPassphrase) < 10 {
			return nil, fmt.Errorf("-srt-passphrase too short: %d characters (minimum 10, per SRT spec)", len(cfg.srtPassphrase))
		}
		if len(cfg.srtPassphrase) > 79 {
			return nil, fmt.Errorf("-srt-passphrase too long: %d characters (maximum 79, per SRT spec)", len(cfg.srtPassphrase))
		}
	}
	// Verify the passphrase file exists at startup so operators get an
	// immediate error instead of discovering it later when the first SRT
	// connection arrives. The file's contents are validated by buildSRTResolver().
	if cfg.srtPassphraseFile != "" {
		if _, err := os.Stat(cfg.srtPassphraseFile); err != nil {
			return nil, fmt.Errorf("-srt-passphrase-file %q: %w", cfg.srtPassphraseFile, err)
		}
	}

	return cfg, nil
}

// explicitBool implements flag.Value for boolean flags that require an explicit
// value argument (e.g. "-record-all true" or "-record-all=false").
//
// Why: Go's flag package treats built-in bool flags specially — writing
// "-record-all true" does NOT consume "true" as the flag's value. Instead,
// "true" becomes a positional argument, which stops all further flag parsing.
// This means any flags AFTER -record-all (like -log-level) silently keep
// their default values. By using a custom Value type without IsBoolFlag(),
// we force the flag package to consume the next argument as the value.
type explicitBool struct {
	val *bool
}

func (b *explicitBool) String() string {
	if b.val == nil {
		return "false"
	}
	if *b.val {
		return "true"
	}
	return "false"
}

func (b *explicitBool) Set(s string) error {
	switch strings.ToLower(s) {
	case "true", "1", "yes":
		*b.val = true
	case "false", "0", "no":
		*b.val = false
	default:
		return fmt.Errorf("invalid boolean value %q (use true/false)", s)
	}
	return nil
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

// validateRelayDestination validates an RTMP or RTMPS URL
func validateRelayDestination(rawURL string) error {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	if parsedURL.Scheme != "rtmp" && parsedURL.Scheme != "rtmps" {
		return fmt.Errorf("URL must use rtmp:// or rtmps:// scheme, got %s", parsedURL.Scheme)
	}

	if parsedURL.Host == "" {
		return fmt.Errorf("URL must have a host")
	}

	return nil
}

// Package auth provides pluggable token-based authentication for RTMP
// publish and play requests.
//
// The package defines a [Validator] interface that all authentication
// backends implement. Four built-in validators are provided:
//
//   - [AllowAllValidator]: accepts every request (default, backward-compatible)
//   - [TokenValidator]: validates against an in-memory map of stream-key → token pairs
//   - [FileValidator]: loads tokens from a JSON file, supports live reload via [FileValidator.Reload]
//   - [CallbackValidator]: delegates validation to an external HTTP webhook
//
// # How Tokens Are Passed
//
// Clients (OBS, FFmpeg, ffplay) embed tokens as URL query parameters in the
// stream name field of publish/play commands:
//
//	rtmp://server/live/mystream?token=secret123
//
// The server parses these query parameters using [ParseStreamURL] and passes
// them to the validator via the [Request] struct.
//
// # Integration Points
//
// Authentication is enforced at the publish/play command level in the
// server package (see authenticateRequest in command_integration.go),
// NOT during connect or handshake.
package auth

import (
	"context"
	"errors"
)

// Validator validates stream access requests. Implementations must be safe
// for concurrent use from multiple goroutines.
type Validator interface {
	// ValidatePublish checks if a client is allowed to publish to a stream.
	// Returns nil on success, a sentinel error on failure.
	ValidatePublish(ctx context.Context, req *Request) error

	// ValidatePlay checks if a client is allowed to play (subscribe to) a stream.
	// Returns nil on success, a sentinel error on failure.
	ValidatePlay(ctx context.Context, req *Request) error
}

// Request contains authentication context extracted from the RTMP session.
// It is built by the command handler from the parsed publish/play command
// and the per-connection state captured during connect.
type Request struct {
	App           string                 // application name from connect (e.g. "live")
	StreamName    string                 // clean stream name without query params (e.g. "mystream")
	StreamKey     string                 // full key: app/streamName (e.g. "live/mystream")
	QueryParams   map[string]string      // parsed from stream name (e.g. {"token": "abc123"})
	ConnectParams map[string]interface{} // extra fields from connect command object
	RemoteAddr    string                 // client IP:port (e.g. "192.168.1.100:54321")
}

// Sentinel errors returned by validators. Callers can use errors.Is to
// check which specific authentication failure occurred.
var (
	ErrUnauthorized = errors.New("authentication failed: invalid credentials")
	ErrTokenMissing = errors.New("authentication failed: token missing")
	ErrForbidden    = errors.New("authentication failed: access denied")
)

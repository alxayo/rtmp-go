# Feature 004: Token-Based Stream Key Authentication

**Feature**: 004-token-auth  
**Status**: Draft  
**Date**: 2026-03-03  
**Branch**: `feature/004-token-auth`

## Overview

Add pluggable token-based authentication to validate publish and play requests.
Three validator backends: static tokens (CLI flags), JSON file, and HTTP webhook callback.

Authentication is enforced at the **publish/play command** level (not during connect or handshake).
This matches industry standard behavior (Nginx-RTMP, Wowza, YouTube, Twitch).

### Design Constraints

- **Zero external dependencies** (stdlib only)
- **Backward-compatible**: default mode is `none` (current behavior)
- **Pluggable**: `Validator` interface allows custom implementations
- Tokens are passed via **URL query parameters** (`?token=xxx`) for OBS/FFmpeg compatibility

---

## Protocol Flow

```
Client → Server: TCP connect
                  Handshake (no auth)
Client → Server: connect command
                  → Store app, tcUrl, connect params
                  → Always respond with _result (success)
Client → Server: createStream
                  → Allocate stream ID, respond _result
Client → Server: publish "mystream?token=abc123" "live"
                  → Parse stream name + query params
                  → Call validator.ValidatePublish()
                  → SUCCESS: send onStatus NetStream.Publish.Start
                  → FAILURE: send onStatus error + close connection
```

### Why Validate at Publish/Play (Not Connect)

1. The `connect` command carries `app` but not the stream name or token
2. OBS/FFmpeg embed tokens in the stream name field (e.g. `mystream?token=xxx`)
3. Industry standard: Nginx-RTMP `on_publish`, YouTube, Twitch all validate at publish time
4. Allows per-stream authorization (different tokens for different streams)

---

## Client Compatibility

### OBS Studio
```
Server:     rtmp://myserver.com/live
Stream Key: mystream?token=secret123
```
Result URL: `rtmp://myserver.com/live/mystream?token=secret123`

### FFmpeg
```bash
ffmpeg -re -i input.mp4 -c copy -f flv "rtmp://localhost:1935/live/mystream?token=secret123"
```

### ffplay
```bash
ffplay "rtmp://localhost:1935/live/mystream?token=secret123"
```

All three embed the token in the publishing/stream name field of the RTMP publish/play command.
The server must parse query parameters from that field.

---

## Architecture

### New Package

```
internal/rtmp/server/auth/
├── auth.go              # Validator interface, request types, sentinel errors
├── auth_test.go         # Interface compliance tests
├── allow_all.go         # AllowAllValidator (default, current behavior)
├── allow_all_test.go
├── token.go             # TokenValidator (in-memory map from CLI flags)
├── token_test.go
├── file.go              # FileValidator (JSON file with SIGHUP reload)
├── file_test.go
├── callback.go          # CallbackValidator (HTTP webhook)
├── callback_test.go
├── url.go               # ParseStreamURL (extract query params from stream name)
└── url_test.go
```

### Modified Files

| File | Change |
|------|--------|
| `internal/rtmp/server/server.go` | Add `AuthValidator` to `Config` |
| `internal/rtmp/server/command_integration.go` | Store connect params; call validator before HandlePublish/HandlePlay |
| `internal/rtmp/rpc/connect.go` | Capture extra connect object fields for auth context |
| `internal/rtmp/rpc/publish.go` | Parse query params from publishingName, expose clean name |
| `internal/rtmp/rpc/play.go` | Parse query params from streamName, expose clean name |
| `internal/rtmp/server/hooks/events.go` | Add `EventAuthFailed` event type |
| `cmd/rtmp-server/flags.go` | Add auth CLI flags |
| `cmd/rtmp-server/main.go` | Wire auth validator from flags |
| `internal/errors/errors.go` | Add `AuthError` type |

---

## Detailed Design

### T001: Define Validator Interface and Types

**File**: `internal/rtmp/server/auth/auth.go`

```go
package auth

import (
    "context"
    "errors"
)

// Validator validates stream access requests. Implementations must be
// safe for concurrent use from multiple goroutines.
type Validator interface {
    // ValidatePublish checks if a client is allowed to publish to a stream.
    // Returns nil on success, a sentinel error on failure.
    ValidatePublish(ctx context.Context, req *Request) error

    // ValidatePlay checks if a client is allowed to play (subscribe to) a stream.
    // Returns nil on success, a sentinel error on failure.
    ValidatePlay(ctx context.Context, req *Request) error
}

// Request contains authentication context extracted from the RTMP session.
// Shared by both publish and play validation.
type Request struct {
    App           string            // Application name from connect (e.g. "live")
    StreamName    string            // Stream name without query params (e.g. "mystream")
    StreamKey     string            // Full key: app/streamName (e.g. "live/mystream")
    QueryParams   map[string]string // Parsed from stream name (e.g. {"token": "abc123"})
    ConnectParams map[string]interface{} // Extra fields from connect command object
    RemoteAddr    string            // Client IP:port
}

// Sentinel errors for authentication failures.
var (
    ErrUnauthorized = errors.New("authentication failed: invalid credentials")
    ErrTokenMissing = errors.New("authentication failed: token missing")
    ErrTokenExpired = errors.New("authentication failed: token expired")
    ErrForbidden    = errors.New("authentication failed: access denied")
)
```

**Tests**: Verify sentinel error messages, Request zero-value safety.

---

### T002: Implement AllowAllValidator

**File**: `internal/rtmp/server/auth/allow_all.go`

```go
package auth

import "context"

// AllowAllValidator permits all publish and play requests without checking
// credentials. This is the default validator when no auth mode is configured.
type AllowAllValidator struct{}

func (v *AllowAllValidator) ValidatePublish(_ context.Context, _ *Request) error { return nil }
func (v *AllowAllValidator) ValidatePlay(_ context.Context, _ *Request) error    { return nil }
```

**Tests**: Confirm both methods return nil for any input.

---

### T003: Implement Stream URL Parser

**File**: `internal/rtmp/server/auth/url.go`

```go
package auth

import (
    "net/url"
    "strings"
)

// ParsedStreamURL holds a stream name separated from its query parameters.
type ParsedStreamURL struct {
    StreamName  string            // "mystream" (without query string)
    QueryParams map[string]string // {"token": "abc123", "expires": "1234567890"}
}

// ParseStreamURL splits a raw stream name (as received in publish/play commands)
// into the clean stream name and any query parameters appended after "?".
//
// Examples:
//   "mystream"                     → {StreamName: "mystream", QueryParams: {}}
//   "mystream?token=abc123"        → {StreamName: "mystream", QueryParams: {"token": "abc123"}}
//   "mystream?token=a&expires=123" → {StreamName: "mystream", QueryParams: {"token": "a", "expires": "123"}}
func ParseStreamURL(raw string) *ParsedStreamURL {
    result := &ParsedStreamURL{QueryParams: make(map[string]string)}

    idx := strings.IndexByte(raw, '?')
    if idx < 0 {
        result.StreamName = raw
        return result
    }

    result.StreamName = raw[:idx]
    if idx+1 < len(raw) {
        values, err := url.ParseQuery(raw[idx+1:])
        if err == nil {
            for k, v := range values {
                if len(v) > 0 {
                    result.QueryParams[k] = v[0]
                }
            }
        }
    }

    return result
}
```

**Tests** (table-driven):

| Input | Expected StreamName | Expected QueryParams |
|-------|--------------------|--------------------|
| `"mystream"` | `"mystream"` | `{}` |
| `"mystream?token=abc"` | `"mystream"` | `{"token": "abc"}` |
| `"mystream?token=a&expires=123"` | `"mystream"` | `{"token": "a", "expires": "123"}` |
| `""` | `""` | `{}` |
| `"stream?"` | `"stream"` | `{}` |
| `"stream?key=a%20b"` | `"stream"` | `{"key": "a b"}` |
| `"stream?=nokey"` | `"stream"` | `{"": "nokey"}` |

---

### T004: Implement TokenValidator

**File**: `internal/rtmp/server/auth/token.go`

```go
package auth

import "context"

// TokenValidator validates requests against a static map of stream keys to
// expected tokens. Thread-safe (map is read-only after construction).
//
// Token lookup: req.QueryParams["token"] must match Tokens[req.StreamKey].
// If the stream key is not in the map, the request is denied.
type TokenValidator struct {
    Tokens map[string]string // streamKey → expected token
}

func (v *TokenValidator) ValidatePublish(_ context.Context, req *Request) error {
    return v.validate(req)
}

func (v *TokenValidator) ValidatePlay(_ context.Context, req *Request) error {
    return v.validate(req)
}

func (v *TokenValidator) validate(req *Request) error {
    token := req.QueryParams["token"]
    if token == "" {
        return ErrTokenMissing
    }
    expected, exists := v.Tokens[req.StreamKey]
    if !exists || token != expected {
        return ErrUnauthorized
    }
    return nil
}
```

**Tests**:

| Scenario | StreamKey | Token | Expected |
|----------|-----------|-------|----------|
| Valid token | `"live/s1"` | `"secret"` | `nil` |
| Wrong token | `"live/s1"` | `"wrong"` | `ErrUnauthorized` |
| Missing token param | `"live/s1"` | `""` | `ErrTokenMissing` |
| Unknown stream key | `"live/unknown"` | `"any"` | `ErrUnauthorized` |
| Empty tokens map | `"live/s1"` | `"any"` | `ErrUnauthorized` |

---

### T005: Implement FileValidator

**File**: `internal/rtmp/server/auth/file.go`

```go
package auth

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "sync"
)

// FileValidator loads tokens from a JSON file. The file format is a simple
// object mapping stream keys to tokens:
//
//   {"live/stream1": "secret123", "live/stream2": "abc456"}
//
// Call Reload() to re-read the file (e.g. on SIGHUP). All methods are safe
// for concurrent use.
type FileValidator struct {
    path   string
    mu     sync.RWMutex
    tokens map[string]string
}

func NewFileValidator(path string) (*FileValidator, error) {
    v := &FileValidator{path: path}
    if err := v.Reload(); err != nil {
        return nil, fmt.Errorf("load auth file %s: %w", path, err)
    }
    return v, nil
}

// Reload re-reads the token file from disk. Safe to call concurrently.
func (v *FileValidator) Reload() error {
    data, err := os.ReadFile(v.path)
    if err != nil {
        return err
    }
    var tokens map[string]string
    if err := json.Unmarshal(data, &tokens); err != nil {
        return fmt.Errorf("parse auth file: %w", err)
    }
    v.mu.Lock()
    v.tokens = tokens
    v.mu.Unlock()
    return nil
}

func (v *FileValidator) ValidatePublish(_ context.Context, req *Request) error {
    return v.validate(req)
}

func (v *FileValidator) ValidatePlay(_ context.Context, req *Request) error {
    return v.validate(req)
}

func (v *FileValidator) validate(req *Request) error {
    token := req.QueryParams["token"]
    if token == "" {
        return ErrTokenMissing
    }
    v.mu.RLock()
    expected, exists := v.tokens[req.StreamKey]
    v.mu.RUnlock()
    if !exists || token != expected {
        return ErrUnauthorized
    }
    return nil
}
```

**Tests**:
- Load valid JSON file → validate success
- Load valid JSON file → validate wrong token → `ErrUnauthorized`
- Load invalid JSON → constructor returns error
- Load missing file → constructor returns error
- Reload replaces tokens (old token rejected, new token accepted)
- Concurrent read/write during Reload (race detector)

---

### T006: Implement CallbackValidator

**File**: `internal/rtmp/server/auth/callback.go`

```go
package auth

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "time"
)

// CallbackValidator sends an HTTP POST to a webhook URL for each
// publish/play request. The external service decides whether to allow
// or deny the request based on the HTTP response status code.
//
// Request body (JSON):
//   {
//     "action": "publish" | "play",
//     "app": "live",
//     "stream_name": "mystream",
//     "stream_key": "live/mystream",
//     "token": "abc123",
//     "remote_addr": "192.168.1.100:54321"
//   }
//
// Expected responses:
//   200 OK         → allow
//   401/403/other  → deny
type CallbackValidator struct {
    URL    string
    Client *http.Client
}

func NewCallbackValidator(callbackURL string, timeout time.Duration) *CallbackValidator {
    return &CallbackValidator{
        URL:    callbackURL,
        Client: &http.Client{Timeout: timeout},
    }
}

type callbackRequest struct {
    Action     string `json:"action"`
    App        string `json:"app"`
    StreamName string `json:"stream_name"`
    StreamKey  string `json:"stream_key"`
    Token      string `json:"token"`
    RemoteAddr string `json:"remote_addr"`
}

func (v *CallbackValidator) ValidatePublish(ctx context.Context, req *Request) error {
    return v.call(ctx, "publish", req)
}

func (v *CallbackValidator) ValidatePlay(ctx context.Context, req *Request) error {
    return v.call(ctx, "play", req)
}

func (v *CallbackValidator) call(ctx context.Context, action string, req *Request) error {
    body := callbackRequest{
        Action:     action,
        App:        req.App,
        StreamName: req.StreamName,
        StreamKey:  req.StreamKey,
        Token:      req.QueryParams["token"],
        RemoteAddr: req.RemoteAddr,
    }
    data, err := json.Marshal(body)
    if err != nil {
        return fmt.Errorf("auth callback marshal: %w", err)
    }

    httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, v.URL, bytes.NewReader(data))
    if err != nil {
        return fmt.Errorf("auth callback request: %w", err)
    }
    httpReq.Header.Set("Content-Type", "application/json")

    resp, err := v.Client.Do(httpReq)
    if err != nil {
        return fmt.Errorf("auth callback failed: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode == http.StatusOK {
        return nil
    }
    return ErrUnauthorized
}
```

**Tests** (use `httptest.NewServer`):
- 200 response → nil
- 401 response → `ErrUnauthorized`
- 403 response → `ErrUnauthorized`
- 500 response → `ErrUnauthorized`
- Connection timeout → returns error (non-nil)
- Context cancellation → returns error

---

### T007: Add AuthError to Error Package

**File**: `internal/errors/errors.go` (append)

```go
// AuthError indicates an authentication or authorization failure.
type AuthError struct {
    Op  string // operation (e.g. "publish.auth", "play.auth")
    Err error  // underlying cause
}

func (e *AuthError) Error() string {
    if e.Err == nil {
        return fmt.Sprintf("auth error: %s", e.Op)
    }
    return fmt.Sprintf("auth error: %s: %v", e.Op, e.Err)
}
func (e *AuthError) Unwrap() error { return e.Err }
func (e *AuthError) isProtocol()   {} // classified as protocol-layer

func NewAuthError(op string, err error) *AuthError {
    return &AuthError{Op: op, Err: err}
}
```

---

### T008: Update RPC Parsers to Separate Query Params

**Modify**: `internal/rtmp/rpc/publish.go`

Changes:
- Import `auth` package
- Parse `publishingName` through `auth.ParseStreamURL()`
- Store clean name (without `?token=...`) in `PublishCommand.PublishingName`
- Add `QueryParams map[string]string` field to `PublishCommand`
- Build `StreamKey` from clean name only: `app + "/" + cleanName`

```go
// PublishCommand (updated)
type PublishCommand struct {
    PublishingName string            // clean name without query params
    PublishingType string            // live|record|append
    StreamKey      string            // app/publishingName (clean)
    QueryParams    map[string]string // parsed from raw name (e.g. {"token": "abc123"})
}
```

In `ParsePublishCommand`, after extracting raw `publishingName` from AMF values:
```go
parsed := auth.ParseStreamURL(publishingName)
// Use parsed.StreamName as the clean publishingName
// Use parsed.QueryParams for auth context
```

**Same change** for `internal/rtmp/rpc/play.go`:
- Add `QueryParams map[string]string` to `PlayCommand`
- Parse stream name through `auth.ParseStreamURL()`

**IMPORTANT**: Update existing tests:
- `publish_test.go` — test with and without query params
- `play_test.go` — test with and without query params

---

### T009: Store Connect Params in Connection State

**Modify**: `internal/rtmp/server/command_integration.go`

Add `connectParams` to `commandState`:
```go
type commandState struct {
    app           string
    streamKey     string
    connectParams map[string]interface{} // extra fields from connect command object
    allocator     *rpc.StreamIDAllocator
    mediaLogger   *MediaLogger
    codecDetector *media.CodecDetector
}
```

In `OnConnect` handler, capture extra connect fields:
```go
d.OnConnect = func(cc *rpc.ConnectCommand, msg *chunk.Message) error {
    st.app = cc.App
    st.connectParams = cc.Extra // New field on ConnectCommand
    // ... rest unchanged
}
```

**Modify**: `internal/rtmp/rpc/connect.go`

Add `Extra` field to `ConnectCommand`:
```go
type ConnectCommand struct {
    TransactionID  float64
    App            string
    FlashVer       string
    TcURL          string
    ObjectEncoding float64
    Extra          map[string]interface{} // all other connect object fields
}
```

In `ParseConnectCommand`, after extracting known fields, capture remaining:
```go
extra := make(map[string]interface{})
for k, v := range obj {
    switch k {
    case "app", "flashVer", "tcUrl", "objectEncoding":
        continue // already extracted
    default:
        extra[k] = v
    }
}
cc.Extra = extra
```

---

### T010: Wire Authentication into Command Handlers

**Modify**: `internal/rtmp/server/command_integration.go`

In `OnPublish` — insert auth checkpoint **before** `HandlePublish`:
```go
d.OnPublish = func(pc *rpc.PublishCommand, msg *chunk.Message) error {
    // ── AUTH CHECKPOINT ──
    if cfg.AuthValidator != nil {
        authReq := &auth.Request{
            App:           st.app,
            StreamName:    pc.PublishingName,
            StreamKey:     pc.StreamKey,
            QueryParams:   pc.QueryParams,
            ConnectParams: st.connectParams,
            RemoteAddr:    c.RemoteAddr(),
        }
        if err := cfg.AuthValidator.ValidatePublish(context.Background(), authReq); err != nil {
            log.Warn("publish authentication failed",
                "stream_key", pc.StreamKey,
                "remote_addr", c.RemoteAddr(),
                "error", err)

            // Send onStatus error to client
            errStatus, _ := buildOnStatus(msg.MessageStreamID, pc.StreamKey,
                "NetStream.Publish.Unauthorized",
                "Authentication failed.")
            _ = c.SendMessage(errStatus)

            // Trigger auth_failed hook
            if len(srv) > 0 && srv[0] != nil {
                srv[0].triggerHookEvent(hooks.EventAuthFailed, c.ID(), pc.StreamKey, map[string]interface{}{
                    "action": "publish",
                    "error":  err.Error(),
                })
            }

            // Close connection after rejection
            c.Close()
            return nil
        }
        log.Info("publish authenticated", "stream_key", pc.StreamKey, "remote_addr", c.RemoteAddr())
    }

    // ── EXISTING LOGIC (unchanged) ──
    if _, err := HandlePublish(reg, c, st.app, msg); err != nil {
        // ...
    }
    // ...
}
```

**Same pattern** for `OnPlay`:
```go
d.OnPlay = func(pl *rpc.PlayCommand, msg *chunk.Message) error {
    // ── AUTH CHECKPOINT ──
    if cfg.AuthValidator != nil {
        authReq := &auth.Request{
            App:           st.app,
            StreamName:    pl.StreamName,
            StreamKey:     pl.StreamKey,
            QueryParams:   pl.QueryParams,
            ConnectParams: st.connectParams,
            RemoteAddr:    c.RemoteAddr(),
        }
        if err := cfg.AuthValidator.ValidatePlay(context.Background(), authReq); err != nil {
            log.Warn("play authentication failed",
                "stream_key", pl.StreamKey,
                "remote_addr", c.RemoteAddr(),
                "error", err)

            errStatus, _ := buildOnStatus(msg.MessageStreamID, pl.StreamKey,
                "NetStream.Play.Unauthorized",
                "Authentication failed.")
            _ = c.SendMessage(errStatus)

            if len(srv) > 0 && srv[0] != nil {
                srv[0].triggerHookEvent(hooks.EventAuthFailed, c.ID(), pl.StreamKey, map[string]interface{}{
                    "action": "play",
                    "error":  err.Error(),
                })
            }

            c.Close()
            return nil
        }
        log.Info("play authenticated", "stream_key", pl.StreamKey, "remote_addr", c.RemoteAddr())
    }

    // ── EXISTING LOGIC (unchanged) ──
    if _, err := HandlePlay(reg, c, st.app, msg); err != nil {
        // ...
    }
    // ...
}
```

---

### T011: Add AuthValidator to Server Config

**Modify**: `internal/rtmp/server/server.go`

```go
type Config struct {
    // ... existing fields ...

    // Authentication (optional). If nil, all requests are allowed (default).
    AuthValidator auth.Validator
}
```

---

### T012: Add CLI Flags

**Modify**: `cmd/rtmp-server/flags.go`

New fields in `cliConfig`:
```go
type cliConfig struct {
    // ... existing fields ...

    // Authentication
    authMode            string   // "none", "token", "file", "callback"
    authTokens          []string // "streamKey=token" pairs (for mode=token)
    authFile            string   // path to JSON token file (for mode=file)
    authCallbackURL     string   // webhook URL (for mode=callback)
    authCallbackTimeout string   // callback HTTP timeout (default "5s")
}
```

New flags:
```go
fs.StringVar(&cfg.authMode, "auth-mode", "none", "Authentication mode: none|token|file|callback")
fs.Var(&authTokens, "auth-token", `Stream token: "streamKey=token" (repeatable, for -auth-mode=token)`)
fs.StringVar(&cfg.authFile, "auth-file", "", "Path to JSON token file (for -auth-mode=file)")
fs.StringVar(&cfg.authCallbackURL, "auth-callback", "", "Webhook URL for auth validation (for -auth-mode=callback)")
fs.StringVar(&cfg.authCallbackTimeout, "auth-callback-timeout", "5s", "Auth callback HTTP timeout")
```

Validation in `parseFlags`:
```go
switch cfg.authMode {
case "none":
    // ok
case "token":
    if len(authTokens) == 0 {
        return nil, errors.New("-auth-mode=token requires at least one -auth-token")
    }
case "file":
    if cfg.authFile == "" {
        return nil, errors.New("-auth-mode=file requires -auth-file")
    }
case "callback":
    if cfg.authCallbackURL == "" {
        return nil, errors.New("-auth-mode=callback requires -auth-callback")
    }
default:
    return nil, fmt.Errorf("invalid -auth-mode %q", cfg.authMode)
}
```

**Modify**: `cmd/rtmp-server/main.go`

Wire validator in `main()`:
```go
// Build auth validator
var authValidator auth.Validator
switch cfg.authMode {
case "token":
    tokens := make(map[string]string)
    for _, t := range cfg.authTokens {
        parts := strings.SplitN(t, "=", 2)
        if len(parts) != 2 {
            log.Error("invalid -auth-token format, expected streamKey=token", "value", t)
            os.Exit(2)
        }
        tokens[parts[0]] = parts[1]
    }
    authValidator = &auth.TokenValidator{Tokens: tokens}
case "file":
    var err error
    authValidator, err = auth.NewFileValidator(cfg.authFile)
    if err != nil {
        log.Error("failed to load auth file", "error", err)
        os.Exit(2)
    }
case "callback":
    timeout, _ := time.ParseDuration(cfg.authCallbackTimeout)
    if timeout == 0 {
        timeout = 5 * time.Second
    }
    authValidator = auth.NewCallbackValidator(cfg.authCallbackURL, timeout)
default: // "none"
    authValidator = &auth.AllowAllValidator{}
}

server := srv.New(srv.Config{
    // ... existing fields ...
    AuthValidator: authValidator,
})
```

---

### T013: Add Auth Hook Event

**Modify**: `internal/rtmp/server/hooks/events.go`

```go
const (
    // ... existing events ...
    EventAuthFailed EventType = "auth_failed"
)
```

---

### T014: Add SIGHUP Handler for File Reload

**Modify**: `cmd/rtmp-server/main.go`

If auth mode is `file`, listen for SIGHUP to reload tokens:
```go
if cfg.authMode == "file" {
    if fv, ok := authValidator.(*auth.FileValidator); ok {
        sighup := make(chan os.Signal, 1)
        signal.Notify(sighup, syscall.SIGHUP)
        go func() {
            for range sighup {
                if err := fv.Reload(); err != nil {
                    log.Error("auth file reload failed", "error", err)
                } else {
                    log.Info("auth file reloaded")
                }
            }
        }()
    }
}
```

---

## Server Behavior Specification

### onStatus Response Codes

| Code | Level | When |
|------|-------|------|
| `NetStream.Publish.Start` | `status` | Auth passed, publish accepted |
| `NetStream.Publish.Unauthorized` | `error` | Auth failed on publish |
| `NetStream.Play.Start` | `status` | Auth passed, play accepted |
| `NetStream.Play.Unauthorized` | `error` | Auth failed on play |
| `NetStream.Play.StreamNotFound` | `error` | Stream not found (existing) |

### Connection Behavior After Auth Failure

1. Server sends `onStatus` with error code
2. Server closes the TCP connection
3. Client receives error and can retry with correct credentials

### Logging

```
// Auth success
INFO "publish authenticated" stream_key=live/mystream remote_addr=192.168.1.100:54321

// Auth failure
WARN "publish authentication failed" stream_key=live/mystream remote_addr=192.168.1.100:54321 error="authentication failed: invalid credentials"
```

**Security**: Never log the full token value. Log `token_provided=true/false` at most.

---

## Task Order (Dependencies)

```
T001 (interface)
  ├── T002 (AllowAllValidator)
  ├── T003 (URL parser)
  ├── T004 (TokenValidator) ← depends on T001
  ├── T005 (FileValidator)  ← depends on T001
  └── T006 (CallbackValidator) ← depends on T001
T007 (AuthError) ← independent
T008 (RPC parser updates) ← depends on T003
T009 (connect params) ← independent
T010 (command_integration wiring) ← depends on T001, T008, T009, T011
T011 (server config) ← depends on T001
T012 (CLI flags) ← depends on T004, T005, T006
T013 (hook event) ← independent
T014 (SIGHUP reload) ← depends on T005, T012
```

### Recommended Implementation Order

1. **T001** → T002, T003, T007, T013 (foundations, parallel)
2. **T004, T005, T006** (validators, parallel)
3. **T008, T009** (RPC changes, parallel)
4. **T011** → **T010** (wiring)
5. **T012** → **T014** (CLI)
6. Integration tests
7. Interop tests (FFmpeg, OBS)

---

## Test Plan

### Unit Tests (per task, >90% coverage)

Each task file lists its specific test cases above.

### Integration Tests

**File**: `tests/integration/auth_test.go`

| Test | Setup | Action | Expected |
|------|-------|--------|----------|
| `TestPublish_NoAuth_Allowed` | `AuthValidator = nil` | Publish `live/test` | Success |
| `TestPublish_TokenAuth_ValidToken` | Token: `live/test=secret` | Publish `live/test?token=secret` | Success |
| `TestPublish_TokenAuth_WrongToken` | Token: `live/test=secret` | Publish `live/test?token=wrong` | `onStatus` error, connection closed |
| `TestPublish_TokenAuth_MissingToken` | Token: `live/test=secret` | Publish `live/test` | `onStatus` error, connection closed |
| `TestPlay_TokenAuth_ValidToken` | Token + publish | Play `live/test?token=secret` | Success |
| `TestPlay_TokenAuth_WrongToken` | Token + publish (no auth for play) | Play `live/test?token=wrong` | `onStatus` error |
| `TestPublish_FileAuth_ValidToken` | JSON file | Publish `live/test?token=file_secret` | Success |
| `TestPublish_FileAuth_Reload` | JSON file, then update | Reload + publish with new token | Success |
| `TestPublish_CallbackAuth_Allow` | httptest server returns 200 | Publish `live/test?token=any` | Success |
| `TestPublish_CallbackAuth_Deny` | httptest server returns 401 | Publish `live/test?token=any` | Error |
| `TestAuthFailedHookEvent` | Token auth + stdio hook | Publish with wrong token | `auth_failed` event emitted |

### Interop Tests (Manual + Script)

**File**: `tests/interop/test_auth.sh`

```bash
#!/bin/bash
# Test 1: Valid token → stream starts
ffmpeg -re -i test.mp4 -c copy -f flv "rtmp://localhost:1935/live/test?token=secret123" 2>&1 | head -5

# Test 2: Invalid token → connection rejected
ffmpeg -re -i test.mp4 -c copy -f flv "rtmp://localhost:1935/live/test?token=wrong" 2>&1 | head -5

# Test 3: No token → connection rejected
ffmpeg -re -i test.mp4 -c copy -f flv "rtmp://localhost:1935/live/test" 2>&1 | head -5
```

---

## CLI Usage Examples

```bash
# No auth (default, current behavior)
./rtmp-server -listen :1935

# Static tokens via CLI flags
./rtmp-server -listen :1935 \
  -auth-mode token \
  -auth-token "live/stream1=secret123" \
  -auth-token "live/camera1=cam_token_456"

# Token file
echo '{"live/stream1": "secret123", "live/stream2": "abc456"}' > tokens.json
./rtmp-server -listen :1935 \
  -auth-mode file \
  -auth-file tokens.json

# Webhook callback
./rtmp-server -listen :1935 \
  -auth-mode callback \
  -auth-callback https://auth.example.com/rtmp/validate \
  -auth-callback-timeout 5s
```

---

## Documentation Updates Required

After implementation:
1. Update `README.md` — move Authentication from Planned to Features, add auth flags to CLI section
2. Update `docs/getting-started.md` — add auth setup section
3. Create `docs/authentication.md` — full auth guide
4. Update `.github/copilot-instructions.md` — add auth package to architecture

---

## References

- [Nginx-RTMP on_publish](http://nginx-rtmp.blogspot.com/2013/06/on-publish.html) — webhook-based auth
- [OBS Studio Stream Key docs](https://obsproject.com/wiki/Getting-Started-With-OBS-Studio)
- [RTMP Spec - connect command](specs/001-rtmp-server-implementation/contracts/commands.md)
- [Current research decision](specs/001-rtmp-server-implementation/research.md#4-authentication-mechanism)

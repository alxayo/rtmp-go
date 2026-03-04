# Feature 008: RTMPS — TLS/SSL Encrypted Connections

**Feature**: 008-rtmps-tls  
**Status**: Draft  
**Date**: 2026-03-04  
**Branch**: `feature/008-rtmps-tls`

## Overview

Add optional TLS encryption support to the RTMP server and client, enabling
RTMPS (`rtmps://`) connections. TLS wraps the existing RTMP protocol
transparently — the RTMP handshake, chunk protocol, AMF commands, and media
messages remain identical; only the transport layer changes from raw TCP to
TLS-encrypted TCP.

### Design Constraints

- **Zero external dependencies** (stdlib `crypto/tls` only)
- **Backward-compatible**: plain RTMP (port 1935) remains the default
- **Opt-in via CLI flags**: `-tls-cert` and `-tls-key` enable RTMPS mode
- **Dual-mode support**: server can listen on both plain RTMP and RTMPS simultaneously
- **Client support**: relay client can connect to `rtmps://` destinations
- **Self-signed & CA-signed**: both certificate types are supported

---

## How RTMPS Works

RTMPS is simply RTMP inside a TLS tunnel. The connection flow:

```
Client (OBS, FFmpeg)                         Server
        │                                              │
        │  ──── 1. TCP Connect (port 1935s) ────────►  │
        │                                              │
        │  ──── 2. TLS ClientHello ─────────────────►  │
        │  ◄─── 3. TLS ServerHello + Certificate ────  │
        │  ──── 4. Key Exchange (ECDHE) ────────────►  │
        │  ◄─── 5. TLS Finished ─────────────────────  │
        │                                              │
        │        🔒 Encrypted tunnel established 🔒    │
        │                                              │
        │  ──── 6. RTMP Handshake (C0/C1/C2) ──────►  │  ┐
        │  ◄─── 7. RTMP Handshake (S0/S1/S2) ──────   │  │ All encrypted
        │  ──── 8. RTMP Connect + Publish ──────────►  │  │ inside TLS
        │  ──── 9. Audio/Video media ───────────────►  │  │
        │                                              │  ┘
```

Key points:
- TLS handshake happens **before** the RTMP handshake
- Go's `*tls.Conn` implements `net.Conn` — the entire RTMP stack works unchanged
- The server uses **one TLS certificate+key pair** for all connections
- Each connection gets unique **session keys** via ECDHE (forward secrecy)
- Default port for RTMPS is **443** (but configurable)

---

## Architecture

### Modified Files

| File | Change |
|------|--------|
| `internal/rtmp/server/server.go` | Add TLS fields to `Config`, create `tls.Listener` when TLS is configured |
| `internal/rtmp/server/server.go` | Support dual-mode: plain RTMP + RTMPS listeners |
| `internal/rtmp/client/client.go` | Detect `rtmps://` scheme, dial with `tls.Dial` instead of `net.Dial` |
| `internal/rtmp/relay/destination.go` | Accept `rtmps://` scheme in URL validation |
| `cmd/rtmp-server/flags.go` | Add `-tls-cert`, `-tls-key`, `-tls-listen` flags |
| `cmd/rtmp-server/main.go` | Wire TLS config from CLI flags to `server.Config` |
| `internal/errors/errors.go` | Add `TLSError` type |

### No New Packages Required

TLS support integrates directly into existing packages. The `crypto/tls` stdlib
package provides everything needed. No new internal packages are created.

---

## Detailed Design

### T001: Add TLS Configuration to Server Config

**File**: `internal/rtmp/server/server.go`

Add TLS fields to the existing `Config` struct:

```go
type Config struct {
    // ... existing fields unchanged ...

    // TLS configuration (optional). When both TLSCertFile and TLSKeyFile
    // are set, the server starts an additional RTMPS listener.
    TLSCertFile string // Path to TLS certificate file (PEM format)
    TLSKeyFile  string // Path to TLS private key file (PEM format)
    TLSListenAddr string // RTMPS listen address (default ":1935s" or ":443")
}
```

Add TLS defaults in `applyDefaults()`:

```go
func (c *Config) applyDefaults() {
    // ... existing defaults unchanged ...
    if c.TLSListenAddr == "" {
        c.TLSListenAddr = ":443"
    }
}
```

Add a validation helper:

```go
// TLSEnabled returns true when both cert and key paths are configured.
func (c *Config) TLSEnabled() bool {
    return c.TLSCertFile != "" && c.TLSKeyFile != ""
}
```

**Tests**:

| Scenario | Input | Expected |
|----------|-------|----------|
| TLS not configured | Cert="" Key="" | `TLSEnabled()` → false |
| Both set | Cert="a.crt" Key="a.key" | `TLSEnabled()` → true |
| Only cert set | Cert="a.crt" Key="" | `TLSEnabled()` → false |
| Only key set | Cert="" Key="a.key" | `TLSEnabled()` → false |
| Default TLS address | TLSListenAddr="" | After `applyDefaults()` → ":443" |

**Commit message**: `feat(server): add TLS configuration fields to server.Config`

---

### T002: Add TLSError to Error Package

**File**: `internal/errors/errors.go`

```go
// TLSError indicates a TLS configuration or handshake failure.
type TLSError struct {
    Op  string // operation (e.g. "load_cert", "tls_listen", "tls_handshake")
    Err error  // underlying cause
}

func (e *TLSError) Error() string {
    if e.Err == nil {
        return fmt.Sprintf("tls error: %s", e.Op)
    }
    return fmt.Sprintf("tls error: %s: %v", e.Op, e.Err)
}

func (e *TLSError) Unwrap() error { return e.Err }
func (e *TLSError) isProtocol()   {} // classified as protocol-layer

func NewTLSError(op string, err error) *TLSError {
    return &TLSError{Op: op, Err: err}
}
```

**Tests**:
- Error message formatting with and without wrapped error
- `errors.Unwrap()` returns inner error
- `errors.Is()` / `errors.As()` work correctly

**Commit message**: `feat(errors): add TLSError type for TLS-related failures`

---

### T003: Implement TLS Listener in Server

**File**: `internal/rtmp/server/server.go`

Add a TLS listener field to `Server`:

```go
type Server struct {
    // ... existing fields ...
    tlsListener net.Listener // RTMPS listener (nil when TLS disabled)
}
```

Modify `Start()` to optionally create a TLS listener alongside the plain one:

```go
func (s *Server) Start() error {
    // ... existing TCP listener setup (unchanged) ...

    // If TLS is configured, start a second listener for RTMPS
    if s.cfg.TLSEnabled() {
        cert, err := tls.LoadX509KeyPair(s.cfg.TLSCertFile, s.cfg.TLSKeyFile)
        if err != nil {
            // Close the already-opened plain listener before returning
            _ = s.l.Close()
            s.l = nil
            s.mu.Unlock()
            return fmt.Errorf("load TLS certificate: %w",
                rerrors.NewTLSError("load_cert", err))
        }

        tlsCfg := &tls.Config{
            Certificates: []tls.Certificate{cert},
            MinVersion:   tls.VersionTLS12,
        }

        tlsLn, err := tls.Listen("tcp", s.cfg.TLSListenAddr, tlsCfg)
        if err != nil {
            _ = s.l.Close()
            s.l = nil
            s.mu.Unlock()
            return fmt.Errorf("tls listen %s: %w", s.cfg.TLSListenAddr,
                rerrors.NewTLSError("tls_listen", err))
        }
        s.tlsListener = tlsLn
        s.log.Info("RTMPS server listening", "addr", tlsLn.Addr().String())

        s.acceptingWg.Add(1)
        go s.acceptLoop() // reuse existing acceptLoop with tlsListener
    }

    // ... existing acceptLoop launch for plain listener ...
}
```

**Key design decision**: Reuse the same `acceptLoop` logic. The `acceptLoop` method
receives connections from `listener.Accept()`. A `*tls.Conn` returned by a TLS
listener implements `net.Conn`, so the RTMP handshake, chunk reader/writer, and
all protocol logic work without any changes.

Refactor `acceptLoop` to accept a `net.Listener` parameter so both loops can
share the same code:

```go
// Before (current):
func (s *Server) acceptLoop() { ... uses s.l ... }

// After (refactored):
func (s *Server) acceptLoop(ln net.Listener, proto string) {
    defer s.acceptingWg.Done()
    for {
        raw, err := ln.Accept()
        if err != nil {
            // ... existing error handling unchanged ...
        }
        s.log.Debug("connection accepted", "proto", proto, "remote", raw.RemoteAddr())
        // ... rest of accept logic (handshake, register, etc.) unchanged ...
    }
}
```

Launch points:
- `go s.acceptLoop(s.l, "rtmp")` — plain listener
- `go s.acceptLoop(s.tlsListener, "rtmps")` — TLS listener

Modify `Stop()` to close the TLS listener:

```go
func (s *Server) Stop() error {
    // ... existing logic ...
    if s.tlsListener != nil {
        _ = s.tlsListener.Close()
        s.tlsListener = nil
    }
    // ... rest unchanged ...
}
```

**Tests** (unit):

| Scenario | Expected |
|----------|----------|
| Start with TLS disabled | Only plain listener created, `tlsListener` is nil |
| Start with invalid cert path | Returns `TLSError`, plain listener also cleaned up |
| Start with valid cert+key | Both listeners created, both addresses logged |
| Stop closes both listeners | Both `s.l` and `s.tlsListener` become nil |

**Tests** (integration — requires test certificates, see T006):

| Scenario | Expected |
|----------|----------|
| Connect via TLS listener | Full RTMP session works (handshake + publish) |
| Connect via plain listener while TLS is enabled | Still works |
| Invalid client cert verification (if mTLS) | Not required — server-only auth |

**Commit message**: `feat(server): add TLS listener for RTMPS connections`

---

### T004: Generate Test Certificates

**File**: `tests/testdata/tls/generate.go` (build-tag guarded)

Create a Go program that generates self-signed test certificates for use in
unit and integration tests. This avoids checking in binary cert files and
ensures reproducibility.

```go
//go:build ignore

package main

import (
    "crypto/ecdsa"
    "crypto/elliptic"
    "crypto/rand"
    "crypto/x509"
    "crypto/x509/pkix"
    "encoding/pem"
    "math/big"
    "os"
    "time"
)

func main() {
    key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

    template := &x509.Certificate{
        SerialNumber: big.NewInt(1),
        Subject:      pkix.Name{CommonName: "localhost"},
        NotBefore:    time.Now(),
        NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour),
        KeyUsage:     x509.KeyUsageDigitalSignature,
        ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
        DNSNames:     []string{"localhost"},
    }

    certDER, _ := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)

    certFile, _ := os.Create("tests/testdata/tls/server.crt")
    pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
    certFile.Close()

    keyDER, _ := x509.MarshalECPrivateKey(key)
    keyFile, _ := os.Create("tests/testdata/tls/server.key")
    pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
    keyFile.Close()
}
```

Also provide a **test helper function** that generates certificates in-memory
for unit tests (no filesystem dependency):

**File**: `internal/rtmp/server/tls_test_helper_test.go`

```go
package server

import (
    "crypto/ecdsa"
    "crypto/elliptic"
    "crypto/rand"
    "crypto/tls"
    "crypto/x509"
    "crypto/x509/pkix"
    "encoding/pem"
    "math/big"
    "os"
    "path/filepath"
    "testing"
    "time"
)

// generateTestCert creates a temporary self-signed cert+key pair on disk
// and returns the file paths. Files are cleaned up when the test finishes.
func generateTestCert(t *testing.T) (certPath, keyPath string) {
    t.Helper()
    dir := t.TempDir()

    key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
    if err != nil {
        t.Fatal(err)
    }

    template := &x509.Certificate{
        SerialNumber: big.NewInt(1),
        Subject:      pkix.Name{CommonName: "localhost"},
        NotBefore:    time.Now(),
        NotAfter:     time.Now().Add(time.Hour),
        KeyUsage:     x509.KeyUsageDigitalSignature,
        ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
        DNSNames:     []string{"localhost"},
    }

    certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
    if err != nil {
        t.Fatal(err)
    }

    certPath = filepath.Join(dir, "server.crt")
    keyPath = filepath.Join(dir, "server.key")

    certFile, _ := os.Create(certPath)
    pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
    certFile.Close()

    keyDER, _ := x509.MarshalECPrivateKey(key)
    keyFile, _ := os.Create(keyPath)
    pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
    keyFile.Close()

    return certPath, keyPath
}
```

**Commit message**: `test: add TLS test certificate generation helpers`

---

### T005: Add TLS CLI Flags

**File**: `cmd/rtmp-server/flags.go`

Add three new flags to `cliConfig` and `parseFlags()`:

```go
type cliConfig struct {
    // ... existing fields ...

    // TLS/RTMPS
    tlsCert   string // path to TLS certificate PEM file
    tlsKey    string // path to TLS private key PEM file
    tlsListen string // RTMPS listen address (e.g. ":443")
}
```

In `parseFlags()`:

```go
fs.StringVar(&cfg.tlsCert, "tls-cert", "", "Path to TLS certificate file (PEM). Enables RTMPS when set with -tls-key")
fs.StringVar(&cfg.tlsKey, "tls-key", "", "Path to TLS private key file (PEM). Enables RTMPS when set with -tls-cert")
fs.StringVar(&cfg.tlsListen, "tls-listen", ":443", "RTMPS listen address (default :443)")
```

Add validation after flag parsing:

```go
// Validate TLS flags: both must be set or both must be empty
if (cfg.tlsCert == "") != (cfg.tlsKey == "") {
    return nil, errors.New("-tls-cert and -tls-key must both be specified")
}

// Validate file existence when TLS is enabled
if cfg.tlsCert != "" {
    if _, err := os.Stat(cfg.tlsCert); err != nil {
        return nil, fmt.Errorf("TLS certificate file not found: %s", cfg.tlsCert)
    }
    if _, err := os.Stat(cfg.tlsKey); err != nil {
        return nil, fmt.Errorf("TLS key file not found: %s", cfg.tlsKey)
    }
}
```

**File**: `cmd/rtmp-server/main.go`

Wire TLS config into `srv.Config`:

```go
server := srv.New(srv.Config{
    // ... existing fields ...
    TLSCertFile:   cfg.tlsCert,
    TLSKeyFile:    cfg.tlsKey,
    TLSListenAddr: cfg.tlsListen,
})
```

**Tests** (`flags_test.go`):

| Scenario | Flags | Expected |
|----------|-------|----------|
| No TLS flags | (none) | TLS fields empty, no error |
| Both cert and key | `-tls-cert a.crt -tls-key a.key` | Both fields populated |
| Only cert | `-tls-cert a.crt` | Error: "must both be specified" |
| Only key | `-tls-key a.key` | Error: "must both be specified" |
| Cert file missing | `-tls-cert missing.crt -tls-key a.key` | Error: "not found" |
| Custom listen addr | `-tls-cert a -tls-key b -tls-listen :8443` | `tlsListen == ":8443"` |

**Commit message**: `feat(cli): add -tls-cert, -tls-key, -tls-listen flags for RTMPS`

---

### T006: Add RTMPS Client Support (for Relay)

**File**: `internal/rtmp/client/client.go`

Modify the `New()` constructor to accept `rtmps://` URLs:

```go
func New(rawurl string) (*Client, error) {
    // Accept both rtmp:// and rtmps://
    if !strings.HasPrefix(rawurl, "rtmp://") && !strings.HasPrefix(rawurl, "rtmps://") {
        return nil, fmt.Errorf("url must start with rtmp:// or rtmps://")
    }
    // ... rest of URL parsing unchanged ...
}
```

Modify `Connect()` to use `tls.Dial` for `rtmps://` URLs:

```go
func (c *Client) Connect() error {
    if c.conn != nil {
        return nil
    }

    host := c.url.Host
    useTLS := c.url.Scheme == "rtmps"

    if !strings.Contains(host, ":") {
        if useTLS {
            host = host + ":443"
        } else {
            host = host + ":1935"
        }
    }

    var conn net.Conn
    var err error

    if useTLS {
        d := &tls.Dialer{
            NetDialer: &net.Dialer{Timeout: DialTimeout},
        }
        conn, err = d.DialContext(context.Background(), "tcp", host)
    } else {
        d := net.Dialer{Timeout: DialTimeout}
        conn, err = d.Dial("tcp", host)
    }
    if err != nil {
        return fmt.Errorf("dial: %w", err)
    }

    c.conn = conn
    c.writer = chunk.NewWriter(conn, defaultChunkSize)
    c.reader = chunk.NewReader(conn, defaultChunkSize)

    // RTMP handshake proceeds identically over TLS — the TLS conn is transparent
    if err := handshake.ClientHandshake(conn); err != nil {
        _ = conn.Close()
        return err
    }

    // ... rest of Connect unchanged ...
}
```

**Note on certificate verification**: By default, `tls.Dialer` verifies the
server's certificate against the system's trusted CA pool. When relaying
to a server with a self-signed certificate, users would need to either:
1. Add the CA to the system trust store, or
2. We add a `-relay-tls-insecure` flag (future enhancement, not in this spec)

For this initial implementation, standard certificate verification is used.

**Tests** (unit):

| Scenario | URL | Expected |
|----------|-----|----------|
| `rtmp://` URL | `rtmp://host/app/stream` | Client created, plain dial |
| `rtmps://` URL | `rtmps://host/app/stream` | Client created, TLS dial |
| `https://` URL | `https://host/app/stream` | Error |
| `rtmps://` default port | `rtmps://host/app/stream` | Dials to `host:443` |
| `rtmps://` custom port | `rtmps://host:8443/app/stream` | Dials to `host:8443` |

**Commit message**: `feat(client): add RTMPS (tls.Dial) support for rtmps:// URLs`

---

### T007: Update Relay Destination to Accept rtmps:// URLs

**File**: `internal/rtmp/relay/destination.go`

Update the URL scheme validation in `NewDestination()`:

```go
func NewDestination(rawURL string, logger *slog.Logger, clientFactory RTMPClientFactory) (*Destination, error) {
    parsedURL, err := url.Parse(rawURL)
    if err != nil {
        return nil, fmt.Errorf("invalid destination URL: %w", err)
    }

    // Accept both rtmp:// and rtmps:// schemes
    if parsedURL.Scheme != "rtmp" && parsedURL.Scheme != "rtmps" {
        return nil, fmt.Errorf("destination URL must use rtmp:// or rtmps:// scheme, got %s", parsedURL.Scheme)
    }

    // ... rest unchanged ...
}
```

**File**: `cmd/rtmp-server/flags.go`

Update `validateRelayDestination()` to accept `rtmps://`:

```go
func validateRelayDestination(dest string) error {
    u, err := url.Parse(dest)
    if err != nil {
        return err
    }
    if u.Scheme != "rtmp" && u.Scheme != "rtmps" {
        return fmt.Errorf("scheme must be rtmp or rtmps, got %q", u.Scheme)
    }
    // ... rest unchanged ...
}
```

**Tests**:

| Scenario | URL | Expected |
|----------|-----|----------|
| `rtmp://` (existing) | `rtmp://cdn/live/key` | Valid |
| `rtmps://` (new) | `rtmps://cdn/live/key` | Valid |
| `http://` | `http://cdn/live/key` | Error: "must use rtmp:// or rtmps://" |

**Commit message**: `feat(relay): accept rtmps:// destination URLs`

---

### T008: Integration Tests for RTMPS

**File**: `tests/integration/tls_test.go`

Integration tests that spin up a server with TLS enabled and verify end-to-end
RTMPS connectivity:

```go
func TestRTMPS_PublishAndPlay(t *testing.T) {
    // 1. Generate test certificate
    certPath, keyPath := generateTestCert(t)

    // 2. Start server with TLS
    server := srv.New(srv.Config{
        ListenAddr:    "127.0.0.1:0",    // random port for plain RTMP
        TLSCertFile:   certPath,
        TLSKeyFile:    keyPath,
        TLSListenAddr: "127.0.0.1:0",    // random port for RTMPS
    })
    require.NoError(t, server.Start())
    defer server.Stop()

    // 3. Connect publisher via RTMPS
    tlsAddr := server.TLSAddr() // new method returning net.Addr
    pubURL := fmt.Sprintf("rtmps://localhost:%d/live/test", tlsAddr.Port)

    // Use custom TLS config to trust the self-signed cert
    pub, err := client.NewWithTLSConfig(pubURL, &tls.Config{
        InsecureSkipVerify: true, // accept self-signed cert in tests
    })
    require.NoError(t, err)
    require.NoError(t, pub.Connect())
    require.NoError(t, pub.Publish())

    // 4. Send test audio/video
    pub.SendAudio(0, testAudioPayload)
    pub.SendVideo(0, testVideoPayload)

    // 5. Connect subscriber via RTMPS and verify receipt
    // ... similar pattern ...
}
```

Additional test cases:

| Test | Description |
|------|-------------|
| `TestRTMPS_PublishAndPlay` | Full publish → subscribe cycle over TLS |
| `TestRTMPS_PlainAndTLS_Coexist` | Publish on plain RTMP, subscribe on RTMPS (and vice versa) |
| `TestRTMPS_InvalidCert_ServerFailsToStart` | Bad cert path → clean error |
| `TestRTMPS_Relay_ToTLSDestination` | Relay from plain RTMP to `rtmps://` destination |

**Commit message**: `test: add RTMPS integration tests`

---

### T009: Expose TLS Address from Server

**File**: `internal/rtmp/server/server.go`

Add a method to expose the TLS listener address (needed for tests and logging):

```go
// TLSAddr returns the RTMPS listener address, or nil if TLS is not enabled.
func (s *Server) TLSAddr() net.Addr {
    s.mu.RLock()
    defer s.mu.RUnlock()
    if s.tlsListener == nil {
        return nil
    }
    return s.tlsListener.Addr()
}
```

**Commit message**: `feat(server): expose TLSAddr() for RTMPS listener address`

---

### T010: Client TLS Config Option

**File**: `internal/rtmp/client/client.go`

Add a constructor variant that accepts a custom `*tls.Config` (needed for
tests with self-signed certs and for advanced relay configurations):

```go
// TLSConfig holds optional TLS settings for the client.
// When nil, the default system TLS configuration is used.
type TLSConfig = tls.Config

// NewWithTLSConfig creates a client with a custom TLS configuration.
// This is useful when connecting to servers with self-signed certificates
// or when specific TLS settings are required.
func NewWithTLSConfig(rawurl string, tlsCfg *tls.Config) (*Client, error) {
    c, err := New(rawurl)
    if err != nil {
        return nil, err
    }
    c.tlsConfig = tlsCfg
    return c, nil
}
```

Add `tlsConfig` field to `Client` struct:

```go
type Client struct {
    // ... existing fields ...
    tlsConfig *tls.Config // optional custom TLS config (nil = system defaults)
}
```

Update `Connect()` to use the custom config when dialing TLS:

```go
if useTLS {
    tlsCfg := c.tlsConfig
    if tlsCfg == nil {
        tlsCfg = &tls.Config{}
    }
    d := &tls.Dialer{
        NetDialer: &net.Dialer{Timeout: DialTimeout},
        Config:    tlsCfg,
    }
    conn, err = d.DialContext(context.Background(), "tcp", host)
}
```

**Commit message**: `feat(client): add NewWithTLSConfig for custom TLS settings`

---

### T011: Update Documentation and README

**Files to update**:

1. **`README.md`** — Move "RTMPS" from Planned to Features, add TLS flags to CLI section
2. **`quick-start.md`** — Add RTMPS quick-start section with self-signed cert example
3. **`.github/copilot-instructions.md`** — Note TLS support in architecture overview
4. **`docs/getting-started.md`** — Add TLS setup section

Example additions to README:

```markdown
### TLS/RTMPS (Encrypted Connections)

Generate a self-signed certificate for testing:
```bash
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 \
  -keyout server.key -out server.crt -days 365 -nodes \
  -subj "/CN=localhost"
```

Start the server with RTMPS enabled:
```bash
./rtmp-server -tls-cert server.crt -tls-key server.key -tls-listen :443
```

Publish over RTMPS:
```bash
ffmpeg -re -i test.mp4 -c copy -f flv rtmps://localhost:443/live/test
```

The server listens on both plain RTMP (:1935) and RTMPS (:443) simultaneously.
```

**Commit message**: `docs: add RTMPS setup guide and update README`

---

## Recommended Implementation Order

Tasks are ordered for progressive buildability. Each step produces a
meaningful, testable commit.

```
Phase 1: Foundations (independent, can be parallel)
├── T001: Add TLS config fields to server.Config
├── T002: Add TLSError to error package
└── T004: Generate test certificate helpers

Phase 2: Server TLS Support
├── T003: Implement TLS listener in server (depends on T001, T002)
└── T009: Expose TLSAddr() method (depends on T003)

Phase 3: Client TLS Support (independent of Phase 2)
├── T006: Add RTMPS client support (tls.Dial)
├── T010: Client TLS config option (depends on T006)
└── T007: Update relay destination URL validation (depends on T006)

Phase 4: CLI + Wiring
└── T005: Add TLS CLI flags and wire to config (depends on T001)

Phase 5: Integration & Verification
└── T008: Integration tests (depends on T003, T006, T004)

Phase 6: Documentation
└── T011: Update README, quick-start, docs
```

### Suggested Commit Sequence

| # | Task | Commit Message |
|---|------|----------------|
| 1 | T002 | `feat(errors): add TLSError type for TLS-related failures` |
| 2 | T001 | `feat(server): add TLS configuration fields to server.Config` |
| 3 | T004 | `test: add TLS test certificate generation helpers` |
| 4 | T003 | `feat(server): add TLS listener for RTMPS connections` |
| 5 | T009 | `feat(server): expose TLSAddr() for RTMPS listener address` |
| 6 | T006 | `feat(client): add RTMPS (tls.Dial) support for rtmps:// URLs` |
| 7 | T010 | `feat(client): add NewWithTLSConfig for custom TLS settings` |
| 8 | T007 | `feat(relay): accept rtmps:// destination URLs` |
| 9 | T005 | `feat(cli): add -tls-cert, -tls-key, -tls-listen flags for RTMPS` |
| 10 | T008 | `test: add RTMPS integration tests` |
| 11 | T011 | `docs: add RTMPS setup guide and update README` |

---

## Test Plan

### Unit Tests (per task)

Each task above lists its specific test table. All tests use the race detector
(`go test -race`).

### Integration Tests

**File**: `tests/integration/tls_test.go`

| Test | Setup | Action | Expected |
|------|-------|--------|----------|
| `TestRTMPS_PublishAndPlay` | Server w/ TLS + self-signed cert | Publish + subscribe via `rtmps://` | Audio/video received |
| `TestRTMPS_PlainAndTLS_Coexist` | Server w/ both listeners | Publish on RTMP, subscribe on RTMPS | Works both ways |
| `TestRTMPS_InvalidCert_ServerFailsToStart` | Bad cert path | `server.Start()` | Returns `TLSError` |
| `TestRTMPS_Relay_ToTLSDestination` | Server + relay to `rtmps://` | Publish to server | Relayed to RTMPS dest |
| `TestServer_NoCert_PlainOnly` | No TLS config | Start + connect | Works, `TLSAddr()` returns nil |

### Interop Tests (Manual)

**File**: `tests/interop/test_rtmps.sh`

```bash
#!/bin/bash
# Prerequisites: server.crt + server.key generated with openssl

# Start server with RTMPS
./rtmp-server -tls-cert server.crt -tls-key server.key -tls-listen :443 -log-level debug &
SERVER_PID=$!
sleep 1

# Test 1: Publish over RTMPS (FFmpeg)
echo "=== Test 1: RTMPS Publish ==="
timeout 5 ffmpeg -re -i test.mp4 -c copy -f flv \
  -tls_verify 0 rtmps://localhost:443/live/test 2>&1 | tail -3

# Test 2: Play over RTMPS (ffplay)
echo "=== Test 2: RTMPS Play ==="
timeout 5 ffplay -tls_verify 0 rtmps://localhost:443/live/test 2>&1 | tail -3

# Test 3: Mixed — publish plain RTMP, play RTMPS
echo "=== Test 3: Mixed Mode ==="
timeout 5 ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/mixed &
timeout 5 ffplay -tls_verify 0 rtmps://localhost:443/live/mixed 2>&1 | tail -3

kill $SERVER_PID
```

---

## Security Considerations

| Concern | Mitigation |
|---------|-----------|
| Private key protection | Document: never commit keys to version control; set `chmod 600` |
| Minimum TLS version | Enforce `tls.VersionTLS12` (TLS 1.0/1.1 are deprecated) |
| Weak cipher suites | Go's `crypto/tls` defaults exclude weak ciphers; no custom config needed |
| Certificate expiry | Document renewal process; suggest Let's Encrypt + certbot |
| Self-signed in production | Warn in docs; explain CA-signed certificates |
| Key file permissions | Validate at startup, log warning if world-readable |

---

## CLI Usage Examples

```bash
# Plain RTMP only (default, current behavior — unchanged)
./rtmp-server -listen :1935

# RTMPS only (still starts plain RTMP on default :1935)
./rtmp-server -tls-cert server.crt -tls-key server.key

# RTMPS on custom port
./rtmp-server -tls-cert server.crt -tls-key server.key -tls-listen :8443

# Dual mode: plain RTMP + RTMPS
./rtmp-server -listen :1935 -tls-cert server.crt -tls-key server.key -tls-listen :443

# With relay to an RTMPS destination
./rtmp-server -listen :1935 \
  -relay-to rtmps://cdn.example.com/live/stream_key

# Full production setup: TLS + auth + recording + relay
./rtmp-server -listen :1935 \
  -tls-cert /etc/letsencrypt/live/stream.example.com/fullchain.pem \
  -tls-key /etc/letsencrypt/live/stream.example.com/privkey.pem \
  -tls-listen :443 \
  -auth-mode file -auth-file tokens.json \
  -record-all \
  -relay-to rtmps://a.rtmp.youtube.com/live2/xxxx-xxxx-xxxx
```

---

## Out of Scope (Future Enhancements)

These items are explicitly NOT part of this feature but may be added later:

| Item | Reason |
|------|--------|
| Mutual TLS (mTLS) | Client certificate auth — complex, rarely used for RTMP |
| `-relay-tls-insecure` flag | Skip cert verification when relaying to self-signed servers |
| TLS certificate hot-reload | Reload cert/key without restart (SIGHUP) |
| ACME / Let's Encrypt auto-provisioning | Built-in certificate automation |
| TLS 1.3-only mode | Go defaults are secure enough; not worth a flag |
| SNI-based routing | Multiple certificates per server — needs virtual hosting |

---

## References

- [Go `crypto/tls` package](https://pkg.go.dev/crypto/tls) — standard library TLS
- [Let's Encrypt](https://letsencrypt.org/) — free CA-signed certificates
- [RTMP Specification — transport](specs/001-rtmp-server-implementation/spec.md) — base protocol
- [Feature 004: Token Auth](specs/004-token-auth/spec.md) — precedent for feature spec format

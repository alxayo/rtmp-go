---
title: "Testing Guide"
weight: 5
---

# Testing Guide

## Running Tests

### All Tests

```bash
go test ./...
```

### All Tests with Race Detector

```bash
go test -race ./...
```

The race detector is mandatory for CI. It catches data races that would otherwise be invisible.

### Package-Level Tests

Run tests for a specific protocol layer:

```bash
go test ./internal/rtmp/handshake/    # Handshake FSM
go test ./internal/rtmp/chunk/        # Chunk reader/writer
go test ./internal/rtmp/amf/          # AMF0 codec
go test ./internal/rtmp/control/      # Control messages
go test ./internal/rtmp/rpc/          # Command dispatch
go test ./internal/rtmp/conn/         # Connection lifecycle
go test ./internal/rtmp/server/       # Server integration
go test ./internal/rtmp/server/auth/  # Authentication
go test ./internal/rtmp/server/hooks/ # Event hooks
go test ./internal/rtmp/media/        # Media handling + recording
go test ./internal/rtmp/relay/        # Multi-destination relay
go test ./internal/rtmp/metrics/      # Expvar counters
go test ./internal/errors/            # Error types
```

### Integration Tests

```bash
go test ./tests/integration/ -count=1
```

The `-count=1` flag disables test caching, ensuring tests always run fresh.

## Golden Binary Vectors

Golden vectors live in `tests/golden/` as `.bin` files. Each file contains exact wire-format bytes for a specific protocol element:

- **Handshake packets** — C0, C1, S0, S1, C2, S2
- **Chunk headers** — FMT 0-3, various CSIDs, extended timestamps
- **AMF0 values** — Number, Boolean, String, Object, Null, Array
- **Control messages** — Set Chunk Size, Window Ack Size, Abort, etc.

Tests read these files and verify that:
1. **Decoding** the bytes produces the expected Go values
2. **Encoding** the Go values produces the exact same bytes

This catches endianness bugs, off-by-one errors, missing fields, and encoding mistakes.

### Regenerating Golden Vectors

If you change a wire format (rare), regenerate the golden files:

```bash
go run tests/golden/gen_*.go
```

> **Warning**: Only regenerate vectors if you intentionally changed the wire format. Accidentally regenerating them will mask bugs.

## Static Analysis

```bash
go vet ./...     # Static analysis
gofmt -l .       # Check formatting (should print nothing)
```

## Manual Interop Testing

For end-to-end validation with real RTMP clients:

### Basic Publish/Subscribe

Start the server:

```bash
go run ./cmd/rtmp-server -listen localhost:1935 -log-level debug
```

Publish a stream:

```bash
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test
```

Subscribe in another terminal:

```bash
ffplay rtmp://localhost:1935/live/test
```

### Late-Join Test

This verifies that sequence header caching works:

1. Start the server
2. Start publishing: `ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test`
3. Wait **10 seconds** for several keyframe cycles to pass
4. Start subscribing: `ffplay rtmp://localhost:1935/live/test`
5. **Expected**: Video should appear immediately (within 1-2 seconds), not after a long black screen

If the subscriber sees a black screen for more than a few seconds, sequence header caching may be broken.

### Relay Test

Test multi-destination relay between two servers:

Terminal 1 — origin server:
```bash
go run ./cmd/rtmp-server -listen localhost:1935 -relay-to rtmp://localhost:1936/live
```

Terminal 2 — edge server:
```bash
go run ./cmd/rtmp-server -listen localhost:1936
```

Terminal 3 — publish to origin:
```bash
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test
```

Terminal 4 — subscribe from edge:
```bash
ffplay rtmp://localhost:1936/live/test
```

## E2E Testing Scripts

The `scripts/` directory contains cross-platform E2E test scripts that validate the full streaming pipeline using FFmpeg, ffplay, and the go-rtmp server.

### Running the Full Suite

**Linux/macOS:**
```bash
./scripts/run-all-tests.sh
```

**Windows (PowerShell):**
```powershell
.\scripts\run-all-tests.ps1
```

### Test Cases

| Test | Validates |
|------|-----------|
| RTMP Publish + Capture | Basic publish → capture → ffprobe verify |
| RTMPS Publish + Capture | TLS connection with dual listener |
| RTMP + HLS via Hook | Publish hook triggers FFmpeg → HLS conversion |
| RTMPS + HLS via Hook | TLS transport with hook-based HLS |
| RTMP + Auth (allowed) | Token auth with valid credentials |
| RTMP + Auth (rejected) | Token auth rejects invalid/missing credentials |
| RTMPS + Auth | TLS + authentication combined |

Each test starts its own server instance on a unique port, runs the scenario, and cleans up on exit.

### Prerequisites

Run `scripts/check-deps.sh` (or `.ps1` on Windows) to verify that FFmpeg, ffplay, ffprobe, and the go-rtmp binary are available.

For full documentation on the scripts, see the [E2E Testing Guide]({{< relref "/docs/guides/e2e-testing" >}}).

## Test Coverage Map

| Area | Test Files | What's Verified |
|------|-----------|-----------------|
| Handshake | `handshake/*_test.go` | State machine transitions, C0/C1/C2 byte format, timeout handling |
| Chunks | `chunk/*_test.go` | FMT 0-3 encoding/decoding, extended timestamps, message reassembly |
| AMF0 | `amf/*_test.go` | All type markers, object end sentinel, round-trip encoding |
| Control | `control/*_test.go` | Set Chunk Size, Window Ack, Abort, Set Peer Bandwidth |
| RPC | `rpc/*_test.go` | connect, createStream, publish, play command parsing |
| Connection | `conn/*_test.go` | Full handshake + command flow over net.Pipe |
| Server | `server/*_test.go` | Registry, pub/sub fan-out, disconnect cleanup |
| Auth | `server/auth/*_test.go` | Token validation, file-based auth, callback auth |
| Hooks | `server/hooks/*_test.go` | Webhook delivery, shell execution, stdio format |
| Media | `media/*_test.go` | Audio/video parsing, codec detection, FLV writing |
| Relay | `relay/*_test.go` | Destination management, reconnection, late-join |
| Integration | `tests/integration/*_test.go` | End-to-end publish/subscribe through full stack |
| E2E Scripts | `scripts/test-e2e.*` | Full pipeline: publish, capture, HLS hooks, auth, TLS |

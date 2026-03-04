# Design Principles

This document explains the design decisions behind go-rtmp and the rules the codebase follows.

## Core Philosophy

**Correctness over features.** Every byte on the wire must match the RTMP specification. We implement a small set of features correctly rather than many features approximately.

**Simplicity over abstraction.** Each package does one thing. No framework magic, no dependency injection containers, no code generation. A beginner should be able to read any file and understand it in isolation.

**Standard library only.** Zero external dependencies. The entire server is built on Go's `net`, `io`, `encoding/binary`, `log/slog`, and `sync` packages. This eliminates supply-chain risk and makes the codebase easy to audit.

## Architecture Decisions

### Layered Protocol Stack

The RTMP protocol has natural layers, and the code mirrors them exactly:

```
TCP Connection
  └─ Handshake       (internal/rtmp/handshake)
      └─ Chunks      (internal/rtmp/chunk)
          └─ Control  (internal/rtmp/control)
          └─ Commands (internal/rtmp/rpc)
          └─ Media    (internal/rtmp/media)
```

Each layer only depends on the one below it. There are no circular imports. This means you can test the chunk layer without starting a server, or test AMF encoding without a network connection.

### One Goroutine per Direction

Each connection runs exactly two goroutines:
- **readLoop**: reads chunks from TCP, reassembles messages, dispatches to handler
- **writeLoop**: drains the outbound queue, fragments messages into chunks, writes to TCP

This avoids shared-state concurrency bugs. The readLoop owns all inbound state. The writeLoop owns the chunk.Writer. They communicate through a bounded channel (the outbound queue).

### Bounded Queues for Backpressure

The outbound message queue has a fixed size (100 messages). When a slow subscriber can't keep up:
1. The queue fills up
2. New sends block briefly (200ms timeout)
3. If still full, the message is dropped

This prevents a single slow viewer from consuming unbounded memory or blocking the publisher.

### Defensive Copying for Media Relay

When broadcasting media to multiple subscribers, each subscriber receives an independent copy of the message payload. This prevents a race condition where one subscriber's chunk writer could modify shared bytes while another subscriber is still reading them.

### Sequence Header Caching

The first audio and video messages from a publisher typically contain "sequence headers" — codec initialization data (H.264 SPS/PPS, AAC AudioSpecificConfig). The server caches these so that when a new subscriber joins mid-stream, it immediately receives the cached headers. Without this, the subscriber's decoder wouldn't know how to interpret the media data, resulting in a black screen until the next keyframe.

### Event Hooks

The server includes an event hook system that notifies external systems when lifecycle events occur. Available events:

- **connection_accept** / **connection_close**: Client connects or disconnects
- **publish_start** / **publish_stop**: Publisher begins or stops streaming
- **play_start** / **play_stop**: Subscriber starts or stops playback
- **codec_detected**: Audio/video codec identified from first media packet
- **subscriber_count**: Updated subscriber count when subscribers join or leave
- **auth_failed**: Authentication rejected for publish or play

Three hook types are supported:

- **Webhook**: HTTP POST with JSON event payload to a URL
- **Shell**: Execute a script with event data as environment variables
- **Stdio**: Print structured event data to stderr for log pipelines

Hooks execute asynchronously via a bounded concurrency pool (default: 10 workers) so they never block RTMP message processing. Each hook has a configurable timeout (default: 30 seconds).

### Token-Based Authentication

Authentication is enforced at the **publish/play command** level through a pluggable `auth.Validator` interface. Four built-in validators are provided:

- **TokenValidator**: In-memory map of streamKey → token pairs (from CLI flags)
- **FileValidator**: Loads tokens from a JSON file; supports live reload via SIGHUP
- **CallbackValidator**: Delegates to an external HTTP webhook (POST with JSON body)

Tokens are passed by clients via URL query parameters in the stream name field (e.g. `mystream?token=secret123`). This approach is compatible with all standard clients (OBS, FFmpeg, ffplay).

The default mode is `none` (accept all requests), preserving backward compatibility.

### Expvar Metrics

The server uses Go's `expvar` package for live monitoring. Expvar was chosen because:

- **Zero dependencies**: part of the standard library
- **Thread-safe**: `expvar.Int` uses atomic int64 internally
- **HTTP-ready**: registers a handler on `DefaultServeMux` at `/debug/vars`
- **JSON output**: human- and machine-readable

Metrics are organized as gauges (go up and down: active connections, publishers, subscribers, streams) and counters (monotonically increasing: total connections, media messages, bytes ingested, relay stats). The HTTP endpoint is opt-in via `-metrics-addr` so it has zero overhead when disabled.

### RTMPS (TLS) Support

RTMPS adds TLS encryption to the RTMP transport using Go's `crypto/tls` package (zero external dependencies). Key design choices:

- **Dual-mode listeners**: The server runs separate `net.Listener` (plain) and `tls.Listener` (encrypted) goroutines. Both feed into the same accept loop and share the same stream registry, so a plain publisher and a TLS subscriber can interact seamlessly.
- **Transparent wrapping**: `tls.Conn` implements `net.Conn`, so the entire RTMP protocol stack (handshake, chunking, commands, media) is unchanged — TLS only wraps the transport.
- **Minimum TLS 1.2**: Enforced via `tls.Config.MinVersion` to prevent downgrade attacks.
- **Client-side scheme detection**: The client automatically dials with TLS when the URL scheme is `rtmps://`, defaulting to port 443. Relay destinations also accept `rtmps://` URLs.
- **Cleanup on failure**: If TLS certificate loading fails during `Server.Start()`, the already-opened plain listener is closed to prevent resource leaks.

## Concurrency Model

| Resource | Protection | Why |
|----------|-----------|-----|
| Stream registry (map of streams) | `sync.RWMutex` | Multiple goroutines look up streams concurrently |
| Stream subscribers (slice) | Per-stream `sync.RWMutex` | Add/remove subscribers without blocking other streams |
| Connection outbound queue | Bounded channel | Lock-free producer/consumer between read and write loops |
| Write chunk size | `sync/atomic` | Updated by control burst, read by write loop |
| Media logger counters | `sync.RWMutex` | Updated by read loop, read by stats ticker |
| Hook execution pool | Buffered channel (semaphore) | Limits concurrent hook goroutines |

### TCP Deadline Enforcement

Each connection enforces TCP read/write deadlines to detect zombie connections:
- **Read deadline**: 90 seconds — closes connections from frozen or stalled publishers
- **Write deadline**: 30 seconds — drops connections to unresponsive subscribers

Deadlines are reset on each successful I/O operation, so normal streaming is unaffected. This prevents resource leaks (file descriptors, goroutines) from clients that hang without properly closing sockets.

### Graceful Shutdown

On shutdown signal (SIGINT/SIGTERM):
1. Server stops accepting new connections
2. Existing connections receive context cancellation
3. Relay client connections are closed to prevent dangling forwarding
4. If connections don't close within the timeout, the process exits forcefully

## Error Handling

Errors are classified by protocol layer using typed error wrappers:
- `HandshakeError` — connection setup failures
- `ChunkError` — framing/parsing issues
- `AMFError` — serialization failures
- `ProtocolError` — command/state violations
- `TLSError` — TLS configuration or handshake failures
- `TimeoutError` — deadline exceeded

Each error includes the operation name (e.g., "read C0+C1") for debuggability. Errors support Go's `errors.Is` / `errors.As` unwrapping.

## Logging Strategy

- **Info**: Connection lifecycle (accept, disconnect), command results (connect, publish, play), recording start/stop
- **Error**: Failures that lose data (write errors, handshake failures)
- **Debug**: Protocol details (only enabled during troubleshooting)

Media hot paths (readLoop, writeLoop, relay) produce **zero** log output at Info level per message. This is intentional — at 60fps video that would be 60 log lines per second per subscriber.

## Testing Strategy

- **Golden vectors**: Binary `.bin` files in `tests/golden/` contain exact wire-format bytes. Tests encode/decode against these to ensure bit-level protocol fidelity.
- **Table-driven tests**: Each protocol feature has parameterized test cases covering normal, edge, and error paths.
- **Integration tests**: Full server lifecycle tests in `tests/integration/` that exercise the end-to-end publisher → subscriber flow.
- **No mocks**: Tests use real `net.Pipe()` connections and real chunk readers/writers. This catches issues that mocks would hide.

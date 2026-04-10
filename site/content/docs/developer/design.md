---
title: "Design Principles"
weight: 2
---

# Design Principles

These principles guide every decision in the go-rtmp codebase. When in doubt, refer back to these.

## Correctness Over Features

Every byte on the wire must match the RTMP specification. We will not ship a feature that compromises protocol correctness. If there's a conflict between "works with most clients" and "matches the spec," the spec wins. Golden binary vectors enforce this — every encoder and decoder is tested against exact byte sequences.

## Simplicity Over Abstraction

Each package does one thing. There's no framework, no plugin system, no dependency injection container. If you need to understand how chunk parsing works, you read `internal/rtmp/chunk/` — nothing else. The code reads like the spec it implements.

## Standard Library Only

go-rtmp has **zero external dependencies**. Every import is from Go's standard library. This eliminates supply-chain risk, simplifies builds, and guarantees that `go build` works with nothing more than a Go toolchain.

## Concurrency Model

### One Goroutine Per Direction

Each connection runs a **readLoop** goroutine that reads from the TCP socket, parses chunks, and dispatches messages. Writes go through a bounded outbound queue. This keeps the concurrency model simple and predictable.

### Bounded Queues for Backpressure

The outbound message queue is bounded at **100 messages** with a **200ms write timeout**. If a subscriber can't keep up, messages are dropped rather than letting memory grow unbounded. This protects the server from slow consumers.

```go
outboundQueue := make(chan *chunk.Message, 100)
```

### Defensive Copying for Media Relay

When a media message is broadcast to multiple subscribers, each subscriber receives an **independent copy** of the payload. This prevents data races and ensures one subscriber's processing can't corrupt another's view of the data.

## Late-Join Support

When a subscriber connects to an active stream, they need codec initialization data (H.264 SPS/PPS, H.265 decoder config, AV1/VP9 config, AAC AudioSpecificConfig) to initialize their decoders. The server **caches sequence headers** from the publisher and replays them to every new subscriber immediately on join. This enables instant video playback without waiting for the next keyframe.

## Event Hooks Are Asynchronous

Hooks (webhooks, shell scripts, stdio) are fired **asynchronously** and never block RTMP processing. A slow webhook endpoint cannot stall media delivery. Hooks run with a configurable concurrency limit and timeout.

## TCP Deadline Enforcement

Every connection has strict TCP deadlines:
- **Read deadline: 90 seconds** — if no data arrives for 90s, the connection is considered dead
- **Write deadline: 30 seconds** — if a write blocks for 30s, the subscriber is too slow

Deadlines are reset on every successful I/O operation. This is the primary mechanism for **zombie detection** — connections that silently die (network failure, crashed client) are cleaned up automatically.

## Graceful Shutdown

The shutdown sequence follows a strict order:

1. **Stop accepting** new TCP connections
2. **Cancel context** to signal all goroutines
3. **Close relay clients** to stop forwarding
4. **Wait** for in-flight operations to complete
5. **Force exit** after timeout

This ensures active streams get a chance to flush before the process exits.

## Error Handling

Errors use **domain-specific wrappers** that carry context about which protocol layer failed:

```go
rerrors.NewHandshakeError("read C0+C1", err)
rerrors.NewChunkError("parse header", err)
rerrors.NewAMFError("decode.value", err)
rerrors.NewProtocolError("unexpected message type", err)
rerrors.NewTimeoutError("read deadline", err)
```

This makes log output immediately actionable — you can see at a glance whether a failure is in handshake, chunk parsing, AMF decoding, or connection management.

## Logging Strategy

**Zero log output per media message at Info level.** A 30fps video stream generates 30 video + 30 audio messages per second. Logging each one at Info would produce 60 log lines/second per stream — unusable in production.

Media-level logging is restricted to **Debug level only**. Info level logs connection lifecycle events (connect, publish, play, disconnect) and errors. This keeps production logs clean and useful.

```go
// Good: lifecycle events at Info
s.log.Info("publisher started", "stream_key", key, "conn_id", id)

// Good: media details at Debug only
s.log.Debug("video frame", "type_id", 9, "size", len(payload))
```

## Testing Philosophy

- **Golden binary vectors** — `.bin` files contain exact wire-format bytes. Tests decode these and verify the output matches expected values. This catches endianness bugs, off-by-one errors, and encoding mistakes.
- **Table-driven tests** — every test function iterates over a slice of test cases. Adding a new case is one line.
- **No mocks** — tests use `net.Pipe()` to create real in-memory TCP connections. The full protocol stack runs in tests, not a simulated version of it.
- **Race detector** — `go test -race` is mandatory. Every CI run includes it.

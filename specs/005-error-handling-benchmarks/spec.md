# Feature 005: Enhanced Error Handling, Graceful Connection Cleanup & Performance Benchmarks

**Feature**: 005-error-handling-benchmarks  
**Status**: Draft  
**Date**: 2026-03-03  
**Branch**: `feature/005-error-handling-benchmarks`

## Overview

Two complementary improvements to the RTMP server:

1. **Enhanced error handling and graceful connection cleanup** — wire the existing cleanup functions (`PublisherDisconnected`, `SubscriberDisconnected`, `MediaLogger.Stop`) into the connection lifecycle via a disconnect callback, and add TCP deadlines to prevent zombie connections.
2. **Performance benchmarks for chunk and AMF0 encode/decode** — add comprehensive `Benchmark*` functions for the two hottest code paths in the server (chunk framing and AMF0 serialization).

### Design Constraints

- **Zero external dependencies** (stdlib only — consistent with the rest of the project)
- **Backward-compatible**: no public API changes; existing behavior preserved for healthy connections
- **Minimal scope**: only the changes listed below; no refactoring beyond what's required

---

## Part 1: Enhanced Error Handling & Graceful Connection Cleanup

### Problem Statement

The codebase has well-structured domain-specific error types (`ProtocolError`, `HandshakeError`, `ChunkError`, `AMFError`, `TimeoutError`) and cleanup functions (`PublisherDisconnected()`, `SubscriberDisconnected()`, `MediaLogger.Stop()`). However, **none of the cleanup functions are called during the normal connection lifecycle**. When a client disconnects, the `readLoop` goroutine exits silently and no cleanup fires.

This causes:
- **Memory leak**: connections accumulate in `s.conns` forever (only cleared on server shutdown)
- **Stream key lockout**: stale publisher references block stream reuse (`ErrPublisherExists`)
- **Goroutine leak**: `MediaLogger` goroutine + `time.Ticker` per connection, never stopped
- **Subscriber waste**: `BroadcastMessage` keeps sending to dead subscribers (timeout per packet)
- **Unclosed recorders**: FLV files left unflushed/unclosed until server shutdown
- **Zombie connections**: no TCP deadlines means stuck connections hold resources indefinitely

### Issues Identified

| # | Severity | Issue | Location |
|---|----------|-------|----------|
| 1 | **Critical** | Connections never removed from `s.conns` on normal disconnect — memory leak | `server.go` ~L174 |
| 2 | **Critical** | `PublisherDisconnected()` never called in production — stale publishers block stream reuse | `publish_handler.go` |
| 3 | **Critical** | `SubscriberDisconnected()` never called in production — stale subscribers accumulate | `play_handler.go` |
| 4 | **Critical** | `MediaLogger.Stop()` never called — goroutine + ticker leak per connection | `command_integration.go` ~L58 |
| 5 | **High** | No disconnect cascade — `readLoop` exit doesn't trigger any cleanup | `conn.go` ~L120-140 |
| 6 | **High** | Recorder not closed on publisher disconnect (only on server shutdown) | `media_dispatch.go` |
| 7 | **High** | No `net.Conn` read/write deadlines — stuck TCP connections hold resources forever | `conn.go` |
| 8 | **Medium** | Relay `Destination.Connect()` leaks client on `Publish()` failure | `destination.go` ~L133 |
| 9 | **Medium** | `main.go` doesn't force-exit on shutdown timeout | `main.go` ~L92-97 |
| 10 | **Low** | `Session` type is dead code — field in struct, never initialized | `conn.go` |

### Root Cause

Issues #1–6 all stem from a single missing feature: **a connection `onDisconnect` callback** that fires when the `readLoop` exits.

---

### Implementation Plan

#### Phase 1: Connection Disconnect Callback (resolves #1–6)

##### 1.1 — Add disconnect handler to `Connection`

**File**: `internal/rtmp/conn/conn.go`

Add an `onDisconnect func()` field and a `SetDisconnectHandler(fn func())` method:

```go
type Connection struct {
    // ... existing fields ...
    onDisconnect func() // called once when readLoop exits
}

func (c *Connection) SetDisconnectHandler(fn func()) {
    c.onDisconnect = fn
}
```

Modify `startReadLoop()` to call the handler via `defer`, **before** `wg.Done()`:

```go
func (c *Connection) startReadLoop() {
    c.wg.Add(1)
    go func() {
        defer c.wg.Done()
        defer func() {
            // Trigger cleanup cascade: cancel context (stops writeLoop),
            // then invoke the disconnect handler.
            c.cancel()
            if c.onDisconnect != nil {
                c.onDisconnect()
            }
        }()
        r := chunk.NewReader(c.netConn, c.readChunkSize)
        for {
            select {
            case <-c.ctx.Done():
                return
            default:
            }
            msg, err := r.ReadMessage()
            if err != nil {
                if errors.Is(err, context.Canceled) || errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) {
                    return
                }
                c.log.Error("readLoop error", "error", err)
                return
            }
            if c.onMessage != nil {
                c.onMessage(msg)
            }
        }
    }()
}
```

Key behaviors:
- `c.cancel()` in the defer ensures the `writeLoop` also terminates.
- The `onDisconnect` callback runs **once**, after readLoop exit, regardless of exit reason.
- `wg.Done()` runs last (outermost defer), so `Close()` → `wg.Wait()` still works.

##### 1.2 — Add `RemoveConnection` to `Server`

**File**: `internal/rtmp/server/server.go`

Add a method for safe removal of a single connection from the tracking map:

```go
func (s *Server) RemoveConnection(id string) {
    s.mu.Lock()
    delete(s.conns, id)
    s.mu.Unlock()
}
```

##### 1.3 — Wire cleanup in `attachCommandHandling`

**File**: `internal/rtmp/server/command_integration.go`

After creating the `commandState`, install the disconnect handler on the connection. The handler must:

1. Stop the `MediaLogger` (goroutine + ticker cleanup)
2. If this connection was a publisher: call `PublisherDisconnected(streamKey)`, close the recorder, trigger `publish_stop` hook
3. If this connection was a subscriber: call `SubscriberDisconnected(streamKey, conn)`, trigger `play_stop` hook
4. Remove the connection from `s.conns`
5. Trigger `connection_close` hook event

```go
// Install disconnect handler — fires when readLoop exits for any reason
c.SetDisconnectHandler(func() {
    // 1. Stop media logger (prevents goroutine leak)
    st.mediaLogger.Stop()

    // 2. Publisher cleanup
    if st.streamKey != "" && st.role == "publisher" {
        stream := reg.Get(st.streamKey)
        if stream != nil {
            // Close recorder first (flush FLV)
            if stream.Recorder != nil {
                stream.Recorder.Close()
                stream.Recorder = nil
            }
            // Unregister publisher (allows stream key reuse)
            reg.PublisherDisconnected(st.streamKey)
        }
        // Fire publish_stop hook
        if srv != nil {
            srv.triggerHookEvent(hooks.EventPublishStop, c.ID(), st.streamKey, nil)
        }
    }

    // 3. Subscriber cleanup
    if st.streamKey != "" && st.role == "subscriber" {
        reg.SubscriberDisconnected(st.streamKey, c)
        if srv != nil {
            srv.triggerHookEvent(hooks.EventPlayStop, c.ID(), st.streamKey, nil)
        }
    }

    // 4. Remove from server connection tracking
    if srv != nil {
        srv.RemoveConnection(c.ID())
    }

    // 5. Connection close hook
    if srv != nil {
        srv.triggerHookEvent(hooks.EventConnectionClose, c.ID(), "", nil)
    }

    log.Info("connection disconnected", "conn_id", c.ID(), "stream_key", st.streamKey)
})
```

**Note**: The `commandState` struct will need a `role` field (set to `"publisher"` or `"subscriber"` by the `OnPublish` / `OnPlay` handlers) to know which cleanup path to take.

##### 1.4 — Add `role` field to `commandState`

**File**: `internal/rtmp/server/command_integration.go`

```go
type commandState struct {
    // ... existing fields ...
    role string // "publisher" or "subscriber" — set by OnPublish/OnPlay handlers
}
```

Set it in the existing `OnPublish` handler (after successful publish):
```go
st.role = "publisher"
```

Set it in the existing `OnPlay` handler (after successful play):
```go
st.role = "subscriber"
```

#### Phase 2: TCP Deadlines (resolves #7)

**File**: `internal/rtmp/conn/conn.go`

##### 2.1 — Read deadline in `readLoop`

Add read deadline constants and set deadlines before each read:

```go
const (
    readTimeout  = 90 * time.Second  // generous for idle subscribers
    writeTimeout = 30 * time.Second  // catches dead TCP peers
)
```

In `readLoop`, before each `ReadMessage()`:

```go
if tc, ok := c.netConn.(interface{ SetReadDeadline(time.Time) error }); ok {
    _ = tc.SetReadDeadline(time.Now().Add(readTimeout))
}
msg, err := r.ReadMessage()
```

##### 2.2 — Write deadline in `writeLoop`

In `writeLoop`, before each `WriteMessage()`:

```go
if tc, ok := c.netConn.(interface{ SetWriteDeadline(time.Time) error }); ok {
    _ = tc.SetWriteDeadline(time.Now().Add(writeTimeout))
}
if err := w.WriteMessage(msg); err != nil {
```

#### Phase 3: Minor Fixes (resolves #8, #9, #10)

##### 3.1 — Fix relay client leak on `Publish()` failure

**File**: `internal/rtmp/relay/destination.go`

In the `Connect()` method, close the client if `Publish()` fails:

```go
if err := d.client.Publish(streamKey); err != nil {
    d.client.Close() // prevent leak
    d.client = nil
    return fmt.Errorf("publish to %s: %w", d.url, err)
}
```

##### 3.2 — Force exit on shutdown timeout

**File**: `cmd/rtmp-server/main.go`

Change the timeout handler from a log-only to an actual exit:

```go
case <-shutdownCtx.Done():
    log.Error("forced exit after timeout")
    os.Exit(1)
```

##### 3.3 — Remove dead `session` field

**File**: `internal/rtmp/conn/conn.go`

Remove the `session *Session` field from the `Connection` struct (it's never initialized or used).

---

### Behavioral Summary (Part 1)

**Before**: Client connects → streams → disconnects → `readLoop` exits silently → connection stays in `s.conns` forever, publisher reference blocks stream reuse, `MediaLogger` goroutine ticks every 30s forever, FLV recorder left unclosed.

**After**: Client connects → streams → disconnects → `readLoop` defer fires → context canceled (stops `writeLoop`) → `onDisconnect` callback runs → MediaLogger stopped, publisher/subscriber unregistered, recorder closed, connection removed from tracking, hook events fired → all resources freed within seconds.

---

## Part 2: Performance Benchmarks

### Problem Statement

The chunk package — the single hottest code path in the server (every byte of audio/video flows through `Reader.ReadMessage()` and `Writer.WriteMessage()`) — has **zero benchmarks**. The AMF package has 8 benchmarks covering only primitive types (Number, Boolean, String, Null), but is missing benchmarks for Objects, Arrays, and the top-level `EncodeAll`/`DecodeAll` APIs used on every RTMP command.

Without benchmarks:
- Performance regressions from code changes go undetected
- Optimization efforts (e.g., the existing `bufpool` package) have no measurable before/after data
- There's no baseline to guide chunk size tuning or buffer allocation strategy

### Current Benchmark Coverage

| Package | Public APIs | Benchmarked | Coverage |
|---------|:-----------:|:-----------:|:--------:|
| `amf` | 18 functions | 8 (primitives only) | 44% |
| `chunk` | 13 functions/methods | **0** | **0%** |

### Existing AMF Benchmarks (8)

- `BenchmarkEncodeNull` / `BenchmarkDecodeNull` — `null_test.go`
- `BenchmarkEncodeNumber` / `BenchmarkDecodeNumber` — `number_test.go`
- `BenchmarkEncodeBoolean` / `BenchmarkDecodeBoolean` — `boolean_test.go`
- `BenchmarkEncodeString` / `BenchmarkDecodeString` — `string_test.go`

---

### Implementation Plan

All benchmarks will call `b.ReportAllocs()` to track allocation counts, providing data for future optimization.

#### File 1: `internal/rtmp/chunk/reader_test.go` — Chunk Read Benchmarks

| Benchmark | Description | Payload |
|-----------|-------------|---------|
| `BenchmarkParseChunkHeader_FMT0` | Full 12-byte header parsing | Pre-built header bytes |
| `BenchmarkParseChunkHeader_FMT1` | Delta 8-byte header (common for continuation) | Pre-built header bytes |
| `BenchmarkParseChunkHeader_FMT3` | Minimal 1-byte header (most common in media) | Pre-built header bytes |
| `BenchmarkReaderReadMessage_SingleChunk` | Message that fits in one chunk | 100-byte audio payload |
| `BenchmarkReaderReadMessage_MultiChunk` | Message spanning multiple chunks | 4096-byte video payload at 128-byte chunk size |

Implementation notes:
- Use `bytes.NewReader()` with pre-built raw chunk bytes (using existing `buildMessageBytes` helper).
- For multi-chunk benchmarks, pre-fragment the message at the benchmark's chunk size.
- Reset the reader on each iteration via `b.ResetTimer()` after setup.

#### File 2: `internal/rtmp/chunk/writer_test.go` — Chunk Write Benchmarks

| Benchmark | Description | Payload |
|-----------|-------------|---------|
| `BenchmarkEncodeChunkHeader_FMT0` | Header serialization (full) | Pre-built `ChunkHeader` struct |
| `BenchmarkWriterWriteMessage_SingleChunk` | Single-chunk message write | 100-byte audio payload |
| `BenchmarkWriterWriteMessage_MultiChunk` | Multi-chunk message write | 4096-byte video payload at 128-byte chunk size |
| `BenchmarkWriterReaderRoundTrip` | End-to-end Write→Read cycle | 4096-byte payload, measuring combined overhead |

Implementation notes:
- Write into `io.Discard` or `bytes.Buffer` (reset per iteration).
- For the round-trip benchmark, write into a `bytes.Buffer`, then read from it.

#### File 3: `internal/rtmp/amf/object_test.go` — Object Benchmarks

| Benchmark | Description | Data |
|-----------|-------------|------|
| `BenchmarkEncodeObject` | Encode a typical RTMP connect-style object | `{"app":"live","type":"nonprivate","flashVer":"FMLE/3.0","tcUrl":"rtmp://localhost/live"}` |
| `BenchmarkDecodeObject` | Decode the same object from pre-encoded bytes | Golden bytes from encoding |

Implementation notes:
- Use a realistic RTMP object (connect command fields) rather than a trivial one.
- Pre-encode the object once in benchmark setup, then decode in the loop.

#### File 4: `internal/rtmp/amf/array_test.go` — Array Benchmarks

| Benchmark | Description | Data |
|-----------|-------------|------|
| `BenchmarkEncodeStrictArray` | Encode a mixed-type array | `[1.0, "test", true, nil, 42.0]` |
| `BenchmarkDecodeStrictArray` | Decode the same array from pre-encoded bytes | Golden bytes from encoding |

#### File 5: `internal/rtmp/amf/amf_test.go` — Top-Level Codec Benchmarks

| Benchmark | Description | Data |
|-----------|-------------|------|
| `BenchmarkEncodeAll_ConnectCommand` | Multi-value encode simulating a full connect command | `["connect", 1.0, {app:"live", ...}]` |
| `BenchmarkDecodeAll_ConnectCommand` | Multi-value decode of the same connect command payload | Pre-encoded bytes |

Implementation notes:
- These are the most realistic benchmarks — they exercise the actual hot path for RTMP command processing.
- The connect command is chosen because it's the most complex command (string + number + large object).

---

### Running Benchmarks

```bash
# All benchmarks
go test -bench . -benchmem ./internal/rtmp/chunk/ ./internal/rtmp/amf/

# Chunk benchmarks only
go test -bench . -benchmem ./internal/rtmp/chunk/

# AMF benchmarks only (including existing ones)
go test -bench . -benchmem ./internal/rtmp/amf/

# Compare before/after with benchstat
go test -bench . -benchmem -count=10 ./internal/rtmp/chunk/ > old.txt
# ... make changes ...
go test -bench . -benchmem -count=10 ./internal/rtmp/chunk/ > new.txt
benchstat old.txt new.txt
```

### Expected Benchmark Output Format

```
BenchmarkParseChunkHeader_FMT0-8          5000000    234 ns/op    0 B/op    0 allocs/op
BenchmarkReaderReadMessage_SingleChunk-8  2000000    612 ns/op   256 B/op   3 allocs/op
BenchmarkWriterWriteMessage_MultiChunk-8  1000000   1430 ns/op   512 B/op   5 allocs/op
BenchmarkEncodeObject-8                   1000000   1102 ns/op   384 B/op   8 allocs/op
BenchmarkEncodeAll_ConnectCommand-8        500000   2340 ns/op   768 B/op  12 allocs/op
```

*(Numbers are illustrative; actual values depend on hardware.)*

---

## File Change Summary

| File | Part | Changes |
|------|------|---------|
| `internal/rtmp/conn/conn.go` | 1 | Add `onDisconnect` field, `SetDisconnectHandler()` method; modify `startReadLoop()` defer chain to call cancel + disconnect handler; add read/write deadlines; remove dead `session` field |
| `internal/rtmp/server/server.go` | 1 | Add `RemoveConnection(id)` method |
| `internal/rtmp/server/command_integration.go` | 1 | Add `role` field to `commandState`; install disconnect handler via `c.SetDisconnectHandler()` |
| `internal/rtmp/relay/destination.go` | 1 | Close client on `Publish()` failure in `Connect()` |
| `cmd/rtmp-server/main.go` | 1 | Add `os.Exit(1)` on shutdown timeout |
| `internal/rtmp/chunk/reader_test.go` | 2 | Add 5 chunk read benchmarks |
| `internal/rtmp/chunk/writer_test.go` | 2 | Add 4 chunk write benchmarks |
| `internal/rtmp/amf/object_test.go` | 2 | Add 2 object encode/decode benchmarks |
| `internal/rtmp/amf/array_test.go` | 2 | Add 2 array encode/decode benchmarks |
| `internal/rtmp/amf/amf_test.go` | 2 | Add 2 top-level codec benchmarks |

## Testing Strategy

### Part 1 — Error Handling & Cleanup

- **Unit tests**: Add tests for the disconnect callback mechanism in `conn_test.go` — verify the handler fires on EOF, on context cancel, and on read error.
- **Integration tests**: Extend existing integration tests in `tests/integration/` to verify:
  - After a publisher disconnects, the same stream key can be reused by a new publisher.
  - After a subscriber disconnects, `BroadcastMessage` no longer attempts to send to it.
  - `ConnectionCount()` returns 0 after all clients disconnect (not just after server shutdown).
- **Goroutine leak test**: Use `runtime.NumGoroutine()` before/after accepting and disconnecting N connections to verify no goroutine leak.

### Part 2 — Benchmarks

- Benchmarks are self-validating (they run the code under test).
- Verify all benchmarks pass: `go test -bench . -benchmem -count=1 ./internal/rtmp/chunk/ ./internal/rtmp/amf/`
- Verify no regressions in existing tests: `go test -race ./...`

## Acceptance Criteria

### Part 1
- [ ] When a client disconnects normally (EOF), the connection is removed from `s.conns`
- [ ] When a publisher disconnects, `PublisherDisconnected()` is called and the stream key becomes available
- [ ] When a subscriber disconnects, `SubscriberDisconnected()` is called
- [ ] `MediaLogger.Stop()` is called on every connection disconnect (no goroutine leak)
- [ ] FLV recorders are closed when the publishing connection disconnects
- [ ] Zombie TCP connections are reaped within 90 seconds (read deadline)
- [ ] `go test -race ./...` passes
- [ ] Relay client is properly closed on `Publish()` failure
- [ ] Server process exits on shutdown timeout

### Part 2
- [ ] Chunk package has benchmarks for header parsing (FMT0, FMT1, FMT3), single-chunk read/write, and multi-chunk read/write
- [ ] AMF package has benchmarks for Object and Array encode/decode
- [ ] AMF package has benchmarks for top-level `EncodeAll`/`DecodeAll` with realistic RTMP command data
- [ ] All benchmarks use `b.ReportAllocs()`
- [ ] `go test -bench . -benchmem ./internal/rtmp/chunk/ ./internal/rtmp/amf/` completes successfully

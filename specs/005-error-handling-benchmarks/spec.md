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
| 1 | **Critical** | Connections never removed from `s.conns` on normal disconnect — memory leak | `server.go` L175 (`s.conns[c.ID()] = c`), only `clear(s.conns)` in `Stop()` |
| 2 | **Critical** | `PublisherDisconnected()` never called — stale publishers block stream reuse | `publish_handler.go` L65-78 (function exists, never invoked) |
| 3 | **Critical** | `SubscriberDisconnected()` never called — stale subscribers accumulate | `play_handler.go` L141-151 (function exists, never invoked) |
| 4 | **Critical** | `MediaLogger.Stop()` never called — goroutine + ticker leak per connection | `command_integration.go` L58 (`NewMediaLogger` creates goroutine; `Stop()` at `media_logger.go` L156 never called) |
| 5 | **High** | No disconnect cascade — `readLoop` exit doesn't trigger any cleanup | `conn.go` L115-135 (goroutine exits, no defer chain) |
| 6 | **High** | Recorder not closed on publisher disconnect (only on server shutdown) | `server.go` L283-307 (`cleanupAllRecorders` only runs in `Stop()`) |
| 7 | **High** | No `net.Conn` read/write deadlines — stuck TCP connections hold resources forever | `conn.go` (no `SetReadDeadline`/`SetWriteDeadline` calls anywhere) |
| 8 | **Medium** | Relay `Destination.Connect()` leaks client on `Connect()` and `Publish()` failure | `destination.go` L130-145 (client created by factory, never closed on error path) |
| 9 | **Medium** | `main.go` doesn't force-exit on shutdown timeout — falls through to end of `main()` | `main.go` L104 (`log.Error` only, no `os.Exit(1)`) |
| 10 | **Low** | `Session` type is entirely dead code — field in Connection struct never initialized; `session.go` + `session_test.go` completely unused | `conn.go` L55, `session.go`, `session_test.go` |

### Root Cause

Issues #1–6 all stem from a single missing feature: **a connection `onDisconnect` callback** that fires when the `readLoop` exits.

### Code Verification Summary

The following API facts were verified against actual source (not assumed):

| Item | Actual Signature / Location |
|------|----------------------------|
| `PublisherDisconnected` | Package-level func: `PublisherDisconnected(reg *Registry, streamKey string, pub sender)` — `publish_handler.go` L65 |
| `SubscriberDisconnected` | Package-level func: `SubscriberDisconnected(reg *Registry, streamKey string, sub sender)` — `play_handler.go` L141 |
| `MediaLogger.Stop()` | Method on `*MediaLogger`: uses `sync.Once`, closes `stopChan`, stops ticker — `media_logger.go` L156-161 |
| `Registry.GetStream` | Method: `(r *Registry) GetStream(key string) *Stream` — `registry.go` L101 |
| `Stream.Recorder` | Field: `Recorder *media.Recorder` — `registry.go` L54; protected by `stream.mu sync.RWMutex` |
| `attachCommandHandling` | `func attachCommandHandling(c *iconn.Connection, reg *Registry, cfg *Config, log *slog.Logger, destMgr *relay.DestinationManager, srv ...*Server)` — `command_integration.go` L52 |
| `sender` interface | `type sender interface { SendMessage(*chunk.Message) error }` — `publish_handler.go` L22; `*iconn.Connection` satisfies this |
| `net.Conn` | Already has `SetReadDeadline(time.Time) error` and `SetWriteDeadline(time.Time) error` — no type assertion needed |
| `Session` usage | `session *Session` field in Connection struct (L55); `NewSession()` only called in `session_test.go`; zero production references |

---

### Implementation Plan

#### Phase 1: Connection Disconnect Callback (resolves #1–6)

##### Step 1.1 — Add disconnect handler to `Connection`

**File**: `internal/rtmp/conn/conn.go`

**Change A** — Add `onDisconnect` field to `Connection` struct (after `onMessage` field, L57):

```go
type Connection struct {
    // ... existing fields ...
    onMessage    func(*chunk.Message) // test hook / dispatcher injection
    onDisconnect func()               // called once when readLoop exits (cleanup cascade)
}
```

**Change B** — Add `SetDisconnectHandler` method (after `SetMessageHandler`, ~L76):

```go
// SetDisconnectHandler installs a callback invoked once when the readLoop
// exits (for any reason: EOF, error, context cancel). MUST be called before Start().
func (c *Connection) SetDisconnectHandler(fn func()) { c.onDisconnect = fn }
```

**Change C** — Modify `startReadLoop()` (L115-135) to add a defer chain that cancels context and invokes the disconnect handler. Also add handling for `net.Error` timeout errors:

```go
// startReadLoop begins the dechunk → dispatch loop.
func (c *Connection) startReadLoop() {
    c.wg.Add(1)
    go func() {
        defer c.wg.Done()
        defer func() {
            // Cleanup cascade: cancel context first (stops writeLoop via ctx.Done()),
            // then invoke the disconnect handler for higher-level cleanup.
            // cancel() is idempotent — safe if Close() already called it.
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
                // Normal disconnect paths — exit silently
                if errors.Is(err, context.Canceled) || errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) {
                    return
                }
                // Timeout from read deadline — connection is dead
                var netErr net.Error
                if errors.As(err, &netErr) && netErr.Timeout() {
                    c.log.Warn("readLoop timeout (zombie connection reaped)")
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

**Defer ordering rationale**: Go defers execute LIFO. `wg.Done()` is the outermost defer (registered first), so it runs *last*. The cleanup defer runs *before* `wg.Done()`, ensuring all cleanup completes before `Close()` → `wg.Wait()` returns.

**Idempotency**: `c.cancel()` is called both in the defer chain AND in `Close()`. This is safe because `context.CancelFunc` is documented as idempotent.

##### Step 1.2 — Add `RemoveConnection` to `Server`

**File**: `internal/rtmp/server/server.go`

Add method after `ConnectionCount()` (~L234):

```go
// RemoveConnection removes a single connection from the tracking map.
// Called by the disconnect handler when a connection's readLoop exits.
func (s *Server) RemoveConnection(id string) {
    s.mu.Lock()
    delete(s.conns, id)
    s.mu.Unlock()
}
```

##### Step 1.3 — Add `role` field to `commandState`

**File**: `internal/rtmp/server/command_integration.go`

Add `role` field to the `commandState` struct (after `codecDetector`, ~L40):

```go
type commandState struct {
    app           string                 // application name from the connect command (e.g. "live")
    streamKey     string                 // current stream key (e.g. "live/mystream")
    connectParams map[string]interface{} // extra fields from connect command object (for auth context)
    allocator     *rpc.StreamIDAllocator // assigns unique message stream IDs for createStream
    mediaLogger   *MediaLogger           // tracks audio/video packet statistics
    codecDetector *media.CodecDetector   // identifies audio/video codecs on first packets
    role          string                 // "publisher" or "subscriber" — set by OnPublish/OnPlay handlers
}
```

##### Step 1.4 — Set `role` in OnPublish handler

**File**: `internal/rtmp/server/command_integration.go`

In the `d.OnPublish` handler, add `st.role = "publisher"` after `st.streamKey = pc.StreamKey` (~L106):

```go
        // Track stream key for this connection
        st.streamKey = pc.StreamKey
        st.role = "publisher"
```

##### Step 1.5 — Set `role` in OnPlay handler

**File**: `internal/rtmp/server/command_integration.go`

In the `d.OnPlay` handler, add `st.role = "subscriber"` after `st.streamKey = pl.StreamKey` (~L137):

```go
        // Track stream key for this connection
        st.streamKey = pl.StreamKey
        st.role = "subscriber"
```

##### Step 1.6 — Install disconnect handler in `attachCommandHandling`

**File**: `internal/rtmp/server/command_integration.go`

After creating `commandState` and before `d := rpc.NewDispatcher(...)` (~L59), install the disconnect handler.

**Important API notes for implementer**:
- `PublisherDisconnected` and `SubscriberDisconnected` are **package-level functions** (not methods on `Registry`). Call: `PublisherDisconnected(reg, streamKey, c)`.
- `Registry.GetStream(key)` returns `*Stream` (not `Registry.Get()`).
- `stream.Recorder` access must be protected by `stream.mu.Lock()` (concurrent access with `cleanupAllRecorders` during server shutdown).
- `srv` is variadic `...*Server` — check `len(srv) > 0 && srv[0] != nil`.
- `*iconn.Connection` satisfies the `sender` interface (has `SendMessage(*chunk.Message) error`).

```go
    // Install disconnect handler — fires when readLoop exits for any reason.
    // Captures: st, reg, c, log, srv (all from enclosing scope via closure).
    c.SetDisconnectHandler(func() {
        // 1. Stop media logger (prevents goroutine + ticker leak)
        st.mediaLogger.Stop()

        // 2. Publisher cleanup: close recorder, unregister publisher, fire hook
        if st.streamKey != "" && st.role == "publisher" {
            stream := reg.GetStream(st.streamKey)
            if stream != nil {
                // Close recorder under lock (concurrent with cleanupAllRecorders)
                stream.mu.Lock()
                if stream.Recorder != nil {
                    if err := stream.Recorder.Close(); err != nil {
                        log.Error("recorder close error on disconnect", "error", err, "stream_key", st.streamKey)
                    }
                    stream.Recorder = nil
                }
                stream.mu.Unlock()
                // Unregister publisher (allows stream key reuse by new publisher)
                PublisherDisconnected(reg, st.streamKey, c)
            }
            if len(srv) > 0 && srv[0] != nil {
                srv[0].triggerHookEvent(hooks.EventPublishStop, c.ID(), st.streamKey, nil)
            }
        }

        // 3. Subscriber cleanup: unregister subscriber, fire hook
        if st.streamKey != "" && st.role == "subscriber" {
            SubscriberDisconnected(reg, st.streamKey, c)
            if len(srv) > 0 && srv[0] != nil {
                srv[0].triggerHookEvent(hooks.EventPlayStop, c.ID(), st.streamKey, nil)
            }
        }

        // 4. Remove from server connection tracking (fixes memory leak)
        if len(srv) > 0 && srv[0] != nil {
            srv[0].RemoveConnection(c.ID())
        }

        // 5. Fire connection close hook
        if len(srv) > 0 && srv[0] != nil {
            srv[0].triggerHookEvent(hooks.EventConnectionClose, c.ID(), "", nil)
        }

        log.Info("connection disconnected", "conn_id", c.ID(), "stream_key", st.streamKey, "role", st.role)
    })
```

**Shutdown race safety**: During server shutdown, `Stop()` calls `c.Close()` for each connection, then `cleanupAllRecorders()`. The disconnect handler may also try to close recorders. This is safe because:
1. Both paths lock `stream.mu` before accessing `stream.Recorder`.
2. The first path to close sets `stream.Recorder = nil`.
3. The second path sees `nil` and skips — no double-close.

#### Phase 2: TCP Deadlines (resolves #7)

**File**: `internal/rtmp/conn/conn.go`

##### Step 2.1 — Add deadline constants

Add after the existing `outboundQueueSize` constant (~L31):

```go
const (
    sendTimeout       = 200 * time.Millisecond
    outboundQueueSize = 100

    // TCP deadlines for zombie connection detection.
    // readTimeout is generous to accommodate idle subscribers that receive
    // no data when no publisher is active. Publishers send data continuously
    // (~30fps) so any timeout > a few seconds catches dead peers.
    readTimeout  = 90 * time.Second
    // writeTimeout catches dead TCP peers that never acknowledge writes.
    writeTimeout = 30 * time.Second
)
```

##### Step 2.2 — Read deadline in `readLoop`

In `startReadLoop()`, before each `ReadMessage()` call, set a read deadline. `net.Conn` already has `SetReadDeadline` — no type assertion is needed:

```go
            _ = c.netConn.SetReadDeadline(time.Now().Add(readTimeout))
            msg, err := r.ReadMessage()
```

The timeout error is already handled in Step 1.1 via `errors.As(err, &netErr) && netErr.Timeout()`.

##### Step 2.3 — Write deadline in `writeLoop`

In `startWriteLoop()` (L138), before each `WriteMessage()` call, set a write deadline:

```go
            case msg, ok := <-c.outboundQueue:
                if !ok {
                    return
                }
                currentChunkSize := atomic.LoadUint32(&c.writeChunkSize)
                w.SetChunkSize(currentChunkSize)
                _ = c.netConn.SetWriteDeadline(time.Now().Add(writeTimeout))
                if err := w.WriteMessage(msg); err != nil {
                    c.log.Error("writeLoop write failed", "error", err)
                    return
                }
```

#### Phase 3: Minor Fixes (resolves #8, #9, #10)

##### Step 3.1 — Fix relay client leak in `Destination.Connect()`

**File**: `internal/rtmp/relay/destination.go`

There are **two** leak paths in `Connect()` (L110-149):

**Leak 1**: After `d.clientFactory(d.URL)` creates a client, if `client.Connect()` fails, the client is not closed. The factory may allocate TCP resources.

**Leak 2**: After `client.Connect()` succeeds, if `client.Publish()` fails, the connected client is not closed.

Fix both by closing the client on each error path:

```go
func (d *Destination) Connect() error {
    d.mu.Lock()
    defer d.mu.Unlock()

    if d.Status == StatusConnected {
        return nil
    }

    d.Status = StatusConnecting
    d.logger.Info("Connecting to destination")

    client, err := d.clientFactory(d.URL)
    if err != nil {
        d.Status = StatusError
        d.LastError = err
        d.logger.Error("Failed to create RTMP client", "error", err)
        return fmt.Errorf("create client: %w", err)
    }

    if err := client.Connect(); err != nil {
        _ = client.Close() // prevent leak: factory may have allocated TCP resources
        d.Status = StatusError
        d.LastError = err
        d.logger.Error("Failed to connect RTMP client", "error", err)
        return fmt.Errorf("client connect: %w", err)
    }

    if err := client.Publish(); err != nil {
        _ = client.Close() // prevent leak: connection established but publish failed
        d.Status = StatusError
        d.LastError = err
        d.logger.Error("Failed to publish to destination", "error", err)
        return fmt.Errorf("client publish: %w", err)
    }

    d.Client = client
    d.Status = StatusConnected
    d.Metrics.ConnectTime = time.Now()
    d.LastError = nil
    d.logger.Info("Connected to destination")
    return nil
}
```

##### Step 3.2 — Force exit on shutdown timeout

**File**: `cmd/rtmp-server/main.go`

At L104, after `log.Error("forced exit after timeout")`, add `os.Exit(1)`:

```go
    case <-shutdownCtx.Done():
        log.Error("forced exit after timeout")
        os.Exit(1)
```

##### Step 3.3 — Remove dead `Session` code

**Three files to modify:**

**File A**: `internal/rtmp/conn/conn.go` — Remove the `session *Session` field from the `Connection` struct (L55). The field is never initialized or assigned anywhere in production code.

**File B**: `internal/rtmp/conn/session.go` — **Delete entire file**. Contains `Session` struct, `SessionState` enum, `NewSession()`, and methods. None are used in production — `NewSession()` is only called in `session_test.go`.

**File C**: `internal/rtmp/conn/session_test.go` — **Delete entire file**. Tests for the dead `Session` type.

---

### Behavioral Summary (Part 1)

**Before**: Client connects → streams → disconnects → `readLoop` exits silently → connection stays in `s.conns` forever, publisher reference blocks stream reuse, `MediaLogger` goroutine ticks every 30s forever, FLV recorder left unclosed.

**After**: Client connects → streams → disconnects → `readLoop` defer fires → context canceled (stops `writeLoop`) → `onDisconnect` callback runs → MediaLogger stopped, publisher/subscriber unregistered, recorder closed, connection removed from tracking, hook events fired → all resources freed within seconds.

**Forced-close path** (server shutdown): `Stop()` → `c.Close()` → `cancel()` triggers readLoop exit → defer fires `onDisconnect` → cleanup runs → `cleanupAllRecorders()` runs (sees `Recorder == nil` for already-cleaned connections, skips) → safe.

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

### Existing Test Helpers Available for Benchmarks

| Helper | File | Description |
|--------|------|-------------|
| `buildMessageBytes(t, csid, ts, msgType, msid, payload)` | `reader_test.go` L31 | Constructs FMT0 single-chunk message bytes; uses `EncodeChunkHeader` |
| `loadGoldenChunk(t, name)` | `reader_test.go` L24 | Loads golden binary test data from `tests/golden/` |
| `loadGoldenHeader(t, name, headerLen)` | `writer_test.go` L26 | Loads first N bytes from golden header file |
| `simpleWriter` struct | `writer_test.go` L151 | Wraps `bytes.Buffer` as `io.Writer` for test captures |

Note: `buildMessageBytes` uses `*testing.T` (not `*testing.B`). For benchmarks, we need a variant that accepts `testing.TB` or build data directly without a test helper.

---

### Implementation Plan

All benchmarks will call `b.ReportAllocs()` to track allocation counts, providing data for future optimization.

#### File 1: `internal/rtmp/chunk/reader_test.go` — Chunk Read Benchmarks

| Benchmark | Description | Setup |
|-----------|-------------|-------|
| `BenchmarkParseChunkHeader_FMT0` | Full 12-byte header parsing via `ParseChunkHeader()` | Pre-encode FMT0 header bytes using `EncodeChunkHeader`; wrap in `bytes.NewReader` per iteration |
| `BenchmarkParseChunkHeader_FMT1` | Delta 8-byte header (common for audio continuation) | Pre-encode FMT1 header bytes with a `prev` header; wrap in `bytes.NewReader` per iteration |
| `BenchmarkParseChunkHeader_FMT3` | Minimal 1-byte header (most common in media streams) | Pre-encode FMT3 header bytes with a `prev` header; wrap in `bytes.NewReader` per iteration |
| `BenchmarkReaderReadMessage_SingleChunk` | Message that fits in one chunk via `Reader.ReadMessage()` | Build FMT0 message bytes (100-byte audio payload, CSID=4, TypeID=8); create new `Reader` per iteration |
| `BenchmarkReaderReadMessage_MultiChunk` | Message spanning multiple chunks via `Reader.ReadMessage()` | Write a 4096-byte video message (TypeID=9) using `Writer` at 128-byte chunk size; read back per iteration |

Implementation approach:
```go
func BenchmarkParseChunkHeader_FMT0(b *testing.B) {
    b.ReportAllocs()
    h := &ChunkHeader{FMT: 0, CSID: 4, Timestamp: 1000, MessageLength: 100, MessageTypeID: 8, MessageStreamID: 1}
    raw, _ := EncodeChunkHeader(h, nil)
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        r := bytes.NewReader(raw)
        _, _ = ParseChunkHeader(r, nil)
    }
}

func BenchmarkReaderReadMessage_MultiChunk(b *testing.B) {
    b.ReportAllocs()
    // Pre-fragment: write a 4096-byte message via Writer, capture bytes
    payload := make([]byte, 4096)
    var buf bytes.Buffer
    w := NewWriter(&buf, 128) // 128-byte chunk size → 32+ chunks
    msg := &Message{CSID: 6, Timestamp: 0, MessageLength: 4096, TypeID: 9, MessageStreamID: 1, Payload: payload}
    _ = w.WriteMessage(msg)
    data := buf.Bytes()
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        r := NewReader(bytes.NewReader(data), 128)
        _, _ = r.ReadMessage()
    }
}
```

#### File 2: `internal/rtmp/chunk/writer_test.go` — Chunk Write Benchmarks

| Benchmark | Description | Setup |
|-----------|-------------|-------|
| `BenchmarkEncodeChunkHeader_FMT0` | Header serialization via `EncodeChunkHeader()` | Pre-build `ChunkHeader` struct; call repeatedly |
| `BenchmarkWriterWriteMessage_SingleChunk` | Single-chunk message via `Writer.WriteMessage()` | 100-byte audio payload, write to `io.Discard`; new `Writer` per iteration to avoid FMT compression |
| `BenchmarkWriterWriteMessage_MultiChunk` | Multi-chunk message via `Writer.WriteMessage()` | 4096-byte video payload at 128-byte chunk size; write to `io.Discard` |
| `BenchmarkWriterReaderRoundTrip` | End-to-end Write→Read cycle | Write 4096-byte message to `bytes.Buffer`, read back via `Reader` |

Implementation approach:
```go
func BenchmarkWriterWriteMessage_MultiChunk(b *testing.B) {
    b.ReportAllocs()
    payload := make([]byte, 4096)
    msg := &Message{CSID: 6, Timestamp: 0, MessageLength: 4096, TypeID: 9, MessageStreamID: 1, Payload: payload}
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        w := NewWriter(io.Discard, 128)
        _ = w.WriteMessage(msg)
    }
}

func BenchmarkWriterReaderRoundTrip(b *testing.B) {
    b.ReportAllocs()
    payload := make([]byte, 4096)
    msg := &Message{CSID: 6, Timestamp: 0, MessageLength: 4096, TypeID: 9, MessageStreamID: 1, Payload: payload}
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        var buf bytes.Buffer
        w := NewWriter(&buf, 128)
        _ = w.WriteMessage(msg)
        r := NewReader(&buf, 128)
        _, _ = r.ReadMessage()
    }
}
```

#### File 3: `internal/rtmp/amf/object_test.go` — Object Benchmarks

| Benchmark | Description | Data |
|-----------|-------------|------|
| `BenchmarkEncodeObject` | Encode a typical RTMP connect-style object via `EncodeObject()` | `{"app":"live","type":"nonprivate","flashVer":"FMLE/3.0","tcUrl":"rtmp://localhost/live"}` |
| `BenchmarkDecodeObject` | Decode the same object from pre-encoded bytes via `DecodeObject()` | Golden bytes from encoding |

Implementation approach:
```go
func BenchmarkEncodeObject(b *testing.B) {
    b.ReportAllocs()
    obj := map[string]interface{}{
        "app":      "live",
        "type":     "nonprivate",
        "flashVer": "FMLE/3.0",
        "tcUrl":    "rtmp://localhost/live",
    }
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        var buf bytes.Buffer
        _ = EncodeObject(&buf, obj)
    }
}

func BenchmarkDecodeObject(b *testing.B) {
    b.ReportAllocs()
    obj := map[string]interface{}{
        "app": "live", "type": "nonprivate",
        "flashVer": "FMLE/3.0", "tcUrl": "rtmp://localhost/live",
    }
    var buf bytes.Buffer
    _ = EncodeObject(&buf, obj)
    data := buf.Bytes()
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        r := bytes.NewReader(data)
        _, _ = DecodeObject(r)
    }
}
```

#### File 4: `internal/rtmp/amf/array_test.go` — Array Benchmarks

| Benchmark | Description | Data |
|-----------|-------------|------|
| `BenchmarkEncodeStrictArray` | Encode a mixed-type array via `EncodeStrictArray()` | `[1.0, "test", true, nil, 42.0]` |
| `BenchmarkDecodeStrictArray` | Decode the same array from pre-encoded bytes via `DecodeStrictArray()` | Golden bytes from encoding |

Implementation approach:
```go
func BenchmarkEncodeStrictArray(b *testing.B) {
    b.ReportAllocs()
    arr := []interface{}{1.0, "test", true, nil, 42.0}
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        var buf bytes.Buffer
        _ = EncodeStrictArray(&buf, arr)
    }
}

func BenchmarkDecodeStrictArray(b *testing.B) {
    b.ReportAllocs()
    arr := []interface{}{1.0, "test", true, nil, 42.0}
    var buf bytes.Buffer
    _ = EncodeStrictArray(&buf, arr)
    data := buf.Bytes()
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        r := bytes.NewReader(data)
        _, _ = DecodeStrictArray(r)
    }
}
```

#### File 5: `internal/rtmp/amf/amf_test.go` — Top-Level Codec Benchmarks

| Benchmark | Description | Data |
|-----------|-------------|------|
| `BenchmarkEncodeAll_ConnectCommand` | Multi-value encode simulating a full connect command via `EncodeAll()` | `["connect", 1.0, {app:"live", type:"nonprivate", flashVer:"FMLE/3.0", tcUrl:"rtmp://localhost/live"}]` |
| `BenchmarkDecodeAll_ConnectCommand` | Multi-value decode of the same connect command payload via `DecodeAll()` | Pre-encoded bytes |

Implementation approach:
```go
func BenchmarkEncodeAll_ConnectCommand(b *testing.B) {
    b.ReportAllocs()
    obj := map[string]interface{}{
        "app": "live", "type": "nonprivate",
        "flashVer": "FMLE/3.0", "tcUrl": "rtmp://localhost/live",
    }
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, _ = EncodeAll("connect", 1.0, obj)
    }
}

func BenchmarkDecodeAll_ConnectCommand(b *testing.B) {
    b.ReportAllocs()
    obj := map[string]interface{}{
        "app": "live", "type": "nonprivate",
        "flashVer": "FMLE/3.0", "tcUrl": "rtmp://localhost/live",
    }
    data, _ := EncodeAll("connect", 1.0, obj)
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, _ = DecodeAll(data)
    }
}
```

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

| File | Part | Action | Changes |
|------|------|--------|---------|
| `internal/rtmp/conn/conn.go` | 1 | Edit | Add `onDisconnect` field, `SetDisconnectHandler()` method; modify `startReadLoop()` defer chain with cancel + disconnect handler + timeout error handling; add read deadline before `ReadMessage()`; add write deadline before `WriteMessage()` in writeLoop; add `readTimeout`/`writeTimeout` constants; remove dead `session *Session` field |
| `internal/rtmp/conn/session.go` | 1 | **Delete** | Entire file is dead code (Session struct, SessionState, NewSession, all methods) |
| `internal/rtmp/conn/session_test.go` | 1 | **Delete** | Tests for dead Session type |
| `internal/rtmp/server/server.go` | 1 | Edit | Add `RemoveConnection(id string)` method |
| `internal/rtmp/server/command_integration.go` | 1 | Edit | Add `role` field to `commandState`; set `st.role` in OnPublish/OnPlay handlers; install disconnect handler via `c.SetDisconnectHandler()` with correct API calls |
| `internal/rtmp/relay/destination.go` | 1 | Edit | Close client on both `Connect()` and `Publish()` failure paths in `Connect()` method |
| `cmd/rtmp-server/main.go` | 1 | Edit | Add `os.Exit(1)` after shutdown timeout log |
| `internal/rtmp/chunk/reader_test.go` | 2 | Edit | Add 5 chunk read benchmarks |
| `internal/rtmp/chunk/writer_test.go` | 2 | Edit | Add 4 chunk write benchmarks |
| `internal/rtmp/amf/object_test.go` | 2 | Edit | Add 2 object encode/decode benchmarks |
| `internal/rtmp/amf/array_test.go` | 2 | Edit | Add 2 array encode/decode benchmarks |
| `internal/rtmp/amf/amf_test.go` | 2 | Edit | Add 2 top-level codec benchmarks |

**Total**: 12 files (9 edits, 2 deletes, 0 new files)

## Testing Strategy

### Part 1 — Error Handling & Cleanup

#### Unit Tests (add to `internal/rtmp/conn/conn_test.go`)

| Test | Description |
|------|-------------|
| `TestDisconnectHandler_FiresOnEOF` | Client closes connection → readLoop gets EOF → verify disconnect handler fires |
| `TestDisconnectHandler_FiresOnContextCancel` | Call `c.Close()` → context canceled → verify disconnect handler fires |
| `TestDisconnectHandler_FiresOnReadError` | Inject malformed data → readLoop gets parse error → verify handler fires |
| `TestDisconnectHandler_NilSafe` | No handler set → readLoop exits without panic |

Implementation pattern:
```go
func TestDisconnectHandler_FiresOnEOF(t *testing.T) {
    logger.UseWriter(io.Discard)
    ln, _ := net.Listen("tcp", "127.0.0.1:0")
    defer ln.Close()
    connCh := make(chan *Connection, 1)
    go func() { c, _ := Accept(ln); connCh <- c }()
    client := dialAndClientHandshake(t, ln.Addr().String())
    serverConn := <-connCh
    var fired atomic.Bool
    serverConn.SetDisconnectHandler(func() { fired.Store(true) })
    serverConn.SetMessageHandler(func(m *chunk.Message) {})
    serverConn.Start()
    client.Close() // triggers EOF in readLoop
    deadline := time.Now().Add(2 * time.Second)
    for time.Now().Before(deadline) && !fired.Load() {
        time.Sleep(10 * time.Millisecond)
    }
    if !fired.Load() {
        t.Fatal("disconnect handler did not fire on EOF")
    }
    _ = serverConn.Close()
}
```

#### Server-Level Tests (add to `internal/rtmp/server/` or `tests/integration/`)

| Test | Description |
|------|-------------|
| `TestConnectionRemovedOnDisconnect` | Start server, connect client, disconnect client → `ConnectionCount()` returns 0 |
| `TestPublisherReuse` | Publish stream → disconnect → publish same stream key again → succeeds (no `ErrPublisherExists`) |
| `TestGoroutineLeakOnDisconnect` | Record `runtime.NumGoroutine()` before/after N connect+disconnect cycles → delta ≤ 2 |

### Part 2 — Benchmarks

- Benchmarks are self-validating (they run the code under test).
- Verify all benchmarks pass: `go test -bench . -benchmem -count=1 ./internal/rtmp/chunk/ ./internal/rtmp/amf/`
- Verify no regressions in existing tests: `go test -race ./...`

## Verification Commands (Definition of Done)

Run these in order as a final check:

```bash
# 1. Compile
go build ./...

# 2. Static analysis
go vet ./...

# 3. Formatting
gofmt -l .    # should print nothing

# 4. All internal tests (includes deleted session tests — should still pass)
go test ./internal/... -count=1

# 5. Full test suite with race detector
go test -race ./... -count=1

# 6. Benchmarks compile and run
go test -bench . -benchmem -count=1 ./internal/rtmp/chunk/ ./internal/rtmp/amf/
```

## Acceptance Criteria

### Part 1
- [ ] When a client disconnects normally (EOF), the connection is removed from `s.conns`
- [ ] When a publisher disconnects, `PublisherDisconnected()` is called and the stream key becomes available
- [ ] When a subscriber disconnects, `SubscriberDisconnected()` is called
- [ ] `MediaLogger.Stop()` is called on every connection disconnect (no goroutine leak)
- [ ] FLV recorders are closed when the publishing connection disconnects
- [ ] Zombie TCP connections are reaped within 90 seconds (read deadline)
- [ ] Relay client is properly closed on both `Connect()` and `Publish()` failure paths
- [ ] Server process exits with code 1 on shutdown timeout
- [ ] Dead `Session` type and files removed
- [ ] `go test -race ./...` passes

### Part 2
- [ ] Chunk package has benchmarks for header parsing (FMT0, FMT1, FMT3), single-chunk read/write, and multi-chunk read/write
- [ ] AMF package has benchmarks for Object and Array encode/decode
- [ ] AMF package has benchmarks for top-level `EncodeAll`/`DecodeAll` with realistic RTMP command data
- [ ] All benchmarks use `b.ReportAllocs()`
- [ ] `go test -bench . -benchmem ./internal/rtmp/chunk/ ./internal/rtmp/amf/` completes successfully

### Code Quality (per Definition of Done)
- [ ] `go build ./...` — zero errors
- [ ] `go vet ./...` — zero warnings
- [ ] `gofmt -l .` — no output (all files formatted)
- [ ] No dead code: removed Session type, no unused functions
- [ ] Exported types/functions have godoc comments
- [ ] Error wrapping follows existing patterns (`internal/errors`)

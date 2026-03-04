# Feature 006: Expvar Metrics — Implementation Plan

**Branch**: `feature/006-expvar-metrics`  
**Date**: 2026-03-04

## Task Dependency Graph

```
T001 (metrics package)
  ├──► T002 (CLI flag + HTTP listener)
  ├──► T003 (instrument connections)
  ├──► T004 (instrument registry)
  │      └──► T005 (instrument publisher disconnect)
  ├──► T006 (instrument media logger)
  └──► T007 (instrument relay)
         └──► T008 (integration test)
```

T001 is the foundation — all other tasks depend on it. T003–T007 are
independent of each other and can be implemented in any order. T008 depends
on all instrumentation being complete.

---

## Tasks

### T001: Create `internal/rtmp/metrics` package

**Priority**: 1 (blocking)  
**Estimated complexity**: Low  
**Files**:
- `internal/rtmp/metrics/metrics.go` — Create new
- `internal/rtmp/metrics/metrics_test.go` — Create new

**What to do**:
1. Create package with all `expvar.Int` declarations (14 counters)
2. Register `expvar.Func` for `rtmp_uptime_seconds` and `rtmp_server_info`
3. Write tests:
   - All counters initialize to 0
   - `Add(1)` / `Add(-1)` work correctly for gauge-style metrics
   - `rtmp_uptime_seconds` returns `> 0`
   - `rtmp_server_info` returns map with `go_version` key
   - Counters appear in `expvar.Handler` JSON output

**Acceptance criteria**:
- `go test -race ./internal/rtmp/metrics/...` passes
- All 14 `rtmp_*` variables registered and queryable

**Commit**: `feat(metrics): add expvar metrics package with counter declarations`

---

### T002: Add `-metrics-addr` CLI flag and HTTP listener

**Priority**: 2  
**Estimated complexity**: Low  
**Files**:
- `cmd/rtmp-server/flags.go` — Edit
- `cmd/rtmp-server/main.go` — Edit

**What to do**:
1. Add `metricsAddr string` to `cliConfig` struct
2. Register `-metrics-addr` flag with `fs.StringVar`, default `""` (disabled)
3. In `main()`, after `server.Start()`, if `metricsAddr != ""`:
   - Import `net/http` and blank-import `expvar` for handler registration
   - Start `http.ListenAndServe(cfg.metricsAddr, nil)` in a goroutine
   - Log the metrics address on startup
4. On shutdown signal, no special cleanup needed (HTTP server stops with process)

**Key decisions**:
- Use `http.DefaultServeMux` — `expvar` auto-registers on it
- Run HTTP server in a goroutine — non-blocking, dies with process
- No TLS or auth — this is an internal debug endpoint

**Acceptance criteria**:
- `./rtmp-server -metrics-addr :8080` starts HTTP listener
- `curl http://localhost:8080/debug/vars` returns valid JSON with runtime vars
- Default (no flag) = no HTTP listener started
- `./rtmp-server -h` shows the new flag

**Commit**: `feat(metrics): add -metrics-addr CLI flag and HTTP metrics endpoint`

---

### T003: Instrument connection lifecycle in server.go

**Priority**: 3  
**Estimated complexity**: Low  
**Files**:
- `internal/rtmp/server/server.go` — Edit

**What to do**:
1. Add `import "github.com/alxayo/go-rtmp/internal/rtmp/metrics"`
2. In `acceptLoop()`, after line `s.conns[c.ID()] = c` (inside `s.mu.Lock()` block):
   ```go
   metrics.ConnectionsActive.Add(1)
   metrics.ConnectionsTotal.Add(1)
   ```
3. In `RemoveConnection()`, after `delete(s.conns, id)`:
   ```go
   metrics.ConnectionsActive.Add(-1)
   ```

**Edge cases**:
- `RemoveConnection` is called exactly once per connection (from disconnect handler)
- The `Stop()` method calls `clear(s.conns)` but doesn't go through
  `RemoveConnection` — this is fine because metrics don't need to be accurate
  during shutdown (the process is ending)

**Acceptance criteria**:
- Accept 3 connections → `ConnectionsActive == 3`, `ConnectionsTotal == 3`
- Disconnect 1 → `ConnectionsActive == 2`, `ConnectionsTotal == 3`
- `go test -race ./internal/rtmp/server/...` passes

**Commit**: `feat(metrics): instrument connection accept and remove`

---

### T004: Instrument stream registry

**Priority**: 3  
**Estimated complexity**: Low  
**Files**:
- `internal/rtmp/server/registry.go` — Edit

**What to do**:
1. Add `import "github.com/alxayo/go-rtmp/internal/rtmp/metrics"`
2. In `CreateStream()`, after the new stream is inserted into the map
   (the `created == true` path, after `r.streams[key] = s`):
   ```go
   metrics.StreamsActive.Add(1)
   ```
3. In `DeleteStream()`, after `delete(r.streams, key)` inside the `if ok` block:
   ```go
   metrics.StreamsActive.Add(-1)
   ```
4. In `SetPublisher()`, after `s.Publisher = pub`:
   ```go
   metrics.PublishersActive.Add(1)
   metrics.PublishersTotal.Add(1)
   ```
5. In `AddSubscriber()`, after `s.Subscribers = append(...)`:
   ```go
   metrics.SubscribersActive.Add(1)
   metrics.SubscribersTotal.Add(1)
   ```
6. In `RemoveSubscriber()`, inside the `if existing == sub` block,
   after the swap-delete:
   ```go
   metrics.SubscribersActive.Add(-1)
   ```

**Edge cases**:
- `CreateStream` has a double-check pattern (fast RLock read, then Lock write).
  Only increment when `created == true` (second return value).
- `RemoveSubscriber` only decrements when a match is actually found.
- `SetPublisher` returns `ErrPublisherExists` without incrementing when
  a publisher is already set.

**Acceptance criteria**:
- Create 2 streams → `StreamsActive == 2`
- Delete 1 → `StreamsActive == 1`
- Set publisher → `PublishersActive == 1`, `PublishersTotal == 1`
- Add 3 subs → `SubscribersActive == 3`
- Remove 1 sub → `SubscribersActive == 2`, `SubscribersTotal == 3`
- `go test -race ./internal/rtmp/server/...` passes

**Commit**: `feat(metrics): instrument stream registry (streams, publishers, subscribers)`

---

### T005: Instrument publisher disconnect

**Priority**: 4 (depends on T004)  
**Estimated complexity**: Low  
**Files**:
- `internal/rtmp/server/publish_handler.go` — Edit

**What to do**:
1. Add `import "github.com/alxayo/go-rtmp/internal/rtmp/metrics"`
2. In `PublisherDisconnected()`, inside the `if s.Publisher == pub` block,
   after `s.Publisher = nil`:
   ```go
   metrics.PublishersActive.Add(-1)
   ```

**Edge cases**:
- Only decrement when `s.Publisher == pub` — if a different publisher took
  over, the old one's disconnect should not affect the counter.
- `PublisherDisconnected` is called from the connection's `onDisconnect`
  handler, which fires exactly once when the readLoop exits.

**Acceptance criteria**:
- Publish → `PublishersActive == 1`
- Disconnect publisher → `PublishersActive == 0`
- `PublishersTotal` remains unchanged
- `go test -race ./internal/rtmp/server/...` passes

**Commit**: `feat(metrics): instrument publisher disconnect`

---

### T006: Instrument media logger

**Priority**: 3  
**Estimated complexity**: Low  
**Files**:
- `internal/rtmp/server/media_logger.go` — Edit

**What to do**:
1. Add `import "github.com/alxayo/go-rtmp/internal/rtmp/metrics"`
2. In `ProcessMessage()`, after the existing counter increments
   (`ml.audioCount++` or `ml.videoCount++`), add:
   ```go
   metrics.BytesIngested.Add(int64(len(msg.Payload)))
   ```
3. In the `if msg.TypeID == 8` audio branch, after `ml.audioCount++`:
   ```go
   metrics.MessagesAudio.Add(1)
   ```
4. In the `else` (video) path, after `ml.videoCount++` (for TypeID == 9):
   ```go
   metrics.MessagesVideo.Add(1)
   ```

**Placement note**: The `ProcessMessage` method is already guarded by
`ml.mu.Lock()`. The `expvar.Int.Add()` call is atomic and does not need
this lock — it works correctly whether called inside or outside the lock.
We place it inside the existing lock scope to keep the code co-located
with the existing counter increments.

**Acceptance criteria**:
- Feed 10 audio + 5 video messages → `MessagesAudio == 10`, `MessagesVideo == 5`
- `BytesIngested == sum of all payload sizes`
- `go test -race ./internal/rtmp/server/...` passes

**Commit**: `feat(metrics): instrument media logger for audio/video/byte counters`

---

### T007: Instrument relay destination

**Priority**: 3  
**Estimated complexity**: Low  
**Files**:
- `internal/rtmp/relay/destination.go` — Edit

**What to do**:
1. Add `import "github.com/alxayo/go-rtmp/internal/rtmp/metrics"`
2. In `SendMessage()`, in the "not connected" early-return path,
   after `d.Metrics.MessagesDropped++`:
   ```go
   metrics.RelayMessagesDropped.Add(1)
   ```
3. In `SendMessage()`, in the send-error path,
   after `d.Metrics.MessagesDropped++`:
   ```go
   metrics.RelayMessagesDropped.Add(1)
   ```
4. In `SendMessage()`, in the success path,
   after `d.Metrics.MessagesSent++` and `d.Metrics.BytesSent += ...`:
   ```go
   metrics.RelayMessagesSent.Add(1)
   metrics.RelayBytesSent.Add(int64(len(msg.Payload)))
   ```

**Note**: The expvar counters are global aggregates across all destinations.
Per-destination metrics remain available via the existing
`DestinationManager.GetMetrics()` method. The expvar counters provide a
quick global overview without needing to enumerate destinations.

**Acceptance criteria**:
- Send 5 messages successfully → `RelayMessagesSent == 5`
- Force 2 drops → `RelayMessagesDropped == 2`
- `RelayBytesSent == sum of successful payload sizes`
- `go test -race ./internal/rtmp/relay/...` passes

**Commit**: `feat(metrics): instrument relay destination send/drop counters`

---

### T008: Integration test — metrics HTTP endpoint

**Priority**: 5 (depends on all above)  
**Estimated complexity**: Medium  
**Files**:
- `tests/integration/metrics_test.go` — Create new

**What to do**:
1. Start a server with `-metrics-addr` on port 0 (OS-assigned)
2. HTTP GET `/debug/vars` → assert 200
3. Parse JSON response
4. Assert all 14 `rtmp_*` keys exist
5. Assert initial gauge values are 0
6. Assert `rtmp_uptime_seconds > 0`
7. Assert `rtmp_server_info` contains `go_version`

**Acceptance criteria**:
- `go test -race ./tests/integration/ -run TestMetrics -count=1` passes
- No flaky timing issues (uptime check uses `> 0`, not exact value)

**Commit**: `test(metrics): add integration test for metrics HTTP endpoint`

---

## Execution Order

| Step | Task | Description |
|------|------|-------------|
| 1 | T001 | Create metrics package (foundation) |
| 2 | T002 | CLI flag + HTTP listener |
| 3 | T003 | Instrument connections |
| 4 | T004 | Instrument registry |
| 5 | T005 | Instrument publisher disconnect |
| 6 | T006 | Instrument media logger |
| 7 | T007 | Instrument relay |
| 8 | T008 | Integration test |

Each task gets its own commit. This allows easy bisection and clean review.

---

## Test Strategy

### Unit Tests (per task)
Each instrumentation point gets tested by:
1. Resetting relevant expvar counters (via `Set(0)`) to isolate tests
2. Performing the action that should increment
3. Asserting the expected counter value

### Integration Test (T008)
Full HTTP endpoint test that validates the complete pipeline: server → expvar →
HTTP → JSON → all expected keys present.

### Race Detection
All tests run with `-race` to verify the atomic operations are correct.

### Manual Verification
```bash
go build -o rtmp-server.exe ./cmd/rtmp-server
./rtmp-server.exe -listen :1935 -metrics-addr :8080 -log-level debug
# In another terminal:
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test
# In another terminal:
curl -s http://localhost:8080/debug/vars | jq . | grep rtmp_
```

---

## Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Counter drift (inc without matching dec) | Low | Low | Each inc/dec pair is co-located; reviewed in T003-T005 |
| Race conditions on expvar access | Very Low | Medium | `expvar.Int` uses `sync/atomic` internally |
| HTTP listener port conflict | Low | Low | Disabled by default; user chooses port |
| Performance impact on hot path | Very Low | Medium | `atomic.AddInt64` is ~1ns; negligible vs I/O |
| Shutdown metrics inaccuracy | Low | None | Metrics don't need to be accurate during shutdown |

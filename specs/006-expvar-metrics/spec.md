# Feature 006: Expvar Metrics — Live Counters for Connections, Publishers, Subscribers

**Feature**: 006-expvar-metrics  
**Status**: Draft  
**Date**: 2026-03-04  
**Branch**: `feature/006-expvar-metrics`

## Overview

Add real-time server metrics via Go's standard `expvar` package. An optional HTTP
endpoint exposes live JSON counters for connections, publishers, subscribers,
streams, media throughput, and relay health. Zero external dependencies.

### Design Constraints

- **Zero external dependencies** (stdlib `expvar` + `net/http` only)
- **Opt-in**: disabled by default; enabled via `-metrics-addr` CLI flag
- **Goroutine-safe**: `expvar.Int` uses `sync/atomic` internally — no additional locking
- **Minimal footprint**: one new package, ~15 one-liner instrumentation points in existing code
- **Non-intrusive**: metrics calls never block or affect RTMP data path performance

---

## What It Provides

### Metrics Endpoint

When enabled, an HTTP server listens on the configured address and serves:

- `GET /debug/vars` — standard expvar JSON output (all Go runtime + RTMP counters)

This endpoint is compatible with:
- **curl / jq** — direct JSON inspection
- **Prometheus** — via `expvar_exporter` or similar scrapers
- **Grafana** — via Prometheus or JSON data sources
- **Custom dashboards** — any HTTP/JSON consumer

### Metrics Catalog

| Metric Name | Type | Description | Source |
|---|---|---|---|
| `rtmp_connections_active` | Gauge | Currently open TCP connections | `Server.conns` map |
| `rtmp_connections_total` | Counter | Total connections accepted since startup | `Server.acceptLoop` |
| `rtmp_streams_active` | Gauge | Currently active publish streams | `Registry.streams` map |
| `rtmp_publishers_active` | Gauge | Currently active publishers | `Stream.SetPublisher` / `PublisherDisconnected` |
| `rtmp_publishers_total` | Counter | Total publish sessions since startup | `Stream.SetPublisher` |
| `rtmp_subscribers_active` | Gauge | Currently active subscribers | `Stream.AddSubscriber` / `RemoveSubscriber` |
| `rtmp_subscribers_total` | Counter | Total play sessions since startup | `Stream.AddSubscriber` |
| `rtmp_messages_audio` | Counter | Total audio messages received | `MediaLogger.ProcessMessage` |
| `rtmp_messages_video` | Counter | Total video messages received | `MediaLogger.ProcessMessage` |
| `rtmp_bytes_ingested` | Counter | Total media bytes received (audio + video) | `MediaLogger.ProcessMessage` |
| `rtmp_relay_messages_sent` | Counter | Total relay messages delivered | `Destination.SendMessage` |
| `rtmp_relay_messages_dropped` | Counter | Total relay messages dropped | `Destination.SendMessage` |
| `rtmp_relay_bytes_sent` | Counter | Total relay bytes transmitted | `Destination.SendMessage` |
| `rtmp_uptime_seconds` | Gauge (func) | Seconds since server start | Computed on each request |
| `rtmp_server_info` | Map (func) | Static info: listen addr, Go version | Computed on each request |

**Gauge** = value goes up and down (active counts).  
**Counter** = monotonically increasing (totals).  
Both are implemented as `expvar.Int` (atomic int64). `Add(1)` to increment, `Add(-1)` to decrement gauges.

---

## Protocol / API

### HTTP Response Format

`GET /debug/vars` returns JSON (standard expvar format):

```json
{
  "cmdline": ["./rtmp-server", "-listen", ":1935", "-metrics-addr", ":8080"],
  "memstats": { ... },
  "rtmp_connections_active": 3,
  "rtmp_connections_total": 47,
  "rtmp_streams_active": 1,
  "rtmp_publishers_active": 1,
  "rtmp_publishers_total": 5,
  "rtmp_subscribers_active": 2,
  "rtmp_subscribers_total": 12,
  "rtmp_messages_audio": 145023,
  "rtmp_messages_video": 87412,
  "rtmp_bytes_ingested": 523948172,
  "rtmp_relay_messages_sent": 87410,
  "rtmp_relay_messages_dropped": 2,
  "rtmp_relay_bytes_sent": 261974086,
  "rtmp_uptime_seconds": 3847,
  "rtmp_server_info": {
    "listen_addr": ":1935",
    "go_version": "go1.21.0"
  }
}
```

### CLI Usage

```bash
# Start with metrics disabled (default — no change to existing behavior)
./rtmp-server -listen :1935

# Start with metrics enabled on port 8080
./rtmp-server -listen :1935 -metrics-addr :8080

# Start with metrics on localhost only (production hardening)
./rtmp-server -listen :1935 -metrics-addr 127.0.0.1:8080

# Query live metrics
curl -s http://localhost:8080/debug/vars | jq '{
  connections: .rtmp_connections_active,
  publishers:  .rtmp_publishers_active,
  subscribers: .rtmp_subscribers_active,
  streams:     .rtmp_streams_active,
  uptime:      .rtmp_uptime_seconds
}'
```

---

## Architecture

### New Package

```
internal/rtmp/metrics/
├── metrics.go       # Package-level expvar variable declarations, init, helper funcs
└── metrics_test.go  # Unit tests for counter behavior
```

The metrics package is a thin, stateless wrapper around `expvar`. It declares
package-level variables and provides no business logic — the instrumentation
is done at the call sites in existing packages.

### Modified Files

| File | Change |
|------|--------|
| `internal/rtmp/server/server.go` | Instrument `acceptLoop` (connection accepted), `RemoveConnection` (connection removed) |
| `internal/rtmp/server/registry.go` | Instrument `CreateStream`, `DeleteStream`, `SetPublisher`, `AddSubscriber`, `RemoveSubscriber` |
| `internal/rtmp/server/publish_handler.go` | Instrument `PublisherDisconnected` |
| `internal/rtmp/server/media_logger.go` | Instrument `ProcessMessage` for audio/video/byte counters |
| `internal/rtmp/relay/destination.go` | Instrument `SendMessage` for relay sent/dropped/bytes |
| `cmd/rtmp-server/flags.go` | Add `-metrics-addr` flag |
| `cmd/rtmp-server/main.go` | Start HTTP metrics server when flag is set |

### Data Flow

```
                          ┌─────────────────────┐
  RTMP Client ──TCP──►    │    RTMP Server       │
                          │                      │
  acceptLoop ─────────►   │  metrics.Connections  │◄── expvar.Int (atomic)
  registry   ─────────►   │  metrics.Streams      │◄── expvar.Int (atomic)
  SetPublisher ───────►   │  metrics.Publishers   │◄── expvar.Int (atomic)
  AddSubscriber ──────►   │  metrics.Subscribers  │◄── expvar.Int (atomic)
  ProcessMessage ─────►   │  metrics.Messages     │◄── expvar.Int (atomic)
  SendMessage  ───────►   │  metrics.Relay        │◄── expvar.Int (atomic)
                          │                      │
                          └─────────┬────────────┘
                                    │
                          HTTP :8080│ /debug/vars
                                    ▼
                          ┌─────────────────────┐
                          │   JSON Response      │
                          │  (curl / Prometheus) │
                          └─────────────────────┘
```

All instrumentation points call `expvar.Int.Add()` which is a single atomic
operation — no mutexes, no allocations, no blocking. The RTMP data path
remains unaffected.

---

## Detailed Design

### T001: Create metrics package with expvar declarations

**File**: `internal/rtmp/metrics/metrics.go`

```go
package metrics

import (
    "expvar"
    "runtime"
    "time"
)

// Server start time — set once during init.
var startTime time.Time

func init() {
    startTime = time.Now()
}

// ── Connection metrics ──────────────────────────────────────────────
var (
    ConnectionsActive = expvar.NewInt("rtmp_connections_active")
    ConnectionsTotal  = expvar.NewInt("rtmp_connections_total")
)

// ── Stream metrics ──────────────────────────────────────────────────
var (
    StreamsActive = expvar.NewInt("rtmp_streams_active")
)

// ── Publisher metrics ───────────────────────────────────────────────
var (
    PublishersActive = expvar.NewInt("rtmp_publishers_active")
    PublishersTotal  = expvar.NewInt("rtmp_publishers_total")
)

// ── Subscriber metrics ──────────────────────────────────────────────
var (
    SubscribersActive = expvar.NewInt("rtmp_subscribers_active")
    SubscribersTotal  = expvar.NewInt("rtmp_subscribers_total")
)

// ── Media metrics ───────────────────────────────────────────────────
var (
    MessagesAudio = expvar.NewInt("rtmp_messages_audio")
    MessagesVideo = expvar.NewInt("rtmp_messages_video")
    BytesIngested = expvar.NewInt("rtmp_bytes_ingested")
)

// ── Relay metrics ───────────────────────────────────────────────────
var (
    RelayMessagesSent    = expvar.NewInt("rtmp_relay_messages_sent")
    RelayMessagesDropped = expvar.NewInt("rtmp_relay_messages_dropped")
    RelayBytesSent       = expvar.NewInt("rtmp_relay_bytes_sent")
)

func init() {
    // Uptime: computed on each request, always current
    expvar.Publish("rtmp_uptime_seconds", expvar.Func(func() interface{} {
        return int64(time.Since(startTime).Seconds())
    }))

    // Server info: static metadata
    expvar.Publish("rtmp_server_info", expvar.Func(func() interface{} {
        return map[string]string{
            "go_version": runtime.Version(),
        }
    }))
}
```

**Why package-level vars**: Allows any package to import `metrics` and call
`metrics.ConnectionsActive.Add(1)` without passing state around. The expvar
registry is process-global by design.

**Tests** (`metrics_test.go`):
- Verify initial values are 0
- Verify Add(1), Add(-1) for gauges
- Verify monotonic Add(1) for counters
- Verify uptime is > 0
- Verify server_info returns a map with go_version

---

### T002: Add `-metrics-addr` CLI flag

**File**: `cmd/rtmp-server/flags.go`

Add field to `cliConfig`:
```go
metricsAddr string // HTTP address for expvar/pprof (default "" = disabled)
```

Add flag registration:
```go
fs.StringVar(&cfg.metricsAddr, "metrics-addr", "",
    "HTTP address for metrics endpoint (e.g. :8080 or 127.0.0.1:8080). Empty = disabled")
```

**File**: `cmd/rtmp-server/main.go`

After `server.Start()`, start the metrics HTTP listener:
```go
if cfg.metricsAddr != "" {
    go func() {
        // Import expvar for side-effect: registers /debug/vars on DefaultServeMux
        log.Info("metrics HTTP server listening", "addr", cfg.metricsAddr)
        if err := http.ListenAndServe(cfg.metricsAddr, nil); err != nil && !errors.Is(err, http.ErrServerClosed) {
            log.Error("metrics HTTP server error", "error", err)
        }
    }()
}
```

**Tests**: Verify flag parsing includes new field, verify default is empty string.

---

### T003: Instrument connection lifecycle

**File**: `internal/rtmp/server/server.go`

In `acceptLoop`, after `s.conns[c.ID()] = c`:
```go
metrics.ConnectionsActive.Add(1)
metrics.ConnectionsTotal.Add(1)
```

In `RemoveConnection`, after `delete(s.conns, id)`:
```go
metrics.ConnectionsActive.Add(-1)
```

**Tests**: Table-driven integration test — accept N connections, verify
`ConnectionsActive == N`, disconnect one, verify `ConnectionsActive == N-1`.
Verify `ConnectionsTotal` only increases.

---

### T004: Instrument stream registry

**File**: `internal/rtmp/server/registry.go`

In `CreateStream`, when a new stream is created (the `created == true` path):
```go
metrics.StreamsActive.Add(1)
```

In `DeleteStream`, after `delete(r.streams, key)`:
```go
metrics.StreamsActive.Add(-1)
```

In `SetPublisher`, after `s.Publisher = pub`:
```go
metrics.PublishersActive.Add(1)
metrics.PublishersTotal.Add(1)
```

In `AddSubscriber`, after `append`:
```go
metrics.SubscribersActive.Add(1)
metrics.SubscribersTotal.Add(1)
```

In `RemoveSubscriber`, after the swap-delete succeeds (inside the `if existing == sub` block):
```go
metrics.SubscribersActive.Add(-1)
```

**Tests**: Create stream → verify `StreamsActive == 1`. Set publisher → verify
`PublishersActive == 1`. Add 3 subscribers → verify `SubscribersActive == 3`.
Remove 1 → verify `SubscribersActive == 2`. Delete stream → verify
`StreamsActive == 0`.

---

### T005: Instrument publisher disconnect

**File**: `internal/rtmp/server/publish_handler.go`

In `PublisherDisconnected`, after `s.Publisher = nil` (inside the `if s.Publisher == pub` block):
```go
metrics.PublishersActive.Add(-1)
```

**Tests**: Publish → disconnect → verify `PublishersActive == 0`,
`PublishersTotal == 1`.

---

### T006: Instrument media logger

**File**: `internal/rtmp/server/media_logger.go`

In `ProcessMessage`, after the existing counter increments:
```go
metrics.BytesIngested.Add(int64(len(msg.Payload)))

if msg.TypeID == 8 {
    metrics.MessagesAudio.Add(1)
} else {
    metrics.MessagesVideo.Add(1)
}
```

These calls are placed after existing `ml.audioCount++` / `ml.videoCount++`
to keep the two counter systems consistent.

**Tests**: Feed N audio + M video messages → verify `MessagesAudio == N`,
`MessagesVideo == M`, `BytesIngested == sum(payload sizes)`.

---

### T007: Instrument relay destination

**File**: `internal/rtmp/relay/destination.go`

In `SendMessage`, after the existing `d.Metrics.MessagesSent++`:
```go
metrics.RelayMessagesSent.Add(1)
metrics.RelayBytesSent.Add(int64(len(msg.Payload)))
```

In the two drop paths (not connected + send error), after `d.Metrics.MessagesDropped++`:
```go
metrics.RelayMessagesDropped.Add(1)
```

**Tests**: Mock client, send messages → verify `RelayMessagesSent` and
`RelayBytesSent`. Force error → verify `RelayMessagesDropped`.

---

### T008: Integration test — full metrics endpoint

**File**: `tests/integration/metrics_test.go`

End-to-end test:
1. Start server with `-metrics-addr` on a random port
2. Verify `GET /debug/vars` returns 200 with valid JSON
3. Verify all `rtmp_*` keys are present
4. Verify initial values are 0
5. Verify `rtmp_uptime_seconds > 0`
6. Verify `rtmp_server_info` contains `go_version`

---

## Scope Boundaries

### In Scope
- `expvar` package-level counters (atomic int64)
- HTTP endpoint for JSON metrics export
- CLI flag to enable/disable
- Per-category counters (connections, streams, publishers, subscribers, media, relay)
- Uptime and server info computed vars
- Unit tests for each instrumentation point
- Integration test for HTTP endpoint

### Out of Scope (future enhancements)
- **Prometheus `/metrics` endpoint** — use prometheus expvar exporter instead
- **Per-stream metric breakdowns** — would require `expvar.Map` nesting
- **Histograms / percentiles** — expvar only supports counters/gauges
- **Authentication on metrics endpoint** — bind to localhost for security
- **pprof endpoint** — separate feature, could share the same HTTP listener
- **Metric retention / history** — expvar is live-only, no time series storage
- **Dashboard / UI** — consumers use their own tools (Grafana, etc.)

---

## Security Considerations

- **Metrics endpoint binds to configurable address** — users should bind to
  `127.0.0.1:8080` in production to prevent external access
- **No sensitive data exposed** — only numeric counters and Go version string
- **No authentication** — standard for internal debug endpoints; network-level
  access control is the appropriate mitigation
- **Disabled by default** — no listener starts unless `-metrics-addr` is explicitly set

---

## Usage Examples

### Monitor during load test
```bash
# Terminal 1: Start server with metrics
./rtmp-server -listen :1935 -metrics-addr :8080 -log-level info

# Terminal 2: Publish a test stream
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test

# Terminal 3: Watch metrics live (refresh every 2s)
watch -n2 'curl -s http://localhost:8080/debug/vars | jq "{
  conns:       .rtmp_connections_active,
  publishers:  .rtmp_publishers_active,
  subscribers: .rtmp_subscribers_active,
  streams:     .rtmp_streams_active,
  audio_msgs:  .rtmp_messages_audio,
  video_msgs:  .rtmp_messages_video,
  bytes_in:    .rtmp_bytes_ingested,
  uptime:      .rtmp_uptime_seconds
}"'
```

### Alerting on subscriber count
```bash
# Simple threshold check
SUBS=$(curl -s http://localhost:8080/debug/vars | jq .rtmp_subscribers_active)
if [ "$SUBS" -gt 100 ]; then
  echo "WARNING: $SUBS subscribers active"
fi
```

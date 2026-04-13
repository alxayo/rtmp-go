---
title: "Metrics & Monitoring"
weight: 6
---

# Metrics & Monitoring

go-rtmp exposes live server statistics via an HTTP endpoint using Go's built-in `expvar` package. Metrics are thread-safe, have zero overhead when disabled, and require no external dependencies.

## Enabling Metrics

```bash
./rtmp-server -metrics-addr :8080
```

When `-metrics-addr` is not specified, no HTTP listener is started and there is zero performance overhead.

## Querying Metrics

```bash
curl http://localhost:8080/debug/vars
```

The response is a JSON object containing all `rtmp_*` and `srt_*` keys, dynamic endpoints, and Go's built-in `memstats` and `cmdline` variables:

```json
{
  "cmdline": ["./rtmp-server", "-metrics-addr", ":8080"],
  "memstats": {"Alloc": 1234567, "TotalAlloc": 9876543, "...": "..."},
  "rtmp_connections_active": 5,
  "rtmp_connections_total": 42,
  "rtmp_streams_active": 2,
  "rtmp_publishers_active": 2,
  "rtmp_subscribers_active": 3,
  "rtmp_recordings_active": 1,
  "rtmp_publishers_total": 8,
  "rtmp_subscribers_total": 15,
  "rtmp_messages_audio": 98765,
  "rtmp_messages_video": 45678,
  "rtmp_bytes_ingested": 1234567890,
  "rtmp_bytes_egress": 987654321,
  "rtmp_subscriber_drops_total": 42,
  "rtmp_auth_successes_total": 30,
  "rtmp_auth_failures_total": 3,
  "rtmp_handshake_failures_total": 1,
  "rtmp_recording_errors_total": 0,
  "rtmp_zombie_connections_total": 2,
  "rtmp_relay_messages_sent": 45678,
  "rtmp_relay_messages_dropped": 12,
  "rtmp_relay_bytes_sent": 987654321,
  "rtmp_uptime_seconds": 3600,
  "rtmp_server_info": {"go_version": "go1.21"},
  "rtmp_streams": [
    {
      "key": "live/stream1",
      "subscribers": 3,
      "video_codec": "H264",
      "audio_codec": "AAC",
      "uptime_seconds": 3600,
      "recording": true
    }
  ],
  "rtmp_relay_destinations": [
    {
      "url": "rtmp://cdn.example.com/live/key",
      "status": "connected",
      "messages_sent": 45678,
      "messages_dropped": 12,
      "bytes_sent": 987654321,
      "reconnect_count": 1
    }
  ],
  "srt_connections_active": 1,
  "srt_connections_total": 5,
  "srt_bytes_received": 567890123,
  "srt_packets_received": 123456,
  "srt_packets_retransmit": 78,
  "srt_packets_dropped": 3
}
```

> **Note:** Go's built-in `memstats` (runtime memory statistics) and `cmdline` (process arguments) variables are always present alongside the `rtmp_*` and `srt_*` keys. These are provided automatically by the `expvar` package.

## Available Metrics

### RTMP Gauges (current values, fluctuate up and down)

| Metric | Description |
|--------|-------------|
| `rtmp_connections_active` | Currently active RTMP connections |
| `rtmp_streams_active` | Currently active streams |
| `rtmp_publishers_active` | Currently active publishers |
| `rtmp_subscribers_active` | Currently active subscribers |
| `rtmp_recordings_active` | Currently active recordings |

### RTMP Counters (monotonically increasing)

| Metric | Description |
|--------|-------------|
| `rtmp_connections_total` | Total connections since server start |
| `rtmp_publishers_total` | Total publishers since server start |
| `rtmp_subscribers_total` | Total subscribers since server start |
| `rtmp_messages_audio` | Total audio messages ingested |
| `rtmp_messages_video` | Total video messages ingested |
| `rtmp_bytes_ingested` | Total bytes ingested (media) |
| `rtmp_bytes_egress` | Total bytes sent to subscribers |
| `rtmp_subscriber_drops_total` | Total messages dropped due to slow subscribers |
| `rtmp_auth_successes_total` | Total successful authentication attempts |
| `rtmp_auth_failures_total` | Total failed authentication attempts |
| `rtmp_handshake_failures_total` | Total RTMP handshake failures |
| `rtmp_recording_errors_total` | Total recording errors (create or close failures) |
| `rtmp_zombie_connections_total` | Total zombie connections reaped (read timeout) |
| `rtmp_relay_messages_sent` | Total relay messages sent successfully |
| `rtmp_relay_messages_dropped` | Total relay messages dropped (failed sends) |
| `rtmp_relay_bytes_sent` | Total relay bytes sent |

### SRT Metrics

These metrics are available when SRT ingest is enabled.

#### Gauges

| Metric | Description |
|--------|-------------|
| `srt_connections_active` | Currently active SRT publisher connections |

#### Counters

| Metric | Description |
|--------|-------------|
| `srt_connections_total` | Total SRT connections since server start |
| `srt_bytes_received` | Total bytes received over SRT |
| `srt_packets_received` | Total data packets received over SRT |
| `srt_packets_retransmit` | Total retransmitted packets over SRT |
| `srt_packets_dropped` | Total packets dropped due to too-late delivery (TLPKTDROP) |

### Dynamic Endpoints

Dynamic endpoints return structured JSON arrays computed on each request. Unlike scalar counters and gauges, these provide rich per-resource views.

#### Per-Stream Visibility (`rtmp_streams`)

Returns a JSON array with live per-stream info, computed on each request:

```json
[
  {
    "key": "live/stream1",
    "subscribers": 3,
    "video_codec": "H264",
    "audio_codec": "AAC",
    "uptime_seconds": 3600,
    "recording": true
  }
]
```

Query examples:

```bash
# List all active streams
curl -s http://localhost:8080/debug/vars | jq '.rtmp_streams'

# Find streams with most subscribers
curl -s http://localhost:8080/debug/vars | jq '.rtmp_streams | sort_by(-.subscribers)'

# Check which streams are recording
curl -s http://localhost:8080/debug/vars | jq '[.rtmp_streams[] | select(.recording)]'
```

#### Per-Destination Relay (`rtmp_relay_destinations`)

Returns a JSON array with per-relay-destination info:

```json
[
  {
    "url": "rtmp://cdn.example.com/live/key",
    "status": "connected",
    "messages_sent": 45678,
    "messages_dropped": 12,
    "bytes_sent": 987654321,
    "reconnect_count": 1
  }
]
```

Query examples:

```bash
# Relay destination health
curl -s http://localhost:8080/debug/vars | jq '.rtmp_relay_destinations'

# Find failing destinations
curl -s http://localhost:8080/debug/vars | jq '[.rtmp_relay_destinations[] | select(.status != "connected")]'
```

### Info

| Metric | Description |
|--------|-------------|
| `rtmp_uptime_seconds` | Seconds since server process start |
| `rtmp_server_info` | Static info object with `go_version` |

## Querying Specific Metrics

Use `jq` to extract individual values:

```bash
# Active connections
curl -s http://localhost:8080/debug/vars | jq '.rtmp_connections_active'

# All active gauges
curl -s http://localhost:8080/debug/vars | jq '{
  connections: .rtmp_connections_active,
  publishers: .rtmp_publishers_active,
  subscribers: .rtmp_subscribers_active,
  streams: .rtmp_streams_active,
  recordings: .rtmp_recordings_active
}'

# Relay health check
curl -s http://localhost:8080/debug/vars | jq '{
  sent: .rtmp_relay_messages_sent,
  dropped: .rtmp_relay_messages_dropped,
  bytes: .rtmp_relay_bytes_sent
}'

# Auth overview
curl -s http://localhost:8080/debug/vars | jq '{
  successes: .rtmp_auth_successes_total,
  failures: .rtmp_auth_failures_total
}'

# SRT ingest stats
curl -s http://localhost:8080/debug/vars | jq '{
  active: .srt_connections_active,
  total: .srt_connections_total,
  bytes: .srt_bytes_received,
  retransmits: .srt_packets_retransmit,
  drops: .srt_packets_dropped
}'
```

## Implementation

Metrics use Go's `expvar` package from the standard library:

- All scalar values are `expvar.Int` (atomic, thread-safe)
- Gauges are incremented/decremented as connections open/close
- Counters are only incremented
- `rtmp_uptime_seconds` is computed on each request via `expvar.Func`
- `rtmp_server_info` is a static `expvar.Func` returning a map
- Dynamic endpoints (`rtmp_streams`, `rtmp_relay_destinations`) are registered as `expvar.Func` values — they compute and return their JSON arrays on each request rather than maintaining persistent state

## Monitoring Integration

### Prometheus

Use the [expvar_exporter](https://github.com/prometheus/expvar_exporter) or a similar Prometheus exporter to scrape the `/debug/vars` endpoint:

```yaml
scrape_configs:
  - job_name: 'rtmp'
    metrics_path: '/debug/vars'
    static_configs:
      - targets: ['localhost:8080']
```

### Grafana

Point Grafana at your Prometheus data source and build dashboards using the `rtmp_*` and `srt_*` metrics. Useful panels:

- **Active connections** gauge: `rtmp_connections_active`
- **Throughput** graph: rate of `rtmp_bytes_ingested` and `rtmp_bytes_egress`
- **Relay health** graph: `rtmp_relay_messages_sent` vs `rtmp_relay_messages_dropped`
- **Auth failure rate** graph: rate of `rtmp_auth_failures_total` — spike alerts indicate brute-force or misconfigured clients
- **Subscriber drops** graph: rate of `rtmp_subscriber_drops_total` — rising drops indicate slow consumers or bandwidth issues
- **Recording status** gauge: `rtmp_recordings_active` alongside `rtmp_recording_errors_total` for error alerts
- **SRT ingest** graph: `srt_packets_received` vs `srt_packets_dropped` and `srt_packets_retransmit`
- **Zombie connections** counter: rate of `rtmp_zombie_connections_total` — persistent zombies suggest network issues

### Custom Scripts

Poll the endpoint periodically for simple monitoring:

```bash
#!/bin/bash
while true; do
  ACTIVE=$(curl -s http://localhost:8080/debug/vars | jq '.rtmp_connections_active')
  echo "$(date): $ACTIVE active connections"
  sleep 10
done
```

## Zero Overhead

When `-metrics-addr` is not specified:

- No HTTP listener is started
- No goroutines are spawned for the metrics server
- The `expvar` counters still exist in memory (negligible) but are not exposed
- There is no measurable performance impact on RTMP processing

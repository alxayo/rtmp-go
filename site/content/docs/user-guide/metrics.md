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

The response is a JSON object containing all `rtmp_*` keys along with Go's default runtime metrics:

```json
{
  "rtmp_connections_active": 5,
  "rtmp_connections_total": 42,
  "rtmp_streams_active": 2,
  "rtmp_publishers_active": 2,
  "rtmp_subscribers_active": 3,
  "rtmp_publishers_total": 8,
  "rtmp_subscribers_total": 15,
  "rtmp_messages_audio": 98765,
  "rtmp_messages_video": 45678,
  "rtmp_bytes_ingested": 1234567890,
  "rtmp_relay_messages_sent": 45678,
  "rtmp_relay_messages_dropped": 12,
  "rtmp_relay_bytes_sent": 987654321,
  "rtmp_uptime_seconds": 3600,
  "rtmp_server_info": {"go_version": "go1.21"}
}
```

## Available Metrics

### Gauges (current values, fluctuate up and down)

| Metric | Description |
|--------|-------------|
| `rtmp_connections_active` | Currently active RTMP connections |
| `rtmp_streams_active` | Currently active streams |
| `rtmp_publishers_active` | Currently active publishers |
| `rtmp_subscribers_active` | Currently active subscribers |

### Counters (monotonically increasing)

| Metric | Description |
|--------|-------------|
| `rtmp_connections_total` | Total connections since server start |
| `rtmp_publishers_total` | Total publishers since server start |
| `rtmp_subscribers_total` | Total subscribers since server start |
| `rtmp_messages_audio` | Total audio messages ingested |
| `rtmp_messages_video` | Total video messages ingested |
| `rtmp_bytes_ingested` | Total bytes ingested (media) |
| `rtmp_relay_messages_sent` | Total relay messages sent successfully |
| `rtmp_relay_messages_dropped` | Total relay messages dropped (failed sends) |
| `rtmp_relay_bytes_sent` | Total relay bytes sent |

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
  streams: .rtmp_streams_active
}'

# Relay health check
curl -s http://localhost:8080/debug/vars | jq '{
  sent: .rtmp_relay_messages_sent,
  dropped: .rtmp_relay_messages_dropped,
  bytes: .rtmp_relay_bytes_sent
}'
```

## Implementation

Metrics use Go's `expvar` package from the standard library:

- All values are `expvar.Int` (atomic, thread-safe)
- Gauges are incremented/decremented as connections open/close
- Counters are only incremented
- `rtmp_uptime_seconds` is computed on each request via `expvar.Func`
- `rtmp_server_info` is a static `expvar.Func` returning a map

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

Point Grafana at your Prometheus data source and build dashboards using the `rtmp_*` metrics. Useful panels:

- **Active connections** gauge: `rtmp_connections_active`
- **Throughput** graph: rate of `rtmp_bytes_ingested`
- **Relay health** graph: `rtmp_relay_messages_sent` vs `rtmp_relay_messages_dropped`

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

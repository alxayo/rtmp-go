---
name: rtmp-monitor
description: 'Monitor the RTMP ingest server and related services on Azure. Use when: check active streams, view subscriber counts, check connection health, view metrics, check server status, diagnose stream issues, check bandwidth usage, view relay stats, check recording status, monitor zombie connections, view handshake failures, check SRT stats, debug stream quality, dashboard overview, health check.'
argument-hint: 'What to check, e.g. "active streams", "full dashboard", "health", "stream key live/test", "bandwidth"'
---

# RTMP Server Monitoring

Monitor the RTMP ingest server, active streams, subscriber counts, and connection health on Azure Container Apps. Queries the expvar metrics endpoint and container logs.

## When to Use

- Check if streams are active and how many subscribers are watching
- View bandwidth (ingress/egress bytes), audio/video message counts
- Diagnose stream quality issues (drops, relay failures, zombie connections)
- Get a full dashboard overview of the server state
- Check health of all container apps in the stack
- Monitor recording status and errors
- View SRT connection stats (if SRT is enabled)

## Prerequisites

- Azure CLI logged in (`az account show`)
- RTMP server must have `-metrics-addr :8080` flag set (metrics endpoint)

## Environment Discovery

```bash
# Defaults — override via argument or env vars
RESOURCE_GROUP="${RESOURCE_GROUP:-event-periscope-ne}"

# Auto-discover
RTMP_APP=$(az containerapp list -g "$RESOURCE_GROUP" \
  --query "[?contains(name,'rtmp-server')].name | [0]" -o tsv)
SIDECAR_APP=$(az containerapp list -g "$RESOURCE_GROUP" \
  --query "[?contains(name,'rec-blob-sidecar')].name | [0]" -o tsv)
TRANSCODER_APP=$(az containerapp list -g "$RESOURCE_GROUP" \
  --query "[?contains(name,'hls-transcoder')].name | [0]" -o tsv)
RTMP_FQDN=$(az containerapp show -g "$RESOURCE_GROUP" -n "$RTMP_APP" \
  --query "properties.configuration.ingress.fqdn" -o tsv)
```

## Procedures

### 1. Full Dashboard

Overview of all key metrics in one pass.

```bash
# Check if metrics endpoint is enabled (look for -metrics-addr in command)
METRICS_ENABLED=$(az containerapp show -g "$RESOURCE_GROUP" -n "$RTMP_APP" \
  --query "properties.template.containers[0].command" -o tsv 2>/dev/null \
  | grep -c 'metrics-addr')

if [ "$METRICS_ENABLED" -eq 0 ]; then
  echo "WARNING: Metrics endpoint not enabled. Add '-metrics-addr :8080' to server flags."
  echo "Falling back to log-based monitoring only."
fi
```

If metrics are enabled and the app is running with a public or tunneled metrics endpoint:

```bash
# Fetch all expvar metrics
METRICS_URL="http://<metrics-endpoint>:8080/debug/vars"
METRICS=$(curl -s "$METRICS_URL")

echo "=== Connections ==="
echo "  Active:     $(echo "$METRICS" | jq -r '.rtmp_connections_active // 0')"
echo "  Total:      $(echo "$METRICS" | jq -r '.rtmp_connections_total // 0')"
echo "  Zombie:     $(echo "$METRICS" | jq -r '.rtmp_zombie_connections_total // 0')"
echo "  Handshake failures: $(echo "$METRICS" | jq -r '.rtmp_handshake_failures_total // 0')"

echo ""
echo "=== Streams ==="
echo "  Active:     $(echo "$METRICS" | jq -r '.rtmp_streams_active // 0')"
echo "  Publishers: $(echo "$METRICS" | jq -r '.rtmp_publishers_active // 0') active / $(echo "$METRICS" | jq -r '.rtmp_publishers_total // 0') total"
echo "  Subscribers:$(echo "$METRICS" | jq -r '.rtmp_subscribers_active // 0') active / $(echo "$METRICS" | jq -r '.rtmp_subscribers_total // 0') total"
echo "  Sub drops:  $(echo "$METRICS" | jq -r '.rtmp_subscriber_drops_total // 0')"

echo ""
echo "=== Media ==="
echo "  Video msgs: $(echo "$METRICS" | jq -r '.rtmp_messages_video // 0')"
echo "  Audio msgs: $(echo "$METRICS" | jq -r '.rtmp_messages_audio // 0')"
echo "  Ingested:   $(echo "$METRICS" | jq -r '.rtmp_bytes_ingested // 0') bytes"
echo "  Egress:     $(echo "$METRICS" | jq -r '.rtmp_bytes_egress // 0') bytes"

echo ""
echo "=== Auth ==="
echo "  Successes:  $(echo "$METRICS" | jq -r '.rtmp_auth_successes_total // 0')"
echo "  Failures:   $(echo "$METRICS" | jq -r '.rtmp_auth_failures_total // 0')"

echo ""
echo "=== Relay ==="
echo "  Msgs sent:    $(echo "$METRICS" | jq -r '.rtmp_relay_messages_sent // 0')"
echo "  Msgs dropped: $(echo "$METRICS" | jq -r '.rtmp_relay_messages_dropped // 0')"
echo "  Bytes sent:   $(echo "$METRICS" | jq -r '.rtmp_relay_bytes_sent // 0')"

echo ""
echo "=== Recording ==="
echo "  Active:     $(echo "$METRICS" | jq -r '.rtmp_recordings_active // 0')"
echo "  Errors:     $(echo "$METRICS" | jq -r '.rtmp_recording_errors_total // 0')"

# Per-stream details (dynamic endpoint)
echo ""
echo "=== Per-Stream Detail ==="
echo "$METRICS" | jq '.rtmp_streams // empty'

# Relay destinations
echo ""
echo "=== Relay Destinations ==="
echo "$METRICS" | jq '.rtmp_relay_destinations // empty'
```

**Note**: The metrics HTTP endpoint (`:8080`) is internal to the container. To access it externally, either:
- Use `az containerapp exec` to curl from inside the container (distroless image — won't work)
- Temporarily expose metrics via an additional ingress port
- Parse metrics from container logs instead (see log-based monitoring below)

### 2. Log-Based Monitoring (No Metrics Endpoint Required)

When metrics endpoint is not accessible, extract state from container logs.

```bash
# Recent startup + connection activity
az containerapp logs show -g "$RESOURCE_GROUP" -n "$RTMP_APP" --tail 100 2>/dev/null \
  | jq -r '.[] | .Log' 2>/dev/null | grep -oP '\{.*\}' | jq -r '
    select(.level != null) |
    [.time, .level, .msg, (.stream_key // .remote // "")] | @tsv
  ' 2>/dev/null | tail -30

# Count connections by type
echo "=== Connection Activity (from logs) ==="
LOGS=$(az containerapp logs show -g "$RESOURCE_GROUP" -n "$RTMP_APP" --tail 200 2>/dev/null)

PUBLISHES=$(echo "$LOGS" | grep -c '"publish started"' 2>/dev/null || echo 0)
PUB_STOPS=$(echo "$LOGS" | grep -c '"publish stopped"' 2>/dev/null || echo 0)
HANDSHAKE_FAILS=$(echo "$LOGS" | grep -c '"RTMP handshake failed"' 2>/dev/null || echo 0)
AUTH_FAILS=$(echo "$LOGS" | grep -c '"auth failed"' 2>/dev/null || echo 0)

echo "  Publish starts: $PUBLISHES"
echo "  Publish stops:  $PUB_STOPS"
echo "  Handshake fails: $HANDSHAKE_FAILS"
echo "  Auth failures:  $AUTH_FAILS"
echo "  Active streams (approx): $((PUBLISHES - PUB_STOPS))"

# Check for errors
echo ""
echo "=== Recent Errors ==="
echo "$LOGS" | grep -i '"level":"ERROR"' | tail -10
```

### 3. Container Health Check

Check the running state of all apps in the stack.

```bash
echo "=== Stack Health ==="
for APP_NAME in "$RTMP_APP" "$SIDECAR_APP" "$TRANSCODER_APP"; do
  if [ -z "$APP_NAME" ]; then continue; fi
  REVISION_INFO=$(az containerapp revision list -g "$RESOURCE_GROUP" -n "$APP_NAME" \
    --query "[?properties.active] | [0].{state: properties.runningState, replicas: properties.replicas, name: name}" \
    -o json 2>/dev/null)
  STATE=$(echo "$REVISION_INFO" | jq -r '.state // "unknown"')
  REPLICAS=$(echo "$REVISION_INFO" | jq -r '.replicas // 0')
  REV=$(echo "$REVISION_INFO" | jq -r '.name // "none"')

  if [ "$STATE" = "RunningAtMaxScale" ] || [ "$STATE" = "Running" ]; then
    STATUS="OK"
  elif [ "$REPLICAS" = "0" ]; then
    STATUS="SCALED-TO-ZERO"
  else
    STATUS="UNHEALTHY"
  fi
  printf "  %-40s %s (replicas: %s, revision: %s)\n" "$APP_NAME" "$STATUS" "$REPLICAS" "$REV"
done

# Port connectivity
echo ""
echo "=== External Connectivity ==="
DOMAIN="${RTMP_SUBDOMAIN:-stream}.${DNS_ZONE_NAME:-event-periscope.com}"
nc -z -w5 "$DOMAIN" 1935 2>/dev/null && echo "  RTMP  :1935 OPEN" || echo "  RTMP  :1935 CLOSED"
nc -z -w5 "$DOMAIN" 1936 2>/dev/null && echo "  RTMPS :1936 OPEN" || echo "  RTMPS :1936 CLOSED"
```

### 4. Stream-Specific Diagnostics

Investigate a specific stream key.

```bash
STREAM_KEY="live/test"  # Replace with actual stream key

# Search logs for this stream
az containerapp logs show -g "$RESOURCE_GROUP" -n "$RTMP_APP" --tail 200 2>/dev/null \
  | grep "$STREAM_KEY" | tail -20

# Check transcoder for this stream
az containerapp logs show -g "$RESOURCE_GROUP" -n "$TRANSCODER_APP" --tail 100 2>/dev/null \
  | grep "$STREAM_KEY" | tail -10

# Check sidecar for recording activity
az containerapp logs show -g "$RESOURCE_GROUP" -n "$SIDECAR_APP" --tail 100 2>/dev/null \
  | grep "$STREAM_KEY" | tail -10
```

### 5. SRT Statistics (If Enabled)

```bash
# Check if SRT is enabled
SRT_ENABLED=$(az containerapp show -g "$RESOURCE_GROUP" -n "$RTMP_APP" \
  --query "properties.template.containers[0].command" -o tsv 2>/dev/null \
  | grep -c 'srt-listen')

if [ "$SRT_ENABLED" -gt 0 ]; then
  echo "SRT is enabled"
  # SRT metrics from expvar (requires metrics endpoint access)
  # srt_connections_active, srt_connections_total
  # srt_bytes_received, srt_packets_received
  # srt_packets_retransmit, srt_packets_dropped
else
  echo "SRT not enabled on this deployment"
fi
```

### 6. Bandwidth Summary

Estimate bandwidth from logs or metrics.

```bash
# From logs: count media-related entries
LOGS=$(az containerapp logs show -g "$RESOURCE_GROUP" -n "$RTMP_APP" --tail 500 2>/dev/null)

VIDEO_MSGS=$(echo "$LOGS" | grep -c '"type_id":9' 2>/dev/null || echo "N/A")
AUDIO_MSGS=$(echo "$LOGS" | grep -c '"type_id":8' 2>/dev/null || echo "N/A")

echo "=== Bandwidth Indicators (from logs) ==="
echo "  Video messages: $VIDEO_MSGS"
echo "  Audio messages: $AUDIO_MSGS"
echo ""
echo "For precise byte counts, use the expvar metrics endpoint:"
echo "  rtmp_bytes_ingested, rtmp_bytes_egress"
echo "  rtmp_relay_bytes_sent"
```

## Available Metrics Reference

All metrics exposed via `/debug/vars` on the `-metrics-addr` port:

| Category | Metric | Type | Description |
|----------|--------|------|-------------|
| Connections | `rtmp_connections_active` | Gauge | Current open connections |
| | `rtmp_connections_total` | Counter | Total connections since start |
| Streams | `rtmp_streams_active` | Gauge | Currently publishing streams |
| Publishers | `rtmp_publishers_active` | Gauge | Active publishers |
| | `rtmp_publishers_total` | Counter | Total publish sessions |
| Subscribers | `rtmp_subscribers_active` | Gauge | Active subscribers |
| | `rtmp_subscribers_total` | Counter | Total subscribe sessions |
| | `rtmp_subscriber_drops_total` | Counter | Subscribers dropped (backpressure) |
| Auth | `rtmp_auth_successes_total` | Counter | Successful auth attempts |
| | `rtmp_auth_failures_total` | Counter | Failed auth attempts |
| Media | `rtmp_messages_video` | Counter | Video messages processed |
| | `rtmp_messages_audio` | Counter | Audio messages processed |
| | `rtmp_bytes_ingested` | Counter | Total bytes received |
| | `rtmp_bytes_egress` | Counter | Total bytes sent to subscribers |
| Handshake | `rtmp_handshake_failures_total` | Counter | Failed RTMP handshakes |
| Recording | `rtmp_recordings_active` | Gauge | Active FLV recordings |
| | `rtmp_recording_errors_total` | Counter | Recording errors |
| Health | `rtmp_zombie_connections_total` | Counter | Zombie connections detected |
| Relay | `rtmp_relay_messages_sent` | Counter | Relay messages forwarded |
| | `rtmp_relay_messages_dropped` | Counter | Relay messages dropped |
| | `rtmp_relay_bytes_sent` | Counter | Relay bytes forwarded |
| SRT | `srt_connections_active` | Gauge | Active SRT connections |
| | `srt_connections_total` | Counter | Total SRT connections |
| | `srt_bytes_received` | Counter | SRT bytes received |
| | `srt_packets_received` | Counter | SRT packets received |
| | `srt_packets_retransmit` | Counter | SRT retransmitted packets |
| | `srt_packets_dropped` | Counter | SRT dropped packets |

**Dynamic endpoints** (computed per request, also under `/debug/vars`):
- `rtmp_streams` — per-stream JSON with key, subscriber count, codecs, uptime
- `rtmp_relay_destinations` — per-relay-destination status and metrics

## Key Gotchas

1. **Metrics endpoint is internal-only** — the expvar server listens on the metrics port inside the container, not exposed via Container Apps ingress. Access it via logs, `az containerapp exec`, or by adding an HTTP ingress port
2. **Container Apps log streaming has a lag** — `az containerapp logs show` may be 10-30s behind real-time. Use `--follow` for live tail
3. **Scale-to-zero means no metrics** — if `minReplicas=0` and no active connections, the container is stopped. Metrics are lost between scale events (counters reset)
4. **Handshake EOF errors from `[::1]` are health probes** — Container Apps TCP health probes connect and disconnect immediately, producing "read C0+C1: EOF" warnings. These are normal
5. **Log retention is limited** — Container Apps retains logs for ~72 hours. For long-term monitoring, enable Azure Monitor or Application Insights

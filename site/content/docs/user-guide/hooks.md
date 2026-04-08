---
title: "Event Hooks"
weight: 5
---

# Event Hooks

go-rtmp fires hooks on RTMP lifecycle events, allowing external systems to react to stream activity in real-time. Hooks are **asynchronous** — they never block RTMP message processing.

## Available Events

| Event | Trigger |
|-------|---------|
| `connection_accept` | Client TCP connection accepted |
| `connection_close` | Client disconnected |
| `handshake_complete` | RTMP handshake finished |
| `stream_create` | Stream first created in registry |
| `stream_delete` | Stream removed (no publishers or subscribers) |
| `publish_start` | Publisher begins streaming |
| `publish_stop` | Publisher stops streaming |
| `play_start` | Subscriber begins playback |
| `play_stop` | Subscriber stops playback |
| `codec_detected` | Audio/video codec identified |
| `subscriber_count` | Subscriber count changed |
| `auth_failed` | Authentication attempt failed |

## Event Payload

Every event is delivered as a JSON object:

```json
{
  "type": "publish_start",
  "timestamp": 1705312222,
  "conn_id": "conn-abc123",
  "stream_key": "live/test",
  "data": {}
}
```

Some events include additional fields in `data`:

| Event | Data Fields |
|-------|-------------|
| `connection_accept` | `remote_addr` |
| `connection_close` | `role`, `duration_sec` |
| `publish_stop` | `audio_packets`, `video_packets`, `total_bytes`, `audio_codec`, `video_codec`, `duration_sec` |
| `play_stop` | `duration_sec` |
| `subscriber_count` | `count` |
| `auth_failed` | `action` (publish/play), `error` |

## Webhook Hook

Send HTTP POST requests to external URLs on specific events:

```bash
./rtmp-server \
  -hook-webhook "publish_start=https://api.example.com/on-publish" \
  -hook-webhook "publish_stop=https://api.example.com/on-unpublish" \
  -hook-webhook "auth_failed=https://alerts.example.com/auth"
```

The `-hook-webhook` flag format is `event_type=url`. It can be repeated for multiple event/URL pairs.

**HTTP request details:**

| Property | Value |
|----------|-------|
| Method | `POST` |
| Content-Type | `application/json` |
| Body | JSON event payload |
| Success | Any `2xx` status code |
| Failure | Logged at ERROR level |

## Shell Hook

Execute scripts on specific events:

```bash
./rtmp-server \
  -hook-script "connection_accept=/opt/scripts/on-connect.sh" \
  -hook-script "publish_start=/opt/scripts/on-publish.sh"
```

The script receives event data as environment variables:

| Variable | Content |
|----------|---------|
| `RTMP_EVENT_TYPE` | Event type string (e.g., `publish_start`) |
| `RTMP_TIMESTAMP` | Unix timestamp |
| `RTMP_CONN_ID` | Connection ID |
| `RTMP_STREAM_KEY` | Stream key (e.g., `live/test`) |
| `RTMP_<KEY>` | Additional data fields (uppercased, prefixed) |

Example script:

```bash
#!/bin/bash
echo "Event: $RTMP_EVENT_TYPE"
echo "Stream: $RTMP_STREAM_KEY"
echo "Time: $RTMP_TIMESTAMP"

if [ "$RTMP_EVENT_TYPE" = "publish_start" ]; then
  curl -s -X POST "https://dashboard.example.com/api/streams" \
    -d "{\"key\": \"$RTMP_STREAM_KEY\", \"status\": \"live\"}"
fi
```

## Stdio Hook

Print events to stderr for log pipeline ingestion:

```bash
# JSON format (one JSON object per line)
./rtmp-server -hook-stdio-format json

# Environment variable format
./rtmp-server -hook-stdio-format env
```

This is useful for piping to log aggregators:

```bash
./rtmp-server -hook-stdio-format json 2>&1 | jq --unbuffered '.type'
```

## Combining Hooks

All three hook types can be used simultaneously:

```bash
./rtmp-server -listen :1935 \
  -hook-stdio-format json \
  -hook-webhook "publish_start=https://api.example.com/on-publish" \
  -hook-webhook "auth_failed=https://alerts.example.com/auth" \
  -hook-script "connection_accept=/opt/scripts/on-connect.sh" \
  -hook-timeout 30s \
  -hook-concurrency 10
```

Each event fires all matching hooks in parallel.

## Configuration

| Flag | Default | Description |
|------|---------|-------------|
| `-hook-timeout` | `30s` | Maximum execution time per hook |
| `-hook-concurrency` | `10` | Maximum number of hooks executing in parallel |

The concurrency limit uses a bounded semaphore. When the pool is full, new hooks queue in goroutines until a slot opens. This prevents hook storms from consuming unlimited resources.

## Execution Model

- Hooks execute **asynchronously** — they never block RTMP message processing
- Each hook runs in its own goroutine with a timeout context
- A bounded worker pool (semaphore channel) limits concurrency
- Errors are logged at ERROR level; successes at DEBUG level
- Execution duration is tracked and logged

## Use Cases

| Scenario | Event | Action |
|----------|-------|--------|
| Trigger transcoding pipeline | `publish_start` | Webhook to start FFmpeg workers |
| Update live dashboard | `subscriber_count` | Webhook to push WebSocket update |
| Alert on unauthorized access | `auth_failed` | Webhook to security monitoring |
| Log stream analytics | `publish_stop` | Shell script to record duration/bytes |
| Feed log pipeline | All events | Stdio JSON to Elasticsearch/Loki |
| Cleanup on disconnect | `connection_close` | Shell script to release resources |

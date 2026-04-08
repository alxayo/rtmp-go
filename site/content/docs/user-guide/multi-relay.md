---
title: "Multi-Destination Relay"
weight: 3
---

# Multi-Destination Relay

go-rtmp can forward published streams to one or more external RTMP servers. This enables simulcasting, CDN distribution, and backup recording without any transcoding.

## Enabling Relay

Use the `-relay-to` flag, which can be specified multiple times:

```bash
./rtmp-server -listen :1935 \
  -relay-to rtmp://cdn1.example.com/live/key \
  -relay-to rtmp://cdn2.example.com/live/key
```

Each destination must use the `rtmp://` scheme and include a host. Destinations are validated at startup — invalid URLs cause the server to exit with an error.

## Use Cases

| Scenario | Configuration |
|----------|---------------|
| Simulcast to YouTube + Twitch | `-relay-to rtmp://a.rtmp.youtube.com/live2/yt-key -relay-to rtmp://live.twitch.tv/app/twitch-key` |
| Forward to CDN origin | `-relay-to rtmp://origin.cdn.example.com/live/ingest` |
| Backup recording server | `-relay-to rtmp://backup.example.com/live/archive` |
| Geographic distribution | `-relay-to rtmp://us-east.example.com/live/key -relay-to rtmp://eu-west.example.com/live/key` |

## How It Works

When a publisher starts streaming:

1. The **Destination Manager** initializes a relay client for each configured destination
2. Each audio (TypeID 8) and video (TypeID 9) message from the publisher is forwarded
3. Messages are sent to all destinations in parallel using a WaitGroup
4. Media is forwarded **exactly as received** — no transcoding, re-encoding, or modification

Relay runs alongside local subscribers and FLV recording simultaneously. A single published stream can be:

- Played by local subscribers
- Recorded to FLV
- Forwarded to multiple external servers

All at the same time.

## Per-Destination Metrics

Each relay destination tracks its own metrics:

| Metric | Type | Description |
|--------|------|-------------|
| Status | Enum | `disconnected`, `connecting`, `connected`, or `error` |
| MessagesSent | Counter | Total messages sent successfully |
| MessagesDropped | Counter | Messages dropped due to errors |
| BytesSent | Counter | Total bytes transmitted |
| LastSentTime | Timestamp | When the last message was sent |
| ConnectTime | Timestamp | When the connection was established |
| ReconnectCount | Counter | Number of reconnection attempts |

Global relay metrics are also exposed via the metrics endpoint:

```
rtmp_relay_messages_sent      # Total across all destinations
rtmp_relay_messages_dropped   # Total failures across all destinations
rtmp_relay_bytes_sent         # Total bytes across all destinations
```

## Error Handling

Relay follows the go-rtmp principle of graceful degradation:

- If a destination fails, the error is logged and `MessagesDropped` is incremented
- Other destinations continue receiving media normally
- Local subscribers and recording are completely unaffected
- Failed sends do not block the publisher

## Cleanup

When the publisher disconnects:

- All relay clients are shut down
- Destination connections are closed
- Metrics reflect the final state

## Example: Simulcast Setup

```bash
# Start server with relay to two destinations
./rtmp-server -listen :1935 \
  -relay-to rtmp://a.rtmp.youtube.com/live2/xxxx-xxxx-xxxx-xxxx \
  -relay-to rtmp://live.twitch.tv/app/live_123456789_abcdefg \
  -log-level info

# Publish from OBS or FFmpeg
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test

# The stream is now live on both YouTube and Twitch simultaneously
```

You can also combine relay with recording and local subscribers:

```bash
./rtmp-server -listen :1935 \
  -record-all true -record-dir ./recordings \
  -relay-to rtmp://cdn.example.com/live/ingest \
  -metrics-addr :8080

# One publisher → local FLV recording + CDN relay + local subscribers + metrics
```

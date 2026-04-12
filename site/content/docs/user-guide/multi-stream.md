---
title: "Multi-Stream"
weight: 8
---

# Multi-Stream

go-rtmp handles multiple simultaneous streams out of the box. Each stream is identified by a unique **stream key** and operates independently — with its own publisher, subscribers, recording, and authentication. Both RTMP and SRT publishers can run side by side on the same server.

## How It Works

Every stream lives in a central **stream registry** keyed by stream key (e.g. `live/cam1`, `live/cam2`). The rules are simple:

- **One publisher per key** — a stream key can have exactly one active publisher at a time. A second publish attempt to the same key is rejected with an error.
- **Unlimited subscribers per key** — any number of viewers can watch any active stream.
- **Full isolation** — streams do not interact. Publishing to `live/cam1` has no effect on `live/cam2`.
- **Mixed protocols** — RTMP and SRT publishers register in the same registry. Subscribers see no difference.

```
  Publisher A (RTMP)          Publisher B (SRT)
  live/cam1                   live/cam2
       │                           │
       ▼                           ▼
  ┌──────────────────────────────────────┐
  │          Stream Registry             │
  │  ┌────────────┐  ┌────────────┐      │
  │  │ live/cam1  │  │ live/cam2  │      │
  │  │ pub + subs │  │ pub + subs │      │
  │  └────────────┘  └────────────┘      │
  └──────────────────────────────────────┘
       │         │         │
       ▼         ▼         ▼
   Viewer 1  Viewer 2  Viewer 3
   (cam1)    (cam1)    (cam2)
```

## Publishing Multiple Streams

Each publisher targets a different stream key. You can mix RTMP and SRT freely — just use different keys.

### Two RTMP Publishers

```bash
# Camera 1 via RTMP
ffmpeg -re -i camera1.mp4 -c copy -f flv rtmp://localhost:1935/live/cam1

# Camera 2 via RTMP
ffmpeg -re -i camera2.mp4 -c copy -f flv rtmp://localhost:1935/live/cam2
```

### SRT Publisher

```bash
# Camera 3 via SRT (stream ID sets the key)
ffmpeg -re -i camera3.mp4 -c copy -f mpegts \
  "srt://localhost:6000?streamid=live/cam3&pkt_size=1316"
```

### Mixed-Protocol Setup

```bash
# Start server with both RTMP and SRT enabled
./rtmp-server -listen :1935 -srt-listen :6000

# Publish cam1 over RTMP
ffmpeg -re -i cam1.mp4 -c copy -f flv rtmp://localhost:1935/live/cam1

# Publish cam2 over SRT
ffmpeg -re -i cam2.mp4 -c copy -f mpegts \
  "srt://localhost:6000?streamid=live/cam2&pkt_size=1316"
```

Both streams appear in the same registry. Subscribers connect via RTMP regardless of how the stream was published.

## Recording

When recording is enabled, each stream gets its own recording file.

### Enabling

```bash
./rtmp-server -record-all true -record-dir ./recordings
```

### File Naming

Files are named with the stream key and a timestamp:

```
{streamkey}_{YYYYMMDD}_{HHMMSS}.{ext}
```

For example, with two active streams:

```
recordings/
├── live_cam1_20250101_143000.flv
├── live_cam2_20250101_143005.mp4
└── live_cam3_20250101_143010.flv
```

### Format Selection

The recording format is auto-detected from the first video frame:

| Codec | Format |
|-------|--------|
| H.264 | FLV |
| H.265 (HEVC) | MP4 |
| AV1 | MP4 |
| VP9 | MP4 |

Each stream selects its format independently — one stream can record as FLV while another records as MP4 on the same server.

## Subscribing to Streams

Subscribers connect to individual stream keys via RTMP play URLs:

```bash
# Watch cam1
ffplay rtmp://localhost:1935/live/cam1

# Watch cam2
ffplay rtmp://localhost:1935/live/cam2

# Watch cam3 (published via SRT, watched via RTMP)
ffplay rtmp://localhost:1935/live/cam3
```

### Late-Join

When a new subscriber connects to an active stream, the server immediately sends cached **video and audio sequence headers**. This means viewers see the first frame quickly without waiting for the next keyframe from the publisher.

### Multiple Viewers

Each stream supports unlimited concurrent viewers. Subscribers receive independent copies of media data, so a slow viewer does not block others or the publisher.

## Authentication

Authentication is applied **per stream key**. Each key can have its own credentials, and auth is enforced independently for both publishing and playback.

### Token Mode

```bash
./rtmp-server -auth-mode token \
  -auth-token "live/cam1=secret1" \
  -auth-token "live/cam2=secret2" \
  -auth-token "live/cam3=secret3"
```

Publishers and subscribers pass the token as a query parameter:

```bash
# Publish with auth
ffmpeg -re -i cam1.mp4 -c copy -f flv "rtmp://localhost:1935/live/cam1?token=secret1"

# Subscribe with auth
ffplay "rtmp://localhost:1935/live/cam1?token=secret1"
```

### File and Callback Modes

- **File mode** — a JSON file maps each stream key to its token independently.
- **Callback mode** — a webhook receives the `stream_key` with each request, allowing full per-stream control.

For full details, see [Authentication]({{< relref "authentication" >}}).

## Constraints

| Constraint | Detail |
|------------|--------|
| One publisher per key | A second publish to the same key is rejected. The first publisher must disconnect before another can start. |
| No wildcard subscribe | Subscribers must specify an exact stream key. There is no way to subscribe to "all streams" at once. |
| All-or-nothing recording | `-record-all true` records every stream. There is no per-stream recording toggle. |
| Unique keys across protocols | An RTMP publisher and an SRT publisher cannot use the same key simultaneously. |

## Example: Complete Multi-Stream Setup

A production-like setup with three cameras, mixed protocols, authentication, and recording.

### Start the Server

```bash
./rtmp-server \
  -listen :1935 \
  -srt-listen :6000 \
  -record-all true \
  -record-dir ./recordings \
  -auth-mode token \
  -auth-token "live/cam1=tok_cam1" \
  -auth-token "live/cam2=tok_cam2" \
  -auth-token "live/cam3=tok_cam3" \
  -log-level info
```

### Publish Three Streams

```bash
# Camera 1: RTMP, H.264
ffmpeg -re -i cam1.mp4 -c copy -f flv \
  "rtmp://localhost:1935/live/cam1?token=tok_cam1"

# Camera 2: RTMP, H.265 (records as MP4)
ffmpeg -re -i cam2.mp4 -c copy -f flv \
  "rtmp://localhost:1935/live/cam2?token=tok_cam2"

# Camera 3: SRT, H.264 (SRT has no per-stream auth — see note below)
ffmpeg -re -i cam3.mp4 -c copy -f mpegts \
  "srt://localhost:6000?streamid=live/cam3&pkt_size=1316"
```

### Subscribe

```bash
# Watch camera 1
ffplay "rtmp://localhost:1935/live/cam1?token=tok_cam1"

# Watch camera 2 from a second viewer
ffplay "rtmp://localhost:1935/live/cam2?token=tok_cam2"

# Watch camera 3 (SRT-published, RTMP-played)
ffplay "rtmp://localhost:1935/live/cam3?token=tok_cam3"
```

### Resulting Recordings

```
recordings/
├── live_cam1_20250715_100000.flv   ← H.264 → FLV
├── live_cam2_20250715_100002.mp4   ← H.265 → MP4
└── live_cam3_20250715_100005.flv   ← H.264 → FLV
```

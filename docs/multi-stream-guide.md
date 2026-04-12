# Multi-Stream Ingest Guide

This document describes how go-rtmp handles multiple simultaneous streams, covering the internal architecture, data flow, protocol bridging, and operational considerations.

## Architecture

### Stream Registry

The stream registry is the central data structure that manages all active streams. It lives in `internal/rtmp/server/registry.go`.

```
type Registry struct {
    mu      sync.RWMutex
    streams map[string]*Stream   // keyed by stream key, e.g. "live/cam1"
}
```

Each `Stream` holds:

- **One Publisher** — the connection currently publishing media to this key.
- **Subscriber slice** — all connections currently playing this key.
- **Cached sequence headers** — the most recent video and audio sequence headers, used for late-join.

### Per-Stream Isolation

Streams are fully isolated by key. Operations on `live/cam1` never touch `live/cam2`. The registry map is the only shared structure, and it is protected by a single `sync.RWMutex`:

| Operation | Lock Type |
|-----------|-----------|
| `SetPublisher()` | Write lock |
| `RemovePublisher()` | Write lock |
| `AddSubscriber()` | Write lock |
| `RemoveSubscriber()` | Write lock |
| `BroadcastMessage()` | Read lock (snapshot subscribers, then release) |
| `GetStream()` | Read lock |

Write operations are infrequent (connect/disconnect events). Broadcast — the hot path — only takes a read lock to snapshot the subscriber list, then sends to each subscriber outside the lock.

### Concurrency Model

Each connection runs one `readLoop` goroutine with context cancellation and TCP deadline enforcement:

- **Read deadline**: 90 seconds (detects zombie publishers)
- **Write deadline**: 30 seconds (detects stuck subscribers)

Outbound delivery uses bounded channels:

```go
outboundQueue := make(chan *chunk.Message, 100)
```

This ensures a slow subscriber's full queue does not block the publisher or other subscribers.

## Data Flow

```
  RTMP Publisher ──► Handshake ──► Command RPC ──► SetPublisher()
                                                        │
  SRT Publisher ──► SRT Bridge ──► MPEG-TS Demux ──► SetPublisher()
                                                        │
                                                        ▼
                                                 ┌─────────────┐
                                                 │   Stream     │
                                                 │  Registry    │
                                                 │             │
                                                 │ live/cam1:  │
                                                 │  pub, subs, │
                                                 │  seq hdrs   │
                                                 │             │
                                                 │ live/cam2:  │
                                                 │  pub, subs, │
                                                 │  seq hdrs   │
                                                 └──────┬──────┘
                                                        │
                              ┌──────────────┬──────────┼──────────┐
                              ▼              ▼          ▼          ▼
                         Subscriber 1  Subscriber 2  Recorder   Relay
                         (RTMP play)   (RTMP play)   (per-key)  (optional)
```

1. **Ingest** — Publisher connects via RTMP or SRT. After handshake and command exchange, the connection calls `SetPublisher(key)` on the registry.
2. **Registration** — The registry creates a `Stream` entry (if new) and assigns the publisher. If a publisher already exists for that key, `ErrPublisherExists` is returned and the connection is rejected.
3. **Media flow** — The publisher's `readLoop` reads chunks, assembles messages, and calls `BroadcastMessage()`. This snapshots the subscriber list under a read lock and sends independent payload copies to each subscriber.
4. **Recording** — If enabled, a recorder consumer is attached to the stream. It receives the same broadcast messages.
5. **Teardown** — When the publisher disconnects, the disconnect handler removes the publisher from the registry, closes all associated subscribers, and stops the recorder.

## Stream Key Semantics

### Format

Stream keys follow the pattern `{app}/{streamName}`, derived from the RTMP `connect` and `publish` commands:

- `connect("live")` + `publish("cam1")` → key = `live/cam1`
- `connect("app")` + `publish("stream")` → key = `app/stream`

For SRT, the stream key is extracted from the **SRT stream ID** parameter:

- `srt://host:6000?streamid=live/cam2` → key = `live/cam2`

### Uniqueness

Keys are exact-match strings. `live/cam1` and `live/CAM1` are different keys. The registry enforces:

- **One publisher per key** — `SetPublisher()` checks for an existing publisher and returns `ErrPublisherExists` if one is active.
- **Cross-protocol uniqueness** — RTMP and SRT share the same registry. An SRT publisher on `live/cam1` blocks an RTMP publisher from using the same key, and vice versa.

### Key Lifecycle

```
1. Publisher connects         → Stream created, publisher set
2. Subscribers connect        → Added to subscriber slice
3. Media flows                → Broadcast to all subscribers
4. Publisher disconnects      → Publisher removed, subscribers closed, stream cleaned up
5. New publisher connects     → Fresh stream, new subscribers can join
```

## Recording Details

### Lazy Initialization

The recorder for a stream is not created at publish time. Instead, it is **lazily initialized** when the first media frame arrives. This allows the server to inspect the video codec before choosing a container format.

### Codec Detection and Format Selection

The first video frame determines the recording format:

| Codec | Container | Reason |
|-------|-----------|--------|
| H.264 | FLV | Native RTMP codec, FLV is the natural fit |
| H.265 (HEVC) | MP4 | FLV does not support HEVC |
| AV1 | MP4 | FLV does not support AV1 |
| VP9 | MP4 | FLV does not support VP9 |

Each stream selects its format independently. A server with three streams might produce:

```
recordings/
├── live_cam1_20250715_100000.flv   ← cam1 publishes H.264
├── live_cam2_20250715_100002.mp4   ← cam2 publishes H.265
└── live_cam3_20250715_100005.flv   ← cam3 publishes H.264
```

### File Naming

Format: `{streamkey}_{YYYYMMDD}_{HHMMSS}.{ext}`

- The stream key has `/` replaced (e.g. `live/cam1` → `live_cam1`).
- The timestamp is when the recording started (first media frame).
- Extension is `.flv` or `.mp4` based on codec detection.

### Configuration

```bash
./rtmp-server -record-all true -record-dir ./recordings
```

- `-record-all true` — enables recording for all streams (no per-stream toggle).
- `-record-dir` — output directory (default: `recordings/`).

## Consumer Patterns

### Late-Join with Cached Sequence Headers

When a publisher sends a video or audio sequence header (the codec configuration record), the stream caches it. When a new subscriber joins mid-stream, the server immediately sends the cached headers before any media frames. This allows the subscriber's decoder to initialize without waiting for the publisher's next keyframe.

```
Publisher sends:  [seq hdr] [frame] [frame] [frame] ...
                                          ↑
                                   Subscriber joins
                                          │
Subscriber receives: [cached seq hdr] [frame] [frame] ...
```

### Fan-Out Broadcasting

`BroadcastMessage()` follows this sequence:

1. Acquire read lock on the stream.
2. Snapshot the subscriber slice (copy the slice header, not the underlying data).
3. Release the read lock.
4. For each subscriber, create an independent copy of the payload and deliver it.

Payload copies prevent data races — each subscriber owns its buffer and can process or discard it independently.

### Non-Blocking Delivery

Delivery to each subscriber uses `TrySendMessage()`, which attempts a non-blocking send on the subscriber's outbound channel:

- If the channel has capacity, the message is queued.
- If the channel is full (slow subscriber), the message is dropped for that subscriber.

This ensures a single slow subscriber cannot:
- Block the publisher's `readLoop`
- Delay delivery to other subscribers
- Cause backpressure up to the ingest path

### Subscriber Independence

Each subscriber operates on its own goroutine with its own outbound queue. Subscribers can:
- Join and leave at any time without affecting other subscribers.
- Watch different streams simultaneously from the same client IP.
- Experience different effective bitrates based on their connection speed (via frame dropping).

## Multi-Protocol

### SRT Bridge

SRT streams arrive as MPEG-TS over UDP. The SRT bridge converts them into the internal `chunk.Message` format used by the RTMP layer:

```
SRT Packet (UDP)
    │
    ▼
MPEG-TS Demuxer
    │
    ├── Video PES → chunk.Message (TypeID 9)
    └── Audio PES → chunk.Message (TypeID 8)
                        │
                        ▼
                  Registry.SetPublisher("live/cam2")
```

After conversion, the SRT stream is indistinguishable from an RTMP stream inside the registry. Subscribers, recorders, and relay clients all receive the same `chunk.Message` type regardless of ingest protocol.

### Protocol Mixing Rules

| Scenario | Allowed? |
|----------|----------|
| RTMP publisher on `live/cam1`, SRT publisher on `live/cam2` | Yes |
| Two RTMP publishers on different keys | Yes |
| Two SRT publishers on different keys | Yes |
| RTMP + SRT publisher on the **same** key | No — `ErrPublisherExists` |
| SRT publisher, RTMP subscriber (same key) | Yes |
| RTMP publisher, RTMP subscriber (same key) | Yes |

### SRT Authentication

> **Current Limitation**: SRT streams have no per-stream authentication. The `-srt-passphrase` flag exists but encryption is not yet functional (see [SRT Ingest](../site/content/docs/user-guide/srt-ingest.md) for details). SRT publishers are accepted based solely on the stream key in the Stream ID. Security relies on stream key obscurity and network-level controls (firewalls, VPNs). For authenticated ingest, use RTMP with token auth or RTMPS.

## Scaling Considerations

### No Global Stream Limit

The registry is a Go map with no hard-coded size limit. The practical limit is determined by:

- **Memory** — each stream holds cached sequence headers (~few KB) plus per-subscriber outbound buffers.
- **CPU** — each connection runs one goroutine. With Go's lightweight goroutines, thousands of concurrent connections are feasible.
- **Network** — inbound bandwidth scales linearly with the number of publishers; outbound scales with publishers × subscribers.

### Per-Connection Goroutine Model

Each connection (publisher or subscriber) runs exactly one goroutine for its read loop. This model:

- Avoids thread pool contention.
- Allows the Go scheduler to multiplex connections across OS threads.
- Keeps memory overhead low (~8 KB stack per goroutine, growing on demand).

### Bounded Outbound Queues

Each subscriber has a bounded channel (default capacity 100 messages). This provides backpressure without blocking:

- If a subscriber falls behind, frames are dropped rather than queued indefinitely.
- Memory usage per subscriber is bounded regardless of publisher bitrate.
- The publisher's read loop is never blocked by subscriber delivery.

### Resource Estimation

| Resource | Per-Stream Overhead | Per-Subscriber Overhead |
|----------|-------------------|------------------------|
| Goroutines | 1 (publisher read loop) | 1 (subscriber write loop) |
| Memory | ~10 KB (seq headers + metadata) | ~100 KB (outbound buffer) |
| File descriptors | 1 (TCP/UDP socket) | 1 (TCP socket) |
| Disk I/O | 1 file if recording | None |

For example, 50 streams with 20 subscribers each ≈ 50 + 1000 goroutines, ~100 MB outbound buffers.

## Complete Example

A production-like setup with mixed protocols, per-stream authentication, and recording.

### Server

```bash
./rtmp-server \
  -listen :1935 \
  -srt-listen :6000 \
  -record-all true \
  -record-dir /data/recordings \
  -auth-mode file \
  -auth-file /etc/rtmp/tokens.json \
  -log-level info
```

Token file (`/etc/rtmp/tokens.json`):

```json
{
  "live/cam1": "tok_front_door",
  "live/cam2": "tok_parking_lot",
  "live/cam3": "tok_lobby"
}
```

### Publishers

```bash
# Camera 1: RTMP, H.264 (records as FLV)
ffmpeg -re -i /dev/video0 -c:v libx264 -c:a aac -f flv \
  "rtmp://server:1935/live/cam1?token=tok_front_door"

# Camera 2: RTMP, H.265 (records as MP4)
ffmpeg -re -i /dev/video1 -c:v libx265 -c:a aac -f flv \
  "rtmp://server:1935/live/cam2?token=tok_parking_lot"

# Camera 3: SRT, H.264 (records as FLV) — no per-stream auth on SRT
ffmpeg -re -i /dev/video2 -c:v libx264 -c:a aac -f mpegts \
  "srt://server:6000?streamid=live/cam3&pkt_size=1316"
```

### Subscribers

```bash
# Security monitor watching all three feeds
ffplay "rtmp://server:1935/live/cam1?token=tok_front_door"
ffplay "rtmp://server:1935/live/cam2?token=tok_parking_lot"
ffplay "rtmp://server:1935/live/cam3?token=tok_lobby"

# Second viewer on cam1 (independent connection)
ffplay "rtmp://server:1935/live/cam1?token=tok_front_door"
```

### Expected Output

Server logs show independent stream lifecycles:

```
INF connection registered conn_id=1 remote=192.168.1.10:50001
INF publisher started stream_key=live/cam1 protocol=rtmp
INF connection registered conn_id=2 remote=192.168.1.11:50002
INF publisher started stream_key=live/cam2 protocol=rtmp
INF connection registered conn_id=3 remote=192.168.1.12:50003
INF publisher started stream_key=live/cam3 protocol=srt
INF subscriber joined stream_key=live/cam1 conn_id=4
INF subscriber joined stream_key=live/cam2 conn_id=5
INF subscriber joined stream_key=live/cam3 conn_id=6
INF subscriber joined stream_key=live/cam1 conn_id=7
```

Recording directory after all three streams publish:

```
/data/recordings/
├── live_cam1_20250715_100000.flv
├── live_cam2_20250715_100002.mp4
└── live_cam3_20250715_100005.flv
```

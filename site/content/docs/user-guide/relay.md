---
title: "Live Relay"
weight: 2
---

# Live Relay

Live relay is the core of go-rtmp. It enables real-time pub/sub streaming: one publisher sends media to a stream key, and any number of subscribers receive it simultaneously.

## How Pub/Sub Works

The server maintains a **stream registry** — a thread-safe map of active streams keyed by their full stream key (e.g., `live/test`).

1. A **publisher** connects and issues a `publish` command for a stream key
2. The server creates a stream entry in the registry
3. **Subscribers** connect and issue `play` commands for the same stream key
4. Each media message from the publisher is broadcast to all active subscribers

Only one publisher is allowed per stream key. Attempting to publish to an occupied key results in an error. There is no limit on the number of subscribers.

## Multiple Subscribers

Each subscriber gets an independent connection with its own outbound queue. Adding or removing subscribers does not affect the publisher or other subscribers. The subscriber list is managed under a read-write lock — broadcast takes a snapshot of subscribers under a read lock, so subscriber joins and leaves don't block media delivery.

## Message Broadcast

When the publisher sends an audio or video message:

1. **Codec detection** runs (one-shot, on the first media frames)
2. A copy is sent to the **FLV recorder** (if recording is enabled)
3. A **snapshot** of all subscribers is taken under a read lock
4. The message payload is **cloned** for each subscriber to prevent data corruption
5. Each subscriber receives its copy independently

## Late-Join Support

When a subscriber joins mid-stream, they need codec initialization data before their decoder can render frames. go-rtmp handles this automatically by caching **sequence headers**:

| Header Type | Detection | Contains |
|-------------|-----------|----------|
| **H.264 Sequence Header** | Video message (TypeID 9) with `avc_packet_type=0` | SPS/PPS parameters |
| **AAC Sequence Header** | Audio message (TypeID 8) with AAC format and `aac_packet_type=0` | AudioSpecificConfig |

When a new subscriber joins, the server sends any cached sequence headers **before** forwarding live media. This means the subscriber's decoder initializes instantly — no waiting for the next keyframe.

## Backpressure

Each subscriber connection has a bounded outbound queue to prevent slow consumers from blocking the publisher:

| Setting | Value | Purpose |
|---------|-------|---------|
| Queue size | 100 messages | Per-connection send buffer |
| Send timeout | 200ms | Maximum wait for queue space |
| On full queue | Drop message | Publisher stays unblocked |

If a subscriber can't keep up (slow network, overloaded client), messages are dropped for that subscriber only. The publisher and other subscribers are completely unaffected.

At 30 fps, the 100-message queue provides roughly 3 seconds of buffer before drops begin.

## Example: Multi-Viewer Setup

```bash
# Terminal 1: Start the server
./rtmp-server -listen :1935 -log-level info

# Terminal 2: Publish a stream
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test

# Terminals 3, 4, 5: Open multiple subscribers
ffplay rtmp://localhost:1935/live/test
ffplay rtmp://localhost:1935/live/test
ffplay rtmp://localhost:1935/live/test
```

All three ffplay windows will display the stream simultaneously. If you start terminal 5 several seconds after the publisher began, late-join caching ensures it starts playing immediately without waiting for a keyframe.

## Cleanup

When a publisher disconnects:

- The stream entry is removed from the registry
- All active subscribers are notified and disconnected
- The FLV recorder (if active) closes the file
- Relay clients (if configured) are shut down
- Metrics are updated (active publishers/subscribers decremented)

Subscriber disconnects are handled independently — the publisher and other subscribers are not affected.

---
title: "Architecture"
weight: 1
---

# Architecture

## High-Level Data Flow

Every RTMP connection follows this path:

```
TCP Accept → Handshake → Control Burst → Command RPC → Media Relay/Recording
```

1. **TCP Accept** — the server accepts an inbound TCP connection
2. **Handshake** — client and server exchange C0/C1/C2 ↔ S0/S1/S2 packets to establish the session
3. **Control Burst** — both sides exchange Window Acknowledgement Size, Set Peer Bandwidth, and Set Chunk Size
4. **Command RPC** — AMF0-encoded commands (`connect`, `createStream`, `publish`/`play`) negotiate the stream
5. **Media Relay/Recording** — audio (TypeID 8) and video (TypeID 9) messages flow through the relay to subscribers and optionally to disk as FLV

## Architecture Diagram

```
                    ┌──────────────────────────────────┐
                    │         TCP Listener(s)           │
                    │   Plain (:1935) + TLS (:1936)     │
                    └──────────┬───────────────────────┘
                               │ Accept()
                               ▼
                    ┌──────────────────────────────────┐
                    │          Handshake Layer          │
                    │    C0/C1/C2 ↔ S0/S1/S2 exchange  │
                    └──────────┬───────────────────────┘
                               │
                               ▼
                    ┌──────────────────────────────────┐
                    │         Chunk Layer               │
                    │  Message ↔ Chunk fragmentation    │
                    └──────────┬───────────────────────┘
                               │
                    ┌──────────┴───────────────────────┐
                    │                                   │
              ┌─────▼─────┐                     ┌──────▼──────┐
              │  Commands  │                     │    Media    │
              │  (TypeID   │                     │  (TypeID    │
              │   20)      │                     │   8=audio   │
              │            │                     │   9=video)  │
              └─────┬──────┘                     └──────┬──────┘
                    │                                   │
              ┌─────▼──────┐                     ┌──────▼──────┐
              │  RPC Layer │                     │Media Dispatch│
              │  connect   │                     │  Record     │
              │ createStream│                    │  Broadcast  │
              │  publish   │                     │  Relay      │
              │  play      │                     │             │
              └──────┬─────┘                     └─────────────┘
                     │
              ┌──────▼──────┐
              │ Event Hooks  │
              └─────────────┘
```

## Package Map

| Package | Purpose | Key Types |
|---------|---------|-----------|
| `internal/rtmp/handshake` | RTMP v3 handshake (C0/C1/C2 ↔ S0/S1/S2) | `Handshake`, `State` |
| `internal/rtmp/chunk` | Message ↔ chunk fragmentation/reassembly | `Reader`, `Writer`, `ChunkHeader`, `Message` |
| `internal/rtmp/amf` | AMF0 binary codec | `EncodeAll()`, `DecodeAll()` |
| `internal/rtmp/control` | Control messages (types 1-6) | `Decode()`, `Handle()`, `Context` |
| `internal/rtmp/rpc` | Command parsing & response building | `Dispatcher`, `ConnectCommand`, `PublishCommand` |
| `internal/rtmp/conn` | Connection lifecycle | `Connection`, `Session` |
| `internal/rtmp/server` | Listener, stream registry, pub/sub | `Server`, `Registry`, `Stream`, `Config` |
| `internal/rtmp/server/auth` | Token-based authentication validators | `Validator`, `TokenValidator`, `FileValidator`, `CallbackValidator` |
| `internal/rtmp/server/hooks` | Event notification | `HookManager`, `Event`, `Hook` |
| `internal/rtmp/media` | Audio/video parsing, codec detection (Enhanced RTMP), FLV recording | `Recorder`, `CodecDetector` |
| `internal/rtmp/relay` | Multi-destination relay | `DestinationManager`, `Destination` |
| `internal/rtmp/metrics` | Expvar counters | `ConnectionsActive`, `BytesIngested` |
| `internal/rtmp/client` | Minimal RTMP client for testing | `Client` |
| `internal/errors` | Domain-specific error types | `ProtocolError`, `ChunkError`, `AMFError` |
| `internal/logger` | Structured logging | `Init()`, `Logger()` |

## Connection Lifecycle

Here's what happens step-by-step when OBS connects and starts streaming:

### 1. TCP Accept

The `Server` goroutine calls `listener.Accept()` in a loop. Each accepted connection gets its own goroutine.

### 2. Handshake (C0/C1/C2 ↔ S0/S1/S2)

The handshake package runs a state machine:
- **Client sends C0+C1** (1 + 1536 bytes) — version byte + timestamp + random data
- **Server responds with S0+S1+S2** (1 + 1536 + 1536 bytes) — version + timestamp + echo of C1
- **Client sends C2** (1536 bytes) — echo of S1
- Handshake is complete. Total: 6145 bytes exchanged. Timeout: 5 seconds.

### 3. Control Burst

Immediately after handshake, both sides send control messages on CSID 2, MSID 0:
- **Window Acknowledgement Size** (TypeID 5) — how many bytes before sending an ACK
- **Set Peer Bandwidth** (TypeID 6) — output bandwidth limit
- **Set Chunk Size** (TypeID 1) — increase from 128-byte default to 4096 bytes

### 4. Command RPC (AMF0)

OBS sends a series of AMF0-encoded commands on TypeID 20:
1. **`connect`** — carries the `app` name (e.g., `live`), `tcUrl`, and other properties. Server responds with `_result` containing connection info.
2. **`releaseStream`** + **`FCPublish`** — optional setup commands from some clients.
3. **`createStream`** — allocates a message stream. Server responds with `_result` containing the stream ID.
4. **`publish`** — begins publishing with the stream name (e.g., `mystream`). Server responds with `onStatus` indicating success.

### 5. Media Flow

Once `publish` succeeds:
- OBS sends **sequence headers** first — H.264 SPS/PPS, H.265 HEVCDecoderConfigurationRecord, or other codec config (video) and AAC AudioSpecificConfig (audio). These are cached by the server for late-join support.
- OBS then sends continuous **audio (TypeID 8)** and **video (TypeID 9)** chunks.
- The server's media dispatch fan-outs each message to all subscribers and optionally writes to disk (FLV recording) and forwards to relay destinations.

### 6. Subscriber Joins (ffplay)

When a subscriber connects:
1. Completes handshake + control burst
2. Sends `connect` → `createStream` → `play`
3. Server immediately sends cached **sequence headers** (late-join support)
4. Server adds subscriber to the stream's fan-out list
5. All subsequent media messages are forwarded in real-time

### 7. Disconnect

When the publisher disconnects:
- TCP connection closes or read deadline fires
- Disconnect handler cleans up: removes from registry, stops recording, notifies hooks
- All subscribers receive connection close

## Key Concepts

### Chunks vs Messages

An RTMP **message** is a logical unit (e.g., one video frame, one audio packet, one command). Messages can be large — a keyframe might be 50KB.

A **chunk** is a transport-level fragment. The default chunk size is 128 bytes. A 50KB video frame becomes ~390 chunks. Chunking allows multiplexing: small audio chunks interleave with large video chunks on the same TCP connection.

### CSID (Chunk Stream ID)

Identifies the logical channel within a connection. Each CSID maintains its own header compression state (FMT types 1-3 delta from the previous chunk on the same CSID).

- CSID 2: control messages
- CSID 3+: command and media streams

### MSID (Message Stream ID)

Identifies the logical stream within a connection. A single connection can carry multiple streams (e.g., a control stream at MSID 0 and a media stream at MSID 1).

### Stream Keys

A stream key is derived from the RTMP URL: `rtmp://host:port/app/streamName`. The server combines `app` + `streamName` to form the registry key (e.g., `live/mystream`). This is used for pub/sub matching — publishers and subscribers on the same key share a stream.

### Sequence Headers

H.264 video requires SPS/PPS (Sequence Parameter Set / Picture Parameter Set) to initialize the decoder. AAC audio requires AudioSpecificConfig. These are sent as the first media messages after `publish` and are identified by a specific byte pattern (type byte 0x00 = sequence header).

Enhanced RTMP codecs (H.265, AV1, VP9) use the same sequence header mechanism but are identified by a FourCC code instead of a CodecID byte.

The server caches these so that late-joining subscribers receive them immediately, enabling instant video playback without waiting for the next keyframe cycle.

## Reading Order for New Contributors

If you're diving into the source code, read packages in this order:

1. `internal/errors/errors.go` — error types used everywhere
2. `internal/logger/logger.go` — logging setup
3. `internal/rtmp/handshake/` — simplest protocol layer, good warm-up
4. `internal/rtmp/amf/` — AMF0 encoding/decoding (used by commands)
5. `internal/rtmp/chunk/` — chunk reader/writer (core transport)
6. `internal/rtmp/control/` — control message handling
7. `internal/rtmp/rpc/` — command dispatch (connects everything)
8. `internal/rtmp/conn/` — connection state machine
9. `internal/rtmp/media/` — audio/video handling + FLV recording
10. `internal/rtmp/relay/` — multi-destination forwarding
11. `internal/rtmp/server/` — ties it all together
12. `internal/rtmp/server/auth/` — authentication layer
13. `internal/rtmp/server/hooks/` — event hooks
14. `internal/rtmp/metrics/` — expvar counters
15. `cmd/rtmp-server/main.go` — CLI entry point

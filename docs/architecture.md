# Architecture Guide

This document explains how the go-rtmp server works from the ground up. If you're new to the codebase, read this first.

## What is RTMP?

RTMP (Real-Time Messaging Protocol) is a TCP-based protocol originally designed by Adobe for streaming audio, video, and data. It's widely used for live streaming — when you broadcast from OBS Studio to Twitch or YouTube, RTMP is typically the protocol carrying your stream.

This project implements an RTMP server (and minimal client) in pure Go with zero external dependencies. It can receive live streams from tools like OBS/FFmpeg, relay them to subscribers, record them to FLV files, and forward them to other RTMP servers.

## High-Level Architecture

```
                    ┌──────────────────────────────────┐
                    │           TCP Listener            │
                    │         (net.Listener)            │
                    └──────────┬───────────────────────┘
                               │ Accept()
                               ▼
                    ┌──────────────────────────────────┐
                    │          Handshake Layer          │
                    │    C0/C1/C2 ↔ S0/S1/S2 exchange  │
                    │      (internal/rtmp/handshake)    │
                    └──────────┬───────────────────────┘
                               │
                               ▼
                    ┌──────────────────────────────────┐
                    │       Control Burst               │
                    │  Set Chunk Size + Window Ack +    │
                    │  Set Peer Bandwidth               │
                    │      (internal/rtmp/conn)         │
                    └──────────┬───────────────────────┘
                               │
                               ▼
                    ┌──────────────────────────────────┐
                    │         Chunk Layer               │
                    │  Message ↔ Chunk fragmentation    │
                    │  FMT 0-3 header compression       │
                    │      (internal/rtmp/chunk)        │
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
              │  Webhooks   │
              │  Shell      │
              │  Stdio      │
              └─────────────┘
```

## Data Flow: Step by Step

Here's what happens when OBS connects and starts streaming:

### 1. TCP Connection
The server listens on port 1935 (default). When OBS connects, `net.Listener.Accept()` returns a raw TCP connection.

### 2. Handshake (`internal/rtmp/handshake`)
Both sides exchange version bytes and random data:
- Client sends C0 (version 0x03) + C1 (1536 bytes of random data)
- Server responds with S0 + S1 + S2 (echo of C1)
- Client sends C2 (echo of S1)

This takes ~5 bytes + 2×1536 bytes each way. The handshake validates that both sides speak RTMP v3.

### 3. Control Burst (`internal/rtmp/conn`)
Immediately after handshake, the server sends three control messages:
- **Set Chunk Size** (4096 bytes): Increases chunk payload size from the default 128.
- **Window Acknowledgement Size** (2,500,000 bytes): Flow control threshold.
- **Set Peer Bandwidth** (2,500,000 bytes): Output rate limit.

TCP deadlines are enforced on the underlying connection:
- **Read deadline**: 90 seconds — detects frozen publishers and stuck subscribers
- **Write deadline**: 30 seconds — prevents slow-loris attacks and half-open connections

### 4. Command Exchange (`internal/rtmp/rpc`)
The client and server exchange AMF0-encoded command messages:

1. **connect** → Server responds with `_result` (NetConnection.Connect.Success)
2. **createStream** → Server allocates a stream ID and responds with `_result`
3. **publish** (or **play**) → Server responds with `onStatus` (NetStream.Publish.Start)

Each command is a chunk message with TypeID 20 containing AMF0-encoded values.

### 5. Media Streaming (`internal/rtmp/media`)
After the publish command succeeds, OBS begins sending:
- **Audio messages** (TypeID 8): AAC/MP3 audio frames
- **Video messages** (TypeID 9): H.264/H.265 video frames

The first audio and video messages are typically *sequence headers* — codec configuration data needed by decoders. The server caches these for late-joining subscribers.

### 6. Media Dispatch (`internal/rtmp/server/media_dispatch.go`)
Each incoming media message is routed through three paths:
1. **Recording**: Written to an FLV file if `-record-all` is enabled
2. **Broadcast**: Forwarded to all subscribers (play clients) on the same stream key
3. **Relay**: Forwarded to external RTMP servers if `-relay-to` is configured

## Package Map

| Package | Purpose | Key Types |
|---------|---------|-----------|
| `internal/rtmp/handshake` | RTMP v3 handshake (C0/C1/C2 ↔ S0/S1/S2) | `Handshake`, `State` |
| `internal/rtmp/chunk` | Message ↔ chunk fragmentation/reassembly | `Reader`, `Writer`, `ChunkHeader`, `Message` |
| `internal/rtmp/amf` | AMF0 binary codec (Number, String, Object, etc.) | `EncodeAll()`, `DecodeAll()` |
| `internal/rtmp/control` | Control messages (types 1-6) | `Decode()`, `Handle()`, `Context` |
| `internal/rtmp/rpc` | Command parsing & response building | `Dispatcher`, `ConnectCommand`, `PublishCommand` |
| `internal/rtmp/conn` | Connection lifecycle (handshake + read/write loops) | `Connection`, `Session` |
| `internal/rtmp/server` | Listener, stream registry, pub/sub | `Server`, `Registry`, `Stream`, `Config` |
| `internal/rtmp/server/auth` | Token-based authentication validators | `Validator`, `TokenValidator`, `FileValidator`, `CallbackValidator` |
| `internal/rtmp/server/hooks` | Event notification (webhooks, shell, stdio) | `HookManager`, `Event`, `Hook` |
| `internal/rtmp/media` | Audio/video parsing, codec detection, FLV recording | `Recorder`, `CodecDetector`, `Stream` |
| `internal/rtmp/relay` | Multi-destination relay to external servers | `DestinationManager`, `Destination` |
| `internal/rtmp/metrics` | Expvar counters for live monitoring | `ConnectionsActive`, `ConnectionsTotal`, `BytesIngested` |
| `internal/rtmp/client` | Minimal RTMP client for testing | `Client` |
| `internal/bufpool` | Memory pool for chunk buffers | `Pool` |
| `internal/errors` | Domain-specific error types | `ProtocolError`, `ChunkError`, `AMFError` |
| `internal/logger` | Structured logging with dynamic level | `Init()`, `Logger()`, `WithConn()` |

## Key Concepts

### Chunks vs Messages
A **message** is a complete unit of data (a command, a video frame, a control instruction). Messages can be large (video keyframes are often tens of KB). A **chunk** is a fixed-size fragment of a message. The chunk layer splits messages into chunks for transmission and reassembles them on receipt.

### CSID (Chunk Stream ID)
Each chunk belongs to a logical chunk stream identified by a CSID. Header compression works per-CSID — if two consecutive messages on the same CSID have the same length and type, only a timestamp delta is transmitted (FMT 2 instead of FMT 0).

### Message Stream ID (MSID)
A higher-level concept than CSID. Stream ID 0 is used for control and command messages. Stream IDs ≥ 1 are allocated by createStream for media data.

### Stream Key
The combination of app name and stream name (e.g., `live/mystream`). This is how publishers and subscribers find each other in the registry.

### Sequence Headers
The first audio and video messages from a publisher usually contain codec configuration ("sequence headers"). The server caches these so that when a new subscriber joins mid-stream, it can immediately send the cached headers — otherwise the subscriber's decoder wouldn't know how to interpret the media data.

## Reading Order for New Contributors

If you want to understand the full codebase, read in this order:

1. **This document** — you're here
2. `internal/rtmp/handshake/doc.go` + `server.go` — simplest layer
3. `internal/rtmp/chunk/doc.go` + `header.go` + `state.go` — core framing
4. `internal/rtmp/amf/doc.go` + `amf.go` — serialization format
5. `internal/rtmp/control/doc.go` + `encoder.go` + `decoder.go` — control messages
6. `internal/rtmp/rpc/doc.go` + `dispatcher.go` — command routing
7. `internal/rtmp/conn/doc.go` + `conn.go` — connection lifecycle
8. `internal/rtmp/server/doc.go` + `server.go` + `registry.go` — putting it all together
9. `internal/rtmp/media/doc.go` + `recorder.go` + `relay.go` — media handling
10. `tests/integration/` — see it all working end-to-end

## Build & Test

```bash
# Build the server
go build -o rtmp-server ./cmd/rtmp-server

# Run all tests
go test ./...

# Run integration tests only
go test ./tests/integration/ -count=1

# Start the server for manual testing
./rtmp-server -listen :1935 -log-level debug

# Stream with FFmpeg
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test

# Play with FFplay
ffplay rtmp://localhost:1935/live/test
```

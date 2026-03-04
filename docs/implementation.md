# Implementation Guide

A code-level walkthrough of the go-rtmp server. Read [Architecture](architecture.md) first for the high-level overview.

## Package Structure

```
internal/
├── bufpool/          Buffer pool to reduce garbage collection pressure
├── errors/           Typed error wrappers (HandshakeError, ChunkError, etc.)
├── logger/           Structured JSON logging with runtime level changes
└── rtmp/
    ├── handshake/    RTMP v3 handshake (C0/C1/C2 ↔ S0/S1/S2)
    ├── chunk/        Message ↔ chunk fragmentation and reassembly
    ├── amf/          AMF0 binary serialization (Number, String, Object, etc.)
    ├── control/      Protocol control messages (types 1-6)
    ├── rpc/          Command parsing (connect, createStream, publish, play)
    ├── conn/         Connection lifecycle (handshake + read/write loops)
    ├── server/       Listener, stream registry, pub/sub coordination
    │   ├── auth/     Token-based authentication (Validator interface + backends)
    │   └── hooks/    Event hooks (webhooks, shell scripts, stdio output)
    ├── media/        Audio/video parsing, codec detection, FLV recording
    ├── relay/        Multi-destination forwarding to external RTMP servers
    ├── metrics/      Expvar counters for connections, publishers, subscribers
    └── client/       Minimal RTMP client for testing
```

## Connection Lifecycle

When a client connects, the following sequence occurs:

### 1. TCP Accept → Handshake (`conn/conn.go: Accept()`)

```
server.acceptLoop()
  └─ raw, _ := listener.Accept()          // raw TCP connection
  └─ handshake.ServerHandshake(raw)        // C0/C1/C2 ↔ S0/S1/S2 exchange
  └─ conn := &Connection{...}             // wrap with lifecycle management
  └─ conn.startWriteLoop()                // begin outbound goroutine
  └─ sendInitialControlBurst(conn)        // Set Chunk Size + Window Ack + Bandwidth
  └─ triggerHookEvent(connection_accept)   // notify external systems
  └─ attachCommandHandling(conn, ...)     // wire up command dispatcher
  └─ conn.Start()                         // begin readLoop goroutine
```

### 2. Command Exchange (`rpc/dispatcher.go: Dispatch()`)

The client sends AMF0 command messages (TypeID 20). The dispatcher routes them:

```
Client                          Server
──────                          ──────
connect("live")          ──►    OnConnect → _result
createStream()           ──►    OnCreateStream → _result(streamID=1) + StreamBegin
publish("mystream")      ──►    Auth check → OnPublish → onStatus(Publish.Start) + hook(publish_start) + recording
```

Each command is:
1. Decoded from AMF0 binary → `[]interface{}`
2. Parsed into a typed struct (`ConnectCommand`, `PublishCommand`, etc.)
3. Passed to the corresponding handler function

### 3. Media Flow (`server/media_dispatch.go: dispatchMedia()`)

Once publishing starts, audio (TypeID 8) and video (TypeID 9) messages arrive:

```
readLoop receives message
  └─ TypeID == 8 or 9?
      └─ mediaLogger.ProcessMessage()     // count packets, detect codec
      └─ stream.Recorder.WriteMessage()   // write to FLV file (if recording)
      └─ stream.BroadcastMessage()        // send to all subscribers
      └─ destMgr.RelayMessage()           // forward to external RTMP servers
```

### 4. Subscriber (Play) Flow (`server/play_handler.go`)

When a subscriber connects and sends `play`:

```
HandlePlay()
  └─ Find stream in registry
  └─ Add subscriber to stream's list
  └─ Send StreamBegin + onStatus(Play.Start)
  └─ Send cached audio sequence header (if available)
  └─ Send cached video sequence header (if available)
```

From this point, every media message from the publisher is broadcast to this subscriber.

## Key Data Structures

### chunk.Message

The fundamental data unit after chunk reassembly:

```go
type Message struct {
    CSID            uint32  // Chunk Stream ID (logical stream for header compression)
    Timestamp       uint32  // Milliseconds (absolute or accumulated)
    MessageLength   uint32  // Payload size in bytes
    TypeID          uint8   // 1-6=control, 8=audio, 9=video, 20=command
    MessageStreamID uint32  // Application-level stream ID (0=control, 1+=media)
    Payload         []byte  // The actual data
}
```

### chunk.ChunkHeader

Controls header compression on the wire:

```go
type ChunkHeader struct {
    FMT       uint8   // 0=full (11 bytes), 1=7 bytes, 2=3 bytes, 3=0 bytes
    CSID      uint32  // Which chunk stream
    Timestamp uint32  // Absolute (FMT0) or delta (FMT1/2)
    // ... plus length, type, stream ID, extended timestamp fields
}
```

### server.Stream

Represents a published live stream:

```go
type Stream struct {
    Key                 string          // "app/streamName"
    Publisher           interface{}     // The publishing connection
    Subscribers         []Subscriber    // All play clients
    AudioSequenceHeader *chunk.Message  // Cached AAC config
    VideoSequenceHeader *chunk.Message  // Cached H.264 SPS/PPS
    Recorder            *media.Recorder // FLV file writer (optional)
}
```

## AMF0 Encoding

RTMP commands use AMF0 (Action Message Format) for serialization. The `amf` package supports:

| AMF0 Type | Go Type | Marker Byte |
|-----------|---------|-------------|
| Number | `float64` | `0x00` |
| Boolean | `bool` | `0x01` |
| String | `string` | `0x02` |
| Object | `map[string]interface{}` | `0x03` |
| Null | `nil` | `0x05` |
| Strict Array | `[]interface{}` | `0x0A` |

Example — the `connect` command on the wire:

```
[String "connect"] [Number 1.0] [Object {"app":"live", "tcUrl":"rtmp://host/live"}]
```

Encoded/decoded with:
```go
data, _ := amf.EncodeAll("connect", 1.0, map[string]interface{}{"app": "live"})
values, _ := amf.DecodeAll(data)
// values[0] = "connect", values[1] = 1.0, values[2] = map[...]
```

## FLV Recording

The `media.Recorder` writes incoming messages to FLV format:

```
┌───────────────┐
│  FLV Header   │  9 bytes + 4-byte PreviousTagSize0 (= 0)
├───────────────┤
│  Tag 1        │  11-byte tag header + audio/video data + 4-byte PreviousTagSize
├───────────────┤
│  Tag 2        │  ...
├───────────────┤
│  ...          │
└───────────────┘
```

Each tag header contains: TypeID (8=audio, 9=video), data size (24-bit), timestamp (24-bit + 8-bit extended), and stream ID (always 0).

## Event Hooks

The hook system (`internal/rtmp/server/hooks/`) notifies external systems when RTMP events occur. It integrates at multiple points:

1. **Server accept loop** (`server.go`): Triggers `connection_accept` on new connections
2. **Disconnect handlers** (`command_integration.go`): Triggers `connection_close`, `publish_stop`, `play_stop`, and `subscriber_count` on disconnect
3. **Command handlers** (`command_integration.go`): Triggers `publish_start`, `play_start`, `subscriber_count`, and `auth_failed`
4. **Media dispatch** (`media_dispatch.go`): Triggers `codec_detected` on first media packet

Each hook runs asynchronously in a bounded goroutine pool (default 10 workers). The `HookManager` maps event types to registered hooks and dispatches via `TriggerEvent()`.

Three hook implementations are provided:
- `WebhookHook`: HTTP POST with JSON payload
- `ShellHook`: Runs a script with event data as `RTMP_*` environment variables
- `StdioHook`: Prints to stderr in JSON or env-var format

## Adding a New Feature

1. **Create the package** under `internal/rtmp/` with a `doc.go` explaining its purpose
2. **Write tests first** using golden binary vectors if it involves wire format
3. **Integrate into the server** via `command_integration.go` (for commands) or `media_dispatch.go` (for media processing)
4. **Add hook events** if the feature has lifecycle events worth notifying (define in `hooks/events.go`)
5. **Document** in `docs/features/` with problem statement, solution, and testing instructions

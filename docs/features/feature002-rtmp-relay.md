# Feature 002: RTMP Server Relay Architecture

**Date**: October 11, 2025  
**Status**: Implemented  
**Related Tasks**: T044 (Media Relay), T049 (Publish Handler), T050 (Play Handler)

---

## Overview

This document explains how the rtmp-server implements a **transparent media relay** (also called a "media forwarder" or "streaming hub") that receives media from publishers and redistributes it to multiple subscribers **without transcoding**.

---

## Table of Contents

1. [Architecture Overview](#architecture-overview)
2. [How ffplay Replays RTMP Streams](#how-ffplay-replays-rtmp-streams)
3. [Relay Flow: Step-by-Step](#relay-flow-step-by-step)
4. [Key Relay Features](#key-relay-features)
5. [Data Flow Diagram](#data-flow-diagram)
6. [Example Usage](#example-usage)
7. [Recording Integration](#recording-integration)
8. [Implementation Details](#implementation-details)
9. [Summary](#summary)

---

## Architecture Overview

```
┌─────────────┐                  ┌──────────────┐                 ┌─────────────┐
│   FFmpeg    │   RTMP Publish   │              │   RTMP Play     │   ffplay    │
│ (Publisher) ├─────────────────►│ rtmp-server  ├────────────────►│ (Player 1)  │
└─────────────┘                  │   (Relay)    │                 └─────────────┘
                                 │              │
                                 │              │                 ┌─────────────┐
                                 │              ├────────────────►│   ffplay    │
                                 │              │   RTMP Play     │ (Player 2)  │
                                 └──────────────┘                 └─────────────┘
```

**Key Concept**: The server acts as a **transparent relay** - media flows through without modification:

```
Publisher (FFmpeg) → [Raw AAC/H.264 bytes] → Server → [Same bytes] → Subscriber (ffplay)
```

---

## How ffplay Replays RTMP Streams

### 1. Live Playback (Real-time from Active Publisher)

When ffplay connects to a live stream being published:

```powershell
ffplay rtmp://localhost:1935/live/test
```

**Protocol Flow**:

```
1. RTMP Handshake: C0/C1/C2 ↔ S0/S1/S2
2. connect command (app="live")
   → Server: _result (NetConnection.Connect.Success)
3. createStream command
   → Server: _result (streamID=1)
4. play command (streamKey="test", start=-2 for live)
   → Server: UserControl(StreamBegin) + onStatus(NetStream.Play.Start)
5. Media Streaming: Server relays messages from publisher
   - Audio Message (AAC Sequence Header) - codec initialization
   - Video Message (AVC Sequence Header) - codec initialization  
   - Video/Audio Messages (interleaved frames)
```

**Implementation** (`internal/rtmp/media/relay.go`):
- ffplay is added as a **Subscriber** to the Stream
- `BroadcastMessage()` relays every media message from publisher to all subscribers
- Non-blocking send with `TrySendMessage()` prevents slow subscribers from blocking others
- Codec detection happens on first audio/video frames

### 2. Playback from Recorded FLV Files

The server can also record streams to FLV files:

```powershell
ffplay recordings\live_test_20251001_120500.flv
```

**How Recording Works** (`internal/rtmp/media/recorder.go`):

**FLV File Structure**:
```
FLV Header (13 bytes): 'FLV' + version + flags + data offset + previous tag size
│
├─ Audio Tag (type 0x08)
│  ├─ Tag Header (11 bytes): type + size + timestamp + streamID
│  ├─ Audio Data (AAC payload)
│  └─ Previous Tag Size (4 bytes)
│
├─ Video Tag (type 0x09)
│  ├─ Tag Header (11 bytes)
│  ├─ Video Data (H.264 payload)
│  └─ Previous Tag Size (4 bytes)
│
└─ ... (more audio/video tags)
```

**Recording Process**:
1. Recorder writes FLV header once: `{'F','L','V', 0x01, 0x05, ...}`
2. Each RTMP media message (type 8=audio, 9=video) becomes an FLV tag
3. Tag preserves: timestamp, payload (codec data unchanged)
4. No transcoding - raw relay from RTMP to FLV format

**ffplay Reading**:
1. Opens the `.flv` file locally (no network/RTMP involved)
2. Reads FLV header to detect audio/video presence
3. Parses each FLV tag sequentially
4. Extracts codec info from sequence headers (AAC, H.264)
5. Decodes and renders frames using timestamps for synchronization

### Key Differences: Live vs Recorded

| Aspect | Live Playback | Recorded Playback |
|--------|--------------|-------------------|
| **Protocol** | RTMP over TCP | Local file I/O |
| **Latency** | 3-5 seconds | None (instant seek) |
| **Connection** | connect → createStream → play | Direct file access |
| **Format** | RTMP chunks → Messages | FLV tags |
| **Buffering** | Network jitter buffer | Disk read buffer |
| **Seeking** | Limited (start parameter) | Full seek support |

**Important**: The same media payloads flow through both paths - live relay and FLV recording use identical audio/video data, ensuring recorded files play identically to live streams!

---

## Relay Flow: Step-by-Step

### 1. Publisher Connects & Publishes

**Flow**: FFmpeg → Server

```go
// cmd/rtmp-server/main.go & internal/rtmp/server/server.go
1. TCP Accept() → Handshake (C0/C1/S0/S1/S2/C2)
2. Send Control Burst (SetChunkSize, WindowAckSize, SetPeerBandwidth)
3. Receive: connect command (app="live")
4. Send: _result (NetConnection.Connect.Success)
5. Receive: createStream command
6. Send: _result (streamID=1) + UserControl StreamBegin
7. Receive: publish command (streamKey="live/test")
8. Send: onStatus (NetStream.Publish.Start)
```

**Registration** (`internal/rtmp/server/publish_handler.go`):

```go
// HandlePublish registers publisher in registry
func HandlePublish(reg *Registry, conn sender, app string, msg *chunk.Message) {
    cmd := ParsePublishCommand(msg, app)
    stream, created := reg.CreateStream(cmd.StreamKey)  // "live/test"
    stream.SetPublisher(conn)                            // Store publisher connection
    // Send onStatus back to publisher
}
```

### 2. Player Connects & Subscribes

**Flow**: ffplay → Server

```go
// Same handshake + control burst
1. Receive: connect command (app="live")
2. Send: _result
3. Receive: createStream command
4. Send: _result (streamID=1)
5. Receive: play command (streamKey="live/test", start=-2)
6. Send: UserControl StreamBegin
7. Send: onStatus (NetStream.Play.Start)
```

**Subscription** (`internal/rtmp/server/play_handler.go`):

```go
func HandlePlay(reg *Registry, conn sender, app string, msg *chunk.Message) {
    cmd := ParsePlayCommand(msg, app)
    stream := reg.GetStream(cmd.StreamKey)  // Lookup "live/test"
    
    if stream == nil || stream.Publisher == nil {
        // Send NetStream.Play.StreamNotFound
        return
    }
    
    stream.AddSubscriber(conn)  // Add ffplay as subscriber
    // Send StreamBegin + Play.Start status
}
```

### 3. Media Relay: Publisher → Subscribers

**Core Relay Logic** (`internal/rtmp/server/command_integration.go`):

```go
func attachCommandHandling(c *Connection, reg *Registry, cfg *Config, log *slog.Logger) {
    c.SetMessageHandler(func(m *chunk.Message) {
        
        // MEDIA MESSAGES (Type 8=Audio, Type 9=Video)
        if m.TypeID == 8 || m.TypeID == 9 {
            
            // 1. Get current publishing stream
            if st.streamKey != "" {
                stream := reg.GetStream(st.streamKey)  // e.g., "live/test"
                
                // 2. Optional: Write to recorder (FLV file)
                if stream.Recorder != nil {
                    stream.Recorder.WriteMessage(m)
                }
                
                // 3. BROADCAST TO ALL SUBSCRIBERS
                stream.BroadcastMessage(detector, m, log)
            }
            return
        }
        
        // Command messages dispatched separately
    })
}
```

**Broadcast Implementation** (`internal/rtmp/media/relay.go`):

```go
func (s *Stream) BroadcastMessage(detector *CodecDetector, msg *chunk.Message, logger *slog.Logger) {
    
    // 1. Detect codecs on first audio/video frames (AAC, H.264)
    if msg.TypeID == 8 || msg.TypeID == 9 {
        detector.Process(msg.TypeID, msg.Payload, s, logger)
    }
    
    // 2. Snapshot subscribers (read lock, no blocking)
    s.mu.RLock()
    subs := make([]Subscriber, len(s.subs))
    copy(subs, s.subs)
    s.mu.RUnlock()
    
    // 3. Send to EACH subscriber
    for _, sub := range subs {
        
        // Non-blocking send (backpressure handling)
        if ts, ok := sub.(TrySendMessage); ok {
            if !ts.TrySendMessage(msg) {
                // Queue full → DROP message (prevents slow subscribers from blocking)
                logger.Debug("Dropped media message (slow subscriber)")
                continue
            }
        } else {
            // Fallback: blocking send
            sub.SendMessage(msg)
        }
    }
}
```

### 4. Subscriber Receives Media

**Write Loop** (`internal/rtmp/conn/conn.go`):

```go
func (c *Connection) startWriteLoop() {
    w := chunk.NewWriter(c.netConn, c.writeChunkSize)
    for {
        select {
        case msg := <-c.outboundQueue:  // Message from BroadcastMessage()
            w.WriteMessage(msg)          // Chunk → TCP → ffplay
        }
    }
}
```

**Flow**: Server → ffplay

```
1. BroadcastMessage() calls subscriber.SendMessage(msg)
2. SendMessage() enqueues msg to outboundQueue (channel)
3. writeLoop reads from outboundQueue
4. Chunker splits message into RTMP chunks
5. TCP socket writes chunks to ffplay
6. ffplay decodes AAC/H.264 → renders video
```

---

## Key Relay Features

### 1. Transparent Relay (No Transcoding)

```go
// Media messages pass through UNCHANGED
Publisher (FFmpeg) → [Raw AAC/H.264 bytes] → Server → [Same bytes] → Subscriber (ffplay)
```

**Characteristics**:
- **No decoding**: Server never looks at AAC/H.264 codec data
- **No encoding**: Just forwards raw FLV tag payloads
- **Codec detection**: Only reads first byte (codec ID) for logging
- **Performance**: Minimal CPU usage, high throughput

### 2. Multi-Subscriber Support

```go
// registry.go: Stream entity
type Stream struct {
    Publisher   interface{}           // Single publisher (FR-024)
    Subscribers []media.Subscriber    // Multiple subscribers (unlimited)
}

// One publisher → Many subscribers
stream.AddSubscriber(player1)
stream.AddSubscriber(player2)
stream.AddSubscriber(player3)
```

**Features**:
- Unlimited concurrent subscribers per stream
- Each subscriber receives identical media data
- Independent subscription lifecycle (join/leave at any time)

### 3. Backpressure Handling

```go
// Slow subscribers DON'T block fast subscribers
func (s *Stream) BroadcastMessage(msg *chunk.Message) {
    for _, sub := range subs {
        if !sub.TrySendMessage(msg) {
            // Drop message if queue full (200ms timeout)
            continue  // Don't block other subscribers
        }
    }
}
```

**Strategy**:
- Non-blocking send with `TrySendMessage()` interface
- 200ms timeout on queue insertion
- Drop frames for slow subscribers (graceful degradation)
- Fast subscribers unaffected by slow ones
- Bounded queue per subscriber: 100 messages

### 4. Concurrency Model

**Per-Connection**:
- **1 readLoop goroutine**: Reads chunks → reassembles messages → dispatches
- **1 writeLoop goroutine**: Chunks messages → writes to TCP
- **Bounded queue**: `outboundQueue chan *chunk.Message` (100 slots)
- **Context cancellation**: Clean shutdown via `context.Context`

**Per-Stream**:
- **Publisher lock-free read**: Snapshot subscribers under `RWMutex`
- **Subscriber management**: Thread-safe Add/Remove operations
- **No global lock**: Each stream independent

**Synchronization**:
```go
// Minimal lock contention
s.mu.RLock()                           // Read lock only
subs := make([]Subscriber, len(s.subs)) // Snapshot
copy(subs, s.subs)
s.mu.RUnlock()                         // Release immediately

// Broadcast without lock
for _, sub := range subs {
    sub.SendMessage(msg)  // No lock held during I/O
}
```

---

## Data Flow Diagram

### Complete System Flow

```
┌────────────────────────────────────────────────────────────────────┐
│                         rtmp-server                                │
│                                                                    │
│  ┌──────────────┐     ┌──────────────┐     ┌─────────────────┐  │
│  │ Publisher    │     │   Registry   │     │  Subscriber 1   │  │
│  │ Connection   │     │              │     │  Connection     │  │
│  │              │     │  ┌────────┐  │     │                 │  │
│  │ readLoop ────┼────►│  │ Stream │  ├────►│ outboundQueue   │  │
│  │  (chunks)    │     │  │        │  │     │  (channel)      │  │
│  │              │     │  │Publisher│ │     │                 │  │
│  │              │     │  │Subs[0-N]│ │     │ writeLoop ──────┼──► TCP
│  └──────────────┘     │  │        │  │     └─────────────────┘  │
│                       │  │Broadcast│ │                           │
│                       │  │Message() │ │     ┌─────────────────┐  │
│                       │  │        │  ├────►│  Subscriber 2   │  │
│                       │  │Recorder │ │     │  Connection     │  │
│                       │  └────────┘  │     └─────────────────┘  │
│                       └──────────────┘                           │
└────────────────────────────────────────────────────────────────────┘
```

### Message Flow (Per Frame)

```
┌─────────────────────────────────────────────────────────────────┐
│                       Single Audio Frame                        │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
                   ┌─────────────────────┐
                   │  Publisher readLoop │
                   │  (dechunks message) │
                   └──────────┬──────────┘
                              │
                              ▼
                   ┌─────────────────────┐
                   │  Message Handler    │
                   │  (Type 8 detected)  │
                   └──────────┬──────────┘
                              │
                              ▼
                   ┌─────────────────────┐
                   │ Stream.Broadcast()  │
                   │ + Recorder.Write()  │
                   └──────────┬──────────┘
                              │
              ┌───────────────┼───────────────┐
              ▼               ▼               ▼
    ┌─────────────┐  ┌─────────────┐  ┌─────────────┐
    │   Sub1      │  │   Sub2      │  │   Sub3      │
    │ outbound    │  │ outbound    │  │ outbound    │
    │   queue     │  │   queue     │  │   queue     │
    └──────┬──────┘  └──────┬──────┘  └──────┬──────┘
           │                │                │
           ▼                ▼                ▼
    ┌─────────────┐  ┌─────────────┐  ┌─────────────┐
    │ writeLoop   │  │ writeLoop   │  │ writeLoop   │
    │ (chunker)   │  │ (chunker)   │  │ (chunker)   │
    └──────┬──────┘  └──────┬──────┘  └──────┬──────┘
           │                │                │
           ▼                ▼                ▼
        TCP/IP          TCP/IP          TCP/IP
           │                │                │
           ▼                ▼                ▼
       ffplay 1         ffplay 2         ffplay 3
```

---

## Example Usage

### Basic Scenario: 1 Publisher → 2 Players

**Terminal 1**: Start server

```powershell
.\rtmp-server.exe -listen :1935
```

**Terminal 2**: Publish stream

```powershell
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test
```

**Terminal 3**: Play (subscriber 1)

```powershell
ffplay rtmp://localhost:1935/live/test
```

**Terminal 4**: Play (subscriber 2)

```powershell
ffplay rtmp://localhost:1935/live/test
```

### Server State

```go
Registry {
    streams: {
        "live/test": Stream {
            Key: "live/test",
            Publisher: Connection{id: "c000001", ...},  // FFmpeg
            Subscribers: [
                Connection{id: "c000002", ...},  // ffplay 1
                Connection{id: "c000003", ...},  // ffplay 2
            ],
            VideoCodec: "H.264 AVC",
            AudioCodec: "AAC",
            Recorder: *Recorder{...}  // Optional
        }
    }
}
```

### Message Flow (Every Frame)

```
FFmpeg (c000001) → Audio Message (Type 8, AAC frame)
                 ↓
    Stream.BroadcastMessage()
                 ↓
         ┌───────┴───────┐
         ↓               ↓
    c000002.Send()  c000003.Send()
         ↓               ↓
    ffplay 1        ffplay 2
    (renders)       (renders)
```

### Expected Logs

```json
{"time":"2025-10-11T10:00:00Z","level":"INFO","msg":"Connection accepted","conn_id":"c000001"}
{"time":"2025-10-11T10:00:00Z","level":"INFO","msg":"Handshake completed","conn_id":"c000001"}
{"time":"2025-10-11T10:00:00Z","level":"INFO","msg":"connect command","conn_id":"c000001","app":"live"}
{"time":"2025-10-11T10:00:00Z","level":"INFO","msg":"createStream","conn_id":"c000001","stream_id":1}
{"time":"2025-10-11T10:00:00Z","level":"INFO","msg":"publish command","conn_id":"c000001","stream_key":"live/test"}
{"time":"2025-10-11T10:00:01Z","level":"INFO","msg":"Codec detected","stream_key":"live/test","video":"H.264 AVC","audio":"AAC"}

{"time":"2025-10-11T10:00:05Z","level":"INFO","msg":"Connection accepted","conn_id":"c000002"}
{"time":"2025-10-11T10:00:05Z","level":"INFO","msg":"play command","conn_id":"c000002","stream_key":"live/test"}
{"time":"2025-10-11T10:00:05Z","level":"INFO","msg":"Subscriber added","stream_key":"live/test","total_subscribers":1}

{"time":"2025-10-11T10:00:10Z","level":"INFO","msg":"Connection accepted","conn_id":"c000003"}
{"time":"2025-10-11T10:00:10Z","level":"INFO","msg":"play command","conn_id":"c000003","stream_key":"live/test"}
{"time":"2025-10-11T10:00:10Z","level":"INFO","msg":"Subscriber added","stream_key":"live/test","total_subscribers":2}
```

---

## Recording Integration

The relay can **optionally record** to FLV files simultaneously with live streaming.

### Configuration

```powershell
# Enable recording for all streams
.\rtmp-server.exe -record-all -record-dir recordings
```

### Implementation

```go
// Same BroadcastMessage() loop
func (s *Stream) BroadcastMessage(msg *chunk.Message) {
    // 1. Relay to live subscribers
    for _, sub := range subscribers {
        sub.SendMessage(msg)
    }
    
    // 2. ALSO write to file (if recording enabled)
    if s.Recorder != nil {
        s.Recorder.WriteMessage(msg)  // Persists to FLV
    }
}
```

### File Naming

```
recordings/
├── live_test_20251011_100500.flv
├── live_test_20251011_120000.flv
└── live_demo_20251011_150000.flv

Format: {streamkey}_{timestamp}.flv
Example: live_test_20251011_100500.flv
         └─┬──┘ └─┬─┘ └──────┬───────┘
           │     │           └─ YYYYMMDD_HHMMSS
           │     └─ stream name
           └─ app name
```

### Benefits

**Identical Media Data**:
- Live relay and FLV recording use **same audio/video bytes**
- No transcoding overhead
- Recorded files play identically to live streams
- No quality loss

**Use Cases**:
- **Archive**: Save broadcasts for later viewing
- **VOD**: Serve recordings as on-demand content
- **Debugging**: Analyze media packet structure
- **Testing**: Validate codec compatibility with ffmpeg/ffplay

---

## Implementation Details

### File References

| Component | File Path | Purpose |
|-----------|-----------|---------|
| **Server** | `internal/rtmp/server/server.go` | Listener + accept loop |
| **Connection** | `internal/rtmp/conn/conn.go` | readLoop + writeLoop per connection |
| **Registry** | `internal/rtmp/server/registry.go` | Stream management |
| **Publish Handler** | `internal/rtmp/server/publish_handler.go` | Publisher registration |
| **Play Handler** | `internal/rtmp/server/play_handler.go` | Subscriber registration |
| **Media Relay** | `internal/rtmp/media/relay.go` | BroadcastMessage() implementation |
| **Recorder** | `internal/rtmp/media/recorder.go` | FLV file writer |
| **Codec Detector** | `internal/rtmp/media/codec.go` | AAC/H.264 detection |
| **Integration** | `internal/rtmp/server/command_integration.go` | Message routing |

### Key Interfaces

```go
// Subscriber interface (minimal contract)
type Subscriber interface {
    SendMessage(*chunk.Message) error
}

// Optional: Non-blocking send
type TrySendMessage interface {
    TrySendMessage(*chunk.Message) bool
}

// Connection implements both interfaces
type Connection struct {
    outboundQueue chan *chunk.Message  // 100 slots
    // ...
}

func (c *Connection) SendMessage(msg *chunk.Message) error {
    // 200ms timeout
    select {
    case c.outboundQueue <- msg:
        return nil
    case <-time.After(200 * time.Millisecond):
        return fmt.Errorf("queue full")
    }
}
```

### Performance Characteristics

**CPU Usage**:
- No codec decoding/encoding
- Only byte copying and header parsing
- Minimal processing per frame

**Memory Usage**:
- Bounded queues: 100 messages × N subscribers
- Message reuse via shallow copy (shared payload []byte)
- No buffering beyond outbound queues

**Latency**:
- Typical: 3-5 seconds (network buffering)
- No additional server-side buffering
- Direct forwarding from readLoop → BroadcastMessage → writeLoop

**Scalability**:
- Each stream independent (no global locks)
- Goroutines per connection (not per message)
- Backpressure prevents cascade failures

---

## Summary

The rtmp-server acts as a relay by:

1. **Accepting publishers**: Registers them in `Stream.Publisher`
2. **Accepting players**: Adds them to `Stream.Subscribers[]`
3. **Broadcasting media**: Every audio/video message from publisher → `BroadcastMessage()` → all subscribers
4. **No transcoding**: Raw byte forwarding (codec-agnostic)
5. **Backpressure handling**: Slow subscribers drop frames, don't block others
6. **Optional recording**: Same media data written to FLV files
7. **Concurrency safety**: Lock-free broadcasting, bounded queues, context cancellation
8. **Protocol compliance**: RTMP v3 spec-compliant handshake, chunking, commands

### Design Principles

✅ **Protocol-First**: Wire format fidelity, byte-for-byte spec compliance  
✅ **Idiomatic Go**: Standard library only, simple/clear code, channels for concurrency  
✅ **Modularity**: Clean separation of concerns (handshake → chunk → control → relay)  
✅ **Concurrency Safety**: One readLoop + writeLoop per connection, no shared state  
✅ **Observability**: Structured logging with slog, debug mode for protocol traces  
✅ **Simplicity**: No transcoding, no complex buffering, YAGNI approach

The design is **simple, scalable, and protocol-compliant** – perfect for RTMP v3 streaming relay scenarios! 🚀

---

## Related Documentation

- [Constitution](docs/000-constitution.md) - Core design principles
- [Specification](specs/001-rtmp-server-implementation/spec.md) - Feature requirements
- [Data Model](specs/001-rtmp-server-implementation/data-model.md) - Entity relationships
- [Quickstart](specs/001-rtmp-server-implementation/quickstart.md) - Getting started guide
- [Media Contracts](specs/001-rtmp-server-implementation/contracts/media.md) - Media message format
- [Tasks](specs/001-rtmp-server-implementation/tasks.md) - Implementation breakdown

---

**Last Updated**: October 11, 2025  
**Author**: Documentation from system analysis  
**Version**: 1.0.0

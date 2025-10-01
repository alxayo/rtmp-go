# Data Model: RTMP Server Implementation

**Feature**: 001-rtmp-server-implementation  
**Date**: 2025-10-01  
**Phase**: 1 (Design & Contracts)

## Overview

This document defines the core entities, their fields, relationships, validation rules, and state transitions for the RTMP server implementation. All entities are derived from the feature specification functional requirements (FR-001 through FR-054) and the research phase decisions.

---

## Entity Diagram

```
┌─────────────┐       ┌──────────────┐       ┌────────────┐
│ Connection  │──────>│   Session    │──────>│   Stream   │
│             │  1:1  │              │  n:1  │            │
│ (TCP conn)  │       │ (RTMP state) │       │ (pub+subs) │
└─────────────┘       └──────────────┘       └────────────┘
       │                                            │
       │ 1:n                                        │ 0:1
       v                                            v
┌─────────────────┐                        ┌────────────┐
│ ChunkStreamState│                        │  Recorder  │
│   (per CSID)    │                        │  (FLV file)│
└─────────────────┘                        └────────────┘
       │
       │ 1:n
       v
┌─────────────┐
│   Message   │
│  (protocol) │
└─────────────┘
```

**Relationships**:
- One Connection has one Session (after handshake + connect)
- One Session can publish to one Stream or play from one Stream
- One Stream has one Publisher Connection and many Subscriber Connections
- One Connection manages multiple ChunkStreamStates (one per CSID)
- One Stream may have one Recorder (if recording enabled)

---

## Core Entities

### Connection

**Package**: `internal/rtmp/conn`  
**Purpose**: Represents a TCP connection from a client, manages low-level I/O, chunk stream state, and flow control.

**Fields**:

| Field            | Type                              | Description                                                  | Default/Initial Value |
|------------------|-----------------------------------|--------------------------------------------------------------|-----------------------|
| `id`             | `string`                          | Unique connection identifier (UUID for logging)              | Generated on accept   |
| `remoteAddr`     | `net.Addr`                        | Peer IP address and port                                     | From net.Conn         |
| `conn`           | `net.Conn`                        | Underlying TCP connection                                    | From listener.Accept  |
| `ctx`            | `context.Context`                 | Cancellation context for goroutines                          | context.Background()  |
| `cancel`         | `context.CancelFunc`              | Cancel function to terminate connection                      | From context.WithCancel |
| `readChunkSize`  | `uint32`                          | Current receive chunk size (bytes)                           | 128                   |
| `writeChunkSize` | `uint32`                          | Current send chunk size (bytes)                              | 128                   |
| `windowAckSize`  | `uint32`                          | Acknowledgement window size (bytes)                          | 2500000               |
| `peerBandwidth`  | `uint32`                          | Peer bandwidth limit (bytes)                                 | 2500000               |
| `limitType`      | `uint8`                           | Peer bandwidth limit type (0=Hard, 1=Soft, 2=Dynamic)        | 2 (Dynamic)           |
| `bytesReceived`  | `uint64`                          | Total bytes received (for ACK tracking)                      | 0                     |
| `lastAckSent`    | `uint64`                          | Byte count at last ACK sent                                  | 0                     |
| `chunkStreams`   | `map[uint32]*ChunkStreamState`    | Per-CSID chunk stream state cache                            | Empty map             |
| `outboundQueue`  | `chan *Message`                   | Bounded channel for writeLoop                                | make(chan, 100)       |
| `session`        | `*Session`                        | RTMP session state (nil until connect command processed)     | nil                   |
| `role`           | `string`                          | Connection role: "publisher", "player", or "" (undetermined) | ""                    |

**Validation Rules**:
- `readChunkSize` and `writeChunkSize` must be >= 128 and <= 65536 (FR-007, FR-008)
- `windowAckSize` and `peerBandwidth` must be > 0 (FR-009, FR-010)
- `outboundQueue` capacity must be > 0 (bounded for backpressure, FR-043)
- `id` must be unique across all active connections

**State Transitions**:

```
Initial (accepted)
    │
    ├─> Handshaking (C0/C1 received)
    │       │
    │       ├─> ControlBurst (S0/S1/S2/C2 exchanged, control messages sent)
    │       │       │
    │       │       ├─> CommandProcessing (connect, createStream, publish/play received)
    │       │       │       │
    │       │       │       ├─> Streaming (audio/video messages flowing)
    │       │       │       │
    │       │       │       └─> Disconnecting (deleteStream or error)
    │       │       │
    │       │       └─> Disconnecting (command error)
    │       │
    │       └─> Disconnecting (handshake failure)
    │
    └─> Closed (connection terminated)
```

**Methods** (conceptual, implementation details in tasks):
- `ReadLoop()`: Goroutine that reads chunks, reassembles messages, dispatches to handlers
- `WriteLoop()`: Goroutine that consumes outboundQueue, encodes chunks, writes to TCP
- `SendMessage(msg *Message)`: Enqueues message to outboundQueue (non-blocking with timeout)
- `SendControlMessage(typeID uint8, payload []byte)`: Helper for protocol control messages (CSID=2, MSID=0)
- `UpdateChunkSize(newSize uint32)`: Updates readChunkSize or writeChunkSize
- `SendAcknowledgement()`: Sends ACK message if bytesReceived exceeds window
- `Close()`: Cancels context, closes TCP connection, waits for goroutines

**Concurrency Safety**:
- `chunkStreams` map accessed only by readLoop (no mutex needed)
- `outboundQueue` is channel (inherently thread-safe)
- `bytesReceived`, `lastAckSent` updated only by readLoop (no mutex needed)
- `readChunkSize` and `writeChunkSize` set by control message handlers (atomic or mutex if needed)

---

### Session

**Package**: `internal/rtmp/conn`  
**Purpose**: Represents an established RTMP session after handshake and connect command. Tracks application context and stream identifiers.

**Fields**:

| Field             | Type                  | Description                                           | Default/Initial Value |
|-------------------|-----------------------|-------------------------------------------------------|-----------------------|
| `app`             | `string`              | Application name from connect command (e.g., "live")  | "" (set by connect)   |
| `tcUrl`           | `string`              | Full RTMP URL from connect command                    | "" (set by connect)   |
| `flashVer`        | `string`              | Client version string (e.g., "FMLE/3.0")              | "" (set by connect)   |
| `objectEncoding`  | `uint8`               | AMF encoding version (0=AMF0, 3=AMF3)                 | 0 (AMF0 only)         |
| `transactionID`   | `uint32`              | Counter for command responses                         | 1                     |
| `streamID`        | `uint32`              | Allocated message stream ID (from createStream)       | 0 (set by createStream) |
| `streamKey`       | `string`              | Full stream key "app/streamname" (from publish/play)  | "" (set by pub/play)  |

**Validation Rules**:
- `app` must not be empty after connect command (FR-013)
- `objectEncoding` must be 0 (AMF0 only) - reject AMF3 clients with error (FR-018)
- `transactionID` must increment for each command response (convention for FFmpeg/OBS compatibility)
- `streamID` must be > 0 after createStream (FR-014)
- `streamKey` format must match "app/streamname" (FR-020)

**State Transitions**:

```
Uninitialized
    │
    ├─> Connected (connect command processed, app set)
    │       │
    │       ├─> StreamCreated (createStream processed, streamID allocated)
    │       │       │
    │       │       ├─> Publishing (publish command processed, role=publisher)
    │       │       │
    │       │       └─> Playing (play command processed, role=player)
    │       │
    │       └─> Disconnecting (command error)
    │
    └─> Closed
```

**Methods** (conceptual):
- `NextTransactionID() uint32`: Atomically increments and returns transactionID
- `SetApp(app string)`: Sets application name from connect command
- `AllocateStreamID() uint32`: Allocates a new stream ID (simple counter, typically 1)
- `SetStreamKey(app, streamName string) string`: Constructs "app/streamname" key

**Concurrency Safety**:
- Session fields set once during command processing, then read-only (no mutex needed)
- `transactionID` may need atomic increment if commands can be processed concurrently (unlikely in typical RTMP flow)

---

### Stream

**Package**: `internal/rtmp/server`  
**Purpose**: Represents a logical audio/video stream identified by application name and stream key. Manages publisher and subscriber relationships.

**Fields**:

| Field           | Type              | Description                                         | Default/Initial Value |
|-----------------|-------------------|-----------------------------------------------------|-----------------------|
| `key`           | `string`          | Unique stream identifier "app/streamname"           | Set at creation       |
| `publisher`     | `*Connection`     | Publishing connection (nil if no active publisher)  | nil                   |
| `subscribers`   | `[]*Connection`   | List of playing connections                         | Empty slice           |
| `metadata`      | `map[string]interface{}` | Stream metadata from @setDataFrame         | Empty map             |
| `videoCodec`    | `string`          | Detected video codec (e.g., "H.264 AVC")            | "" (set on first frame) |
| `audioCodec`    | `string`          | Detected audio codec (e.g., "AAC")                  | "" (set on first frame) |
| `startTime`     | `time.Time`       | Stream start timestamp                              | time.Now() at creation |
| `recorder`      | `*Recorder`       | Optional recorder (nil if recording disabled)       | nil                   |
| `mu`            | `sync.RWMutex`    | Protects subscribers list                           | Initialized           |

**Validation Rules**:
- `key` must match format "app/streamname" and not be empty (FR-020)
- Only one `publisher` per stream at a time (FR-024 implies single publisher)
- `subscribers` can grow dynamically (FR-024)
- `videoCodec` and `audioCodec` logged but not validated (codec-agnostic, FR-027, FR-028)

**State Transitions**:

```
Created (stream registered in registry, no publisher)
    │
    ├─> Active (publisher connected, media flowing)
    │       │
    │       ├─> Idle (publisher disconnected, subscribers may remain)
    │       │
    │       └─> Deleted (no publisher, no subscribers, grace period expired)
    │
    └─> Deleted (stream removed from registry)
```

**Methods** (conceptual):
- `SetPublisher(conn *Connection) error`: Assigns publisher, returns error if already exists (FR-024 single publisher constraint)
- `AddSubscriber(conn *Connection)`: Adds subscriber to list (thread-safe with RWMutex)
- `RemoveSubscriber(conn *Connection)`: Removes subscriber from list
- `RemovePublisher()`: Clears publisher, sends StreamEOF to subscribers (FR-032)
- `DetectCodecs(audioMsg, videoMsg *Message)`: Extracts codec info from FLV tag first byte (FR-028)
- `BroadcastMessage(msg *Message)`: Sends message to all subscribers (calls conn.SendMessage for each)
- `StartRecording(outputPath string) error`: Creates Recorder if recording enabled (FR-025, FR-026)
- `StopRecording()`: Flushes and closes Recorder

**Concurrency Safety**:
- `publisher` field written by single connection handler, read by others (atomic pointer or mutex)
- `subscribers` list protected by RWMutex (read-heavy: BroadcastMessage reads, Add/Remove write)
- `metadata`, `videoCodec`, `audioCodec` written once, then read-only (or protected by mu)

---

### Message

**Package**: `internal/rtmp/chunk`  
**Purpose**: Represents a complete RTMP protocol message (control, command, audio, video, data).

**Fields**:

| Field           | Type       | Description                                           | Default/Initial Value |
|-----------------|------------|-------------------------------------------------------|-----------------------|
| `csid`          | `uint32`   | Chunk stream ID (2-65599)                             | Required              |
| `timestamp`     | `uint32`   | Absolute or delta timestamp (milliseconds)            | Required              |
| `msgLength`     | `uint32`   | Length of payload (bytes)                             | len(payload)          |
| `msgTypeID`     | `uint8`    | Message type (1-22, see RTMP spec)                    | Required              |
| `msgStreamID`   | `uint32`   | Message stream ID (little-endian)                     | 0 for control, >0 for media |
| `payload`       | `[]byte`   | Message payload (raw bytes)                           | Required              |

**Validation Rules**:
- `csid` must be >= 2 (0 and 1 reserved for protocol use, FR-007)
- `msgTypeID` must be in valid range: 1-6 (control), 8 (audio), 9 (video), 15-22 (command/data/shared object) (FR-007)
- `msgLength` must equal `len(payload)` (integrity check)
- Control messages (types 1-6) must have `csid=2` and `msgStreamID=0` (protocol convention)
- `msgStreamID` little-endian encoding (RTMP spec quirk)

**Message Type IDs**:

| Type ID | Name                     | CSID (convention) | MSID (convention) |
|---------|--------------------------|-------------------|-------------------|
| 1       | Set Chunk Size           | 2                 | 0                 |
| 2       | Abort Message            | 2                 | 0                 |
| 3       | Acknowledgement          | 2                 | 0                 |
| 4       | User Control Message     | 2                 | 0                 |
| 5       | Window Acknowledgement Size | 2              | 0                 |
| 6       | Set Peer Bandwidth       | 2                 | 0                 |
| 8       | Audio Message            | 4                 | streamID          |
| 9       | Video Message            | 6                 | streamID          |
| 15      | Data Message AMF3        | 3                 | streamID          |
| 18      | Data Message AMF0        | 3                 | streamID          |
| 17      | Command Message AMF3     | 3                 | 0 or streamID     |
| 20      | Command Message AMF0     | 3                 | 0 or streamID     |
| 19      | Shared Object AMF0       | 3                 | 0                 |
| 16      | Shared Object AMF3       | 3                 | 0                 |
| 22      | Aggregate Message        | varies            | streamID          |

**Methods** (conceptual):
- `Encode() []byte`: Serializes message to byte array (for sending)
- `Decode(data []byte) error`: Parses byte array into Message struct (for receiving)
- `IsControl() bool`: Returns true if msgTypeID in [1-6]
- `IsMedia() bool`: Returns true if msgTypeID in [8, 9]
- `IsCommand() bool`: Returns true if msgTypeID in [17, 20]

**Concurrency Safety**:
- Message instances are immutable after creation (safe to pass between goroutines)
- Payload slice is not copied (avoid unnecessary allocation), receivers must not modify

---

### ChunkStreamState

**Package**: `internal/rtmp/chunk`  
**Purpose**: Tracks per-CSID state for chunk header compression (FMT 1, 2, 3). Enables protocol to omit repeated fields in subsequent chunks.

**Fields**:

| Field             | Type       | Description                                   | Default/Initial Value |
|-------------------|------------|-----------------------------------------------|-----------------------|
| `csid`            | `uint32`   | Chunk stream ID                               | Set at creation       |
| `lastTimestamp`   | `uint32`   | Last absolute timestamp                       | 0                     |
| `lastMsgLength`   | `uint32`   | Last message length                           | 0                     |
| `lastMsgTypeID`   | `uint8`    | Last message type ID                          | 0                     |
| `lastMsgStreamID` | `uint32`   | Last message stream ID (little-endian)        | 0                     |
| `buffer`          | `[]byte`   | Accumulating buffer for incomplete message    | nil or empty slice    |
| `bytesReceived`   | `uint32`   | Bytes received for current message            | 0                     |

**Purpose**:
- FMT 0 (full header): Update all fields, start new message
- FMT 1 (no MSID): Reuse lastMsgStreamID, update others
- FMT 2 (timestamp delta only): Reuse lastMsgLength, lastMsgTypeID, lastMsgStreamID
- FMT 3 (no header): Reuse all fields, continue current message

**Validation Rules**:
- `csid` must match the CSID of received chunk (sanity check)
- `buffer` grows until `bytesReceived == lastMsgLength`, then message complete
- `buffer` must not exceed reasonable size (e.g., 10MB) to prevent memory exhaustion (FR-045)

**State Transitions**:

```
Initial (no message in progress)
    │
    ├─> Accumulating (FMT 0 received, buffer growing)
    │       │
    │       ├─> Complete (bytesReceived == lastMsgLength)
    │       │       │
    │       │       └─> Initial (buffer cleared, message dispatched)
    │       │
    │       └─> Aborted (Abort Message received, buffer cleared)
    │
    └─> Initial (ready for next message)
```

**Methods** (conceptual):
- `UpdateHeader(fmt uint8, header *MessageHeader)`: Updates fields based on FMT
- `AppendData(data []byte)`: Appends chunk data to buffer
- `IsComplete() bool`: Returns true if bytesReceived == lastMsgLength
- `ExtractMessage() *Message`: Constructs Message from complete buffer, resets state
- `Abort()`: Clears buffer and resets state (response to Abort Message type 2)

**Concurrency Safety**:
- Each ChunkStreamState instance accessed only by owning Connection's readLoop (no mutex needed)

---

### Recorder

**Package**: `internal/rtmp/media`  
**Purpose**: Records published stream to FLV file for playback or archival. Optional per-stream.

**Fields**:

| Field              | Type        | Description                                | Default/Initial Value    |
|--------------------|-------------|--------------------------------------------|--------------------------|
| `file`             | `*os.File`  | FLV output file handle                     | Opened at start          |
| `streamKey`        | `string`    | Stream identifier (for logging)            | Set at creation          |
| `startTime`        | `time.Time` | Recording start timestamp                  | time.Now() at creation   |
| `bytesWritten`     | `uint64`    | Total bytes written to file                | 0                        |
| `videoFrameCount`  | `uint64`    | Count of video frames recorded             | 0                        |
| `audioFrameCount`  | `uint64`    | Count of audio frames recorded             | 0                        |
| `headerWritten`    | `bool`      | True if FLV header written                 | false                    |

**Validation Rules**:
- `file` must be writable (FR-025, NFR-003)
- FLV header must be written before first tag (FR-025)
- Write errors logged but do not interrupt live streaming (FR-038, FR-044)

**FLV File Structure**:
```
FLV Header (13 bytes):
  - Signature: "FLV" (3 bytes)
  - Version: 0x01 (1 byte)
  - Flags: 0x05 (1 byte, audio + video)
  - Header length: 0x00000009 (4 bytes, big-endian)
  - Previous tag size 0: 0x00000000 (4 bytes)

FLV Tags (per audio/video message):
  - Tag type: 0x08 (audio) or 0x09 (video) (1 byte)
  - Data size: message length (3 bytes, big-endian)
  - Timestamp: lower 24 bits (3 bytes), upper 8 bits (1 byte) = 4 bytes total
  - Stream ID: 0x000000 (3 bytes, always zero)
  - Data: audio/video payload
  - Previous tag size: tag size + 11 (4 bytes, big-endian)
```

**State Transitions**:

```
Uninitialized
    │
    ├─> Active (file opened, header written)
    │       │
    │       ├─> Writing (audio/video tags appended)
    │       │
    │       └─> Error (write failure, continue without recording)
    │
    └─> Closed (file flushed and closed)
```

**Methods** (conceptual):
- `WriteHeader() error`: Writes FLV header to file
- `WriteAudioTag(msg *Message) error`: Converts audio message to FLV tag, writes to file
- `WriteVideoTag(msg *Message) error`: Converts video message to FLV tag, writes to file
- `Close() error`: Flushes buffers, closes file, logs summary (bytes, frames, duration)

**Error Handling**:
- Write errors logged with context (stream key, error message) (FR-038)
- Recording stops on error, but live streaming continues (FR-038)
- Disk full scenario: log error, close file, set recorder to nil

**Concurrency Safety**:
- Recorder accessed only by stream's broadcast goroutine (no mutex needed)
- File I/O is blocking (os.File is not thread-safe, but single-threaded access)

---

## Relationships Summary

1. **Connection → Session** (1:1):
   - Connection creates Session after successful connect command
   - Session lifetime tied to Connection lifetime

2. **Session → Stream** (n:1):
   - Multiple Sessions (connections) can reference the same Stream (one publisher, many players)
   - Session stores streamKey to look up Stream in registry

3. **Connection → ChunkStreamState** (1:n):
   - One Connection manages multiple ChunkStreamStates (one per CSID)
   - ChunkStreamStates owned by Connection, no cross-connection sharing

4. **Stream → Recorder** (1:0..1):
   - Stream may have one Recorder if recording enabled
   - Recorder lifetime tied to Stream lifetime (or publisher disconnect)

5. **Stream → Connection** (1:1 publisher, 1:n subscribers):
   - Stream has one publisher Connection (or nil if no active publisher)
   - Stream has many subscriber Connections (or empty list)

---

## Data Flow

### Publish Flow (Publisher → Server → Stream)

```
1. Publisher Connection receives Audio/Video Message
2. ReadLoop decodes chunks → reassembles Message
3. Dispatcher routes Message to Stream (looked up by streamKey)
4. Stream.BroadcastMessage():
   a. Detect codecs (first frame only)
   b. If Recorder exists: write FLV tag
   c. For each Subscriber Connection: SendMessage(msg)
5. Subscriber WriteLoop encodes Message → chunks → TCP
```

### Play Flow (Server → Player)

```
1. Player Connection sends play command
2. Server looks up Stream by streamKey
3. Stream.AddSubscriber(playerConn)
4. Server sends User Control StreamBegin + onStatus NetStream.Play.Start
5. When Publisher sends Message:
   Stream.BroadcastMessage() → playerConn.SendMessage()
6. Player WriteLoop encodes Message → chunks → TCP → Player
```

---

## Database Schema (if applicable)

**Not applicable**: RTMP server is stateless (no persistent storage for streams). Stream registry is in-memory only.

**Future Enhancement**: If persistence needed (e.g., stream metadata, recording index), consider:
- SQLite for simple deployment (single file, no server)
- PostgreSQL for production (ACID, replication)
- Schema: streams table (key, start_time, end_time, codec_info, recording_path)

---

## Configuration (conceptual)

**Server Configuration** (flags or config file):

| Setting            | Type     | Description                           | Default    |
|--------------------|----------|---------------------------------------|------------|
| `listen_addr`      | string   | TCP listen address                    | ":1935"    |
| `chunk_size`       | uint32   | Default send chunk size               | 4096       |
| `window_ack_size`  | uint32   | Acknowledgement window                | 2500000    |
| `peer_bandwidth`   | uint32   | Peer bandwidth limit                  | 2500000    |
| `log_level`        | string   | Logging level (debug/info/warn/error) | "info"     |
| `record_all`       | bool     | Enable recording for all streams      | false      |
| `record_dir`       | string   | Recording output directory            | "recordings" |
| `max_connections`  | int      | Maximum concurrent connections (0=unlimited) | 0     |

**Per-Stream Configuration** (optional, future enhancement):

| Setting           | Type   | Description                   | Default |
|-------------------|--------|-------------------------------|---------|
| `stream_key`      | string | Stream identifier "app/name"  | -       |
| `record_enabled`  | bool   | Enable recording for stream   | false   |
| `max_subscribers` | int    | Max players for stream        | 0 (unlimited) |

---

## Summary

This data model defines:
- **6 core entities**: Connection, Session, Stream, Message, ChunkStreamState, Recorder
- **Validation rules** derived from functional requirements (FR-001 through FR-054)
- **State transitions** for connection, session, and stream lifecycles
- **Relationships** enabling publish/subscribe pattern
- **Concurrency safety** guidelines for Go implementation
- **Configuration** parameters for operational flexibility

All entities align with Constitution Principle III (Modularity and Package Discipline) and are testable per Constitution Principle IV (Test-First with Golden Vectors).

**Next Phase**: Phase 1 continues with contract generation (contracts/*.md) and quickstart scenario (quickstart.md).

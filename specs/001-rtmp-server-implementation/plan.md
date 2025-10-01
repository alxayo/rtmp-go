# Implementation Plan: RTMP Server Implementation

**Branch**: `001-rtmp-server-implementation` | **Date**: 2025-10-01 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `c:\code\alxayo\go-rtmp\specs\001-rtmp-server-implementation\spec.md`

## Execution Flow (/plan command scope)
```
1. Load feature spec from Input path ✓
   → Loaded successfully from specs/001-rtmp-server-implementation/spec.md
2. Fill Technical Context ✓
   → Project Type: Single project (Go binary server + client)
   → Structure Decision: Go standard project layout
3. Fill Constitution Check section ✓
4. Evaluate Constitution Check section ✓
   → No violations detected
   → Update Progress Tracking: Initial Constitution Check ✓
5. Execute Phase 0 → research.md ✓
   → All clarifications resolved from spec.md Session 2025-10-01
6. Execute Phase 1 → contracts, data-model.md, quickstart.md, .github/copilot-instructions.md ✓
7. Re-evaluate Constitution Check section ✓
   → No new violations
   → Update Progress Tracking: Post-Design Constitution Check ✓
8. Plan Phase 2 → Describe task generation approach ✓
9. STOP - Ready for /tasks command
```

**IMPORTANT**: The /plan command STOPS at step 9. Phase 2 execution (tasks.md creation) is handled by the /tasks command.

## Summary
Primary requirement: Implement an RTMP server that handles handshake, chunking, command processing, and basic audio/video streaming with support for multiple concurrent publishers and players. Server operates in trusted network environment (no authentication), supports 10-50 concurrent connections with 3-5 second latency tolerance, and includes optional stream recording to FLV format. Codec-agnostic relay approach with logging of detected codec information.

Technical approach: Protocol-first implementation following RTMP v3 specification, using Go standard library with minimal dependencies. Modular architecture with distinct packages for handshake, chunking, control, AMF0 encoding, RPC commands, and media relay. Concurrent per-connection goroutines (readLoop/writeLoop) with context-based lifecycle management. Test-driven development with golden test vectors and FFmpeg/OBS interoperability validation.

## Technical Context
**Language/Version**: Go 1.21+  
**Primary Dependencies**: Go standard library only (net, io, encoding/binary, context, sync); no external dependencies for core protocol  
**Storage**: Local filesystem for optional FLV recording (when enabled via configuration)  
**Testing**: Go testing framework with table-driven tests, golden vectors, integration tests, FFmpeg/OBS interop tests  
**Target Platform**: Linux/macOS/Windows server (cross-platform via Go)  
**Project Type**: Single project (Go server + CLI client)  
**Performance Goals**: 10-50 concurrent connections, 3-5 second end-to-end latency, handshake <50ms local/<200ms WAN  
**Constraints**: Protocol compliance (RTMP v3), memory <10MB per connection, no transcoding/transmuxing  
**Scale/Scope**: Small-scale development/testing environment, codec-agnostic transparent relay, single server deployment  

## Constitution Check
*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

### I. Protocol-First Implementation ✓
- [x] All handshake sequences follow RTMP v3 spec (C0/C1/C2 ↔ S0/S1/S2)
- [x] Chunk headers (Basic Header, Message Header, Extended Timestamp) match spec byte-for-byte
- [x] State machines explicit: handshake FSM, chunk stream state per CSID
- [x] Extended timestamp handling (0xFFFFFF trigger), MSID little-endian, CSID encoding rules
- [x] No protocol shortcuts or assumptions

### II. Idiomatic Go ✓
- [x] Standard library only (no external dependencies for core protocol)
- [x] Simple, clear code following Effective Go and Go Code Review Comments
- [x] MixedCaps naming, early returns, make zero value useful
- [x] Structured error handling with context (`fmt.Errorf` with `%w`)
- [x] Document all exported symbols
- [x] Channels for goroutine communication

### III. Modularity and Package Discipline ✓
- [x] Package structure: `cmd/`, `internal/rtmp/{handshake,chunk,control,amf,rpc,media,conn,server,client}`, `internal/{bufpool,logger,errors}`
- [x] No circular dependencies
- [x] Single responsibility per package
- [x] Clear package boundaries

### IV. Test-First with Golden Vectors ✓
- [x] Golden test vectors for handshake, chunk headers (FMT 0-3), control messages, AMF0
- [x] Unit tests per package
- [x] Integration tests for handshake → command → media flows
- [x] Interop tests with FFmpeg (publish) and ffplay (play)
- [x] Target >80% coverage for protocol-critical paths

### V. Concurrency Safety and Backpressure ✓
- [x] One readLoop + writeLoop goroutine per connection
- [x] Context-based cancellation and shutdown
- [x] Bounded outbound channels with backpressure policy (drop slow consumers)
- [x] Mutex for shared state (stream registry)
- [x] No goroutine leaks (cleanup on connection close)

### VI. Observability and Debuggability ✓
- [x] Structured logging (connection lifecycle, stream events, codec detection, errors)
- [x] Log peer addresses, stream keys, connection IDs
- [x] Debug mode for protocol traces (optional hex dumps)
- [x] Error context includes connection/stream/message type
- [x] Timeout logging for network I/O

### VII. Simplicity and Incrementalism ✓
- [x] Simple handshake only (no complex handshake)
- [x] AMF0 only (no AMF3)
- [x] Basic commands: connect, _result, createStream, publish, play, deleteStream
- [x] Transparent byte forwarding (no transcoding)
- [x] YAGNI: no speculative features

**Status**: All constitutional principles satisfied. No violations to justify.

## Project Structure

### Documentation (this feature)
```
specs/001-rtmp-server-implementation/
├── spec.md              # Feature specification
├── plan.md              # This file (/plan command output)
├── research.md          # Phase 0 output (/plan command)
├── data-model.md        # Phase 1 output (/plan command)
├── quickstart.md        # Phase 1 output (/plan command)
├── contracts/           # Phase 1 output (/plan command)
│   ├── handshake.md     # Handshake protocol contract
│   ├── chunking.md      # Chunk stream protocol contract
│   ├── control.md       # Control message contracts
│   ├── amf0.md          # AMF0 encoding/decoding contract
│   ├── commands.md      # RTMP command contracts
│   └── media.md         # Media message contracts
└── tasks.md             # Phase 2 output (/tasks command)
```

### Source Code (repository root)
```
go-rtmp/
├── cmd/
│   ├── rtmp-server/     # Server executable
│   │   └── main.go
│   └── rtmp-client/     # CLI client for testing
│       └── main.go
├── internal/
│   ├── rtmp/
│   │   ├── handshake/   # Handshake FSM (C0/C1/C2 ↔ S0/S1/S2)
│   │   ├── chunk/       # Chunk header parsing/serialization, dechunker, chunker
│   │   ├── control/     # Protocol control messages (Set Chunk Size, WAS, SPB, Ack)
│   │   ├── amf/         # AMF0 encoder/decoder
│   │   ├── rpc/         # Command processing (connect, createStream, publish, play)
│   │   ├── media/       # Media message handling (Audio type 8, Video type 9)
│   │   ├── conn/        # Connection abstraction (readLoop, writeLoop)
│   │   ├── server/      # Server listener, connection manager, stream registry
│   │   └── client/      # Client implementation (publish, play)
│   ├── bufpool/         # Buffer pooling for memory efficiency
│   ├── logger/          # Structured logging
│   └── errors/          # Domain-specific error types
├── tests/
│   ├── golden/          # Golden test vectors (hex dumps)
│   ├── integration/     # Integration tests (loopback, synthetic flows)
│   └── interop/         # FFmpeg/OBS interop test scripts
├── docs/                # Existing protocol documentation
├── go.mod
├── go.sum
└── README.md
```

**Structure Decision**: Go standard project layout with `cmd/` for executables, `internal/` for implementation packages (not intended for external import), and `tests/` for test artifacts. Package structure mirrors RTMP protocol layers (handshake → chunking → control → AMF → RPC → media) following Constitution Principle III (Modularity and Package Discipline).

## Phase 0: Outline & Research

### Research Tasks Completed
All clarifications from the feature specification have been resolved (see spec.md Clarifications section, Session 2025-10-01):

1. **Codec Handling Strategy**: Hybrid approach - codec-agnostic transparent relay with logging/reporting of detected codec information from FLV metadata headers for monitoring purposes.

2. **Maximum Concurrent Connections**: Target 10-50 simultaneous connections (small-scale development/testing environment).

3. **Target Latency**: 3-5 seconds end-to-end latency (relaxed for easier buffering and stability).

4. **Authentication Mechanism**: No authentication (accept all connections, trusted network assumption suitable for development/testing).

5. **Stream Recording**: Yes, optional recording to FLV format, configurable per-stream or globally.

### Technology Decisions

**Decision**: Use Go standard library exclusively for core protocol implementation  
**Rationale**: 
- Aligns with Constitution Principle II (Idiomatic Go) emphasizing minimal dependencies
- `net` package provides robust TCP handling with deadline support
- `encoding/binary` for byte order operations (big-endian for most fields, little-endian for MSID)
- `io` package for buffered reading with `io.ReadFull` for precise byte counts
- `sync` for concurrency primitives (Mutex, WaitGroup, Context)
- Reduces supply chain risk and simplifies deployment (single binary)

**Alternatives Considered**:
- External RTMP libraries (rejected: violates from-scratch implementation requirement, hides protocol details)
- Networking frameworks (rejected: standard library sufficient, unnecessary abstraction)
- AMF libraries (rejected: AMF0 subset is simple enough to implement, learning opportunity)

**Decision**: AMF0 encoding/decoding only (no AMF3)  
**Rationale**:
- AMF0 is sufficient for core RTMP commands (connect, createStream, publish, play)
- FFmpeg, OBS, and most RTMP clients default to AMF0 (objectEncoding=0)
- Simpler type system: Number (float64), Boolean, String, Object (map), Null, Array
- Aligns with Constitution Principle VII (Simplicity and Incrementalism)

**Alternatives Considered**:
- AMF3 support (deferred: added complexity for minimal benefit, can be added later if needed)

**Decision**: Simple handshake only (RTMP v3)  
**Rationale**:
- Documented in Adobe RTMP specification publicly available portion
- Supported by all modern RTMP clients (FFmpeg, OBS, Wirecast)
- Complex handshake (with HMAC-SHA256 digest and DH key exchange) is legacy Flash Player requirement
- Aligns with Constitution Principle VII (Simplicity) and spec's Out of Scope (RTMPE)

**Alternatives Considered**:
- Complex handshake (rejected: out of scope per spec, unnecessary for target use case)

**Decision**: Codec-agnostic transparent relay with metadata logging  
**Rationale**:
- Per clarification: hybrid approach accepting any codec but logging information
- FLV container metadata (Audio/Video Data tags) contain codec information (CodecID field)
- Server extracts and logs: video codec (H.264 AVC, VP6, etc.), audio codec (AAC, MP3, etc.)
- No validation or transcoding required
- Maximum interoperability: works with any encoded stream

**Alternatives Considered**:
- Strict codec validation (rejected: limits flexibility, not required for relay server)
- Full codec parsing (rejected: unnecessary complexity, out of scope)

**Decision**: FLV format for optional stream recording  
**Rationale**:
- RTMP messages (Audio type 8, Video type 9) are already FLV tag format
- Minimal transformation: add FLV header (13 bytes) + per-tag headers (11 bytes + 4 bytes back pointer)
- Industry standard format readable by ffplay, VLC, and other media players
- Supports timestamp preservation for DVR-style playback

**Alternatives Considered**:
- Raw RTMP dump (rejected: not playable without custom tooling)
- MP4 container (rejected: requires muxing complexity, transcoding)
- HLS/DASH (rejected: out of scope, requires segmentation)

**Decision**: Structured logging with configurable levels (debug, info, warn, error)  
**Rationale**:
- Constitution Principle VI (Observability) requires protocol-level visibility
- Use Go standard library `log/slog` (available since Go 1.21)
- Structured fields: connection_id, peer_addr, stream_key, msg_type, csid, msid, timestamp
- Debug mode enables hex dumps of raw bytes for wire-level debugging
- No external dependency (avoids logrus, zap, zerolog)

**Alternatives Considered**:
- External logging libraries (deferred: stdlib sufficient for initial implementation)
- Plain text logs (rejected: harder to parse for monitoring/alerting)

### Integration Research

**FFmpeg Interoperability Patterns**:
- **Publish**: `ffmpeg -re -i input.mp4 -c copy -f flv rtmp://localhost:1935/live/streamkey`
  - Expects: connect command with app="live", tcUrl="rtmp://localhost:1935/live"
  - Expects: createStream response with stream ID
  - Sends: publish command with stream name="streamkey"
  - Sends: @setDataFrame metadata message (AMF0) with video/audio codec info
  - Sends: Audio (type 8) and Video (type 9) messages interleaved
- **Play**: `ffplay rtmp://localhost:1935/live/streamkey`
  - Expects: connect → _result
  - Sends: createStream → expects stream ID
  - Sends: play command with stream name
  - Expects: User Control (StreamBegin event)
  - Expects: onStatus (NetStream.Play.Start)
  - Expects: Audio/Video messages

**OBS Interoperability Patterns**:
- Similar to FFmpeg but also sends:
  - releaseStream command before connect (can be no-op)
  - FCPublish command before publish (Flash-era, can be no-op)
  - FCUnpublish on disconnect (can be no-op)
- Solution: Implement minimal response handlers (respond with _result or _error)

**Chunk Size Negotiation Best Practices**:
- Default chunk size: 128 bytes (per RTMP spec)
- Server should send Set Chunk Size (4096 bytes) immediately after handshake for better throughput
- Client may send Set Chunk Size; server must respect it for reading
- Each direction tracks its own chunk size independently

**Window Acknowledgement and Bandwidth**:
- Server sends Window Acknowledgement Size (typically 2,500,000 bytes)
- Server sends Set Peer Bandwidth (2,500,000 bytes, limit type Dynamic=2)
- Client must send Acknowledgement message when bytes received exceeds window
- Implements backpressure: server waits for ACK before sending more data

**Output**: research.md with all decisions and rationale documented

## Phase 1: Design & Contracts

### Data Model (data-model.md)

#### Core Entities

**Connection** (internal/rtmp/conn)
- Fields:
  - `id`: string (UUID, for logging and tracking)
  - `remoteAddr`: net.Addr (peer address)
  - `conn`: net.Conn (underlying TCP connection)
  - `ctx`: context.Context (cancellation)
  - `cancel`: context.CancelFunc
  - `readChunkSize`: uint32 (current receive chunk size, default 128)
  - `writeChunkSize`: uint32 (current send chunk size, default 128)
  - `windowAckSize`: uint32 (acknowledgement window)
  - `peerBandwidth`: uint32
  - `bytesReceived`: uint64 (for ACK tracking)
  - `lastAckSent`: uint64
  - `chunkStreams`: map[uint32]*ChunkStreamState (per-CSID state)
  - `outboundQueue`: chan *Message (bounded channel for writeLoop)
- Validation Rules:
  - Chunk sizes must be >= 128 and <= 65536
  - windowAckSize and peerBandwidth > 0
- State Transitions:
  - Handshaking → ControlBurst → CommandProcessing → Streaming → Disconnecting → Closed

**Session** (internal/rtmp/conn)
- Fields:
  - `app`: string (application name from connect command)
  - `tcUrl`: string (full URL from connect command)
  - `flashVer`: string (client version)
  - `objectEncoding`: uint8 (0=AMF0, 3=AMF3; we only support 0)
  - `transactionID`: uint32 (counter for command responses)
  - `streamID`: uint32 (allocated stream ID, typically 1)
- Validation Rules:
  - app must not be empty
  - objectEncoding must be 0 (AMF0 only)
- State Transitions:
  - Uninitialized → Connected (after connect command) → StreamCreated (after createStream) → Publishing/Playing

**Stream** (internal/rtmp/server)
- Fields:
  - `key`: string (format: "app/streamname")
  - `publisher`: *Connection (publishing connection, nil if not active)
  - `subscribers`: []*Connection (list of playing connections)
  - `metadata`: map[string]interface{} (from @setDataFrame)
  - `videoCodec`: string (detected from FLV metadata, e.g., "H.264 AVC")
  - `audioCodec`: string (detected from FLV metadata, e.g., "AAC")
  - `startTime`: time.Time
  - `recorder`: *Recorder (optional, nil if recording disabled)
- Validation Rules:
  - Only one publisher per stream at a time
  - key must match format "app/streamname"
- State Transitions:
  - Created → Active (publisher connected) → Idle (publisher disconnected) → Deleted

**Message** (internal/rtmp/chunk)
- Fields:
  - `csid`: uint32 (chunk stream ID)
  - `timestamp`: uint32 (absolute or delta, depends on header format)
  - `msgLength`: uint32
  - `msgTypeID`: uint8 (1-22, see spec)
  - `msgStreamID`: uint32 (little-endian)
  - `payload`: []byte
- Validation Rules:
  - csid >= 2 (0 and 1 reserved)
  - msgTypeID in valid range (1, 2, 3, 4, 5, 6, 8, 9, 15-22)
  - msgLength matches len(payload)
- Protocol Rules:
  - Control messages (types 1-6) always on csid=2, msid=0
  - Command messages (types 18, 20) typically on csid=3
  - Audio messages (type 8) typically on csid=4
  - Video messages (type 9) typically on csid=6

**ChunkStreamState** (internal/rtmp/chunk)
- Fields:
  - `csid`: uint32
  - `lastTimestamp`: uint32
  - `lastMsgLength`: uint32
  - `lastMsgTypeID`: uint8
  - `lastMsgStreamID`: uint32
  - `buffer`: []byte (accumulating chunks)
  - `bytesReceived`: uint32 (within current message)
- Purpose: Track per-CSID state for header compression (FMT 1, 2, 3)

**Recorder** (internal/rtmp/media)
- Fields:
  - `file`: *os.File (FLV output file)
  - `streamKey`: string
  - `startTime`: time.Time
  - `bytesWritten`: uint64
  - `videoFrameCount`: uint64
  - `audioFrameCount`: uint64
- Validation Rules:
  - FLV file must be writable
  - Gracefully handle write errors without interrupting live stream

### API Contracts (contracts/)

#### Handshake Contract (contracts/handshake.md)

**Simple Handshake Sequence**:

Client → Server: C0 (1 byte) + C1 (1536 bytes)
- C0: version byte 0x03
- C1: timestamp(4) + zero(4) + random(1528)

Server → Client: S0 (1 byte) + S1 (1536 bytes) + S2 (1536 bytes)
- S0: version byte 0x03 (must match C0)
- S1: timestamp(4) + zero(4) + random(1528)
- S2: C1.timestamp(4) + C1.zero(4) + C1.random(1528) (echo C1)

Client → Server: C2 (1536 bytes)
- C2: S1.timestamp(4) + S1.zero(4) + S1.random(1528) (echo S1)

**State Machine** (Server):
```
Initial → RecvC0C1 → SentS0S1S2 → RecvC2 → Completed
```

**Error Cases**:
- Version mismatch (C0 != 0x03): reject connection
- Truncated messages: timeout or reject
- Timeout (5 seconds): close connection

**Test Scenarios**:
- Valid handshake: golden byte sequence
- Wrong version (0x06): expect rejection
- Truncated C1 (< 1536 bytes): expect timeout/error
- Replay test: C2 must echo S1 random bytes

#### Chunking Contract (contracts/chunking.md)

**Chunk Format**:
```
Chunk = BasicHeader + MessageHeader + [ExtendedTimestamp] + ChunkData
```

**Basic Header** (1-3 bytes):
- fmt (2 bits): 0-3 (determines message header format)
- csid (6/14/22 bits): chunk stream ID
- Encoding:
  - csid 2-63: 1 byte (fmt << 6 | csid)
  - csid 64-319: 2 bytes (fmt << 6 | 0, csid - 64)
  - csid 320-65599: 3 bytes (fmt << 6 | 1, (csid - 64) low byte, high byte)

**Message Header** (0/3/7/11 bytes):
- FMT 0 (11 bytes): timestamp(3) + msgLength(3) + msgTypeID(1) + msgStreamID(4, little-endian)
- FMT 1 (7 bytes): timestampDelta(3) + msgLength(3) + msgTypeID(1)
- FMT 2 (3 bytes): timestampDelta(3)
- FMT 3 (0 bytes): reuse all previous fields

**Extended Timestamp** (0 or 4 bytes):
- Present if timestamp or timestampDelta in message header == 0xFFFFFF
- 4 bytes, big-endian uint32

**Chunk Data**:
- Up to chunkSize bytes (default 128, negotiable up to 65536)
- If message length > chunkSize, split into multiple chunks with FMT 3 continuation

**Test Scenarios**:
- FMT 0 full header: parse and serialize
- FMT 1, 2, 3: verify field inheritance from previous chunk
- Extended timestamp: message with timestamp >= 16777215
- Interleaved chunks: csid=4 (audio), csid=6 (video) alternating
- Chunk size change: default 128 → Set Chunk Size 4096 → verify reassembly

#### Control Messages Contract (contracts/control.md)

**Set Chunk Size (Type 1)**:
- Payload: 4 bytes big-endian uint32 (new chunk size)
- CSID: 2, MSID: 0
- Constraint: 1 <= chunkSize <= 2147483647 (bit 31 must be 0)
- Effect: Receiver updates its readChunkSize for decoding subsequent chunks

**Abort Message (Type 2)**:
- Payload: 4 bytes big-endian uint32 (csid to abort)
- CSID: 2, MSID: 0
- Effect: Discard partially received message on specified csid

**Acknowledgement (Type 3)**:
- Payload: 4 bytes big-endian uint32 (total bytes received so far)
- CSID: 2, MSID: 0
- Trigger: When bytesReceived - lastAckSent >= windowAckSize

**User Control Message (Type 4)**:
- Payload: 2 bytes event type + event-specific data
- Events:
  - StreamBegin (0): streamID(4 bytes)
  - StreamEOF (1): streamID(4 bytes)
  - StreamDry (2): streamID(4 bytes)
  - SetBufferLength (3): streamID(4) + bufferLength(4)
  - StreamIsRecorded (4): streamID(4)
  - PingRequest (6): timestamp(4)
  - PingResponse (7): timestamp(4)

**Window Acknowledgement Size (Type 5)**:
- Payload: 4 bytes big-endian uint32 (window size in bytes)
- CSID: 2, MSID: 0
- Effect: Peer must send Acknowledgement after receiving this many bytes

**Set Peer Bandwidth (Type 6)**:
- Payload: 4 bytes big-endian uint32 (bandwidth) + 1 byte limit type
- Limit Types: 0=Hard, 1=Soft, 2=Dynamic
- CSID: 2, MSID: 0
- Effect: Receiver adjusts send rate (implementation-specific)

**Test Scenarios**:
- Set Chunk Size 4096: verify bytes encode/decode correctly
- Acknowledgement: send after receiving exactly windowAckSize bytes
- User Control StreamBegin: verify stream ID field
- Window Ack Size 2500000: verify effect on ACK sending

#### AMF0 Contract (contracts/amf0.md)

**Supported Types**:
- Number (0x00): 8-byte IEEE 754 double, big-endian
- Boolean (0x01): 1 byte (0x00=false, 0x01=true)
- String (0x02): 2-byte length (big-endian) + UTF-8 bytes
- Object (0x03): key-value pairs (string + value) + 3-byte end marker (0x00 0x00 0x09)
- Null (0x05): no payload
- ECMA Array (0x08): 4-byte count + key-value pairs + end marker (treated as Object)
- Strict Array (0x0A): 4-byte count + values (no keys)

**Encoding Rules**:
- All integers big-endian
- Strings prefixed with 2-byte length
- Objects terminated with 0x00 0x00 0x09
- Recursive: values can be nested objects/arrays

**Decoding Rules**:
- Read type marker (1 byte), then parse based on type
- Object end detected by key length == 0 and marker 0x09
- Return parsed value as Go type: float64, bool, string, map[string]interface{}, nil, []interface{}

**Test Scenarios**:
- Encode/decode Number 1.5: [0x00, 0x3F, 0xF8, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00]
- Encode/decode String "test": [0x02, 0x00, 0x04, 0x74, 0x65, 0x73, 0x74]
- Encode/decode Object {"key": "value"}: [0x03, 0x00, 0x03, 'k','e','y', 0x02, 0x00, 0x05, 'v','a','l','u','e', 0x00, 0x00, 0x09]
- Nested object: object containing object
- Strict array [1, 2, 3]: [0x0A, 0x00, 0x00, 0x00, 0x03, 0x00, ...doubles...]

#### Commands Contract (contracts/commands.md)

**Command Message Format (Type 20, AMF0)**:
- Payload: [commandName(string), transactionID(number), commandObject(object|null), ...optionalArgs]
- CSID: 3 (by convention), MSID: 0 for NetConnection commands, streamID for NetStream commands

**connect Command** (Client → Server):
- Payload: ["connect", 1, {app: "live", flashVer: "FMLE/3.0", tcUrl: "rtmp://server:1935/live", ...}]
- Response: _result with connect success
  - ["_result", 1, {fmsVer: "GO-RTMP/1.0", capabilities: 31}, {level: "status", code: "NetConnection.Connect.Success", description: "Connection succeeded", objectEncoding: 0}]

**createStream Command** (Client → Server):
- Payload: ["createStream", transactionID, null]
- Response: _result with stream ID
  - ["_result", transactionID, null, streamID(number, e.g., 1.0)]

**publish Command** (Client → Server):
- Payload: ["publish", 0, null, streamName(string), publishType(string, e.g., "live")]
- MSID: streamID (from createStream response)
- Response: onStatus NetStream.Publish.Start
  - ["onStatus", 0, null, {level: "status", code: "NetStream.Publish.Start", description: "Publishing..."}]

**play Command** (Client → Server):
- Payload: ["play", 0, null, streamName(string), start(number, -2=live, -1=recorded, >=0=offset), duration(number, -1=all), reset(boolean)]
- MSID: streamID
- Response: User Control StreamBegin + onStatus NetStream.Play.Start
  - User Control: type=StreamBegin, streamID
  - onStatus: ["onStatus", 0, null, {level: "status", code: "NetStream.Play.Start", description: "Playing..."}]

**deleteStream Command** (Client → Server):
- Payload: ["deleteStream", 0, null, streamID(number)]
- Response: None (close stream resources)

**releaseStream, FCPublish, FCUnpublish** (Client → Server):
- Payload: [commandName, transactionID, null, streamName]
- Response: _result or _error (can be no-op)

**Test Scenarios**:
- connect command: parse app, tcUrl; respond with _result
- createStream: allocate stream ID, respond with _result
- publish: associate stream with connection, send onStatus
- play: find stream, send StreamBegin + onStatus, start media relay
- deleteStream: clean up stream resources

#### Media Messages Contract (contracts/media.md)

**Audio Message (Type 8)**:
- Payload: FLV Audio Tag format
  - Byte 0 (SoundFormat, SoundRate, SoundSize, SoundType bits)
  - Byte 1+: Audio data (AAC: AACPacketType + audio payload)
- Timestamp: presentation time in milliseconds
- CSID: 4 (by convention)
- MSID: streamID

**Video Message (Type 9)**:
- Payload: FLV Video Tag format
  - Byte 0: FrameType(4 bits) + CodecID(4 bits)
  - Byte 1+: Video data (AVC: AVCPacketType + CompositionTime + NALU data)
- Timestamp: presentation time in milliseconds
- CSID: 6 (by convention)
- MSID: streamID

**Codec Detection**:
- Audio CodecID (byte 0 bits 4-7): 2=MP3, 10=AAC, 11=Speex, etc.
- Video CodecID (byte 0 bits 0-3): 2=Sorenson H.263, 7=AVC (H.264), 12=HEVC, etc.

**Transparent Relay**:
- Server does not parse beyond first byte (codec detection)
- Payload forwarded as-is to subscribers
- Timestamp preserved or adjusted (relative to stream start)

**Test Scenarios**:
- Audio AAC frame: parse CodecID=10, relay to subscribers
- Video AVC frame: parse CodecID=7, relay to subscribers
- Timestamp synchronization: audio and video frames with same timestamp align
- Missing publisher: play command before publish → onStatus error

### Quickstart (quickstart.md)

**Quickstart Test Scenario**: FFmpeg publish + ffplay playback

**Prerequisites**:
- Go 1.21+ installed
- FFmpeg/ffplay installed
- Sample media file (e.g., test.mp4)

**Steps**:
1. Build server: `go build -o bin/rtmp-server ./cmd/rtmp-server`
2. Run server: `./bin/rtmp-server -listen :1935 -log-level info`
3. Publish stream (terminal 2): `ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test`
4. Play stream (terminal 3): `ffplay rtmp://localhost:1935/live/test`
5. Verify: Video plays in ffplay window within 3-5 seconds
6. Stop: Ctrl+C in FFmpeg, observe clean disconnect in server logs
7. Stop: Ctrl+C in ffplay, observe clean disconnect in server logs

**Expected Results**:
- Server logs: "Connection accepted from ...", "Handshake completed", "connect command from app=live", "createStream allocated streamID=1", "publish started: live/test", "play started: live/test", "Stream live/test: H.264 AVC video, AAC audio", "Recording started: recordings/live_test_20251001_120000.flv" (if enabled)
- FFmpeg output: "Connection to tcp://localhost:1935 successful", no errors
- ffplay: Video playback starts, no buffering warnings

**Validation**:
- [ ] Handshake completes successfully
- [ ] FFmpeg publishes without errors
- [ ] ffplay receives and plays stream
- [ ] Server logs show codec detection
- [ ] No memory leaks (run for 10 minutes, check memory stability)
- [ ] Graceful disconnect handling (Ctrl+C both clients)

### Agent File Update

**Action**: Update `.github/copilot-instructions.md` with current plan context

**Instructions**:
- Preserve existing Go and RTMP protocol instructions between markers
- Add section: "## Current Implementation Focus (2025-10-01)"
- Content:
  - Feature: RTMP Server Implementation (branch 001-rtmp-server-implementation)
  - Phase: Implementation (plan.md and contracts complete)
  - Priority tasks: Handshake FSM, Chunking (reader/writer), AMF0 codec, Control messages
  - Key references: specs/001-rtmp-server-implementation/{spec.md, plan.md, contracts/}
  - Test requirement: Golden vectors for handshake, chunking, AMF0; FFmpeg interop
- Keep under 150 lines total (constitution guidance)

**Output**: Updated `.github/copilot-instructions.md` with incremental context

## Phase 2: Task Planning Approach
*This section describes what the /tasks command will do - DO NOT execute during /plan*

**Task Generation Strategy**:
- Load `.specify/templates/tasks-template.md` as base template
- Generate tasks from Phase 1 design documents:
  - **Contracts** (contracts/*.md): Each contract section → verification task
  - **Data Model** (data-model.md): Each entity → implementation task
  - **Quickstart** (quickstart.md): Integration test scenario → validation task
- Order tasks by protocol layer dependencies:
  1. Foundation: bufpool, logger, errors
  2. Handshake: FSM implementation, golden tests
  3. Chunking: header parsing, dechunker, chunker, golden tests
  4. Control: message encoding/decoding, handlers
  5. AMF0: encoder, decoder, golden tests
  6. RPC: command processor, response builder
  7. Media: relay implementation, codec detection
  8. Server: listener, connection manager, stream registry
  9. Client: publish/play implementation
  10. Integration: FFmpeg/OBS interop tests, quickstart validation

**Task Structure**:
- Each task follows format:
  - **ID**: T001, T002, etc. (sequential numbering)
  - **Title**: Brief description (e.g., "Implement handshake FSM - server side")
  - **Description**: What to implement, expected behavior, references to contracts
  - **Dependencies**: Prerequisite task IDs (e.g., T001 depends on T000)
  - **Test Requirements**: Golden vectors, unit tests, integration tests
  - **Parallelizable**: [P] marker if can be done concurrently with other tasks
  - **Estimated Complexity**: Small (1-2 hours), Medium (3-6 hours), Large (1-2 days)

**Ordering Strategy**:
- TDD order: Test infrastructure before implementation (golden vectors first)
- Protocol layer order: Handshake → Chunking → Control → AMF0 → RPC → Media
- Within layer: Encoder/decoder before handlers
- Mark independent tasks with [P]:
  - bufpool, logger, errors can be parallel (T000, T001, T002) [P]
  - Handshake client/server FSMs can be parallel (T004, T005) [P]
  - AMF0 encoder/decoder can be parallel (T015, T016) [P]
  - Contract tests can be parallel once implementation exists

**Estimated Output**:
- Approximately 40-50 tasks total:
  - 3 foundation tasks (bufpool, logger, errors)
  - 6 handshake tasks (FSM, golden tests, integration)
  - 10 chunking tasks (headers, reader, writer, tests)
  - 6 control tasks (messages, handlers, tests)
  - 8 AMF0 tasks (types, encoder, decoder, tests)
  - 10 RPC tasks (command handlers, response builders, tests)
  - 4 media tasks (relay, codec detection, tests)
  - 6 server tasks (listener, stream registry, connection manager, tests)
  - 4 client tasks (publish, play, CLI, tests)
  - 6 integration tasks (FFmpeg, OBS, quickstart, performance)

**Task Template Example**:
```
## T003: Implement Handshake State Machine (Server)
**Description**: Implement server-side handshake FSM following contracts/handshake.md. States: Initial → RecvC0C1 → SentS0S1S2 → RecvC2 → Completed. Use io.ReadFull with 5-second deadlines.

**Dependencies**: T000 (logger), T002 (errors)

**Test Requirements**:
- Golden test: valid C0+C1 → S0+S1+S2 byte sequences
- Error test: wrong version byte (0x06) → expect rejection
- Error test: truncated C1 (1000 bytes) → expect timeout
- Integration test: loopback handshake (client → server)

**Parallelizable**: No (blocks on T000, T002)

**Estimated Complexity**: Medium (4-6 hours)

**Files**:
- internal/rtmp/handshake/server.go
- internal/rtmp/handshake/server_test.go
- tests/golden/handshake_valid_c0c1.bin
- tests/golden/handshake_valid_s0s1s2.bin
```

**IMPORTANT**: Phase 2 execution (actual tasks.md creation) is performed by the /tasks command, NOT by /plan. This section only describes the approach.

## Complexity Tracking
*Fill ONLY if Constitution Check has violations that must be justified*

No violations detected. Section intentionally left empty.

## Progress Tracking
*This checklist is updated during execution flow*

**Phase Status**:
- [x] Phase 0: Research complete (/plan command)
- [x] Phase 1: Design complete (/plan command)
- [x] Phase 2: Task planning approach complete (/plan command)
- [ ] Phase 3: Tasks generated (/tasks command)
- [ ] Phase 4: Implementation complete
- [ ] Phase 5: Validation passed

**Gate Status**:
- [x] Initial Constitution Check: PASS
- [x] Post-Design Constitution Check: PASS
- [x] All NEEDS CLARIFICATION resolved (from spec.md Session 2025-10-01)
- [x] Complexity deviations documented (none required)

**Artifacts Generated**:
- [x] plan.md (this file)
- [x] research.md (Phase 0)
- [x] data-model.md (Phase 1)
- [x] contracts/handshake.md (Phase 1)
- [x] contracts/chunking.md (Phase 1)
- [x] contracts/control.md (Phase 1)
- [x] contracts/amf0.md (Phase 1)
- [x] contracts/commands.md (Phase 1)
- [x] contracts/media.md (Phase 1)
- [x] quickstart.md (Phase 1)
- [x] .github/copilot-instructions.md updated (Phase 1)

**Next Step**: Run `/tasks` command to generate tasks.md from this plan.

---
*Based on Constitution v1.0.0 - See `.specify/memory/constitution.md`*

# Tasks: RTMP Server Implementation

**Input**: Design documents from `c:\code\alxayo\go-rtmp\specs\001-rtmp-server-implementation\`
**Prerequisites**: plan.md, research.md, data-model.md, contracts/, quickstart.md

## Execution Flow (main)
```
1. Load plan.md from feature directory ✓
   → Tech stack: Go 1.21+, standard library only
   → Structure: Go standard project layout
2. Load design documents: ✓
   → data-model.md: 6 entities (Connection, Session, Stream, Message, ChunkStreamState, Recorder)
   → contracts/: handshake, chunking, control, amf0, commands, media
   → quickstart.md: FFmpeg publish + ffplay playback scenario
3. Generate tasks by category: ✓
   → Setup: project init, dependencies, linting
   → Tests: golden tests, contract tests, integration tests
   → Core: handshake, chunking, control, AMF0, RPC, media
   → Integration: server, client, stream registry
   → Polish: interop tests, performance, docs
4. Apply task rules: ✓
   → Tests before implementation (TDD)
   → Independent packages marked [P]
   → Protocol layers ordered: handshake → chunking → control → AMF0 → RPC → media
5. Total tasks: 54 tasks
```

## Format: `[ID] [P?] Description`
- **[P]**: Can run in parallel (different files, no dependencies)
- Include exact file paths in descriptions

## Path Conventions
- **Repository root**: `c:\code\alxayo\go-rtmp\`
- **Source code**: `internal/`, `cmd/`
- **Tests**: Package tests (`*_test.go`), integration tests (`tests/`)
- **Golden vectors**: `tests/golden/`

---

## Phase 3.1: Setup & Foundation

### T001 [X]: Initialize Go Module and Project Structure
**Description**: Initialize Go module and create base directory structure following Go standard project layout.
**Files**:
- `go.mod` (if not exists)
- `cmd/rtmp-server/`
- `cmd/rtmp-client/`
- `internal/rtmp/`
- `internal/bufpool/`
- `internal/logger/`
- `internal/errors/`
- `tests/golden/`
- `tests/integration/`
- `tests/interop/`
**Commands**:
```powershell
go mod init github.com/alxayo/go-rtmp  # if not exists
go mod tidy
```
**Validation**: `go.mod` exists with module path
**Estimated Complexity**: Small (30 min)

---

### T002 [X]: Implement Buffer Pool
**Description**: Create buffer pool package for memory-efficient chunk buffer reuse. Reduces GC pressure for high-throughput streams.
**Files**:
- `internal/bufpool/pool.go`
- `internal/bufpool/pool_test.go`
**Requirements**:
- Pool sizes: 128, 4096, 65536 bytes (common chunk sizes)
- `Get(size int) []byte` and `Put(buf []byte)` methods
- Thread-safe using `sync.Pool`
- Unit tests: acquire, release, concurrent access
**Dependencies**: None
**Test Coverage**: >90%
**Estimated Complexity**: Small (1-2 hours)

---

### T003 [X]: Implement Structured Logger
**Description**: Create logger package using Go `log/slog` for structured logging with configurable levels.
**Files**:
- `internal/logger/logger.go`
- `internal/logger/logger_test.go`
**Requirements**:
- Levels: Debug, Info, Warn, Error
- Structured fields: conn_id, peer_addr, stream_key, msg_type, csid, msid, timestamp
- JSON output format
- Configurable log level via environment or flag
- Unit tests: log level filtering, field extraction
**Dependencies**: None
**Test Coverage**: >80%
**Estimated Complexity**: Small (1-2 hours)

---

### T004 [X]: Define Domain-Specific Errors
**Description**: Create error types package with wrapped errors for protocol violations, timeouts, and validation failures.
**Files**:
- `internal/errors/errors.go`
- `internal/errors/errors_test.go`
**Requirements**:
- Error types: `ProtocolError`, `HandshakeError`, `ChunkError`, `AMFError`, `TimeoutError`
- Wrap errors with context using `fmt.Errorf("%w", err)`
- Helper functions: `IsTimeout(err)`, `IsProtocolError(err)`
- Unit tests: error wrapping, type checking
**Dependencies**: None
**Test Coverage**: >80%
**Estimated Complexity**: Small (1 hour)

---

## Phase 3.2: Tests First (TDD) ⚠️ MUST COMPLETE BEFORE 3.3

### T005 [X]: Create Golden Test Vectors for Handshake
**Description**: Generate binary golden test files for RTMP handshake (C0+C1, S0+S1+S2, C2) following contracts/handshake.md.
**Files**:
- `tests/golden/handshake_valid_c0c1.bin`
- `tests/golden/handshake_valid_s0s1s2.bin`
- `tests/golden/handshake_valid_c2.bin`
- `tests/golden/handshake_invalid_version.bin`
**Requirements**:
- Valid handshake: C0=0x03, C1=1536 bytes (timestamp + zero + random)
- Invalid version: C0=0x06 (RTMPE)
- Generate using Go script or manual hex editor
**Dependencies**: None
**Estimated Complexity**: Small (1 hour)

---

### T006 [X]: Create Golden Test Vectors for Chunk Headers
**Description**: Generate binary golden test files for chunk headers (FMT 0-3) and multi-chunk messages following contracts/chunking.md.
**Files**:
- `tests/golden/chunk_fmt0_audio.bin`
- `tests/golden/chunk_fmt1_video.bin`
- `tests/golden/chunk_fmt2_delta.bin`
- `tests/golden/chunk_fmt3_continuation.bin`
- `tests/golden/chunk_extended_timestamp.bin`
- `tests/golden/chunk_interleaved.bin`
**Requirements**:
- FMT 0: Full header (11 bytes), CSID=4, audio message
- FMT 1: No stream ID (7 bytes), CSID=6, video message
- FMT 2: Timestamp delta only (3 bytes)
- FMT 3: No header, continuation chunk
- Extended timestamp: Timestamp >= 0xFFFFFF
- Interleaved: Audio + video chunks interleaved
**Dependencies**: None
**Estimated Complexity**: Medium (2-3 hours)

---

### T007 [X]: Create Golden Test Vectors for AMF0 Encoding
**Description**: Generate binary golden test files for AMF0 types (Number, Boolean, String, Object, Null, Array) following contracts/amf0.md.
**Files**:
- `tests/golden/amf0_number_0.bin`
- `tests/golden/amf0_number_1_5.bin`
- `tests/golden/amf0_boolean_true.bin`
- `tests/golden/amf0_boolean_false.bin`
- `tests/golden/amf0_string_test.bin`
- `tests/golden/amf0_string_empty.bin`
- `tests/golden/amf0_object_simple.bin`
- `tests/golden/amf0_object_nested.bin`
- `tests/golden/amf0_null.bin`
- `tests/golden/amf0_array_strict.bin`
**Requirements**:
- Number: 0.0, 1.5 (IEEE 754 double, big-endian)
- Boolean: true (0x01 0x01), false (0x01 0x00)
- String: "test" (0x02 0x00 0x04 "test")
- Object: {"key": "value"}
- Nested object: {"a": {"b": 1.0}}
- Strict array: [1.0, 2.0, 3.0]
**Dependencies**: None
**Estimated Complexity**: Medium (2-3 hours)

---

### T008 [X]: Create Golden Test Vectors for Control Messages
**Description**: Generate binary golden test files for control messages (Set Chunk Size, Acknowledgement, Window Ack Size, Set Peer Bandwidth) following contracts/control.md.
**Files**:
- `tests/golden/control_set_chunk_size_4096.bin`
- `tests/golden/control_acknowledgement_1M.bin`
- `tests/golden/control_window_ack_size_2_5M.bin`
- `tests/golden/control_set_peer_bandwidth_dynamic.bin`
- `tests/golden/control_user_control_stream_begin.bin`
**Requirements**:
- Set Chunk Size: 4096 (0x00 0x00 0x10 0x00)
- Acknowledgement: 1,000,000 bytes
- Window Ack Size: 2,500,000 bytes
- Set Peer Bandwidth: 2,500,000 bytes, Dynamic (0x02)
- User Control Stream Begin: Stream ID=1
**Dependencies**: None
**Estimated Complexity**: Small (1-2 hours)

---

### T009 [X]: Create Integration Test: Handshake Flow
**Description**: Write integration test for complete handshake flow (client + server loopback) following contracts/handshake.md.
**Files**:
- `tests/integration/handshake_test.go`
**Requirements**:
- Test valid handshake: C0+C1 → S0+S1+S2 → C2 → Completed
- Test invalid version: C0=0x06 → connection rejected
- Test timeout: Truncated C1 → timeout after 5 seconds
- Use `net.Pipe()` for loopback testing
**Dependencies**: T005 (golden vectors)
**Test Scenarios**: 3 scenarios (valid, invalid version, timeout)
**Estimated Complexity**: Medium (3-4 hours)

---

### T010 [X]: Create Integration Test: Chunking Flow
**Description**: Write integration test for dechunking and chunking (reassemble multi-chunk messages) following contracts/chunking.md.
**Files**:
- `tests/integration/chunking_test.go`
**Requirements**:
- Test single chunk message (FMT 0, message fits in 128 bytes)
- Test multi-chunk message (FMT 0 + FMT 3, 384 bytes, chunk size 128)
- Test interleaved chunks (audio CSID=4 + video CSID=6)
- Test extended timestamp (timestamp >= 0xFFFFFF)
- Test Set Chunk Size message (change from 128 to 4096)
**Dependencies**: T006 (golden vectors)
**Test Scenarios**: 5 scenarios
**Estimated Complexity**: Medium (4-5 hours)

---

### T011 [X]: Create Integration Test: Command Flow
**Description**: Write integration test for RTMP command flow (connect → createStream → publish/play) following contracts/commands.md.
**Files**:
- `tests/integration/commands_test.go`
**Requirements**:
- Test connect command: client sends connect → server responds _result
- Test createStream: client sends createStream → server allocates stream ID
- Test publish flow: connect → createStream → publish → onStatus NetStream.Publish.Start
- Test play flow: connect → createStream → play → onStatus NetStream.Play.Start
**Dependencies**: T007 (AMF0 golden vectors)
**Test Scenarios**: 4 scenarios
**Estimated Complexity**: Large (6-8 hours)

---

### T012 [P] [X]: Create Integration Test: Quickstart Scenario
**Description**: Write integration test validating end-to-end quickstart scenario (FFmpeg publish + ffplay playback) following quickstart.md.
**Files**:
- `tests/integration/quickstart_test.go`
**Requirements**:
- Test server startup and listen on port 1935
- Test connection acceptance and handshake
- Test connect command processing
- Test createStream and publish command
- Test codec detection (H.264 AVC, AAC)
- Mock FFmpeg behavior (send handshake, connect, publish, audio/video messages)
- Assert server logs contain expected messages
**Dependencies**: All previous integration tests
**Test Scenarios**: 1 end-to-end scenario
**Estimated Complexity**: Large (8-10 hours)

---

## Phase 3.3: Core Implementation (ONLY after tests are failing)

### Handshake Layer

### T013 [P] [X]: Implement Handshake Data Structures
**Description**: Define handshake state machine types and constants following contracts/handshake.md.
**Files**:
- `internal/rtmp/handshake/types.go`
- `internal/rtmp/handshake/types_test.go`
**Requirements**:
- Constants: C0/S0 version (0x03), C1/S1/C2/S2 size (1536)
- State enum: Initial, RecvC0C1, SentS0S1S2, RecvC2, Completed
- Handshake struct: state, c1, s1, timestamps
- Unit tests: state transitions, validation
**Dependencies**: T002 (bufpool), T003 (logger), T004 (errors)
**Test Coverage**: >90%
**Estimated Complexity**: Small (1 hour)

---

### T014 [X]: Implement Server Handshake FSM
**Description**: Implement server-side handshake finite state machine following contracts/handshake.md.
**Files**:
- `internal/rtmp/handshake/server.go`
- `internal/rtmp/handshake/server_test.go`
**Requirements**:
- State transitions: Initial → RecvC0C1 → SentS0S1S2 → RecvC2 → Completed
- Read C0+C1 (1+1536 bytes) with `io.ReadFull` and 5-second deadline
- Validate C0 version == 0x03, reject if != 0x03
- Generate S1 random data (1528 bytes) using `crypto/rand`
- Echo C1 into S2 (byte-for-byte copy)
- Send S0+S1+S2 (1+1536+1536 bytes) atomically
- Read C2 (1536 bytes)
- Optional validation: C2 should echo S1 (log warning if mismatch)
- Golden tests: T005 (valid handshake, invalid version, truncated C1)
**Dependencies**: T005 (golden tests), T013 (types)
**Test Coverage**: >90%
**Estimated Complexity**: Medium (4-6 hours)

---

### T015 [P] [X]: Implement Client Handshake FSM
**Description**: Implement client-side handshake finite state machine following contracts/handshake.md.
**Files**:
- `internal/rtmp/handshake/client.go`
- `internal/rtmp/handshake/client_test.go`
**Requirements**:
- State transitions: Initial → SentC0C1 → RecvS0S1 → SentC2 → Completed
- Send C0+C1 (1+1536 bytes), C0=0x03
- Generate C1 random data (1528 bytes)
- Read S0+S1 (1+1536 bytes) with 5-second deadline
- Validate S0 version == 0x03
- Echo S1 into C2 (byte-for-byte copy)
- Send C2 (1536 bytes)
- Optional: Read S2 and validate echoes C1
- Golden tests: T005
**Dependencies**: T005 (golden tests), T013 (types)
**Test Coverage**: >90%
**Estimated Complexity**: Medium (3-5 hours)

---

### T016 [X]: Integrate Handshake into Connection
**Description**: Wire handshake FSM into connection lifecycle, called immediately after TCP accept/connect.
**Files**:
- `internal/rtmp/conn/conn.go` (update)
- `internal/rtmp/conn/conn_test.go` (update)
**Requirements**:
- After `net.Listener.Accept()`, call `handshake.ServerHandshake(conn)`
- Log handshake completion with duration
- On handshake error: close connection, log error with context
- Integration test: T009 (handshake flow)
**Dependencies**: T009 (integration test), T014 (server handshake), T015 (client handshake)
**Estimated Complexity**: Small (1-2 hours)


---

### Chunking Layer

### T017 [P] [X]: Implement Chunk Header Parsing
**Description**: Implement Basic Header and Message Header parsing for all FMT types (0-3) following contracts/chunking.md.
**Files**:
- `internal/rtmp/chunk/header.go`
- `internal/rtmp/chunk/header_test.go`
**Requirements**:
- Parse Basic Header: extract fmt (2 bits) and csid (6/14/22 bits)
- CSID encoding: 1 byte (csid 2-63), 2 bytes (64-319), 3 bytes (320-65599)
- Parse Message Header based on FMT:
  - FMT 0: 11 bytes (timestamp, msgLength, msgTypeID, msgStreamID)
  - FMT 1: 7 bytes (timestampDelta, msgLength, msgTypeID)
  - FMT 2: 3 bytes (timestampDelta)
  - FMT 3: 0 bytes (reuse all previous fields)
- Handle extended timestamp: if timestamp/delta == 0xFFFFFF, read 4 additional bytes
- Read msgStreamID as **little-endian** (RTMP quirk)
- Golden tests: T006 (chunk headers FMT 0-3, extended timestamp)
**Dependencies**: T006 (golden tests), T002 (bufpool)
**Test Coverage**: >95%
**Estimated Complexity**: Medium (5-6 hours)

---

### T018 [P] [X]: Implement Chunk Header Serialization
**Description**: Implement Basic Header and Message Header serialization (encoding) for all FMT types following contracts/chunking.md.
**Files**:
- `internal/rtmp/chunk/writer.go`
- `internal/rtmp/chunk/writer_test.go`
**Requirements**:
- Encode Basic Header: fmt + csid (1-3 bytes based on csid range)
- Encode Message Header based on FMT (0/3/7/11 bytes)
- Handle extended timestamp: write 0xFFFFFF in header, then 4-byte extended timestamp
- Write msgStreamID as **little-endian**
- Golden tests: T006 (verify encoded bytes match golden vectors)
**Dependencies**: T006 (golden tests), T002 (bufpool)
**Test Coverage**: >95%
**Estimated Complexity**: Medium (4-5 hours)

---

### T019 [X]: Implement ChunkStreamState Management
**Description**: Implement per-CSID state cache for header compression following data-model.md ChunkStreamState entity.
**Files**:
- `internal/rtmp/chunk/state.go`
- `internal/rtmp/chunk/state_test.go`
**Requirements**:
- Track per CSID: lastTimestamp, lastMsgLength, lastMsgTypeID, lastMsgStreamID
- Track partial message: buffer, bytesReceived
- Update state based on FMT:
  - FMT 0: update all fields, reset buffer
  - FMT 1: update timestamp (add delta), length, type; reuse stream ID
  - FMT 2: update timestamp (add delta); reuse length, type, stream ID
  - FMT 3: reuse all fields, continue accumulating buffer
- Check message complete: bytesReceived == lastMsgLength
- Extract complete message, reset buffer
- Unit tests: state transitions, buffer accumulation, message extraction
**Dependencies**: T017 (header parsing)
**Test Coverage**: >90%
**Estimated Complexity**: Medium (4-5 hours)

---

### T020 [X]: Implement Dechunker (Reader)
**Description**: Implement chunk reader that reassembles complete messages from interleaved chunks following contracts/chunking.md.
**Files**:
- `internal/rtmp/chunk/reader.go`
- `internal/rtmp/chunk/reader_test.go`
**Requirements**:
- Maintain map of ChunkStreamState (one per CSID)
- Read loop:
  1. Read Basic Header → extract fmt, csid
  2. Get/create ChunkStreamState for csid
  3. Read Message Header based on fmt → update state
  4. Read chunk data (min(chunkSize, msgLength - bytesReceived))
  5. Append to state buffer
  6. If message complete: yield Message, reset state
- Handle chunk size changes: respect new chunk size for subsequent reads
- Handle interleaved chunks: multiple CSIDs in flight simultaneously
- Golden tests: T006 (interleaved chunks, multi-chunk messages)
- Integration test: T010 (chunking flow)
**Dependencies**: T010 (integration test), T017 (header parsing), T019 (state management)
**Test Coverage**: >90%
**Estimated Complexity**: Large (6-8 hours)

---

### T021 [X]: Implement Chunker (Writer)
**Description**: Implement chunk writer that fragments messages into chunks following contracts/chunking.md.
**Files**:
- `internal/rtmp/chunk/writer.go` (update)
- `internal/rtmp/chunk/writer_test.go` (update)
**Requirements**:
- Fragment message payload based on chunkSize
- First chunk: FMT 0 (full header) + chunk data
- Continuation chunks: FMT 3 (no header) + chunk data
- Handle extended timestamp for all chunks if used in first chunk
- Write atomically per chunk (buffered writer recommended)
- Golden tests: T006 (verify multi-chunk encoding)
**Dependencies**: T010 (integration test), T018 (header serialization)
**Test Coverage**: >90%
**Estimated Complexity**: Medium (5-6 hours)

---

### Control Layer

### T022 [P] [X]: Implement Control Message Encoding
**Description**: Implement encoding functions for all control message types (1-6) following contracts/control.md.
**Files**:
- `internal/rtmp/control/encoder.go`
- `internal/rtmp/control/encoder_test.go`
**Requirements**:
- Set Chunk Size (type 1): 4-byte big-endian uint32
- Abort Message (type 2): 4-byte big-endian uint32 (CSID)
- Acknowledgement (type 3): 4-byte big-endian uint32 (sequence number)
- User Control Message (type 4): 2-byte event type + event-specific data
  - Event 0: Stream Begin (4-byte stream ID)
  - Event 6: Ping Request (4-byte timestamp)
  - Event 7: Ping Response (4-byte timestamp)
- Window Acknowledgement Size (type 5): 4-byte big-endian uint32
- Set Peer Bandwidth (type 6): 4-byte bandwidth + 1-byte limit type
- Return Message struct with csid=2, msid=0, payload
- Golden tests: T008 (control messages)
**Dependencies**: T008 (golden tests)
**Test Coverage**: >95%
**Estimated Complexity**: Medium (3-4 hours)

---

### T023 [P] [X]: Implement Control Message Decoding
**Description**: Implement decoding functions for all control message types following contracts/control.md.
**Files**:
- `internal/rtmp/control/decoder.go`
- `internal/rtmp/control/decoder_test.go`
**Requirements**:
- Parse control message payload based on type ID
- Set Chunk Size: extract uint32, validate bit 31 == 0, > 0
- Acknowledgement: extract uint32 sequence number
- User Control Message: parse event type, extract event-specific data
- Window Ack Size: extract uint32, validate > 0
- Set Peer Bandwidth: extract uint32 + uint8 limit type, validate limit type <= 2
- Return structured Go types (not raw bytes)
- Golden tests: T008
**Dependencies**: T008 (golden tests)
**Test Coverage**: >95%
**Estimated Complexity**: Medium (3-4 hours)

---

### T024 [X]: Implement Control Message Handlers
**Description**: Implement control message handlers that update connection state following contracts/control.md.
**Files**:
- `internal/rtmp/control/handler.go`
- `internal/rtmp/control/handler_test.go`
**Requirements**:
- Set Chunk Size handler: update `conn.readChunkSize`, log change
- Acknowledgement handler: track peer ACK for flow control (optional)
- User Control handler:
  - Stream Begin: log event
  - Ping Request: respond with Ping Response (echo timestamp)
- Window Ack Size handler: update `conn.windowAckSize`
- Set Peer Bandwidth handler: update `conn.peerBandwidth`, `conn.limitType`
- Integrate handlers into message dispatcher
- Unit tests: state updates, Ping Request → Ping Response
**Dependencies**: T022 (encoder), T023 (decoder)
**Test Coverage**: >85%
**Estimated Complexity**: Medium (4-5 hours)

---

### T025 [X]: Implement Control Burst Sequence
**Description**: Implement control burst (WAS, SPB, SCS) sent immediately after handshake following contracts/control.md.
**Files**:
- `internal/rtmp/conn/control_burst.go`
- `internal/rtmp/conn/control_burst_test.go`
**Requirements**:
- After handshake completion, server sends:
  1. Window Acknowledgement Size (2,500,000 bytes)
  2. Set Peer Bandwidth (2,500,000 bytes, Dynamic limit type 2)
  3. Set Chunk Size (4096 bytes)
- Send all three messages sequentially, non-blocking
- Log each control message sent
- Unit tests: verify correct sequence and payloads
**Dependencies**: T024 (control handlers)
**Estimated Complexity**: Small (2-3 hours)

---

### AMF0 Layer

### T026 [P] [X]: Implement AMF0 Number Encoding/Decoding
**Description**: Implement AMF0 Number type (0x00) encoding and decoding following contracts/amf0.md.
**Files**:
- `internal/rtmp/amf/number.go`
- `internal/rtmp/amf/number_test.go`
**Requirements**:
- Encode: write 0x00 marker + 8-byte IEEE 754 double (big-endian)
- Decode: read 0x00 marker + 8-byte IEEE 754 double
- Handle edge cases: 0.0, 1.0, 1.5, -1.0, NaN, Infinity
- Golden tests: T007 (amf0_number_0.bin, amf0_number_1_5.bin)
**Dependencies**: T007 (golden tests)
**Test Coverage**: >95%
**Estimated Complexity**: Small (1-2 hours)

---

### T027 [P] [X]: Implement AMF0 Boolean Encoding/Decoding
**Description**: Implement AMF0 Boolean type (0x01) encoding and decoding following contracts/amf0.md.
**Files**:
- `internal/rtmp/amf/boolean.go`
- `internal/rtmp/amf/boolean_test.go`
**Requirements**:
- Encode: write 0x01 marker + 0x01 (true) or 0x00 (false)
- Decode: read 0x01 marker + 1 byte (0x00=false, else true)
- Golden tests: T007 (amf0_boolean_true.bin, amf0_boolean_false.bin)
**Dependencies**: T007 (golden tests)
**Test Coverage**: >95%
**Estimated Complexity**: Small (30 min)

---

### T028 [P] [X]: Implement AMF0 String Encoding/Decoding
**Description**: Implement AMF0 String type (0x02) encoding and decoding following contracts/amf0.md.
**Files**:
- `internal/rtmp/amf/string.go`
- `internal/rtmp/amf/string_test.go`
**Requirements**:
- Encode: write 0x02 marker + 2-byte length (big-endian) + UTF-8 bytes
- Decode: read 0x02 marker + 2-byte length + UTF-8 bytes
- Handle edge cases: empty string, max length (65535 bytes), UTF-8 multibyte (世界)
- Golden tests: T007 (amf0_string_test.bin, amf0_string_empty.bin)
**Dependencies**: T007 (golden tests)
**Test Coverage**: >95%
**Estimated Complexity**: Small (1 hour)

---

### T029 [P] [X]: Implement AMF0 Null Encoding/Decoding
**Description**: Implement AMF0 Null type (0x05) encoding and decoding following contracts/amf0.md.
**Files**:
- `internal/rtmp/amf/null.go`
- `internal/rtmp/amf/null_test.go`
**Requirements**:
- Encode: write 0x05 marker (no payload)
- Decode: read 0x05 marker, return nil
- Golden tests: T007 (amf0_null.bin)
**Dependencies**: T007 (golden tests)
**Test Coverage**: >95%
**Estimated Complexity**: Small (15 min)

---

### T030: Implement AMF0 Object Encoding/Decoding
**Description**: Implement AMF0 Object type (0x03) encoding and decoding following contracts/amf0.md.
**Files**:
- `internal/rtmp/amf/object.go`
- `internal/rtmp/amf/object_test.go`
**Requirements**:
- Encode: write 0x03 marker + key-value pairs + 0x00 0x00 0x09 end marker
- Key: 2-byte length + UTF-8 (no marker)
- Value: AMF0 encoded (with marker), recursive call
- Decode: read 0x03 marker + loop until key length == 0 and marker == 0x09
- Handle nested objects recursively
- Golden tests: T007 (amf0_object_simple.bin, amf0_object_nested.bin)
**Dependencies**: T007 (golden tests), T026-T029 (primitive types)
**Test Coverage**: >90%
**Estimated Complexity**: Medium (3-4 hours)

---

### T031 [P]: Implement AMF0 Strict Array Encoding/Decoding
**Description**: Implement AMF0 Strict Array type (0x0A) encoding and decoding following contracts/amf0.md.
**Files**:
- `internal/rtmp/amf/array.go`
- `internal/rtmp/amf/array_test.go`
**Requirements**:
- Encode: write 0x0A marker + 4-byte count (big-endian) + values (with markers)
- Decode: read 0x0A marker + 4-byte count + loop count times
- Handle nested arrays recursively
- Golden tests: T007 (amf0_array_strict.bin)
**Dependencies**: T007 (golden tests), T026-T030 (all types)
**Test Coverage**: >90%
**Estimated Complexity**: Small (2 hours)

---

### T032: Implement AMF0 Generic Encoder/Decoder
**Description**: Implement generic AMF0 encoder/decoder that dispatches based on Go type or type marker following contracts/amf0.md.
**Files**:
- `internal/rtmp/amf/amf.go`
- `internal/rtmp/amf/amf_test.go`
**Requirements**:
- Encoder: switch on Go type (nil, float64, bool, string, map, []interface{})
- Decoder: read type marker, dispatch to type-specific decoder
- Handle all supported types (0x00, 0x01, 0x02, 0x03, 0x05, 0x0A)
- Reject unsupported types (0x06, 0x07, 0x0B+) with error
- Round-trip tests: encode → decode → verify equality
- Integration test: T011 (command flow uses AMF0)
**Dependencies**: T011 (integration test), T026-T031 (all types)
**Test Coverage**: >90%
**Estimated Complexity**: Medium (3-4 hours)

---

### RPC/Commands Layer

### T033 [P]: Implement Connect Command Parsing
**Description**: Implement `connect` command parsing following contracts/commands.md.
**Files**:
- `internal/rtmp/rpc/connect.go`
- `internal/rtmp/rpc/connect_test.go`
**Requirements**:
- Parse command message (type 20): ["connect", transactionID, commandObject]
- Extract fields: app, flashVer, tcUrl, objectEncoding
- Validate: app not empty, objectEncoding == 0 (AMF0 only)
- Return structured `ConnectCommand` type
- Unit tests: valid connect, missing app, AMF3 encoding (reject)
**Dependencies**: T032 (AMF0 generic)
**Test Coverage**: >90%
**Estimated Complexity**: Small (2 hours)

---

### T034 [P]: Implement Connect Response Builder
**Description**: Implement `_result` response builder for connect command following contracts/commands.md.
**Files**:
- `internal/rtmp/rpc/connect_response.go`
- `internal/rtmp/rpc/connect_response_test.go`
**Requirements**:
- Build `_result` command: ["_result", transactionID, properties, information]
- Properties: fmsVer, capabilities, mode
- Information: level="status", code="NetConnection.Connect.Success", description
- Encode as AMF0 command message (type 20)
- Unit tests: verify encoded message matches expected structure
**Dependencies**: T032 (AMF0 generic)
**Test Coverage**: >90%
**Estimated Complexity**: Small (2 hours)

---

### T035 [P]: Implement CreateStream Command Parsing
**Description**: Implement `createStream` command parsing following contracts/commands.md.
**Files**:
- `internal/rtmp/rpc/createstream.go`
- `internal/rtmp/rpc/createstream_test.go`
**Requirements**:
- Parse command: ["createStream", transactionID, null]
- Return `CreateStreamCommand` with transactionID
- Unit tests: valid createStream
**Dependencies**: T032 (AMF0 generic)
**Test Coverage**: >90%
**Estimated Complexity**: Small (1 hour)

---

### T036 [P]: Implement CreateStream Response Builder
**Description**: Implement `_result` response builder for createStream following contracts/commands.md.
**Files**:
- `internal/rtmp/rpc/createstream_response.go`
- `internal/rtmp/rpc/createstream_response_test.go`
**Requirements**:
- Build `_result` command: ["_result", transactionID, null, streamID]
- Allocate stream ID (simple counter, typically 1)
- Unit tests: verify encoded message
**Dependencies**: T032 (AMF0 generic)
**Test Coverage**: >90%
**Estimated Complexity**: Small (1 hour)

---

### T037: Implement Publish Command Parsing
**Description**: Implement `publish` command parsing following contracts/commands.md.
**Files**:
- `internal/rtmp/rpc/publish.go`
- `internal/rtmp/rpc/publish_test.go`
**Requirements**:
- Parse command: ["publish", 0, null, publishingName, publishingType]
- Extract: publishingName (stream key), publishingType ("live"|"record"|"append")
- Construct full stream key: "app/publishingName"
- Return `PublishCommand`
- Unit tests: valid publish, missing publishingName
**Dependencies**: T032 (AMF0 generic)
**Test Coverage**: >90%
**Estimated Complexity**: Small (2 hours)

---

### T038: Implement Play Command Parsing
**Description**: Implement `play` command parsing following contracts/commands.md.
**Files**:
- `internal/rtmp/rpc/play.go`
- `internal/rtmp/rpc/play_test.go`
**Requirements**:
- Parse command: ["play", 0, null, streamName, start, duration, reset]
- Extract: streamName (stream key), start (-2=live, -1=recorded, >=0=offset)
- Construct full stream key: "app/streamName"
- Return `PlayCommand`
- Unit tests: valid play, missing streamName
**Dependencies**: T032 (AMF0 generic)
**Test Coverage**: >90%
**Estimated Complexity**: Small (2 hours)

---

### T039 [P]: Implement OnStatus Message Builder
**Description**: Implement `onStatus` message builder for stream events following contracts/commands.md.
**Files**:
- `internal/rtmp/rpc/onstatus.go`
- `internal/rtmp/rpc/onstatus_test.go`
**Requirements**:
- Build `onStatus` command: ["onStatus", 0, null, infoObject]
- Info object: level, code, description, details (optional)
- Common codes: NetStream.Publish.Start, NetStream.Play.Start, NetStream.Play.StreamNotFound
- Encode as AMF0 command message (type 20), msid=streamID
- Unit tests: verify encoded messages for all status codes
**Dependencies**: T032 (AMF0 generic)
**Test Coverage**: >90%
**Estimated Complexity**: Small (2 hours)

---

### T040: Implement Command Message Dispatcher
**Description**: Implement command message dispatcher that routes commands to handlers following contracts/commands.md.
**Files**:
- `internal/rtmp/rpc/dispatcher.go`
- `internal/rtmp/rpc/dispatcher_test.go`
**Requirements**:
- Parse command name from message payload (first AMF0 value)
- Route to handler based on command name:
  - "connect" → connect handler
  - "createStream" → createStream handler
  - "publish" → publish handler
  - "play" → play handler
  - "deleteStream" → deleteStream handler
  - Unknown commands → log warning, optionally respond with _error
- Unit tests: dispatch to correct handler, unknown command handling
- Integration test: T011 (command flow)
**Dependencies**: T011 (integration test), T033-T039 (command parsers/builders)
**Estimated Complexity**: Medium (3-4 hours)

---

### Media Layer

### T041 [P]: Implement Audio Message Parsing
**Description**: Implement audio message (type 8) parsing for codec detection following contracts/media.md.
**Files**:
- `internal/rtmp/media/audio.go`
- `internal/rtmp/media/audio_test.go`
**Requirements**:
- Parse audio tag header (byte 0): extract soundFormat (bits 7-4)
- Codec ID mapping: 2=MP3, 10=AAC, 11=Speex
- For AAC: parse AACPacketType (byte 1): 0x00=Sequence Header, 0x01=Raw
- Return `AudioMessage` struct: codec, packetType, payload
- No frame parsing or validation (transparent relay)
- Unit tests: AAC sequence header, AAC raw frame, MP3 frame
**Dependencies**: None
**Test Coverage**: >90%
**Estimated Complexity**: Small (2-3 hours)

---

### T042 [P]: Implement Video Message Parsing
**Description**: Implement video message (type 9) parsing for codec detection following contracts/media.md.
**Files**:
- `internal/rtmp/media/video.go`
- `internal/rtmp/media/video_test.go`
**Requirements**:
- Parse video tag header (byte 0): extract frameType (bits 7-4), codecID (bits 3-0)
- Codec ID mapping: 7=AVC (H.264), 12=HEVC (H.265)
- Frame type: 1=Keyframe, 2=Inter frame
- For AVC: parse AVCPacketType (byte 1): 0x00=Sequence Header, 0x01=NALU
- Return `VideoMessage` struct: codec, frameType, packetType, payload
- No frame parsing or validation (transparent relay)
- Unit tests: AVC sequence header, AVC keyframe, AVC inter frame
**Dependencies**: None
**Test Coverage**: >90%
**Estimated Complexity**: Small (2-3 hours)

---

### T043: Implement Codec Detection Logger
**Description**: Implement codec detection logger that extracts and logs codec info from first audio/video messages following contracts/media.md.
**Files**:
- `internal/rtmp/media/codec_detector.go`
- `internal/rtmp/media/codec_detector_test.go`
**Requirements**:
- Detect video codec from first video message (type 9)
- Detect audio codec from first audio message (type 8)
- Log codec info: stream_key, videoCodec, audioCodec
- Store codec info in Stream entity (data-model.md)
- Unit tests: detect H.264 + AAC, detect MP3 only
**Dependencies**: T041 (audio parsing), T042 (video parsing)
**Estimated Complexity**: Small (1-2 hours)

---

### T044: Implement Media Message Relay
**Description**: Implement media message broadcast from publisher to all subscribers following data-model.md Stream entity.
**Files**:
- `internal/rtmp/media/relay.go`
- `internal/rtmp/media/relay_test.go`
**Requirements**:
- On publisher sends audio/video message: call `Stream.BroadcastMessage(msg)`
- Detect codecs on first frame: call codec detector
- Loop over all subscribers: call `subscriber.SendMessage(msg)`
- Handle slow subscribers: drop message if outbound queue full (backpressure)
- Unit tests: relay to 1 subscriber, relay to 3 subscribers, slow subscriber handling
**Dependencies**: T043 (codec detector)
**Estimated Complexity**: Medium (3-4 hours)

---

### T045 [P]: Implement FLV Recorder
**Description**: Implement optional FLV file recorder for streams following data-model.md Recorder entity.
**Files**:
- `internal/rtmp/media/recorder.go`
- `internal/rtmp/media/recorder_test.go`
**Requirements**:
- Write FLV header (13 bytes): signature "FLV", version, flags, header length
- Write FLV tags for audio (type 0x08) and video (type 0x09) messages
- Tag structure: type, data size, timestamp (3+1 bytes split), stream ID, data, previous tag size
- Gracefully handle write errors: log error, close file, set recorder to nil, continue live stream
- Unit tests: write header, write audio tag, write video tag, disk full simulation
**Dependencies**: None
**Test Coverage**: >85%
**Estimated Complexity**: Medium (4-5 hours)

---

## Phase 3.4: Integration - Server & Stream Management

### T046: Implement Connection Entity
**Description**: Implement Connection entity with readLoop and writeLoop goroutines following data-model.md Connection entity.
**Files**:
- `internal/rtmp/conn/conn.go`
- `internal/rtmp/conn/conn_test.go`
**Requirements**:
- Fields: id, remoteAddr, conn, ctx, cancel, readChunkSize, writeChunkSize, windowAckSize, chunkStreams, outboundQueue, session
- ReadLoop goroutine: dechunk messages, dispatch to handlers
- WriteLoop goroutine: consume outboundQueue, chunk and send messages
- SendMessage(msg): enqueue to outboundQueue with timeout (backpressure)
- Close(): cancel context, close TCP, wait for goroutines
- Unit tests: readLoop message dispatch, writeLoop chunking, graceful close
**Dependencies**: T020 (dechunker), T021 (chunker)
**Test Coverage**: >85%
**Estimated Complexity**: Large (8-10 hours)

---

### T047: Implement Session Entity
**Description**: Implement Session entity for RTMP session state following data-model.md Session entity.
**Files**:
- `internal/rtmp/conn/session.go`
- `internal/rtmp/conn/session_test.go`
**Requirements**:
- Fields: app, tcUrl, flashVer, objectEncoding, transactionID, streamID, streamKey
- Methods: NextTransactionID(), AllocateStreamID(), SetStreamKey(app, streamName)
- State transitions: Uninitialized → Connected → StreamCreated → Publishing/Playing
- Unit tests: transaction ID increment, stream ID allocation, stream key construction
**Dependencies**: None
**Test Coverage**: >90%
**Estimated Complexity**: Small (2-3 hours)

---

### T048: Implement Stream Registry
**Description**: Implement server-side stream registry for managing active streams following data-model.md Stream entity.
**Files**:
- `internal/rtmp/server/registry.go`
- `internal/rtmp/server/registry_test.go`
**Requirements**:
- Map: stream key → Stream
- Methods: CreateStream(key), GetStream(key), DeleteStream(key)
- Thread-safe: use `sync.RWMutex`
- Stream entity: key, publisher, subscribers, metadata, videoCodec, audioCodec, startTime, recorder
- Unit tests: create stream, add publisher, add subscribers, delete stream
**Dependencies**: T047 (session), T044 (media relay), T045 (recorder)
**Test Coverage**: >90%
**Estimated Complexity**: Medium (4-5 hours)

---

### T049: Implement Publish Handler
**Description**: Implement publish command handler that registers publisher in stream registry following contracts/commands.md.
**Files**:
- `internal/rtmp/server/publish_handler.go`
- `internal/rtmp/server/publish_handler_test.go`
**Requirements**:
- Parse publish command: extract stream key
- Look up or create Stream in registry
- Check: only one publisher per stream (reject if already exists)
- Set stream.publisher = conn
- Send onStatus NetStream.Publish.Start
- On publisher disconnect: remove publisher, send Stream EOF to subscribers
- Unit tests: successful publish, duplicate publisher rejection, publisher disconnect
**Dependencies**: T037 (publish command), T039 (onStatus), T048 (registry)
**Estimated Complexity**: Medium (4-5 hours)

---

### T050: Implement Play Handler
**Description**: Implement play command handler that subscribes player to stream following contracts/commands.md.
**Files**:
- `internal/rtmp/server/play_handler.go`
- `internal/rtmp/server/play_handler_test.go`
**Requirements**:
- Parse play command: extract stream key
- Look up Stream in registry
- If stream not found or no publisher: send onStatus NetStream.Play.StreamNotFound
- Add subscriber to stream.subscribers list
- Send User Control Stream Begin (event 0)
- Send onStatus NetStream.Play.Start
- Start receiving media messages from publisher
- On subscriber disconnect: remove from stream.subscribers
- Unit tests: successful play, stream not found, subscriber disconnect
**Dependencies**: T038 (play command), T039 (onStatus), T048 (registry), T024 (control handler)
**Estimated Complexity**: Medium (5-6 hours)

---

### T051: Implement RTMP Server Listener
**Description**: Implement RTMP server TCP listener and connection manager following plan.md server structure.
**Files**:
- `internal/rtmp/server/server.go`
- `internal/rtmp/server/server_test.go`
**Requirements**:
- Listen on TCP port (default 1935)
- Accept connections: spawn goroutine per connection
- Handshake → Control Burst → Command Processing → Streaming
- Track active connections: map conn_id → Connection
- Graceful shutdown: cancel all connections, wait for goroutines
- Configuration: listenAddr, chunkSize, windowAckSize, recordAll, recordDir, logLevel
- Unit tests: server start/stop, accept connection, graceful shutdown
**Dependencies**: T016 (handshake), T025 (control burst), T040 (dispatcher), T046 (connection), T048 (registry)
**Test Coverage**: >80%
**Estimated Complexity**: Large (6-8 hours)

---

### T052: Implement RTMP Client (for testing)
**Description**: Implement simple RTMP client for integration testing following plan.md client structure.
**Files**:
- `internal/rtmp/client/client.go`
- `internal/rtmp/client/client_test.go`
**Requirements**:
- Connect to server: handshake, connect command, createStream command
- Publish mode: send publish command, send audio/video messages
- Play mode: send play command, receive audio/video messages
- CLI interface: `rtmp-client publish rtmp://host/app/stream file.flv`
- Unit tests: connect flow, publish flow, play flow
**Dependencies**: T015 (client handshake), T033-T038 (command parsers)
**Estimated Complexity**: Medium (5-6 hours)

---

## Phase 3.5: Polish & Validation

### T053 [P]: Implement Server CLI
**Description**: Implement command-line interface for RTMP server following quickstart.md.
**Files**:
- `cmd/rtmp-server/main.go`
- `cmd/rtmp-server/flags.go`
**Requirements**:
- Flags: -listen (default ":1935"), -log-level (default "info"), -record-all (default false), -record-dir (default "recordings"), -chunk-size (default 4096)
- Version flag: -version
- Graceful shutdown on SIGINT/SIGTERM
- Example: `rtmp-server.exe -listen :1935 -log-level debug -record-all`
**Dependencies**: T051 (server)
**Estimated Complexity**: Small (2-3 hours)

---

### T054: FFmpeg Interoperability Test
**Description**: Validate FFmpeg publish and ffplay playback following quickstart.md integration test scenario.
**Files**:
- `tests/interop/ffmpeg_test.sh` (or .ps1 for Windows)
- `tests/interop/README.md`
**Requirements**:
- Test 1: Start server, FFmpeg publish test.mp4, verify no errors
- Test 2: Start server, FFmpeg publish, ffplay playback, verify video plays
- Test 3: Concurrent publishers (2 streams), concurrent players (2 streams)
- Test 4: Recording enabled, verify FLV file playable
- Automated test script (requires FFmpeg/ffplay installed)
- Manual test instructions for developers
**Dependencies**: T012 (quickstart integration test), T051 (server), T053 (CLI)
**Validation**: All tests pass
**Estimated Complexity**: Medium (4-5 hours)

---

## Dependencies Graph

```
Setup (T001-T004) [parallel]
    ↓
Golden Tests (T005-T008) [parallel]
    ↓
Integration Tests Skeleton (T009-T012) [parallel]
    ↓
Handshake (T013-T016)
    ↓
Chunking (T017-T021) [T017,T018,T019 parallel; then T020,T021]
    ↓
Control (T022-T025) [T022,T023 parallel; then T024,T025]
    ↓
AMF0 (T026-T032) [T026-T029,T031 parallel; then T030; then T032]
    ↓
RPC (T033-T040) [T033-T039 parallel; then T040]
    ↓
Media (T041-T045) [T041,T042,T045 parallel; then T043,T044]
    ↓
Integration (T046-T052) [T046,T047 parallel; then T048-T052]
    ↓
Polish (T053-T054) [T053 then T054]
```

## Parallel Execution Examples

### Phase 3.1: Setup (All parallel)
```powershell
# Launch T002-T004 together:
Task: "Implement Buffer Pool in internal/bufpool/pool.go"
Task: "Implement Structured Logger in internal/logger/logger.go"
Task: "Define Domain-Specific Errors in internal/errors/errors.go"
```

### Phase 3.2: Golden Tests (All parallel)
```powershell
# Launch T005-T008 together:
Task: "Create Golden Test Vectors for Handshake in tests/golden/handshake_*.bin"
Task: "Create Golden Test Vectors for Chunk Headers in tests/golden/chunk_*.bin"
Task: "Create Golden Test Vectors for AMF0 Encoding in tests/golden/amf0_*.bin"
Task: "Create Golden Test Vectors for Control Messages in tests/golden/control_*.bin"
```

### Phase 3.3: AMF0 Primitives (Parallel)
```powershell
# Launch T026-T029, T031 together:
Task: "Implement AMF0 Number Encoding/Decoding in internal/rtmp/amf/number.go"
Task: "Implement AMF0 Boolean Encoding/Decoding in internal/rtmp/amf/boolean.go"
Task: "Implement AMF0 String Encoding/Decoding in internal/rtmp/amf/string.go"
Task: "Implement AMF0 Null Encoding/Decoding in internal/rtmp/amf/null.go"
Task: "Implement AMF0 Strict Array Encoding/Decoding in internal/rtmp/amf/array.go"
```

---

## Notes

- **[P] tasks**: Different files, no dependencies, safe to run in parallel
- **TDD**: All tests (T005-T012) written before implementation (T013+)
- **Commit strategy**: Commit after each task completion
- **Integration test validation**: Run integration tests after each layer (handshake, chunking, control, etc.)
- **FFmpeg interop**: Final validation before feature completion

## Validation Checklist

*GATE: Checked before returning*

- [x] All contracts have corresponding tests (handshake, chunking, control, AMF0, commands, media)
- [x] All entities have implementation tasks (Connection, Session, Stream, Message, ChunkStreamState, Recorder)
- [x] All tests come before implementation (Phase 3.2 before 3.3)
- [x] Parallel tasks truly independent (marked [P])
- [x] Each task specifies exact file path
- [x] No task modifies same file as another [P] task
- [x] Protocol layer ordering correct: handshake → chunking → control → AMF0 → RPC → media
- [x] Dependencies explicitly listed per task
- [x] Quickstart scenario covered (T012, T054)

---

**Status**: Tasks complete. 54 tasks ready for execution. Proceed with Phase 3.1 (Setup & Foundation).

# GitHub Copilot Instructions for go-rtmp

## Current Implementation Focus (2025-10-01)

**Feature**: RTMP Server Implementation (branch `001-rtmp-server-implementation`)  
**Phase**: Implementation (plan.md and contracts complete)  
**Status**: Ready for task generation and implementation

### Priority Tasks
1. **Handshake FSM** (internal/rtmp/handshake): Simple RTMP v3 handshake state machine (C0/C1/C2 ↔ S0/S1/S2)
2. **Chunking** (internal/rtmp/chunk): Chunk header parsing/serialization, dechunker (reader), chunker (writer)
3. **AMF0 Codec** (internal/rtmp/amf): Encoder/decoder for Number, Boolean, String, Object, Null, Array types
4. **Control Messages** (internal/rtmp/control): Set Chunk Size, Window Ack Size, Set Peer Bandwidth, Acknowledgement

### Key References
- **Specification**: `specs/001-rtmp-server-implementation/spec.md`
- **Implementation Plan**: `specs/001-rtmp-server-implementation/plan.md`
- **Contracts**: `specs/001-rtmp-server-implementation/contracts/*.md`
- **Data Model**: `specs/001-rtmp-server-implementation/data-model.md`
- **Quickstart**: `specs/001-rtmp-server-implementation/quickstart.md`

### Test Requirements
- Golden test vectors for handshake, chunk headers (FMT 0-3), AMF0 encoding/decoding
- Integration tests for handshake → connect → createStream → publish/play flows
- FFmpeg interoperability: `ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test`
- ffplay playback: `ffplay rtmp://localhost:1935/live/test`

### Constitutional Principles
1. **Protocol-First**: Wire format fidelity, byte-for-byte spec compliance, no protocol shortcuts
2. **Idiomatic Go**: Standard library only, simple/clear code, early returns, channels for concurrency
3. **Modularity**: Package structure mirrors protocol layers (handshake → chunk → control → amf → rpc → media)
4. **Test-First**: Golden vectors, unit tests, integration tests, FFmpeg/OBS interop tests (>80% coverage)
5. **Concurrency Safety**: One readLoop + writeLoop goroutine per connection, context cancellation, bounded queues
6. **Observability**: Structured logging (log/slog), debug mode for protocol traces, error context
7. **Simplicity**: Simple handshake only, AMF0 only, no transcoding, YAGNI

### Code Generation Guidelines

When generating RTMP protocol code, always:

**Handshake (internal/rtmp/handshake)**:
- Version byte must be 0x03 (RTMP v3)
- C1/S1/S2/C2 exactly 1536 bytes: timestamp(4) + zero(4) + random(1528)
- Use `io.ReadFull` with 5-second deadlines
- S2 must echo C1 exactly, C2 must echo S1 exactly
- State machine: Initial → RecvC0C1 → SentS0S1S2 → RecvC2 → Completed

**Chunking (internal/rtmp/chunk)**:
- Basic Header: fmt(2 bits) + csid(6/14/22 bits) → 1-3 bytes
- Message Header: FMT 0(11 bytes), FMT 1(7 bytes), FMT 2(3 bytes), FMT 3(0 bytes)
- Extended Timestamp: 4 bytes when timestamp >= 0xFFFFFF
- Default chunk size: 128 bytes; support negotiation up to 65536
- MSID is little-endian (quirk), all other multi-byte fields big-endian

**Control Messages (internal/rtmp/control)**:
- All control messages (types 1-6) on CSID=2, MSID=0
- Set Chunk Size (type 1): 4-byte big-endian uint32
- Window Ack Size (type 5): 4-byte big-endian uint32
- Set Peer Bandwidth (type 6): 4-byte bandwidth + 1-byte limit type (0=Hard, 1=Soft, 2=Dynamic)
- Acknowledgement (type 3): 4-byte big-endian uint32 (total bytes received)

**AMF0 Encoding (internal/rtmp/amf)**:
- Type markers: 0x00=Number, 0x01=Boolean, 0x02=String, 0x03=Object, 0x05=Null, 0x0A=Array
- Number: 8-byte IEEE 754 double (big-endian)
- String: 2-byte length + UTF-8 bytes
- Object: key-value pairs + end marker (0x00 0x00 0x09)
- Go type mapping: float64, bool, string, map[string]interface{}, nil, []interface{}

**Error Handling**:
- Wrap errors with context: `fmt.Errorf("handshake C1: %w", err)`
- Check errors immediately after function calls
- Don't ignore errors (no bare `_` unless justified)
- Place error returns as last return value

**Testing**:
- Table-driven tests for parsers: `tests := []struct{name, input, expected, wantErr}`
- Golden vectors in `tests/golden/*.bin` files
- Use `testing.T` for unit tests, `testing.B` for benchmarks
- Mark test helpers with `t.Helper()`

**Logging**:
- Use `log/slog` for structured logging
- Levels: Debug (protocol details), Info (lifecycle), Warn (recoverable), Error (critical)
- Fields: conn_id, peer_addr, stream_key, msg_type, csid, msid, timestamp
- Example: `slog.Info("Handshake completed", "conn_id", connID, "duration_ms", duration.Milliseconds())`

**Concurrency**:
- One readLoop + writeLoop goroutine per connection
- Use `context.Context` for cancellation: `ctx, cancel := context.WithCancel(context.Background())`
- Bounded outbound queue: `outbound := make(chan *Message, 100)`
- Protect shared state with `sync.RWMutex`: stream registry (map[string]*Stream)

### Recent Changes (Keep Last 3)
1. **2025-10-01**: Created implementation plan with research, data model, contracts, quickstart for 001-rtmp-server-implementation
2. **2025-10-01**: Established constitution (v1.0.0) with 7 core principles: Protocol-First, Idiomatic Go, Modularity, Test-First, Concurrency Safety, Observability, Simplicity
3. **2025-10-01**: Defined feature specification with clarifications (codec handling, max connections, latency, authentication, recording)

---

*This file is auto-updated by `.specify/scripts/powershell/update-agent-context.ps1`. Manual additions between markers are preserved.*

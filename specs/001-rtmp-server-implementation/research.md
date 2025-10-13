# Research: RTMP Server Implementation

**Feature**: 001-rtmp-server-implementation  
**Date**: 2025-10-01  
**Phase**: 0 (Outline & Research)

## Overview

This document consolidates research findings and technology decisions for the RTMP server implementation. All clarifications from the feature specification (spec.md, Session 2025-10-01) have been resolved and incorporated into the technical approach.

---

## Clarifications Resolved

### 1. Codec Handling Strategy

**Decision**: Hybrid approach - codec-agnostic transparent relay with logging of detected codec information

**Rationale**:
- Maximizes interoperability: server accepts any codec without validation or transcoding
- Provides operational visibility: logs codec information extracted from FLV metadata headers
- Aligns with Constitution Principle VII (Simplicity): no complex codec parsing or validation logic
- Meets clarification requirement: "Server accepts any codec but logs/reports codec information for monitoring purposes"

**Implementation Details**:
- Server examines first byte of Audio (type 8) and Video (type 9) message payloads
- Audio CodecID extracted from bits 4-7: 2=MP3, 10=AAC, 11=Speex, etc.
- Video CodecID extracted from bits 0-3: 2=Sorenson H.263, 7=AVC (H.264), 12=HEVC, etc.
- Codec information logged when stream starts: "Stream live/test: H.264 AVC video, AAC audio"
- Payloads forwarded byte-for-byte to subscribers (transparent relay)

**Alternatives Considered**:
- Strict codec validation (H.264/AAC only): Rejected - reduces flexibility, not required for relay server
- Full codec parsing (decode SPS/PPS, audio config): Rejected - unnecessary complexity, out of scope
- No codec detection: Rejected - fails to meet monitoring requirement from clarification

---

### 2. Maximum Concurrent Connections

**Decision**: Target 10-50 simultaneous client connections

**Rationale**:
- Aligns with clarification: "10-50 connections (Small-scale development/testing environment)"
- Suitable for single-server deployment without clustering
- Conservative target enables focus on protocol correctness over horizontal scaling
- Memory budget: <10MB per connection × 50 = <500MB total (reasonable for development hardware)

**Implementation Details**:
- No hard connection limit enforced initially (rely on OS TCP limits)
- Graceful degradation: server accepts connections until system resources exhausted
- Future enhancement: configurable max connections with rejection policy

**Performance Implications**:
- One goroutine per connection (readLoop + writeLoop) = 100 goroutines @ 50 connections
- Bounded outbound queues (e.g., 100 messages × 50 connections × 10KB avg = ~50MB buffered)
- Context-based shutdown ensures clean goroutine termination

**Alternatives Considered**:
- High-scale target (1000+ connections): Deferred - requires advanced techniques (io_uring, epoll optimization)
- Very low scale (5-10 connections): Rejected - insufficient for realistic testing scenarios

---

### 3. Target Latency

**Decision**: 3-5 seconds end-to-end latency (publisher to player)

**Rationale**:
- Aligns with clarification: "3-5 seconds (Relaxed latency for easier buffering and stability)"
- Provides comfortable buffer window for network jitter and packet reordering
- Sufficient for typical live streaming use cases (not ultra-low-latency gaming/conferencing)
- Simplifies implementation: no aggressive optimization needed

**Latency Budget Breakdown**:
- Encoding (publisher): ~0-100ms (client responsibility)
- Network (publisher → server): ~10-100ms (WAN)
- Server processing: <50ms (handshake + chunk decode/encode)
- Buffering (server outbound queue): ~500-1000ms
- Network (server → player): ~10-100ms (WAN)
- Decoding/rendering (player): ~0-200ms (client responsibility)
- Jitter buffer (player): ~1-3 seconds
- **Total**: ~1.5-4.5 seconds (within 3-5 second target)

**Implementation Details**:
- Server does not introduce intentional delay beyond network I/O and goroutine scheduling
- Timestamps preserved from publisher to player (no timestamp rewriting)
- Backpressure handling: drop slow consumers rather than introducing server-side buffering delay

**Alternatives Considered**:
- Ultra-low latency (<1 second): Rejected - requires WebRTC/UDP, complex jitter buffer, out of scope
- High latency (>10 seconds): Rejected - unnecessary, degrades user experience

---

### 4. Authentication Mechanism

**Decision**: No authentication (accept all connections)

**Rationale**:
- Aligns with clarification: "No authentication (Accept all connections, suitable for development/testing in trusted networks)"
- Trusted network assumption: server deployed behind firewall or on local network
- Simplifies initial implementation: focus on protocol correctness
- Aligns with Constitution Principle VII (Simplicity and Incrementalism)

**Security Implications**:
- Any client can publish or play any stream
- No rate limiting or abuse prevention
- Not suitable for public internet deployment without additional safeguards (reverse proxy, firewall rules)

**Implementation Details**:
- connect command processing does not verify credentials
- publish and play commands succeed without authorization checks
- Future enhancement: token-based authentication (query parameter, custom RTMP command)

**Alternatives Considered**:
- Basic auth (username/password in connect command): Deferred - adds complexity, can be added later
- Token-based auth (signed URLs): Deferred - requires crypto, key management
- IP whitelist: Out of scope (network-layer concern, handled by firewall)

---

### 5. Stream Recording

**Decision**: Yes, optional recording to FLV format, configurable per-stream or globally

**Rationale**:
- Aligns with clarification: "Yes, optional (Recording can be enabled/disabled per stream or via configuration)"
- FLV format natural fit: RTMP messages are already FLV tag format
- Minimal transformation: add FLV header + tag headers + back pointers
- Enables DVR-style playback and content archival

**Implementation Details**:
- Configuration: global flag (`-record-all`) or per-stream config file
- FLV file structure:
  - Header: "FLV" signature (3 bytes) + version (1 byte) + flags (1 byte, audio+video) + header length (4 bytes, always 9) + previous tag size 0 (4 bytes)
  - Tags: For each audio/video message, write FLV tag (type, size, timestamp, stream ID) + payload + previous tag size
- File naming: `recordings/{app}_{streamname}_{timestamp}.flv`
- Error handling: Recording errors (disk full, permission denied) logged but do not interrupt live streaming
- Graceful close: Flush and close file when publisher disconnects

**Recording Flow**:
1. Publisher sends publish command with stream key "live/test"
2. Server checks configuration: recording enabled for "live/test"?
3. If yes: Open `recordings/live_test_20251001_120000.flv`, write FLV header
4. For each audio/video message: Write FLV tag to file
5. On publisher disconnect: Flush and close file, log "Recording saved: ..."

**Alternatives Considered**:
- MP4 container: Rejected - requires muxing, moov atom construction, more complexity
- Raw RTMP dump: Rejected - not playable without custom tooling
- No recording: Rejected - fails to meet clarification requirement

---

## Technology Stack Decisions

### Programming Language: Go 1.21+

**Decision**: Use Go with standard library only (no external dependencies for core protocol)

**Rationale**:
- Aligns with Constitution Principle II (Idiomatic Go): "Use standard library wherever possible; minimize external dependencies"
- Excellent concurrency primitives: goroutines, channels, context, sync package
- Strong networking support: net.Conn, io.ReadFull, binary.BigEndian
- Single binary deployment: no runtime dependencies
- Cross-platform: same codebase for Linux/macOS/Windows

**Standard Library Packages Used**:
- `net`: TCP listener, connection handling
- `io`: Buffered reading (io.ReadFull), byte counting
- `encoding/binary`: Big-endian and little-endian encoding (MSID is little-endian)
- `context`: Cancellation, timeouts, per-connection lifecycle
- `sync`: Mutex, RWMutex, WaitGroup for concurrency safety
- `log/slog`: Structured logging (available since Go 1.21)
- `os`: File I/O for recording
- `time`: Timestamps, deadlines

**Alternatives Considered**:
- Rust: Rejected - steeper learning curve, less ecosystem maturity for RTMP tooling
- Python: Rejected - GIL limits concurrency, not suitable for 50+ concurrent connections
- C/C++: Rejected - memory safety burden, slower development
- External RTMP libraries (e.g., github.com/nareix/joy4): Rejected - violates from-scratch implementation requirement, hides protocol details

---

### AMF Encoding: AMF0 Only

**Decision**: Implement AMF0 encoder/decoder; do not support AMF3

**Rationale**:
- AMF0 sufficient for core RTMP commands (connect, createStream, publish, play)
- FFmpeg, OBS, and most RTMP clients default to AMF0 (objectEncoding=0)
- Simpler type system: Number (float64), Boolean, String, Object (map), Null, Array (indexed/associative)
- Aligns with Constitution Principle VII (Simplicity): "Support AMF0 before AMF3 (AMF0 is sufficient for most use cases)"

**AMF0 Types to Implement**:
- 0x00 Number: 8-byte IEEE 754 double (big-endian)
- 0x01 Boolean: 1 byte (0x00=false, 0x01=true)
- 0x02 String: 2-byte length + UTF-8 bytes
- 0x03 Object: key-value pairs + end marker (0x00 0x00 0x09)
- 0x05 Null: no payload
- 0x08 ECMA Array: 4-byte count + key-value pairs + end marker (treated as Object in Go)
- 0x0A Strict Array: 4-byte count + values (no keys)

**Not Implementing** (out of scope):
- 0x04 MovieClip (deprecated)
- 0x06 Undefined (edge case)
- 0x07 Reference (complex, rarely used)
- 0x09 Object End Marker (handled as special case in Object parsing)
- 0x0B Date (can be represented as Number timestamp)
- 0x0C Long String (AMF3 feature)
- AMF3 types (0x11): Deferred to future iteration if needed

**Go Type Mapping**:
- AMF0 Number ↔ float64
- AMF0 Boolean ↔ bool
- AMF0 String ↔ string
- AMF0 Object ↔ map[string]interface{}
- AMF0 Null ↔ nil
- AMF0 Strict Array ↔ []interface{}

**Alternatives Considered**:
- AMF3 support: Deferred - added complexity, minimal benefit (most clients use AMF0)
- External AMF library: Rejected - AMF0 subset simple enough to implement, educational value

---

### Handshake: Simple (RTMP v3) Only

**Decision**: Implement simple handshake; do not implement complex handshake

**Rationale**:
- Simple handshake documented in public Adobe RTMP specification
- Supported by all modern RTMP clients (FFmpeg, OBS, Wirecast, vlc)
- Complex handshake (HMAC-SHA256 + Diffie-Hellman) is legacy Flash Player requirement
- Aligns with Constitution Principle VII: "Implement 'simple' handshake first; complex handshake is optional"
- Out of scope per feature spec: "Complex handshake with cryptographic validation" explicitly excluded

**Simple Handshake Sequence**:
1. Client → Server: C0 (version 0x03) + C1 (1536 bytes: timestamp + zero + random)
2. Server → Client: S0 (version 0x03) + S1 (1536 bytes) + S2 (1536 bytes echoing C1)
3. Client → Server: C2 (1536 bytes echoing S1)

**Implementation Details**:
- Use `io.ReadFull` with 5-second deadline for each read
- Verify C0 version byte == 0x03, reject otherwise
- S1 random data can be freshly generated or zero-filled (spec allows both)
- S2 must echo C1's timestamp + zero + random fields exactly
- State machine: Initial → RecvC0C1 → SentS0S1S2 → RecvC2 → Completed

**Alternatives Considered**:
- Complex handshake: Deferred - not required for target clients, can be added later if needed
- RTMPE/RTMPS: Out of scope per feature spec

---

### Concurrency Model: Goroutines per Connection

**Decision**: One readLoop goroutine + one writeLoop goroutine per connection

**Rationale**:
- Aligns with Constitution Principle V: "One goroutine per connection (readLoop + writeLoop)"
- Go's goroutines are lightweight (2KB initial stack, grow/shrink as needed)
- 50 connections × 2 goroutines = 100 goroutines (trivial for Go runtime)
- Clean separation: readLoop decodes chunks → dispatcher, writeLoop consumes outbound queue → encodes chunks

**Lifecycle Management**:
- Context per connection: cancel context on disconnect propagates to both goroutines
- WaitGroup to ensure both goroutines complete before connection cleanup
- Bounded outbound queue (e.g., 100 messages): if full, drop messages or disconnect slow consumer

**Shared State**:
- Stream registry (map[string]*Stream) protected by RWMutex
- Per-connection chunk stream state (map[uint32]*ChunkStreamState) accessed only by readLoop (no mutex needed)

**Alternatives Considered**:
- Single-threaded event loop (libuv style): Rejected - Go's goroutines are idiomatic, easier to reason about
- Thread pool: Rejected - goroutines more efficient than OS threads in Go
- Async/await: Rejected - Go's approach is goroutines + channels (no async/await syntax)

---

### Logging: Structured with log/slog

**Decision**: Use Go 1.21+ standard library `log/slog` for structured logging

**Rationale**:
- Constitution Principle VI: "Structured logging at appropriate levels (debug for protocol details, info for lifecycle events, warn/error for issues)"
- No external dependency (available in stdlib since Go 1.21)
- Structured fields: connection_id, peer_addr, stream_key, msg_type, csid, msid, timestamp
- Configurable log levels: debug, info, warn, error

**Log Levels**:
- **Debug**: Protocol-level details (chunk headers, message types, raw bytes in hex)
- **Info**: Lifecycle events (connection accepted, handshake completed, stream started/stopped)
- **Warn**: Recoverable errors (malformed message, slow consumer dropped)
- **Error**: Critical errors (network I/O failure, panic recovery)

**Example Log Entries**:
```
{"time":"2025-10-01T12:00:00Z","level":"INFO","msg":"Connection accepted","conn_id":"c1a2b3c4","peer_addr":"192.168.1.100:54321"}
{"time":"2025-10-01T12:00:01Z","level":"INFO","msg":"Handshake completed","conn_id":"c1a2b3c4"}
{"time":"2025-10-01T12:00:02Z","level":"INFO","msg":"Stream started","conn_id":"c1a2b3c4","stream_key":"live/test","role":"publisher"}
{"time":"2025-10-01T12:00:03Z","level":"INFO","msg":"Codec detected","stream_key":"live/test","video":"H.264 AVC","audio":"AAC"}
{"time":"2025-10-01T12:00:04Z","level":"DEBUG","msg":"Message received","conn_id":"c1a2b3c4","csid":6,"type":9,"length":1024,"timestamp":12345}
```

**Alternatives Considered**:
- External libraries (logrus, zap, zerolog): Deferred - stdlib sufficient for initial implementation
- Plain text logs: Rejected - harder to parse for monitoring/alerting
- No logging: Rejected - violates Constitution Principle VI

---

### Testing Strategy: Golden Vectors + FFmpeg Interop

**Decision**: Implement three-tier testing approach

**Rationale**:
- Constitution Principle IV: "All protocol-level code must be validated against golden test vectors... Interop tests with FFmpeg/OBS"
- Protocol compliance requires byte-level correctness (golden vectors)
- Real-world compatibility requires testing with industry-standard tools

**Tier 1: Unit Tests with Golden Vectors**:
- Handshake: C0+C1 byte sequences → S0+S1+S2 response
- Chunk headers: FMT 0-3, CSID encoding, extended timestamp
- AMF0 encoding: Number, Boolean, String, Object, Null, Array
- Control messages: Set Chunk Size, WAS, SPB, Acknowledgement, User Control

**Tier 2: Integration Tests**:
- Loopback: Client connects to server, handshake → connect → createStream → publish/play
- Synthetic media: Generate fake audio/video messages, verify relay to subscribers
- Error scenarios: Truncated handshake, malformed chunks, out-of-order messages

**Tier 3: Interoperability Tests**:
- FFmpeg publish: `ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test`
- FFplay playback: `ffplay rtmp://localhost:1935/live/test`
- OBS Studio publish: Configure OBS with RTMP URL, verify stream ingestion
- Multi-client: Multiple publishers + players simultaneously

**Test Data**:
- Golden vectors stored in `tests/golden/` as .bin files (raw bytes)
- Sample media: Small test.mp4 file (~10 seconds, H.264+AAC)
- Automated: CI/CD runs unit + integration tests on every commit
- Manual: Interop tests run before releases

**Alternatives Considered**:
- Only unit tests: Rejected - insufficient for protocol validation
- Only integration tests: Rejected - harder to debug, slower feedback
- No real-world testing: Rejected - risk of spec misinterpretation

---

## Integration Patterns

### FFmpeg Interoperability

**Publish Flow** (FFmpeg → Server):
1. FFmpeg sends C0+C1 handshake
2. Server responds S0+S1+S2
3. FFmpeg sends C2
4. FFmpeg sends connect command: `["connect", 1, {app: "live", flashVer: "FMLE/3.0", tcUrl: "rtmp://localhost:1935/live", ...}]`
5. Server responds _result with connect success
6. FFmpeg sends releaseStream (optional, can be no-op response)
7. FFmpeg sends FCPublish (optional, can be no-op response)
8. FFmpeg sends createStream: `["createStream", 2, null]`
9. Server responds _result with stream ID: `["_result", 2, null, 1.0]`
10. FFmpeg sends publish: `["publish", 0, null, "test", "live"]` on MSID=1
11. Server responds onStatus NetStream.Publish.Start
12. FFmpeg sends @setDataFrame metadata (AMF0 data message) with video/audio info
13. FFmpeg sends Audio (type 8) and Video (type 9) messages interleaved
14. Server relays to subscribers

**Play Flow** (ffplay ← Server):
1. ffplay sends handshake (similar to FFmpeg)
2. ffplay sends connect → createStream → play: `["play", 0, null, "test", -2, -1, true]`
3. Server responds User Control StreamBegin + onStatus NetStream.Play.Start
4. Server sends Audio/Video messages relayed from publisher
5. ffplay decodes and renders

**Key Compatibility Notes**:
- FFmpeg expects transaction IDs to increment: connect=1, createStream=2, etc.
- releaseStream and FCPublish can be responded with simple _result or _error (Flash-era commands)
- @setDataFrame message contains codec metadata (width, height, framerate, videocodecid, audiocodecid)
- Server must handle both "live" and "record" publish types (same behavior for relay)

---

### OBS Studio Interoperability

**Differences from FFmpeg**:
- OBS sends releaseStream before connect (unusual ordering)
- OBS sends FCPublish/FCUnpublish (Flash Media Live Encoder commands)
- Otherwise similar command flow

**Solution**:
- Implement minimal handlers for releaseStream, FCPublish, FCUnpublish:
  - Parse command name and stream name
  - Respond with `["_result", transactionID, null]` or `["_error", transactionID, null, {code: "NetConnection.Call.Failed"}]`
  - Log but do not enforce ordering

---

## Performance Considerations

### Memory Management

**Buffer Pooling** (internal/bufpool):
- Allocate buffers from pool for chunk reassembly (reduce GC pressure)
- Pool sizes: 128 bytes (default chunk), 4096 bytes (large chunk), 65536 bytes (max chunk)
- Return buffers to pool after message processed

**Per-Connection Memory Budget**:
- Chunk stream state: ~1KB per CSID × 10 CSIDs = 10KB
- Outbound queue: 100 messages × 1KB avg = 100KB
- Read buffer: 65KB (max chunk size)
- Write buffer: 65KB
- **Total**: ~240KB per connection (conservative)
- **50 connections**: ~12MB (well under target)

**Stream Registry Memory**:
- Stream metadata: ~1KB per stream
- Subscriber list: 8 bytes per pointer × 10 subscribers = 80 bytes
- **100 streams × 10 subscribers**: ~100MB (reasonable)

**Alternatives Considered**:
- No pooling: Rejected - higher GC pressure, allocation overhead
- Pre-allocate all buffers: Rejected - wastes memory for idle connections

### Chunk Size Optimization

**Default**: 128 bytes (per RTMP spec)  
**Optimized**: 4096 bytes (recommended)

**Rationale**:
- Smaller chunks = more overhead (basic header + message header per chunk)
- Larger chunks = better throughput (fewer headers, better CPU cache utilization)
- 4096 bytes common in production (FFmpeg default, Wowza, nginx-rtmp)

**Negotiation**:
- Server sends Set Chunk Size (4096) immediately after handshake
- Client may send Set Chunk Size; server honors it for reading

**Trade-off**:
- Larger chunks increase latency (must wait for full chunk before forwarding)
- For 3-5 second target latency, 4096-byte chunks add ~0.5-1ms (negligible)

---

## Risk Mitigation

### Protocol Compliance Risks

**Risk**: Misinterpreting RTMP specification → incompatibility with clients  
**Mitigation**:
- Golden test vectors derived from Wireshark captures of FFmpeg/OBS sessions
- Byte-level comparison in tests (expected vs. actual)
- Interop tests with multiple clients (FFmpeg, OBS, ffplay) before release

**Risk**: Edge cases not covered by spec (e.g., extended timestamp handling)  
**Mitigation**:
- Research existing open-source implementations (nginx-rtmp, red5) for reference
- Document assumptions and trade-offs in code comments
- Fuzz testing for parsers (chunk headers, AMF0)

### Concurrency Risks

**Risk**: Race conditions on shared state (stream registry, connection state)  
**Mitigation**:
- RWMutex for stream registry (read-heavy workload)
- Per-connection state accessed only by owning goroutine
- Go race detector (`go test -race`) in CI

**Risk**: Goroutine leaks on error paths  
**Mitigation**:
- Context cancellation propagates to all goroutines
- WaitGroup ensures all goroutines complete before cleanup
- Defer statements for resource cleanup (file close, connection close)

### Operational Risks

**Risk**: Server crashes on malformed input  
**Mitigation**:
- Input validation (version byte, chunk sizes, message lengths)
- Panic recovery in goroutines (log stack trace, close connection gracefully)
- Fuzz testing with random inputs

**Risk**: Memory leaks on long-running streams  
**Mitigation**:
- Buffer pooling (return buffers after use)
- Periodic GC (Go runtime handles automatically)
- Integration test: run for 10 minutes, monitor memory with pprof

**Risk**: Slow consumer blocks server  
**Mitigation**:
- Bounded outbound queue (drop messages if full)
- Disconnect policy: if queue full for >5 seconds, disconnect slow consumer
- Log warning before disconnecting

---

## Future Enhancements (Out of Scope for Initial Implementation)

1. **Complex Handshake**: HMAC-SHA256 + Diffie-Hellman for legacy Flash Player support
2. **RTMPS**: TLS/SSL transport for secure streaming
3. **AMF3**: Extended type system for advanced clients
4. **Authentication**: Token-based auth, username/password verification
5. **Adaptive Bitrate**: Quality switching for players
6. **Cluster Mode**: Multi-server deployment with stream routing
7. **HLS/DASH Transmuxing**: Output to HTTP-based protocols
8. **Advanced Recording**: MP4 output, segmentation, DVR seeking
9. **Metrics**: Prometheus exporter, Grafana dashboards
10. **Admin API**: REST API for stream management, connection stats

---

## References

- Adobe RTMP Specification 1.0: https://rtmp.veriskope.com/docs/spec/
- FFmpeg RTMP Implementation: https://github.com/FFmpeg/FFmpeg/tree/master/libavformat (rtmp.c, rtmppkt.c)
- nginx-rtmp-module: https://github.com/arut/nginx-rtmp-module
- Effective Go: https://go.dev/doc/effective_go
- Go Code Review Comments: https://go.dev/wiki/CodeReviewComments
- Google Go Style Guide: https://google.github.io/styleguide/go/

---

**Status**: Research complete. All clarifications resolved. Ready for Phase 1 (Design & Contracts).

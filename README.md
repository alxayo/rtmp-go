# go-rtmp

An educational yet production‑ready, spec‑driven RTMP (Real‑Time Messaging Protocol) server and client implementation in pure Go (Go 1.21+). It focuses on correctness (wire‑format fidelity), simplicity (RTMP v3 simple handshake, AMF0 only), observability, and strong test coverage using golden vectors, integration flows, and FFmpeg / ffplay interoperability.

> **Status:** ✅ **Core Features Operational** (October 2025)  
> **Recording:** ✅ Automatic FLV recording with H.264/AAC  
> **Relay:** ✅ Live streaming to multiple subscribers with late-join support  
> **Testing:** ✅ Validated with OBS Studio and ffplay

---

## 1. Quick Start

**Want to get started immediately?** See **[quick-start.md](quick-start.md)** for a complete step-by-step guide to:
- Start the RTMP server with recording enabled
- Publish from OBS Studio or FFmpeg
- Play live streams with ffplay or VLC
- Test multiple subscribers and verify recordings

**5-Minute Setup:**
```bash
# 1. Build
go build -o rtmp-server.exe ./cmd/rtmp-server

# 2. Start server with recording
./rtmp-server.exe -listen localhost:1935 -log-level info -record-all true -record-dir ./recordings

# 3. Stream from OBS (rtmp://localhost:1935/live/test)

# 4. Watch live
ffplay rtmp://localhost:1935/live/test

# 5. Play recording
ffplay ./recordings/live_test_*.flv
```

---

## 2. Key Goals & Current Status

### ✅ Implemented & Operational
- ✅ **RTMP v3 simple handshake** (C0/C1/C2 ↔ S0/S1/S2)
- ✅ **Chunk stream parsing & serialization** (FMT 0–3, extended timestamps)
- ✅ **Control messages** (Set Chunk Size, Window Ack Size, Set Peer Bandwidth, Acknowledgement, User Control)
- ✅ **AMF0 command codec** (Number, Boolean, String, Object, Null, Strict Array)
- ✅ **Command flows** (connect → createStream → publish / play)
- ✅ **Streaming relay** with transparent media forwarding (no transcoding)
- ✅ **Automatic FLV recording** (H.264 video + AAC audio)
- ✅ **Late-join support** with sequence header caching (H.264 SPS/PPS, AAC config)
- ✅ **Multiple concurrent subscribers** with thread-safe payload cloning
- ✅ **Interoperability** validated with FFmpeg (publish) + ffplay/VLC (playback)
- ✅ **Strong concurrency isolation** (readLoop + writeLoop per connection, bounded queues, context cancellation)

### 🚧 In Progress / Planned
- 🚧 Enhanced error handling and graceful degradation
- 🚧 Performance benchmarks and optimization
- 📋 Authentication and authorization
- 📋 RTMPS (TLS/SSL support)
- 📋 Advanced features (DVR, transcoding, clustering)

---

## 3. Highlighted Features

### 🎥 Automatic FLV Recording
Record all published streams to FLV files automatically:
- **Container Format**: FLV (Flash Video)
- **Video Codec**: H.264 (AVC)
- **Audio Codec**: AAC
- **Filename Pattern**: `{app}_{stream}_{YYYYMMDD}_{HHMMSS}.flv`
- **Concurrent Operation**: Recording continues while relaying to live subscribers

```bash
# Enable recording for all streams
./rtmp-server -record-all true -record-dir ./recordings
```

### 📡 Live Streaming Relay with Late-Join Support
Stream to unlimited concurrent subscribers with robust codec initialization:
- **Multiple Subscribers**: Unlimited concurrent viewers per stream
- **Late-Join Support**: Subscribers joining mid-stream receive H.264 SPS/PPS and AAC AudioSpecificConfig
- **Thread-Safe**: Independent payload copies prevent memory corruption
- **Zero Transcoding**: Transparent media forwarding (low CPU usage)

**The Critical Fix:**
When subscribers join a live stream after it has started, they need codec initialization packets (H.264 SPS/PPS, AAC config) that were sent at stream start. The server now:
1. **Caches sequence headers** when publisher sends them (timestamp 0)
2. **Delivers cached headers** to late-joining subscribers before media packets
3. **Ensures decoder initialization** regardless of when subscriber connects

This solves the common "No start code is found" error in H.264 decoders.

### 🎯 Production Use Cases
- **Live Events**: Stream to multiple viewers simultaneously
- **Recording Archive**: Automatic recording of all streams for later playback
- **Hybrid Workflow**: Record while streaming live (e.g., webinar + archive)
- **Testing & Development**: OBS/FFmpeg integration for RTMP testing

---

## 4. Repository Structure (Condensed)

```
cmd/
  rtmp-server/        # Server CLI
  rtmp-client/        # Client CLI (publish/play helper)
internal/
  rtmp/
    handshake/        # Handshake FSM
    chunk/            # Chunk reader/writer (dechunker/chunker)
    control/          # Control message encode/decode
    amf/              # AMF0 encoder/decoder
    rpc/              # Command parsing/builders (_result, onStatus, etc.)
    media/            # Media message helpers (audio/video/data)
    conn/             # Connection lifecycle (readLoop/writeLoop)
    server/           # Listener, registry, publish/play coordination
    client/           # Programmatic client (Connect, Publish, Play)
  bufpool/            # Buffer pooling utilities
  logger/             # Structured slog wrapper
  errors/             # Domain error helpers
specs/001-rtmp-server-implementation/
  spec.md
  plan.md
  tasks.md
  research.md
  data-model.md
  contracts/
docs/                 # Deep protocol & implementation notes
tests/
  golden/             # Binary golden vectors
  integration/        # Handshake + command flow tests
  interop/            # FFmpeg / ffplay scripts & README
```

Core design & planning documents:

- Feature Specification: [specs/001-rtmp-server-implementation/spec.md](specs/001-rtmp-server-implementation/spec.md)
- Implementation Plan: [specs/001-rtmp-server-implementation/plan.md](specs/001-rtmp-server-implementation/plan.md)
- Task Breakdown: [specs/001-rtmp-server-implementation/tasks.md](specs/001-rtmp-server-implementation/tasks.md)
- Data Model: [specs/001-rtmp-server-implementation/data-model.md](specs/001-rtmp-server-implementation/data-model.md)
- Handshake Contract: [specs/001-rtmp-server-implementation/contracts/handshake.md](specs/001-rtmp-server-implementation/contracts/handshake.md)
- Chunking Contract: [specs/001-rtmp-server-implementation/contracts/chunking.md](specs/001-rtmp-server-implementation/contracts/chunking.md)
- Control Contract: [specs/001-rtmp-server-implementation/contracts/control.md](specs/001-rtmp-server-implementation/contracts/control.md)
- AMF0 Contract: [specs/001-rtmp-server-implementation/contracts/amf0.md](specs/001-rtmp-server-implementation/contracts/amf0.md)
- Commands Contract: [specs/001-rtmp-server-implementation/contracts/commands.md](specs/001-rtmp-server-implementation/contracts/commands.md)
- Media Contract: [specs/001-rtmp-server-implementation/contracts/media.md](specs/001-rtmp-server-implementation/contracts/media.md)

Supplemental protocol guides:

- Handshake Deep Dive: [docs/RTMP_basic_handshake_deep_dive.md](docs/RTMP_basic_handshake_deep_dive.md)
- Implementation Guide: [docs/001-rtmp_protocol_implementation_guide.md](docs/001-rtmp_protocol_implementation_guide.md)
- Task Strategy Breakdown: [docs/rtmp_implementation_plan_task breakdown.md](docs/rtmp_implementation_plan_task%20breakdown.md)
- Copilot Build Instructions: [docs/rtmp_copilot_instructions.md](docs/rtmp_copilot_instructions.md)
- Constitution: [docs/000-constitution.md](docs/000-constitution.md)

---

## 5. Features (Current Implementation)

| Layer | Status | Notes |
|-------|--------|-------|
| **Handshake (simple v3)** | ✅ Complete | Golden vectors + integration tests |
| **Chunk Header Parser/Writer** | ✅ Complete | FMT 0–3, extended timestamp support |
| **Control Messages** | ✅ Complete | Control burst: WAS → SPB → SCS, User Control events |
| **AMF0 Codec** | ✅ Complete | All core types with golden vectors |
| **Command Parsing** | ✅ Complete | connect, createStream, publish, play, onStatus, _result |
| **Media Relay** | ✅ **Complete** | **Transparent forwarding with late-join support** |
| **Recording** | ✅ **Complete** | **Automatic FLV recording (H.264/AAC)** |
| **Sequence Header Caching** | ✅ **Complete** | **H.264 SPS/PPS + AAC config for late joiners** |
| **Client Library** | ✅ Functional | Connect, publish, play operations |
| **Server Registry** | ✅ Complete | Stream registry with publish/play coordination |
| **Interop Validation** | ✅ Complete | OBS Studio (publish) + ffplay/VLC (playback) |

### Recent Additions (October 2025)

**Recording Feature:**
- Automatic FLV file creation for all published streams
- H.264 video + AAC audio support
- Filename pattern: `{app}_{stream}_{timestamp}.flv`
- Concurrent recording while streaming to subscribers

**Relay Enhancement:**
- Fixed critical issue: Late-joining subscribers now receive codec initialization
- Implemented sequence header caching (H.264 SPS/PPS, AAC AudioSpecificConfig)
- Thread-safe payload cloning prevents corruption between subscribers
- Support for unlimited concurrent subscribers per stream

**Documentation:**
- `quick-start.md` - Comprehensive usage guide
- `RELAY_FIX_SEQUENCE_HEADERS.md` - Technical implementation details
- `RELAY_MMCO_ERROR_ANALYSIS.md` - Analysis of H.264 decoder warnings
- `RELAY_COMPLETE.md` - Feature summary

(See detailed task status in [specs/001-rtmp-server-implementation/tasks.md](specs/001-rtmp-server-implementation/tasks.md))

---

## 6. Build & Run

### 6.1 Prerequisites
- Go 1.21+ (no external Go deps)
- (Optional) FFmpeg + ffplay on PATH for interop tests
- Test media file (e.g., `test.mp4`)

### 6.2 Build Server & Client

```bash
go build -o bin/rtmp-server ./cmd/rtmp-server
go build -o bin/rtmp-client ./cmd/rtmp-client
```

Version check:
```bash
./bin/rtmp-server -version
```

### 6.3 Run Server

```bash
# Basic server with recording enabled
./bin/rtmp-server -listen localhost:1935 -log-level info -record-all true -record-dir ./recordings

# Debug mode with detailed logging
./bin/rtmp-server -listen localhost:1935 -log-level debug -record-all true -record-dir ./recordings

# Production mode (minimal logging)
./bin/rtmp-server -listen localhost:1935 -log-level warn -record-all true -record-dir ./recordings
```

**Available flags:**
- `-listen` (default: `localhost:1935`) - Listen address and port
- `-log-level` (default: `info`) - Logging verbosity: `debug`, `info`, `warn`, `error`
- `-record-all` (default: `false`) - Automatically record all published streams
- `-record-dir` (default: `./recordings`) - Directory for FLV recording files

**Expected output:**
```json
{"level":"INFO","msg":"RTMP server listening","addr":"127.0.0.1:1935"}
{"level":"INFO","msg":"server started","addr":"127.0.0.1:1935","version":"dev"}
```

### 6.4 Publish with OBS Studio or FFmpeg

**Option 1: OBS Studio (Recommended)**
1. Settings → Stream
   - Service: `Custom...`
   - Server: `rtmp://localhost:1935/live`
   - Stream Key: `test`
2. Click "Start Streaming"

**Option 2: FFmpeg**
```bash
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test
```

**Server logs (on successful publish):**
```json
{"level":"INFO","msg":"Connection accepted","conn_id":"c000001"}
{"level":"INFO","msg":"recorder initialized","stream_key":"live/test","file":"recordings/live_test_20251013_121100.flv"}
{"level":"INFO","msg":"Cached audio sequence header","stream_key":"live/test","size":7}
{"level":"INFO","msg":"Cached video sequence header","stream_key":"live/test","size":52}
{"level":"INFO","msg":"Codecs detected","stream_key":"live/test","videoCodec":"H264","audioCodec":"AAC"}
```

### 6.5 Play with ffplay or VLC

**Option 1: ffplay**
```bash
ffplay rtmp://localhost:1935/live/test
```

**Option 2: VLC Media Player**
```bash
vlc rtmp://localhost:1935/live/test
```

**Server logs (on subscriber join):**
```json
{"level":"INFO","msg":"Connection accepted","conn_id":"c000002"}
{"level":"INFO","msg":"Subscriber added","stream_key":"live/test","total_subscribers":1}
{"level":"INFO","msg":"Sent cached audio sequence header to subscriber","size":7}
{"level":"INFO","msg":"Sent cached video sequence header to subscriber","size":52}
```

**Expected behavior:**
- Video plays within 1-2 seconds of connection
- Smooth playback with no buffering (for live streams)
- Multiple subscribers can connect simultaneously
- Late-joining subscribers receive codec initialization automatically

### 6.6 Verify Recording

```bash
# List recorded files
ls ./recordings/

# Play recorded file
ffplay ./recordings/live_test_20251013_121100.flv

# Check recording info
ffprobe ./recordings/live_test_20251013_121100.flv
```

---

## 7. Programmatic Client Usage

Minimal sequence (high-level; see [`internal/rtmp/client`](internal/rtmp/client)):

1. Dial + handshake
2. Send connect command → await _result
3. Send createStream → receive stream ID
4. Send publish or play
5. Exchange media (forwarding layer)

Integration examples in:
- Connect: [internal/rtmp/client/client_test.go](internal/rtmp/client/client_test.go)
- Play/Publish flows: same test file

---

## 8. Testing Strategy

| Test Type | Location | Description |
|-----------|----------|-------------|
| Golden Vectors | [tests/golden](tests/golden) | Binary snapshots (handshake, control, AMF0) |
| Unit Tests | `internal/.../*_test.go` | Per-layer logic (FSM, codec, control) |
| Integration Tests | [tests/integration](tests/integration) | Handshake + command sequences |
| Interop Scripts | [tests/interop](tests/interop) | FFmpeg publish / ffplay playback harness |
| Benchmarks (planned) | chunk & AMF packages | Encode/decode performance |
| Fuzz (optional) | AMF0 & chunk parsing | Safety & bounds validation |

Run all tests (race detector recommended):
```bash
go test -race ./...
```

Run integration only:
```bash
go test -race ./tests/integration -count=1
```

Run a specific package:
```bash
go test -race ./internal/rtmp/handshake
```

Interop helper (see [tests/interop/README.md](tests/interop/README.md)):
```bash
(cd tests/interop && ./ffmpeg_test.sh)
```

Environment vars (subset):
- INCLUDE=PublishOnly,PublishAndPlay
- SERVER_FLAGS="-log-level debug"

---

## 9. Handshake Overview

Server FSM (see [`internal/rtmp/handshake`](internal/rtmp/handshake) and contract):
```
Initial → RecvC0C1 → SentS0S1S2 → RecvC2 → Completed
```
Rules:
- Version byte must be 0x03
- C1/S1/S2/C2 each 1536 bytes (time (4) + zero (4) + random (1528))
- S2 echoes C1; C2 echoes S1
- 5s read deadlines (`io.ReadFull` usage)
Golden scenarios defined in [contracts/handshake.md](specs/001-rtmp-server-implementation/contracts/handshake.md).

---

## 10. Chunking (Summary)

(See [contracts/chunking.md](specs/001-rtmp-server-implementation/contracts/chunking.md))
- Basic Header: fmt (2 bits) + csid (variable length)
- Message Header size varies by fmt (0: 11 bytes → 3: 0 bytes)
- Extended timestamp when timestamp ≥ 0xFFFFFF
- Default chunk size 128; negotiated via Set Chunk Size
- MSID little-endian quirk; others big-endian
Planned tests: round trip header encode/decode across FMT transitions and extended timestamp boundary.

---

## 11. Control Messages

(See [contracts/control.md](specs/001-rtmp-server-implementation/contracts/control.md))

Sent on CSID=2, MSID=0:
- Set Chunk Size (type 1)
- Abort (type 2) (future)
- Acknowledgement (type 3)
- User Control (type 4) (StreamBegin, StreamEOF)
- Window Acknowledgement Size (type 5)
- Set Peer Bandwidth (type 6)

Golden vectors under [tests/golden](tests/golden).

---

## 12. AMF0 Codec

(See [contracts/amf0.md](specs/001-rtmp-server-implementation/contracts/amf0.md))
Supported markers: Number(0x00), Boolean(0x01), String(0x02), Object(0x03), Null(0x05), Strict Array(0x0A)
Mapping:
- float64, bool, string, map[string]any, nil, []any
Golden examples included (e.g., `amf0_string_test.bin`).

Used by commands (type 20 messages):
- connect
- _result / _error
- createStream
- publish / play
- onStatus

---

## 13. Command Flows

Reference: [contracts/commands.md](specs/001-rtmp-server-implementation/contracts/commands.md)

Publish Flow (simplified):
```
connect → _result
createStream → _result(streamID=1)
publish(streamID=1) → onStatus(NetStream.Publish.Start)
(audio/video messages)
```

Play Flow:
```
connect → _result
createStream → _result(streamID=1)
play(streamID=1) → UserControl(StreamBegin) + onStatus(NetStream.Play.Start)
(media messages forwarded)
```

Status builders: see RPC package ([internal/rtmp/rpc](internal/rtmp/rpc)).

---

## 14. Logging & Observability

- Structured logging using slog (see [docs/rtmp_copilot_instructions.md](docs/rtmp_copilot_instructions.md) section 5.5)
- Fields: conn_id, remote, csid, msid, type, stream_key
- Debug mode prints protocol transitions (handshake state, chunk headers)

Planned enhancements:
- Expvar counters (active connections, publishers, subscribers)
- Optional protocol trace toggle

---

## 15. Concurrency Model

Per connection:
- readLoop: decode handshake → chunks → messages → dispatch
- writeLoop: drain outbound queue → chunk encode → flush
- Cancellation: context + channel closure on error or disconnect

Registry (server):
- Tracks streams (publisher + subscribers)
- Mutex protected; stream key: "app/streamName"
- Backpressure: bounded subscriber queues (drop or disconnect policy, configurable future)

See conceptual data model: [data-model.md](specs/001-rtmp-server-implementation/data-model.md)

---

## 16. Development Workflow

1. Identify failing test (TDD golden/integration)
2. Implement minimal code to pass
3. Add protocol debug logging if ambiguous
4. Run:
   ```bash
   go vet ./...
   go test -race ./...
   ```
5. Optionally run interop script with FFmpeg

---

## 17. Common Troubleshooting

| Symptom | Cause | Action |
|---------|-------|--------|
| **Handshake timeout** | Partial C1/C2 or deadline missed | Enable debug logging, verify lengths (1536) |
| **FFmpeg stalls on publish** | Control burst missing | Confirm Set Chunk Size / Window Ack Size sent |
| **Player: "No start code is found"** | ❌ **[FIXED]** Late-join without sequence headers | ✅ Ensure server version has sequence header caching |
| **Player no video** | Stream registry mismatch | Verify stream key and publish started event |
| **Multiple H.264 errors** | Started subscriber before publisher | ⚠️ **Always start OBS/FFmpeg first, then ffplay** |
| **Single "mmco: unref short failure"** | Normal mid-GOP join behavior | ✅ Expected and harmless, video plays normally |
| **No recording file** | `-record-all` flag not set | Add `-record-all true` flag when starting server |
| **Recording corrupt** | Server killed during recording | Use graceful shutdown (Ctrl+C once, wait for cleanup) |
| **High CPU** | Tight loop after closed conn | Check context cancellation & error propagation |
| **ACK not sent** | bytesReceived < window | Adjust test payload size or window config |

### Critical Notes

**⚠️ Publisher-First Requirement:**  
Always start the publisher (OBS/FFmpeg) **before** subscribers (ffplay/VLC). This ensures sequence headers are cached before subscribers connect.

**✅ Late-Join Support:**  
The server caches H.264 SPS/PPS and AAC AudioSpecificConfig. Subscribers joining after stream start receive these cached headers automatically.

**ℹ️ Expected Warnings:**  
A single `[h264] mmco: unref short failure` warning in ffplay is normal when joining mid-GOP. The decoder recovers automatically.

**Detailed troubleshooting:** See `quick-start.md` and `RELAY_MMCO_ERROR_ANALYSIS.md`  
**Interop tips:** [tests/interop/README.md](tests/interop/README.md)

---

## 18. Roadmap (Excerpt)

From [tasks.md](specs/001-rtmp-server-implementation/tasks.md):
- Remaining Core: Extended chunk tests, full media relay, user control events
- Polish: Recording (optional), performance benchmarks, fuzzing (AMF0 / chunk)
- Future (Out of Scope Now): RTMPS, authentication, transcoding, clustering

---

## 19. Design Principles Summary

Documented in [docs/000-constitution.md](docs/000-constitution.md):
1. Protocol-First
2. Idiomatic Go
3. Modularity
4. Test-First
5. Concurrency Safety
6. Observability
7. Simplicity (YAGNI)

---

## 20. Example End-to-End Session (Narrative)

### Publisher Flow (OBS → Server)
1. Client connects TCP → handshake completes <50ms local
2. Server sends control burst (WAS, SPB, Set Chunk Size)
3. Client sends `connect` command (AMF0)
4. Server responds `_result` (NetConnection.Connect.Success)
5. Client sends `createStream` → server returns streamID=1
6. Client sends `publish` → server responds onStatus(NetStream.Publish.Start)
7. **Server initializes FLV recorder** (if `-record-all true`)
8. Client sends audio sequence header (AAC AudioSpecificConfig)
9. **Server caches audio sequence header** (7 bytes)
10. Client sends video sequence header (H.264 SPS/PPS)
11. **Server caches video sequence header** (typically 52 bytes)
12. Client sends media frames (audio type_id=8, video type_id=9)
13. **Server writes media to FLV file**
14. Server broadcasts media to all subscribers

### Subscriber Flow (ffplay → Server)
1. Subscriber connects TCP → handshake completes
2. Server sends control burst
3. Subscriber sends `connect` → server responds `_result`
4. Subscriber sends `createStream` → server returns streamID=1
5. Subscriber sends `play` → server responds StreamBegin + onStatus(NetStream.Play.Start)
6. **Server sends cached audio sequence header** → subscriber (critical for decoder init)
7. **Server sends cached video sequence header** → subscriber (H.264 SPS/PPS)
8. **Server relays ongoing live media packets** → subscriber
9. Subscriber's H.264/AAC decoders initialize successfully
10. Playback begins (typically < 1 second from connection)

### Key Technical Features
- **Sequence Header Caching**: Late-joining subscribers receive codec initialization regardless of when they connect
- **Payload Cloning**: Each subscriber receives independent copy of media packets (thread-safe)
- **Concurrent Operation**: Recording and relay work simultaneously without interference
- **ACK Logic**: Triggers when bytesReceived > Window Ack Size
- **Graceful Cleanup**: Publisher disconnect → recording finalized → subscribers notified

---

## 21. Security & Hardening (Planned Baseline)

- Size validation: chunk size ≤ 65536, message length sanity (≤16MB)
- Random handshake payload (crypto/rand)
- Optional token in RTMP URL query (future)
- Graceful connection drop on protocol violations

---

## 22. Contributing

Current phase emphasizes protocol core; contributions should:
- Add/adjust failing test first
- Follow error wrapping: fmt.Errorf("context: %w", err)
- Keep functions small & documented
- Avoid external dependencies

Proposed PR checklist:
- go vet & go test -race pass
- Added/updated golden vectors if wire format changed
- Added logging fields if new state introduced

---

## 23. License

(Choose and add a LICENSE file—e.g., MIT/Apache-2.0—placeholder here.)

---

## 24. Quick Commands Cheat Sheet

```bash
# Build
go build -o rtmp-server.exe ./cmd/rtmp-server     # Windows
go build -o rtmp-server ./cmd/rtmp-server         # Linux/macOS

# Run server (basic with recording)
./rtmp-server -listen localhost:1935 -log-level info -record-all true -record-dir ./recordings

# Run server (debug mode)
./rtmp-server -listen localhost:1935 -log-level debug -record-all true -record-dir ./recordings

# Run server with log file
./rtmp-server -listen localhost:1935 -log-level debug -record-all true -record-dir ./recordings > debug.log

# Publish with OBS Studio
# Settings → Stream
# Server: rtmp://localhost:1935/live
# Stream Key: test

# Publish with FFmpeg
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test

# Play with ffplay
ffplay rtmp://localhost:1935/live/test

# Play with VLC
vlc rtmp://localhost:1935/live/test

# Play recorded file
ffplay ./recordings/live_test_20251013_121100.flv

# List recordings
ls ./recordings/

# Check recording info
ffprobe ./recordings/live_test_20251013_121100.flv

# All tests
go test -race ./...

# Integration only
go test -race ./tests/integration -count=1

# Interop script
(cd tests/interop && ./ffmpeg_test.sh)
```

---

## 25. Contact / Support

Use issues for:
- Protocol compliance gaps
- Interop anomalies (attach ffmpeg -loglevel debug excerpts)
- Test flakiness reports (include OS + Go version)

---

Happy streaming. Contributions and protocol trace captures welcome.
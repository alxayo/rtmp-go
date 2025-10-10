# go-rtmp

An educational yet production‑minded, spec‑driven RTMP (Real‑Time Messaging Protocol) server and client implementation in pure Go (Go 1.21+). It focuses on correctness (wire‑format fidelity), simplicity (RTMP v3 simple handshake, AMF0 only), observability, and strong test coverage using golden vectors, integration flows, and FFmpeg / ffplay interoperability.

> Status: Phase 3 (Core Implementation & Integration). Design artifacts and task plan completed. Many tasks marked [X] in planning documents indicate stubs/tests prepared; ongoing code fill‑in follows TDD.

---

## 1. Key Goals

- RTMP v3 simple handshake (C0/C1/C2 ↔ S0/S1/S2)
- Chunk stream parsing & serialization (FMT 0–3, extended timestamps)
- Control messages: Set Chunk Size, Window Acknowledgement Size, Set Peer Bandwidth, Acknowledgement, User Control
- AMF0 command codec (Number, Boolean, String, Object, Null, Strict Array, etc.)
- Command flows: connect → createStream → publish / play → deleteStream
- Streaming relay (transparent media forwarding; no transcoding)
- Interoperability: FFmpeg (publish) + ffplay (play) validation
- Strong isolation per connection: readLoop + writeLoop, bounded queues, context cancellation

---

## 2. Repository Structure (Condensed)

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

## 3. Features (Current & Planned)

| Layer | Status | Notes |
|-------|--------|-------|
| Handshake (simple v3) | Implemented/Tested | Golden vectors + integration tests |
| Chunk Header Parser/Writer | In progress | FMT 0–3, extended timestamp |
| Control Messages | In progress | Burst: WAS → SPB → SCS |
| AMF0 Codec | Implemented (core types) | Golden vectors included |
| Command Parsing | Partial | connect, createStream, publish, play, onStatus sequence |
| Media Relay | Basic scaffolding | Transparent forward planned |
| Client Library | Partial | Connect/play tests exist |
| Server Registry | Implemented skeleton | Stream registry & publish/play |
| Interop Scripts | Added | Under tests/interop |

(See task status table in [specs/001-rtmp-server-implementation/tasks.md](specs/001-rtmp-server-implementation/tasks.md))

---

## 4. Build & Run

### 4.1 Prerequisites
- Go 1.21+ (no external Go deps)
- (Optional) FFmpeg + ffplay on PATH for interop tests
- Test media file (e.g., `test.mp4`)

### 4.2 Build Server & Client

```bash
go build -o bin/rtmp-server ./cmd/rtmp-server
go build -o bin/rtmp-client ./cmd/rtmp-client
```

Version check:
```bash
./bin/rtmp-server -version
```

### 4.3 Run Server

```bash
./bin/rtmp-server -listen :1935 -log-level info -chunk-size 4096
```

Key flags (planned per spec):
- -listen (default :1935)
- -log-level (debug|info|warn|error)
- -record-all (bool)
- -record-dir (default recordings)
- -chunk-size (initial outbound)

### 4.4 Publish with FFmpeg

```bash
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test
```

### 4.5 Play with ffplay

```bash
ffplay rtmp://localhost:1935/live/test
```

Expected: 3–5s startup, then continuous playback.

---

## 5. Programmatic Client Usage

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

## 6. Testing Strategy

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

## 7. Handshake Overview

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

## 8. Chunking (Summary)

(See [contracts/chunking.md](specs/001-rtmp-server-implementation/contracts/chunking.md))
- Basic Header: fmt (2 bits) + csid (variable length)
- Message Header size varies by fmt (0: 11 bytes → 3: 0 bytes)
- Extended timestamp when timestamp ≥ 0xFFFFFF
- Default chunk size 128; negotiated via Set Chunk Size
- MSID little-endian quirk; others big-endian
Planned tests: round trip header encode/decode across FMT transitions and extended timestamp boundary.

---

## 9. Control Messages

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

## 10. AMF0 Codec

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

## 11. Command Flows

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

## 12. Logging & Observability

- Structured logging using slog (see [docs/rtmp_copilot_instructions.md](docs/rtmp_copilot_instructions.md) section 5.5)
- Fields: conn_id, remote, csid, msid, type, stream_key
- Debug mode prints protocol transitions (handshake state, chunk headers)

Planned enhancements:
- Expvar counters (active connections, publishers, subscribers)
- Optional protocol trace toggle

---

## 13. Concurrency Model

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

## 14. Development Workflow

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

## 15. Common Troubleshooting

| Symptom | Cause | Action |
|---------|-------|--------|
| Handshake timeout | Partial C1/C2 or deadline missed | Enable debug logging, verify lengths (1536) |
| FFmpeg stalls on publish | Control burst missing | Confirm Set Chunk Size / Window Ack Size sent |
| Player no video | Stream registry mismatch | Verify stream key and publish started event |
| High CPU | Tight loop after closed conn | Check context cancellation & error propagation |
| ACK not sent | bytesReceived < window | Adjust test payload size or window config |

Interop tips: [tests/interop/README.md](tests/interop/README.md)

---

## 16. Roadmap (Excerpt)

From [tasks.md](specs/001-rtmp-server-implementation/tasks.md):
- Remaining Core: Extended chunk tests, full media relay, user control events
- Polish: Recording (optional), performance benchmarks, fuzzing (AMF0 / chunk)
- Future (Out of Scope Now): RTMPS, authentication, transcoding, clustering

---

## 17. Design Principles Summary

Documented in [docs/000-constitution.md](docs/000-constitution.md):
1. Protocol-First
2. Idiomatic Go
3. Modularity
4. Test-First
5. Concurrency Safety
6. Observability
7. Simplicity (YAGNI)

---

## 18. Example End-to-End Session (Narrative)

1. Client connects TCP → handshake completes <50ms local.
2. Server sends control burst (WAS, SPB, Set Chunk Size).
3. Client sends connect (AMF0).
4. Server responds _result (NetConnection.Connect.Success).
5. Client createStream → server returns streamID=1.
6. Client publish → server onStatus(NetStream.Publish.Start).
7. Media chunks forwarded to any subscribers (play clients).
8. Subscriber play → server sends StreamBegin + onStatus(NetStream.Play.Start).
9. ACK logic triggers when bytesReceived > Window Ack Size.
10. Publisher disconnect → onStatus(NetStream.Unpublish.Success) (planned) → subscriber teardown.

---

## 19. Security & Hardening (Planned Baseline)

- Size validation: chunk size ≤ 65536, message length sanity (≤16MB)
- Random handshake payload (crypto/rand)
- Optional token in RTMP URL query (future)
- Graceful connection drop on protocol violations

---

## 20. Contributing

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

## 21. License

(Choose and add a LICENSE file—e.g., MIT/Apache-2.0—placeholder here.)

---

## 22. Quick Commands Cheat Sheet

```bash
# Build
go build ./cmd/rtmp-server
go build ./cmd/rtmp-client

# Run server (debug)
./rtmp-server -log-level debug

# Publish test
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test

# Play
ffplay rtmp://localhost:1935/live/test

# All tests
go test -race ./...

# Integration only
go test -race ./tests/integration -count=1

# Interop script
(cd tests/interop && ./ffmpeg_test.sh)
```

---

## 23. Contact / Support

Use issues for:
- Protocol compliance gaps
- Interop anomalies (attach ffmpeg -loglevel debug excerpts)
- Test flakiness reports (include OS + Go version)

---

Happy streaming. Contributions and protocol trace captures welcome.
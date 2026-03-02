# go-rtmp

A production-ready RTMP server in pure Go. Zero external dependencies.

Stream from OBS/FFmpeg → go-rtmp server → multiple viewers + FLV recording + multi-destination relay.

> **Status:** ✅ Core features operational  
> **Recording:** ✅ Automatic FLV (H.264 + AAC)  
> **Relay:** ✅ Multi-subscriber with late-join support  
> **Tested with:** OBS Studio, FFmpeg, ffplay, VLC

## Quick Start

```bash
# Build
go build -o rtmp-server ./cmd/rtmp-server

# Run (with recording)
./rtmp-server -listen :1935 -record-all true

# Publish (terminal 2)
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test

# Watch (terminal 3)
ffplay rtmp://localhost:1935/live/test
```

See [docs/getting-started.md](docs/getting-started.md) for the full guide with CLI flags, OBS setup, and troubleshooting.

## Features

| Feature | Description |
|---------|-------------|
| **RTMP v3 Handshake** | C0/C1/C2 ↔ S0/S1/S2 with 5s timeouts |
| **Chunk Streaming** | FMT 0-3 header compression, extended timestamps |
| **Control Messages** | Set Chunk Size, Window Ack, Peer Bandwidth, User Control |
| **AMF0 Codec** | Number, Boolean, String, Object, Null, Strict Array |
| **Command Flow** | connect → createStream → publish / play |
| **Live Relay** | Transparent forwarding to unlimited subscribers |
| **FLV Recording** | Automatic recording of all streams to FLV files |
| **Late-Join** | Sequence header caching (H.264 SPS/PPS, AAC config) |
| **Multi-Destination** | Relay to external RTMP servers (`-relay-to` flag) |
| **Media Logging** | Per-connection codec detection and bitrate stats |

## Architecture

```
TCP Accept → Handshake → Control Burst → Command RPC → Media Relay/Recording
```

```
internal/rtmp/
├── handshake/    RTMP v3 handshake (C0/C1/C2 ↔ S0/S1/S2)
├── chunk/        Message ↔ chunk fragmentation and reassembly
├── amf/          AMF0 binary codec
├── control/      Protocol control messages (types 1-6)
├── rpc/          Command parsing (connect, publish, play)
├── conn/         Connection lifecycle (read/write loops)
├── server/       Listener, stream registry, pub/sub
├── media/        Audio/video parsing, codec detection, FLV recording
├── relay/        Multi-destination forwarding
└── client/       Minimal test client
```

See [docs/architecture.md](docs/architecture.md) for the full system overview with diagrams.

## Documentation

| Document | Description |
|----------|-------------|
| [Getting Started](docs/getting-started.md) | Build, run, test — everything to get going |
| [Architecture](docs/architecture.md) | System overview, data flow, package map |
| [Design](docs/design.md) | Design principles, concurrency model, key decisions |
| [RTMP Protocol](docs/rtmp-protocol.md) | Wire-level reference: chunks, AMF0, commands |
| [Implementation](docs/implementation.md) | Code walkthrough, data structures, media flow |
| [Testing Guide](docs/testing-guide.md) | Unit tests, golden vectors, interop testing |
| [Documentation Index](docs/README.md) | Full index of all docs |

## Testing

```bash
# All tests
go test ./...

# Static analysis
go vet ./...

# Specific package
go test ./internal/rtmp/chunk/
```

Tests use golden binary vectors in `tests/golden/` for wire-format validation.
Integration tests in `tests/integration/` exercise the full publish → subscribe flow.

## CLI Flags

```
-listen        TCP listen address (default :1935)
-log-level     debug | info | warn | error (default info)
-record-all    Record all streams to FLV (default false)
-record-dir    Recording directory (default recordings)
-chunk-size    Outbound chunk size, 1-65536 (default 4096)
-relay-to      RTMP relay destination URL (repeatable)
-version       Print version and exit
```

## Requirements

- Go 1.21+
- No external dependencies (stdlib only)
- FFmpeg/ffplay for testing (optional)

## Roadmap

### In Progress
- Enhanced error handling and graceful connection cleanup
- Performance benchmarks for chunk and AMF0 encode/decode
- Fuzz testing for AMF0 and chunk parsing (bounds safety)

### Planned
- **RTMPS** — TLS/SSL encrypted connections
- **Authentication** — token-based stream key validation
- **Expvar metrics** — live counters for connections, publishers, subscribers
- **Configurable backpressure** — drop or disconnect policy for slow subscribers
- **DVR / time-shift** — seek into live stream history
- **Transcoding** — server-side codec conversion (e.g. H.265 → H.264)
- **Clustering** — horizontal scaling across multiple server instances

## License

See [LICENSE](LICENSE) file.
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
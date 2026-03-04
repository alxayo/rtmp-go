# Testing Guide

How to test the go-rtmp server at every level: unit tests, integration tests, and manual interop tests.

## Run All Tests

```bash
go test ./...
```

This runs tests across all packages. Most complete in under 15 seconds. Some packages (handshake) take ~10 seconds due to timeout tests.

## Package-Level Tests

### Core Utilities

```bash
go test ./internal/errors/     # Error type classification and unwrapping
go test ./internal/logger/     # Log level parsing and field extraction
```

### Protocol Layers

```bash
go test ./internal/rtmp/handshake/   # Handshake FSM state machine (C0/C1/C2 ↔ S0/S1/S2)
go test ./internal/rtmp/chunk/       # Chunk header parsing, message reassembly, round-trip encode/decode
go test ./internal/rtmp/amf/         # AMF0 encoding/decoding for all supported types
go test ./internal/rtmp/control/     # Control message encoding/decoding (types 1-6)
go test ./internal/rtmp/rpc/         # Command parsing (connect, createStream, publish, play)
```

### Server Components

```bash
go test ./internal/rtmp/media/         # Audio/video parsing, codec detection, FLV recording
go test ./internal/rtmp/server/        # Stream registry, publish/play handlers, media logging
go test ./internal/rtmp/server/hooks/  # Event hook registration, execution, cleanup
go test ./internal/rtmp/client/        # Minimal RTMP client
```

### Integration Tests

```bash
go test ./tests/integration/         # End-to-end server lifecycle tests
```

## Golden Vector Tests

The `tests/golden/` directory contains binary `.bin` files with exact RTMP wire-format bytes. These ensure bit-level protocol fidelity:

```
tests/golden/
├── amf0_*.bin          # AMF0 encoded values (number, string, object, array)
├── chunk_*.bin         # Chunk headers for FMT 0-3 with various CSIDs
├── control_*.bin       # Control message payloads (Set Chunk Size, Window Ack, etc.)
└── handshake_*.bin     # Handshake packets (C0/C1/S0/S1)
```

To regenerate golden vectors:

```bash
go run -tags amf0gen tests/golden/gen_amf0_vectors.go
go run tests/golden/gen_chunk_vectors.go
go run tests/golden/gen_control_vectors.go
go run tests/golden/gen_handshake_vectors.go
```

## Manual Interop Testing

### Basic Publish + Play

```bash
# Terminal 1: Start server
./rtmp-server -listen localhost:1935 -log-level debug

# Terminal 2: Publish test stream
ffmpeg -re -f lavfi -i testsrc=size=640x480:rate=30 \
       -f lavfi -i sine=frequency=440:sample_rate=44100 \
       -c:v libx264 -preset ultrafast -c:a aac \
       -f flv rtmp://localhost:1935/live/test

# Terminal 3: Subscribe
ffplay rtmp://localhost:1935/live/test
```

### Recording Verification

```bash
# Start with recording enabled
./rtmp-server -listen localhost:1935 -record-all true -record-dir ./recordings

# Publish a stream, then stop it

# Verify the recording
ffprobe recordings/live_test_*.flv
ffplay recordings/live_test_*.flv
```

### Multi-Subscriber Test

```bash
# Publish once, then open multiple ffplay instances:
ffplay rtmp://localhost:1935/live/test &
ffplay rtmp://localhost:1935/live/test &
ffplay rtmp://localhost:1935/live/test &
```

All three should display the same stream independently.

### Late-Join Test

1. Start publishing a stream
2. Wait 10 seconds (ensure keyframes have been sent)
3. Start `ffplay` — it should display video immediately (no black screen)

This works because the server caches the H.264 sequence header and sends it to late-joining subscribers.

### Relay Test

```bash
# Start two servers
./rtmp-server -listen localhost:1935
./rtmp-server -listen localhost:1936

# Or start one with relay
./rtmp-server -listen localhost:1935 -relay-to rtmp://localhost:1936/live/test

# Publish to first server
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test

# Play from second server
ffplay rtmp://localhost:1936/live/test
```

## Wireshark Analysis

For deep protocol debugging, capture RTMP traffic with Wireshark:

1. Start capture on loopback interface
2. Apply display filter: `rtmpt || tcp.port == 1935`
3. Right-click an RTMP packet → "Decode As" → RTMP (if not auto-detected)
4. Expand the RTMP protocol tree to see individual chunk headers and messages

See [wireshark_rtmp_capture_guide.md](wireshark_rtmp_capture_guide.md) for detailed instructions.

## What the Tests Cover

| Area | Test File(s) | What's Verified |
|------|-------------|-----------------|
| Handshake FSM | `handshake/*_test.go` | State transitions, version validation, C1/C2 echo |
| Chunk encoding | `chunk/header_test.go`, `writer_test.go` | FMT 0-3 wire format, extended timestamps, CSID encoding |
| Chunk decoding | `chunk/reader_test.go` | Reassembly from interleaved chunks, dynamic chunk size |
| Chunk state | `chunk/state_test.go` | Per-CSID state machine, overflow detection |
| AMF0 round-trip | `amf/*_test.go` | Encode → decode for all types, golden vectors |
| Control messages | `control/*_test.go` | Encode/decode for types 1-6, validation |
| RPC commands | `rpc/*_test.go` | Parse connect/createStream/publish/play from AMF0 |
| Connection lifecycle | `conn/conn_test.go` | Accept, handshake, control burst, read/write loops |
| Stream registry | `server/registry_test.go` | Create, get, delete streams; pub/sub management |
| Publish handler | `server/publish_handler_test.go` | onStatus response, single publisher enforcement |
| Play handler | `server/play_handler_test.go` | Subscriber addition, sequence header delivery |
| Media logging | `server/media_logger_test.go` | Packet counting, codec detection, stats |
| Audio/video parsing | `media/*_test.go` | Codec detection, frame type classification |
| Event hooks | `server/hooks/hooks_test.go` | Hook registration, execution, concurrency pool, cleanup |
| Integration | `tests/integration/*_test.go` | Full publish→play flow, multi-subscriber relay |

# GitHub Copilot Instructions for go-rtmp

A production-ready RTMP server/client in pure Go (1.21+). Standard library only, no external dependencies.

## Architecture Overview

```
internal/rtmp/
├── handshake/   # RTMP v3 simple handshake FSM (C0/C1/C2 ↔ S0/S1/S2)
├── chunk/       # Chunk reader/writer (FMT 0-3, extended timestamps)
├── control/     # Control messages (Set Chunk Size, Window Ack, etc.)
├── amf/         # AMF0 codec (Number, Boolean, String, Object, Null, Array)
├── rpc/         # Command parsing (connect, createStream, publish, play)
├── conn/        # Connection lifecycle (readLoop per connection)
├── server/      # Listener + stream registry + pub/sub coordination
│   ├── auth/    # Token-based authentication (Validator interface + backends)
│   └── hooks/   # Event hooks (webhooks, shell scripts, stdio)
├── relay/       # Multi-destination relay with late-join support
└── media/       # Audio/video message handling + FLV recording
```

**Data flow**: TCP Accept → Handshake → Control Burst → Command RPC → Media Relay/Recording

## Build & Test Commands

```bash
go build -o rtmp-server.exe ./cmd/rtmp-server   # Build server
go test -race ./...                              # All tests with race detector
go test -race ./tests/integration -count=1      # Integration tests only
go run -tags amf0gen tests/golden/gen_amf0_vectors.go  # Regenerate golden vectors
```

**Interop testing** (validates with real RTMP clients):
```bash
./rtmp-server.exe -listen localhost:1935 -log-level debug -record-all true
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test  # Publish
ffplay rtmp://localhost:1935/live/test                                   # Subscribe
```

## Project Conventions

### Wire Format Fidelity
All multi-byte integers are **big-endian** except MSID in chunk headers (little-endian quirk). Use `encoding/binary.BigEndian` consistently and verify against golden vectors in `tests/golden/*.bin`.

### Concurrency Pattern
Each connection runs **one readLoop goroutine** with context cancellation. Use bounded channels for backpressure:
```go
outboundQueue := make(chan *chunk.Message, 100)  // Bounded queue
```
Protect shared state (stream registry) with `sync.RWMutex`.

### Error Handling
Use domain-specific error wrappers from `internal/errors`:
```go
rerrors.NewHandshakeError("read C0+C1", err)
rerrors.NewChunkError("parse header", err)
rerrors.NewAMFError("decode.value", err)
```

### Logging (log/slog)
Always include context fields: `conn_id`, `stream_key`, `type_id`, `timestamp`
```go
s.log.Info("connection registered", "conn_id", c.ID(), "remote", addr)
```

### Testing Pattern
Table-driven tests with golden binary vectors:
```go
tests := []struct{ name, file string; want interface{} }{
    {"number_0", "amf0_number_0.bin", 0.0},
}
```

## Protocol Reference (Critical Details)

| Component | Key Constraint |
|-----------|---------------|
| Handshake | Version 0x03, C1/S1/S2/C2 = 1536 bytes, 5s timeouts |
| Chunks | Default 128 bytes, extended timestamp when ≥0xFFFFFF |
| Control | CSID=2, MSID=0, types 1-6 |
| AMF0 | Object ends with 0x00 0x00 0x09 |
| Media | TypeID 8=audio, 9=video; cache sequence headers for late-join |

## Key Files for Onboarding

- [specs/001-rtmp-server-implementation/spec.md](specs/001-rtmp-server-implementation/spec.md) - Full specification
- [specs/001-rtmp-server-implementation/contracts/*.md](specs/001-rtmp-server-implementation/contracts/) - Wire format contracts
- [quick-start.md](quick-start.md) - End-to-end usage guide
- [docs/archived/000-constitution.md](docs/archived/000-constitution.md) - Design principles

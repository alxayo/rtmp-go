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

### RTMPS (Encrypted)

```bash
# Generate self-signed cert (or use Let's Encrypt for production)
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 \
  -nodes -keyout key.pem -out cert.pem -days 365 -subj "/CN=localhost"

# Run with both RTMP and RTMPS
./rtmp-server -listen :1935 -tls-listen :443 -tls-cert cert.pem -tls-key key.pem

# Publish over RTMPS
ffmpeg -re -i test.mp4 -c copy -f flv rtmps://localhost:443/live/test

# Watch over RTMPS
ffplay rtmps://localhost:443/live/test
```

## Features

| Feature | Description |
|---------|-------------|
| **RTMPS (TLS)** | Encrypted RTMP via TLS termination (`-tls-listen`, `-tls-cert`, `-tls-key`) |
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
| **Event Hooks** | Webhooks, shell scripts, and stdio notifications on RTMP events |
| **Authentication** | Pluggable token-based validation for publish/play (static tokens, file, webhook) |
| **Metrics** | Expvar counters for connections, publishers, subscribers, media (HTTP `/debug/vars`) |
| **Connection Cleanup** | TCP deadline enforcement (read 90s, write 30s), disconnect handlers, zombie detection |

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
│   ├── auth/     Token-based authentication (validators)
│   └── hooks/    Event hook system (webhooks, shell, stdio)
├── media/        Audio/video parsing, codec detection, FLV recording
├── relay/        Multi-destination forwarding
├── metrics/      Expvar counters for live monitoring
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
-listen              TCP listen address (default :1935)
-tls-listen          RTMPS listen address (e.g. :443). Requires -tls-cert and -tls-key
-tls-cert            Path to PEM-encoded TLS certificate file
-tls-key             Path to PEM-encoded TLS private key file
-log-level           debug | info | warn | error (default info)
-record-all          Record all streams to FLV (default false)
-record-dir          Recording directory (default recordings)
-chunk-size          Outbound chunk size, 1-65536 (default 4096)
-relay-to            RTMP relay destination URL (repeatable)
-auth-mode           Authentication mode: none|token|file|callback (default none)
-auth-token          Stream token: "streamKey=token" (repeatable, for token mode)
-auth-file           Path to JSON token file (for file mode)
-auth-callback       Webhook URL for auth validation (for callback mode)
-auth-callback-timeout  Auth callback timeout (default 5s)
-hook-script         Shell hook: event_type=/path/to/script (repeatable)
-hook-webhook        Webhook: event_type=https://url (repeatable)
-hook-stdio-format   Stdio hook output: json | env (default disabled)
-hook-timeout        Hook execution timeout (default 30s)
-hook-concurrency    Max concurrent hook executions (default 10)
-metrics-addr        HTTP address for metrics endpoint (e.g. :8080). Empty = disabled
-version             Print version and exit
```

## Requirements

- Go 1.21+
- No external dependencies (stdlib only)
- FFmpeg/ffplay for testing (optional)

## Roadmap

### v0.1.2 (current)
- Expvar metrics: live counters for connections, publishers, subscribers, media bytes (HTTP `/debug/vars`)
- Enhanced error handling: disconnect handlers, TCP deadline enforcement (read 90s, write 30s), relay client cleanup
- Performance optimizations: AMF0 decode allocations, chunk writer buffer reuse, RPC lazy-init
- Dead code removal: unused bufpool package, unreachable error sentinels

### In Progress
- Fuzz testing for AMF0 and chunk parsing (bounds safety)

### Planned
- **Configurable backpressure** — drop or disconnect policy for slow subscribers
- **Clustering & HA** — horizontal scaling with cross-node relay and dual-ingest failover ([design](specs/007-clustering-ha/clustering_ha.md))
- **DVR / time-shift** — seek into live stream history
- **Transcoding** — server-side codec conversion (e.g. H.265 → H.264)

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for a detailed history of all releases and changes.

## License

See [LICENSE](LICENSE) file.
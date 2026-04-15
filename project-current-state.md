# go-rtmp — Comprehensive Repository Research Report

## Executive Summary

**go-rtmp** is a production-ready RTMP/SRT streaming server written in pure Go (1.25.1) with **zero external dependencies**. The repository ([alxayo/rtmp-go](https://github.com/alxayo/rtmp-go)) contains **199 Go files** totaling **34,550 lines of code** (83 test files), organized as a layered protocol stack implementing RTMP v3, Enhanced RTMP (E-RTMP v2), RTMPS (TLS), and SRT (UDP) ingest[^1]. The project supports H.264, H.265/HEVC, AV1, VP9 codecs with automatic FLV recording, multi-destination relay, token-based authentication, event hooks (webhooks, shell, stdio), and expvar metrics[^2]. It has been validated against OBS Studio, FFmpeg, ffplay, and VLC[^3].

The codebase follows a strict "stdlib only" philosophy — every protocol layer (RTMP handshake, chunk framing, AMF0, SRT reliability, MPEG-TS demuxing, AES key wrap) is implemented from scratch using only Go's standard library[^4].

---

## Architecture Overview

```
                     ┌────────────────────────────────────────────────────┐
                     │              go-rtmp Server Binary                 │
                     │              cmd/rtmp-server/main.go               │
                     └─────────┬──────────────────┬──────────────────────┘
                               │                  │
                ┌──────────────▼─────────┐  ┌─────▼──────────────┐
                │  RTMP Listener (TCP)   │  │  SRT Listener (UDP)│
                │  + RTMPS (TLS)         │  │  (multiplexed)     │
                │  server/server.go      │  │  srt/listener.go   │
                └──────────┬─────────────┘  └─────┬──────────────┘
                           │                      │
                ┌──────────▼─────────────┐  ┌─────▼──────────────┐
                │  RTMP Connection       │  │  SRT Connection    │
                │  Handshake → Control   │  │  Handshake → TSBPD │
                │  → Chunk R/W → RPC     │  │  → Reliability     │
                │  conn/conn.go          │  │  conn/conn.go      │
                └──────────┬─────────────┘  └─────┬──────────────┘
                           │                      │
                           │                ┌─────▼──────────────┐
                           │                │  Bridge            │
                           │                │  TS Demux → Codec  │
                           │                │  Convert → Message │
                           │                │  srt/bridge.go     │
                           │                └─────┬──────────────┘
                ┌──────────▼──────────────────────▼──────────────┐
                │           ingress.Manager                      │
                │     (protocol-agnostic publish coordinator)    │
                │           ingress/manager.go                   │
                └──────────────────────┬─────────────────────────┘
                                       │
                ┌──────────────────────▼─────────────────────────┐
                │             Stream Registry                     │
                │    streamKey → { Publisher, Subscribers,        │
                │                  CachedHeaders, Recorder }     │
                │             server/registry.go                  │
                └───────┬──────────┬──────────┬──────────────────┘
                        │          │          │
                ┌───────▼──┐ ┌────▼────┐ ┌───▼─────────────┐
                │ FLV      │ │Broadcast│ │ Relay           │
                │ Recorder │ │to Subs  │ │ Destinations    │
                │media/    │ │         │ │ relay/manager.go│
                │recorder  │ │         │ │                 │
                └──────────┘ └─────────┘ └─────────────────┘
```

**Data flow paths**[^5]:
- **RTMP**: TCP Accept → Handshake → Control Burst → Command RPC → Media Relay/Recording
- **SRT**: UDP Accept → SRT Handshake → MPEG-TS Demux → Codec Convert → Media Relay/Recording

---

## Package Map & Component Deep-Dive

### Package Summary Table

| Package | Path | Files | ~LOC | Purpose |
|---------|------|-------|------|---------|
| **handshake** | `internal/rtmp/handshake/` | 6 | 750 | RTMP v3 C0/C1/C2 ↔ S0/S1/S2 FSM |
| **chunk** | `internal/rtmp/chunk/` | 13 | 1,500 | Message ↔ chunk fragmentation/reassembly |
| **amf** | `internal/rtmp/amf/` | 16 | 600 | AMF0 binary codec (6 types) |
| **control** | `internal/rtmp/control/` | 7 | 400 | Protocol control messages (types 1-6) |
| **rpc** | `internal/rtmp/rpc/` | 16 | 600 | Command parsing & dispatch |
| **conn** | `internal/rtmp/conn/` | 5 | 500 | Connection lifecycle, read/write loops |
| **server** | `internal/rtmp/server/` | 14 | 3,000 | Listener, registry, pub/sub |
| **auth** | `internal/rtmp/server/auth/` | 6 | 500 | Pluggable authentication |
| **hooks** | `internal/rtmp/server/hooks/` | 7 | 750 | Event hooks (webhook, shell, stdio) |
| **media** | `internal/rtmp/media/` | 18 | 1,000 | Audio/video parsing, FLV recording |
| **relay** | `internal/rtmp/relay/` | 3 | 200 | Multi-destination relay |
| **metrics** | `internal/rtmp/metrics/` | 2 | 150 | Expvar counters |
| **client** | `internal/rtmp/client/` | 3 | 400 | Test/relay RTMP client |
| **srt** | `internal/srt/` | 20+ | 2,900 | SRT protocol (listener, bridge, crypto) |
| **srt/conn** | `internal/srt/conn/` | 8 | 1,200 | SRT connection state machine |
| **srt/packet** | `internal/srt/packet/` | 7 | 800 | SRT wire format |
| **srt/handshake** | `internal/srt/handshake/` | 3 | 600 | SRT v5 handshake FSM |
| **ts** | `internal/ts/` | 10 | 1,000 | MPEG-TS demuxer |
| **codec** | `internal/codec/` | 10 | 800 | H.264/H.265/AAC format converters |
| **ingress** | `internal/ingress/` | 4 | 300 | Protocol-agnostic publish manager |
| **errors** | `internal/errors/` | 2 | 185 | Domain-specific error types |
| **logger** | `internal/logger/` | 2 | 146 | Structured slog logging |
| **cmd** | `cmd/rtmp-server/` | 2 | 214 | CLI entry point |

---

### 1. Handshake (`internal/rtmp/handshake/`)

Implements the RTMP v3 simple handshake — the first phase of every RTMP connection[^6].

**Protocol flow**:
```
Client → C0 (1B: version 0x03) + C1 (1536B: timestamp + zero + random)
Server → S0 (1B) + S1 (1536B) + S2 (1536B: echo C1)
Client → C2 (1536B: echo S1)
```

**Key types**:
```go
type Handshake struct {
    state       State                // FSM: Initial→RecvC0C1→SentS0S1S2→RecvC2→Completed
    c1          [PacketSize]byte     // 1536-byte client payload
    s1          [PacketSize]byte     // 1536-byte server payload
    haveC1, haveS1, haveC2  bool
    c1Timestamp, s1Timestamp uint32
}
```

**Key constants**: `PacketSize = 1536`, `Version = 0x03`, 5-second read/write timeouts[^7].

**Notable details**: Atomic writes via `writeFull()` prevent partial buffer transmission. Deadlines are cleared after handshake completes to avoid spurious timeouts during media streaming[^8].

---

### 2. Chunk Layer (`internal/rtmp/chunk/`)

The core framing layer — fragments large messages into fixed-size chunks (default 128 bytes) that can be interleaved across multiple chunk streams (CSIDs)[^9].

**Key types**:
```go
type Message struct {
    CSID            uint32   // Chunk stream ID
    Timestamp       uint32   // Accumulated timestamp (ms)
    MessageLength   uint32   // Total payload size
    TypeID          uint8    // 8=audio, 9=video, 20=command
    MessageStreamID uint32   // Application stream ID
    Payload         []byte   // Reassembled data
}

type ChunkHeader struct {
    FMT                    uint8   // 0-3 (header compression level)
    CSID                   uint32  // 2-65599
    Timestamp              uint32
    MessageLength          uint32
    MessageTypeID          uint8
    MessageStreamID        uint32  // Little-endian on wire (RTMP quirk)
    HasExtendedTimestamp   bool    // When timestamp ≥ 0xFFFFFF
}
```

**Header compression (FMT values)**[^10]:
| FMT | Header Size | Fields Included | Use Case |
|-----|-------------|-----------------|----------|
| 0 | 11 bytes | Full header | First message on stream |
| 1 | 7 bytes | Timestamp delta + length + type | Changed message type/length |
| 2 | 3 bytes | Timestamp delta only | Same type/length, different time |
| 3 | 0 bytes | Continuation | Same message, next chunk |

**CSID encoding**: 1-byte (2-63), 2-byte (64-319), 3-byte (320-65599)[^11].

**Reader/Writer**: `Reader` reassembles interleaved chunks into complete `Message` objects. `Writer` selects optimal FMT based on previous header state and fragments messages. Both use scratch buffers to minimize allocations[^12].

---

### 3. AMF0 Codec (`internal/rtmp/amf/`)

Action Message Format version 0 — the binary serialization format used by all RTMP commands[^13].

**Supported type markers**:
| Marker | Type | Wire Format |
|--------|------|-------------|
| `0x00` | Number | 8B IEEE 754 double |
| `0x01` | Boolean | 1B (0=false, 1=true) |
| `0x02` | String | 2B length + UTF-8 |
| `0x03` | Object | Key-value pairs + `0x00 0x00 0x09` terminator |
| `0x05` | Null | No payload |
| `0x0A` | Strict Array | 4B count + elements |

**Key API**: `EncodeAll(values ...interface{}) ([]byte, error)` and `DecodeAll(data []byte) ([]interface{}, error)`[^14].

**Notable**: Object keys are sorted alphabetically for deterministic output, enabling golden vector testing[^15].

---

### 4. Control Messages (`internal/rtmp/control/`)

Protocol control messages (TypeID 1-6) manage the transport layer. Always transmitted on CSID 2, MessageStreamID 0[^16].

| Type | Message | Payload |
|------|---------|---------|
| 1 | Set Chunk Size | 4B (31-bit value) |
| 2 | Abort Message | 4B (CSID) |
| 3 | Acknowledgement | 4B (sequence number) |
| 4 | User Control | 2B event type + data |
| 5 | Window Acknowledgement Size | 4B |
| 6 | Set Peer Bandwidth | 4B bandwidth + 1B limit type |

**Handler pattern**: Mutates pointers to connection state via `Context` struct to avoid import cycles. Auto-responds to Ping Requests with Ping Responses[^17].

---

### 5. RPC Commands (`internal/rtmp/rpc/`)

Parses AMF0-encoded command messages (TypeID 20) and dispatches to registered handlers[^18].

**Command flow**: `connect` → `createStream` → `publish` (or `play`) → media streaming → `deleteStream`

**Key types**:
```go
type ConnectCommand struct {
    TransactionID  float64
    App            string
    FlashVer       string
    TcURL          string
    FourCcList     []string  // Enhanced RTMP: ["hvc1","av01","vp09"]
}

type Dispatcher struct {
    OnConnect      ConnectHandler
    OnCreateStream CreateStreamHandler
    OnPublish      PublishHandler
    OnPlay         PlayHandler
    OnDeleteStream DeleteStreamHandler
    OnCloseStream  CloseStreamHandler
}
```

**Enhanced RTMP negotiation**: The `connect` command's `fourCcList` field advertises supported Enhanced RTMP codecs, enabling H.265/AV1/VP9 signaling[^19].

**Graceful handling**: OBS/FFmpeg extensions (releaseStream, FCPublish, FCUnpublish) are logged and safely ignored. Unknown commands trigger a warn log but don't error[^20].

---

### 6. Connection Lifecycle (`internal/rtmp/conn/`)

Manages a single RTMP connection from accept through teardown[^21].

**Lifecycle stages**:
```
Accept(listener) → Handshake → Control Burst → SetMessageHandler → Start() → readLoop/writeLoop → Close()
```

**Key type**:
```go
type Connection struct {
    id             string           // "c000001" (monotonic counter)
    netConn        net.Conn
    readChunkSize  uint32           // Updated via control messages
    writeChunkSize uint32           // Atomic for cross-goroutine access
    windowAckSize  uint32
    outboundQueue  chan *chunk.Message  // Bounded: 100 messages
    onMessage      func(*chunk.Message)
    onDisconnect   func()
    ctx            context.Context
    cancel         context.CancelFunc
}
```

**Concurrency model**: One `readLoop` goroutine + one `writeLoop` goroutine per connection. The outbound queue provides backpressure (100 messages ≈ 3 seconds at 30fps). `SendMessage()` has a 200ms timeout to avoid blocking the caller[^22].

**Zombie detection**: Read deadline = 90 seconds, write deadline = 30 seconds. Timeout errors trigger connection reap[^23].

---

### 7. Server & Registry (`internal/rtmp/server/`)

The orchestration layer — TCP listener, stream registry, pub/sub coordination, and SRT integration[^24].

**Server** (`server.go`): Main entry point. Creates TCP (and optionally TLS/SRT) listeners. Runs accept loop, spawns per-connection goroutines, coordinates with registry and hooks[^25].

**Registry** (`registry.go`): Maps stream keys to `Stream` objects. Enforces single-publisher-per-stream. Manages subscriber lists and cached sequence headers for late-join[^26].

```go
type Stream struct {
    Publisher    *Connection
    Subscribers  []*Connection
    Recorder     *Recorder        // Optional FLV
    CachedHeaders                 // H.264/H.265/AV1/VP9 + AAC configs
}
```

**Media dispatch** (`media_dispatch.go`): Routes incoming audio/video messages to: (1) FLV recorder, (2) subscriber broadcast, (3) relay destinations[^27].

**SRT accept** (`srt_accept.go`): When SRT publishers connect, creates a virtual publisher adapter implementing `ingress.Publisher`, spawns a Bridge goroutine for TS demux + codec conversion, and fires publish hooks. SRT publishers appear identical to RTMP publishers from the subscriber's perspective[^28].

---

### 8. Authentication (`internal/rtmp/server/auth/`)

Pluggable token-based authentication via the `Validator` interface[^29]:

```go
type Validator interface {
    ValidatePublish(ctx context.Context, req *Request) error
    ValidatePlay(ctx context.Context, req *Request) error
}
```

| Backend | CLI Flag | Description |
|---------|----------|-------------|
| AllowAll | `-auth-mode none` | Accept all (default) |
| Token | `-auth-mode token -auth-token key=secret` | In-memory map |
| File | `-auth-mode file -auth-file tokens.json` | JSON file with live reload |
| Callback | `-auth-mode callback -auth-callback https://...` | HTTP webhook |

**URL-based tokens**: Tokens can be passed as query parameters (e.g., `rtmp://host/app/stream?token=secret`)[^30].

---

### 9. Event Hooks (`internal/rtmp/server/hooks/`)

Event-driven integration system for external services[^31].

**Events**: `publish_start`, `publish_stop`, `play_start`, `play_stop`, `stream_created`, `stream_destroyed`[^32].

| Hook Type | CLI Flag | Behavior |
|-----------|----------|----------|
| Shell | `-hook-script event=./script.sh` | Execute shell script (bash on Linux/macOS, powershell on Windows) |
| Webhook | `-hook-webhook event=https://url` | HTTP POST JSON payload |
| Stdio | `-hook-stdio-format json\|env` | Write to stdout |

**HookManager**: Enforces execution timeout (default 30s) and concurrency limit (default 10)[^33].

---

### 10. Media & Recording (`internal/rtmp/media/`)

Parses audio (TypeID 8) and video (TypeID 9) messages, detects codecs, records to FLV[^34].

**Dual-format parsing**: Supports both legacy FLV tags (4-bit SoundFormat/CodecID) and Enhanced RTMP (FourCC)[^35]:

| Format | Video Codecs | Audio Codecs |
|--------|-------------|--------------|
| Legacy | H.264 (CodecID=7), H.265 (CodecID=12) | AAC, MP3, Speex |
| Enhanced RTMP | H.265 (hvc1), AV1 (av01), VP9 (vp09), VVC (vvc1) | Opus, FLAC, AC-3, E-AC-3 |

**FLV Recorder** (`recorder.go`): Writes FLV header + tag headers + PreviousTagSize fields. Disabled gracefully on first write error[^36].

**Late-join**: Sequence headers (SPS/PPS for H.264, VPS/SPS/PPS for H.265, AudioSpecificConfig for AAC) are cached in the registry and sent to new subscribers immediately[^37].

---

### 11. Relay (`internal/rtmp/relay/`)

Multi-destination RTMP relay for forwarding live streams to CDNs or secondary servers[^38].

```go
type DestinationManager struct {
    destinations  map[string]*Destination
    mu            sync.RWMutex
    clientFactory RTMPClientFactory  // Pluggable for testing
}
```

**Behavior**: Fans out audio/video messages to all destinations in parallel via `sync.WaitGroup`. Connection failures are isolated — one failing destination doesn't affect others. Tracks metrics per destination (messages/bytes sent, drops, reconnects)[^39].

---

### 12. SRT Protocol Stack (`internal/srt/`)

Full SRT (Secure Reliable Transport) implementation over UDP for low-latency live video contribution[^40].

**Architecture**:
```
UDP Socket → Listener (multiplexed by remoteAddr+socketID)
    → Handshake (Induction + Conclusion phases)
    → Conn (Sender + Receiver + TSBPD + Reliability)
    → Bridge (TS Demux → Codec Convert → chunk.Message)
```

**Key components**:

- **Listener** (`listener.go`): Single UDP socket multiplexing multiple connections by (remoteAddr, peerSocketID) tuple[^41].
- **Handshake** (`handshake/`): SRT v5 two-phase handshake (Induction with SYN cookie, Conclusion with extensions). Negotiates TSBPD, Stream ID, encryption[^42].
- **Connection** (`conn/`): Full state machine (Connected → Closing → Closed) with Sender (retransmit buffer, RTT EWMA), Receiver (reorder buffer, loss detection), TSBPD (jitter buffer), and Reliability loop (ACK 10ms, NAK 20ms, delivery 1ms, keepalive 1s)[^43].
- **Packets** (`packet/`): 16-byte header + payload. Control types: Handshake, KeepAlive, ACK, NAK, ACKACK, Shutdown[^44].
- **Circular arithmetic** (`circular/`): 31-bit sequence numbers with wraparound-safe comparisons (half-space rule)[^45].
- **Crypto** (`crypto/`): AES-CTR cipher, even/odd KeySet, KM message parser, PBKDF2-HMAC-SHA1 key derivation, AES Key Wrap (RFC 3394). Supports AES-128/192/256 with post-handshake key rotation[^46].

**Bridge** (`bridge.go`): The SRT→RTMP conversion pipeline. Reads from SRT connection, feeds bytes to MPEG-TS demuxer, receives `MediaFrame` callbacks, converts H.264 (Annex B→AVCC), H.265 (Annex B→HEVCDecoderConfigurationRecord), AAC (ADTS→raw), and produces `chunk.Message` objects for the ingress manager[^47].

---

### 13. MPEG-TS Demuxer (`internal/ts/`)

Parses MPEG Transport Stream — the standard container for SRT streams[^48].

**Key types**:
```go
type Demuxer struct {
    streams    map[uint16]*ElementaryStream  // PID → stream descriptor
    assemblers map[uint16]*PESAssembler       // PID → frame reassembly
    handler    FrameHandler                   // Callback per complete frame
}

type MediaFrame struct {
    Stream *ElementaryStream  // Which elementary stream
    PTS    int64              // Presentation timestamp (90kHz)
    DTS    int64              // Decode timestamp
    Data   []byte             // Raw codec data (NALUs or ADTS)
    IsKey  bool               // Keyframe indicator
}
```

**State machine**: PAT (PID 0) → PMT → PES reassembly → Frame delivery[^49]. Handles partial packets between `Feed()` calls via remainder buffer. Supports H.264 (`StreamType=0x1B`), H.265 (`StreamType=0x24`), and AAC ADTS (`StreamType=0x0F`)[^50].

---

### 14. Codec Converters (`internal/codec/`)

Converts between transport-layer codec formats[^51]:

| Conversion | Direction | Purpose |
|------------|-----------|---------|
| H.264 Annex B → AVCC | TS → RTMP | Start code delimited → length-prefixed NALUs |
| H.265 Annex B → HEVC | TS → RTMP | Extract VPS/SPS/PPS, build HEVCDecoderConfigurationRecord |
| AAC ADTS → Raw | TS → RTMP | Strip ADTS framing, build AudioSpecificConfig |

**Key functions**: `SplitAnnexB()`, `ExtractSPSPPS()`, `BuildAVCSequenceHeader()`, `BuildHEVCSequenceHeader()`, `StripADTS()`, `BuildAACSequenceHeader()`[^52].

---

### 15. Ingress Manager (`internal/ingress/`)

Protocol-agnostic publish session coordinator ensuring stream key exclusivity[^53].

```go
type Publisher interface {
    StreamKey() string
    ID() string
    Protocol() string    // "rtmp" or "srt"
    RemoteAddr() string
}
```

`BeginPublish()` validates that no other publisher holds the stream key. Both RTMP and SRT publishers use this single entry point[^54].

---

### 16. Metrics (`internal/rtmp/metrics/`)

Package-level `expvar.Int` variables exposed via HTTP `/debug/vars`[^55]:

| Category | Metrics |
|----------|---------|
| Connections | `ConnectionsActive` (gauge), `ConnectionsTotal` (counter) |
| Streams | `StreamsActive` |
| Publishers | `PublishersActive`, `PublishersTotal` |
| Subscribers | `SubscribersActive`, `SubscribersTotal` |
| Media | `MessagesAudio`, `MessagesVideo`, `BytesIngested` |
| Relay | `RelayMessagesSent`, `RelayMessagesDropped`, `RelayBytesSent` |
| SRT | `SRTConnectionsActive`, `SRTConnectionsTotal`, `SRTBytesReceived` |

All operations are atomic via Go's `expvar` package[^56].

---

### 17. Error Handling (`internal/errors/`)

Domain-specific error types with Go 1.13+ unwrap chains[^57]:

| Error Type | Package |
|------------|---------|
| `ProtocolError` | Generic RTMP |
| `HandshakeError` | Connection setup |
| `ChunkError` | Chunk framing |
| `AMFError` | AMF0 codec |
| `TimeoutError` | Deadline exceeded |
| `SRTError` | SRT protocol |
| `TSError` | MPEG-TS parsing |

Helper functions: `IsProtocolError(err)`, `IsTimeout(err)` handle wrapped error chains[^58].

---

### 18. Logging (`internal/logger/`)

Structured JSON logging via Go's `log/slog` with runtime level control[^59].

**Level precedence**: CLI flag (`-log-level`) → env var (`RTMP_LOG_LEVEL`) → default (`info`)[^60].

**Context helpers**: `WithConn()`, `WithStream()`, `WithMessageMeta()` attach structured fields (`conn_id`, `stream_key`, `type_id`, `timestamp`)[^61].

---

## CLI Entry Point (`cmd/rtmp-server/`)

**`flags.go`**: Parses 25+ CLI flags covering listen addresses, TLS, SRT, logging, recording, chunking, relay, authentication, hooks, and metrics[^62].

**`main.go`**: Orchestration sequence[^63]:
1. Parse and validate flags
2. Initialize logger
3. Build auth validator
4. Create `server.Server` with config
5. Start TCP/TLS/SRT listeners
6. Start metrics HTTP server (if configured)
7. Set up SIGHUP for auth file reload
8. Block on SIGINT/SIGTERM
9. Graceful shutdown (5s timeout)

**Version injection**: `go build -ldflags "-X main.version=v0.1.4"`[^64].

---

## Testing Infrastructure

### Test Pyramid

| Layer | Count | Location | What's Tested |
|-------|-------|----------|---------------|
| **Unit tests** | ~70 files | `internal/*/` | Per-package: AMF0 encoding, chunk parsing, handshake FSM, codec conversion, control messages |
| **Integration tests** | 9 scenarios | `tests/integration/` | End-to-end with real TCP servers: handshake, chunking, commands, relay, multi-relay, H.265, metrics, TLS, quickstart |
| **Golden vectors** | 4 generators | `tests/golden/` | Binary reference fixtures for handshake, chunks, control, AMF0 wire formats |
| **Interop tests** | 4 scenarios | `tests/interop/` | FFmpeg publish/play/concurrency/recording (Bash + PowerShell) |
| **E2E scripts** | 12 scripts | `scripts/` | Cross-platform paired .sh/.ps1 scripts |

### Golden Vector System

Wire-format correctness is validated against golden binary vectors generated by dedicated Go programs[^65]:

| Generator | Build Tag | Vectors |
|-----------|-----------|---------|
| `gen_handshake_vectors.go` | `hsgen` | C0/C1/S0/S1 exchange |
| `gen_chunk_vectors.go` | `chunkgen` | FMT 0/1/3, interleaving, extended timestamps |
| `gen_control_vectors.go` | `ctrlgen` | Set Chunk Size, Window Ack, Peer Bandwidth |
| `gen_amf0_vectors.go` | `amf0gen` | Numbers, booleans, strings, objects, arrays |

**Regeneration**: `make golden-vectors` or `go run -tags amf0gen tests/golden/gen_amf0_vectors.go`[^66].

### Integration Test Matrix

| Test File | Scenarios | Key Validations |
|-----------|-----------|-----------------|
| `handshake_test.go` | 3 | Valid handshake, invalid version (0x06), truncated C1 timeout |
| `chunking_test.go` | 5 | Single chunk, multi-chunk (384B), interleaved streams, extended timestamp, chunk size change |
| `commands_test.go` | Full | connect → createStream → publish/play through live server |
| `relay_test.go` | 2 | Single pub/sub, multiple subscribers (1:3 fan-out) with byte-level verification |
| `multi_destination_relay_test.go` | 3 | Single destination, 3 destinations, failure isolation |
| `h265_test.go` | — | H.265/HEVC NAL unit extraction, VPS/SPS/PPS detection |
| `metrics_test.go` | — | Expvar HTTP endpoint serves all `rtmp_*` counters |
| `tls_test.go` | — | RTMPS with runtime-generated self-signed ECDSA certs |
| `quickstart_test.go` | — | Full lifecycle: bootstrap→handshake→commands→media |

[^67]

---

## CI/CD Workflows

Five GitHub Actions workflows provide comprehensive automation[^68]:

| Workflow | Trigger | Jobs |
|----------|---------|------|
| **test.yml** | Push/PR | Golden vectors, unit tests (3 OS × Go 1.25.1), integration (20m), interop (25m), build validation, benchmarks |
| **ci.yml** | Push to main, PRs | Format, vet, unit tests, integration, build smoke (8m) |
| **quality.yml** | Push/PR | go mod tidy, gofmt, go vet, staticcheck, govulncheck |
| **docs.yml** | site/ changes | Hugo 0.158.0 build → GitHub Pages deploy |
| **release.yml** | Tags | Cross-platform binaries + GitHub Release + changelog |

[^69]

---

## Build & Development

### Makefile Targets (Selected)

| Target | Command |
|--------|---------|
| `make build` | `go build -o rtmp-server ./cmd/rtmp-server` |
| `make test` | `go test ./...` |
| `make test-race` | `go test -race ./...` |
| `make test-integration` | `go test ./tests/integration/... -timeout 10m` |
| `make benchmark` | Benchmarks for chunk, amf, handshake packages |
| `make lint` | staticcheck |
| `make security` | govulncheck |
| `make ci` | fmt + vet + test-race |
| `make release-check` | lint + security + test + coverage |
| `make build-all` | Linux amd64/arm64, macOS arm64, Windows amd64 |

[^70]

---

## Specification Documents

The project maintains detailed specs for each feature[^71]:

| Spec | Path | Content |
|------|------|---------|
| 001 - Core RTMP | `specs/001-rtmp-server-implementation/` | Full protocol spec + wire format contracts (handshake, chunking, control, commands, AMF0, media) |
| 002 - Relay | `specs/002-rtmp-relay-feature/` | Relay design, implementation plan, gap analysis |
| 003 - Multi-Destination | `specs/003-multi-destination-relay/` | Multi-relay implementation plan |
| 004 - Token Auth | `specs/004-token-auth/` | Authentication specification |
| 005 - Error Handling | `specs/005-error-handling-benchmarks/` | Error handling + benchmark spec |
| 006 - Metrics | `specs/006-expvar-metrics/` | Expvar metrics design |
| 007 - Clustering | `specs/007-clustering-ha/` | Planned: horizontal scaling, cross-node relay |

---

## Version History

| Version | Date | Highlights |
|---------|------|------------|
| **v0.2.0** | 2025-07-14 | SRT ingest, H.265/HEVC, MPEG-TS demuxer, AES encryption, 6 SRT metrics |
| **v0.1.4** | 2026-04-10 | Enhanced RTMP (E-RTMP v2): H.265/AV1/VP9 via FourCC, enhanced audio, 27 new tests |
| **v0.1.3** | 2026-04-09 | RTMPS (TLS 1.2+), Hugo documentation site, 12 E2E scripts |
| **v0.1.2** | — | Expvar metrics, error handling improvements, performance optimizations |
| **v0.1.1** | — | FLV recording, relay, auth, hooks |
| **v0.1.0** | — | Core RTMP server: handshake, chunks, AMF0, commands, pub/sub |

[^72]

---

## Key Design Decisions

1. **Zero dependencies**: Every protocol layer implemented from scratch using only Go stdlib. No CGo, no external packages[^73].
2. **Wire format fidelity**: All multi-byte integers are big-endian except MSID in chunk headers (little-endian RTMP quirk). Golden vectors validate byte-level correctness[^74].
3. **One readLoop per connection**: Each connection gets exactly one readLoop goroutine with context cancellation and TCP deadline enforcement[^75].
4. **Bounded channels for backpressure**: `outboundQueue := make(chan *chunk.Message, 100)` prevents unbounded memory growth from slow subscribers[^76].
5. **Protocol-agnostic ingress**: The `ingress.Manager` abstraction means RTMP and SRT publishers share identical downstream paths (registry, recording, relay, hooks)[^77].
6. **Enhanced RTMP**: Full E-RTMP v2 support with FourCC codec negotiation, backward-compatible with legacy CodecID signaling[^78].

---

## Documentation Site

Hugo-based documentation site (`site/`) using the hugo-book theme, deployed to GitHub Pages[^79]:

- **User Guide**: Authentication, HLS, relay, multi-relay, recording, RTMPS, hooks, metrics
- **Developer Guide**: Architecture, design, testing, contributing, protocol reference
- **Project**: Changelog, roadmap

**Build**: `hugo --gc --minify` from `site/` directory. Requires Hugo 0.158.0+[^80].

---

## Roadmap (Planned Features)

- **Configurable backpressure**: Drop or disconnect policy for slow subscribers[^81]
- **Clustering & HA**: Horizontal scaling with cross-node relay and dual-ingest failover (spec exists at `specs/007-clustering-ha/clustering_ha.md`)[^82]
- **DVR / time-shift**: Seek into live stream history[^83]
- **Transcoding**: Server-side codec conversion (e.g., H.265 → H.264)[^84]

---

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Architecture & package structure | ✅ High | Verified by reading source code and directory structure |
| Protocol implementation details | ✅ High | Verified by reading implementation files and golden vectors |
| CLI flags and configuration | ✅ High | Verified from `flags.go` |
| Test infrastructure | ✅ High | Verified by reading test files and CI workflows |
| SRT implementation depth | ✅ High | Verified by reading all SRT subpackages |
| Line counts | ⚠️ Approximate | Derived from `wc -l` on source files; may vary by counting method |
| Roadmap/planned features | ⚠️ Medium | Based on README.md; may change |
| H.265 SRT asymmetry | ✅ High | H.265 is now supported on both RTMP and SRT paths per recent commits |

---

## Footnotes

[^1]: `go.mod:1-3` — Module `github.com/alxayo/go-rtmp`, Go 1.25.1, no require directives
[^2]: `README.md:70-91` — Feature table
[^3]: `README.md:13` — Tested with OBS Studio, FFmpeg, ffplay, VLC
[^4]: `README.md:192-194` — "No external dependencies (stdlib only)"
[^5]: `README.md:94-98` — Architecture data flow paths
[^6]: `internal/rtmp/handshake/types.go` — Handshake struct and State FSM
[^7]: `internal/rtmp/handshake/server.go` — `PacketSize = 1536`, `Version = 0x03`, 5s timeouts
[^8]: `internal/rtmp/handshake/server.go` — `writeFull()` and deadline clearing
[^9]: `internal/rtmp/chunk/header.go` — ChunkHeader struct definition
[^10]: `internal/rtmp/chunk/header.go` — FMT 0-3 header compression logic
[^11]: `internal/rtmp/chunk/header.go` — CSID encoding (1/2/3 byte forms)
[^12]: `internal/rtmp/chunk/reader.go`, `internal/rtmp/chunk/writer.go` — Reader/Writer with scratch buffers
[^13]: `internal/rtmp/amf/amf.go` — AMF0 type markers and dispatch
[^14]: `internal/rtmp/amf/amf.go` — `EncodeAll()` and `DecodeAll()` functions
[^15]: `internal/rtmp/amf/object.go` — Sorted key encoding for deterministic output
[^16]: `internal/rtmp/control/encoder.go` — CSID=2, MSID=0 for all control messages
[^17]: `internal/rtmp/control/handler.go` — Context struct, auto Ping Response
[^18]: `internal/rtmp/rpc/dispatcher.go` — Dispatcher struct and Dispatch() method
[^19]: `internal/rtmp/rpc/connect.go:91-101` — FourCcList Enhanced RTMP negotiation
[^20]: `internal/rtmp/rpc/dispatcher.go` — Unknown command handling (warn log, no error)
[^21]: `internal/rtmp/conn/conn.go` — Connection struct and lifecycle
[^22]: `internal/rtmp/conn/conn.go` — outboundQueue (100), SendMessage 200ms timeout
[^23]: `internal/rtmp/conn/conn.go` — Read deadline 90s, write deadline 30s
[^24]: `internal/rtmp/server/server.go` — Server struct and Start()
[^25]: `internal/rtmp/server/server.go` — acceptLoop, TLS listener, SRT listener
[^26]: `internal/rtmp/server/registry.go` — Stream registry with CachedHeaders
[^27]: `internal/rtmp/server/media_dispatch.go` — Media routing to recorder/broadcast/relay
[^28]: `internal/rtmp/server/srt_accept.go` — SRT virtual publisher adapter
[^29]: `internal/rtmp/server/auth/auth.go` — Validator interface
[^30]: `internal/rtmp/server/auth/url.go` — URL-based token extraction
[^31]: `internal/rtmp/server/hooks/hook.go` — Hook interface
[^32]: `internal/rtmp/server/hooks/events.go` — Event type constants
[^33]: `internal/rtmp/server/hooks/manager.go` — HookManager with timeout and concurrency
[^34]: `internal/rtmp/media/audio.go`, `internal/rtmp/media/video.go` — Audio/video parsing
[^35]: `internal/rtmp/media/video.go:82-158` — Legacy CodecID and Enhanced RTMP FourCC parsing
[^36]: `internal/rtmp/media/recorder.go` — FLV recorder with graceful disable
[^37]: `internal/rtmp/server/registry.go:238-270` — Sequence header caching for late-join
[^38]: `internal/rtmp/relay/manager.go` — DestinationManager
[^39]: `internal/rtmp/relay/destination.go` — Parallel fan-out, failure isolation, per-destination metrics
[^40]: `internal/srt/doc.go` — SRT package documentation
[^41]: `internal/srt/listener.go` — UDP multiplexed listener
[^42]: `internal/srt/handshake/listener.go` — Two-phase handshake with SYN cookie
[^43]: `internal/srt/conn/reliability.go:43-95` — ACK/NAK/TSBPD/keepalive tickers
[^44]: `internal/srt/packet/header.go` — 16-byte SRT packet header
[^45]: `internal/srt/circular/number.go` — 31-bit circular arithmetic with half-space rule
[^46]: `internal/srt/crypto/keywrap.go` — AES Key Wrap (RFC 3394)
[^47]: `internal/srt/bridge.go` — SRT→RTMP bridge pipeline
[^48]: `internal/ts/demuxer.go` — MPEG-TS Demuxer
[^49]: `internal/ts/psi.go` — PAT/PMT parsing
[^50]: `internal/ts/stream_types.go:35-39` — StreamType constants
[^51]: `internal/codec/doc.go` — Codec converter package documentation
[^52]: `internal/codec/h264.go`, `internal/codec/h265.go`, `internal/codec/aac.go` — Converter functions
[^53]: `internal/ingress/manager.go` — Manager struct
[^54]: `internal/ingress/publisher.go` — Publisher interface
[^55]: `internal/rtmp/metrics/metrics.go` — Expvar variable declarations
[^56]: `internal/rtmp/metrics/metrics.go` — All `expvar.Int` (atomic)
[^57]: `internal/errors/` — Error type definitions
[^58]: `internal/errors/` — `IsProtocolError()`, `IsTimeout()` helpers
[^59]: `internal/logger/logger.go` — Logger initialization
[^60]: `internal/logger/logger.go` — Level precedence: flag → env → default
[^61]: `internal/logger/logger.go` — WithConn, WithStream, WithMessageMeta helpers
[^62]: `cmd/rtmp-server/flags.go` — 25+ CLI flags
[^63]: `cmd/rtmp-server/main.go` — Main orchestration sequence
[^64]: `cmd/rtmp-server/main.go` — Version variable with ldflags injection
[^65]: `tests/golden/` — Golden vector generators
[^66]: `Makefile` — `golden-vectors` target
[^67]: `tests/integration/` — Integration test files
[^68]: `.github/workflows/` — CI/CD workflow files
[^69]: `.github/workflows/test.yml`, `ci.yml`, `quality.yml`, `docs.yml`, `release.yml`
[^70]: `Makefile` — Build targets
[^71]: `specs/` — Specification documents directory
[^72]: `CHANGELOG.md` — Version history
[^73]: `go.mod` — No external dependencies
[^74]: Custom instructions — Wire format fidelity convention
[^75]: Custom instructions — Concurrency pattern
[^76]: `internal/rtmp/conn/conn.go` — Bounded outbound queue
[^77]: `internal/ingress/manager.go` — Protocol-agnostic publish coordination
[^78]: `internal/rtmp/media/video.go`, `internal/rtmp/rpc/connect.go` — Enhanced RTMP support
[^79]: `site/hugo.toml` — Hugo configuration
[^80]: `.github/workflows/docs.yml` — Hugo 0.158.0 requirement
[^81]: `README.md:224` — Planned: configurable backpressure
[^82]: `specs/007-clustering-ha/clustering_ha.md` — Clustering spec
[^83]: `README.md:225` — Planned: DVR / time-shift
[^84]: `README.md:226` — Planned: transcoding

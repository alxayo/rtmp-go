# SRT Protocol Implementation Plan for go-rtmp

## Document Status

| Field | Value |
|-------|-------|
| **Author** | Copilot (Claude Opus 4.6) |
| **Project** | go-rtmp (github.com/alxayo/go-rtmp) |
| **Base Version** | v0.1.4 (tag) |
| **Go Version** | 1.25.1 |
| **Constraint** | Pure Go — zero external dependencies |

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Gap Analysis](#2-gap-analysis)
3. [Architecture Overview](#3-architecture-overview)
4. [Implementation Phases](#4-implementation-phases)
5. [Phase 1: SRT Packet Types](#phase-1-srt-packet-types)
6. [Phase 2: Circular Sequence Numbers](#phase-2-circular-sequence-numbers)
7. [Phase 3: Crypto Primitives](#phase-3-crypto-primitives)
8. [Phase 4: UDP Multiplexed Listener](#phase-4-udp-multiplexed-listener)
9. [Phase 5: SRT Handshake v5](#phase-5-srt-handshake-v5)
10. [Phase 6: Connection State Machine](#phase-6-connection-state-machine)
11. [Phase 7: Reliability Layer](#phase-7-reliability-layer-arqnaktsbpd)
12. [Phase 8: MPEG-TS Demuxer](#phase-8-mpeg-ts-demuxer)
13. [Phase 9: Ingress Lifecycle Abstraction](#phase-9-ingress-lifecycle-abstraction)
14. [Phase 10: SRT-to-RTMP Bridge](#phase-10-srt-to-rtmp-bridge)
15. [Phase 11: Server Integration](#phase-11-server-integration)
16. [Phase 12: E2E Testing & Documentation](#phase-12-e2e-testing--documentation)
17. [Phase 13 (Future): Encryption](#phase-13-future-encryption)
18. [Phase 14 (Future): HEVC/AV1 via SRT](#phase-14-future-hevcav1-via-srt)
19. [Task Sequence & Dependencies](#task-sequence--dependencies)
20. [Risk Register](#risk-register)
21. [References](#references)

---

## 1. Executive Summary

### Goal
Add SRT (Secure Reliable Transport) ingest support to go-rtmp so that publishers can send
MPEG-TS streams over SRT (e.g., from FFmpeg or OBS) and the server routes media into the
existing RTMP stream registry. RTMP subscribers receive the media transparently.

### Scope (MVP — v0.2.0)
- **SRT Listener mode only** (server receives streams; no Caller or Rendezvous)
- **Live streaming mode only** (no file transfer / buffer mode)
- **Plaintext only** (no encryption — deferred to Phase 13)
- **H.264 + AAC only** (HEVC/AV1 deferred to Phase 14)
- **Stream ID routing** for stream key mapping
- **FFmpeg and OBS interop** as validation targets

### Scope (Future — v0.3.0+)
- AES-128/192/256 encryption with PBKDF2 + AES Key Wrap
- HEVC (H.265), AV1, VP9 via Enhanced RTMP bridge
- SRT playback (subscribers pull via SRT)
- TS muxer for SRT egress
- Connection bonding, FEC

### Estimated Effort
- **MVP**: ~8,000–10,000 LOC (implementation + tests)
- **12 phases, ~35 commits**
- **Pure Go, zero dependencies** — all crypto via `crypto/*` stdlib

---

## 2. Gap Analysis

### 2.1 What Exists Today

| Component | Location | Description |
|-----------|----------|-------------|
| TCP Listener | `server/server.go` | Plain + TLS listeners, accept loop, connection tracking |
| RTMP Handshake | `handshake/` | FSM-based C0/C1/C2 ↔ S0/S1/S2 exchange |
| Chunk Protocol | `chunk/` | FMT 0-3 reader/writer, extended timestamps |
| AMF0 Codec | `amf/` | Number, Boolean, String, Object, Null, Array |
| RPC Commands | `rpc/` | connect, createStream, publish, play |
| Connection | `conn/` | readLoop/writeLoop, deadlines, outbound queue |
| Stream Registry | `server/registry.go` | Pub/sub map, sequence header caching, broadcast |
| Media Pipeline | `media/` | Codec detection, FLV recording, relay broadcast |
| Enhanced RTMP | `media/video.go`, `media/audio.go` | IsExHeader, FourCC, H.265/AV1/VP9 parsing |
| Relay | `relay/` | Multi-destination RTMP relay |
| Auth | `server/auth/` | Token, file, callback validators |
| Hooks | `server/hooks/` | Shell, webhook, stdio event hooks |
| Metrics | `metrics/` | expvar counters (connections, publishers, subscribers) |
| Errors | `errors/` | Domain-specific error wrappers with classification |
| CLI | `cmd/rtmp-server/` | Flag parsing, server bootstrap |

### 2.2 What Is Missing for SRT

| Gap | Description | Complexity | Phase |
|-----|-------------|------------|-------|
| **SRT Packet Format** | Binary marshal/unmarshal for 16-byte SRT headers, data packets, 10 control packet types, handshake CIF with extensions | High | 1 |
| **Sequence Number Arithmetic** | 31-bit circular numbers with wraparound comparison, distance, range operations | Low | 2 |
| **Crypto Primitives** | AES Key Wrap (RFC 3394), PBKDF2 (stdlib), AES-CTR — all stdlib-only | Medium | 3 |
| **UDP Multiplexed Listener** | Single UDP socket serving multiple SRT connections, demuxing by remote addr + socket ID | High | 4 |
| **SRT Handshake v5** | Induction → Conclusion with HSREQ/HSRSP/SID extensions, SYN cookie anti-amplification | High | 5 |
| **Connection State Machine** | Lifecycle states, send/receive queues, timers, context cancellation, read/write APIs | High | 6 |
| **Reliability (ARQ)** | ACK/NAK/ACKACK, retransmission queues, loss detection, periodic NAK, TSBPD, too-late drop | Very High | 7 |
| **MPEG-TS Demuxer** | 188-byte packet parser, PAT/PMT/PES, continuity counters, adaptation fields | Medium | 8 |
| **ES→RTMP Conversion** | H.264 Annex B → AVCC, AAC ADTS → AudioSpecificConfig, DTS→RTMP timestamp, composition time | High | 10 |
| **Ingress Abstraction** | Protocol-agnostic publish lifecycle (begin/push/end) with auth, hooks, metrics | Medium | 9 |
| **Server Integration** | SRT config flags, optional UDP listener, SRT accept loop, bridge wiring | Medium | 11 |
| **SRT Error Types** | New error wrappers: SRTHandshakeError, SRTPacketError, TSError | Low | 1 |

### 2.3 Existing Patterns to Follow

| Pattern | How It's Used Today | How SRT Will Use It |
|---------|-------------------|-------------------|
| **Dual Listener** | TLS listener parallel to TCP | SRT UDP listener parallel to TCP+TLS |
| **Accept Loop** | `acceptLoop(l net.Listener)` per listener | `srtAcceptLoop()` on UDP socket |
| **Connection Tracking** | `conns map[string]*iconn.Connection` | Same map, SRT connections get unique IDs |
| **Disconnect Handler** | `c.SetDisconnectHandler(func(){...})` | SRT bridge fires same disconnect lifecycle |
| **Media Dispatch** | `dispatchMedia(m, st, reg, destMgr, log)` | SRT bridge calls same dispatch path |
| **Codec Detection** | `CodecDetector.Process(typeID, payload, store, log)` | SRT bridge constructs RTMP-format payloads |
| **Sequence Headers** | `IsVideoSequenceHeader()` / `IsAudioSequenceHeader()` | Bridge generates RTMP sequence headers from TS |
| **Error Wrapping** | `rerrors.NewHandshakeError("op", err)` | `rerrors.NewSRTError("op", err)` |
| **Metrics** | `metrics.ConnectionsActive.Add(1)` | `metrics.SRTConnectionsActive.Add(1)` |
| **Hook Events** | `EventConnectionAccept`, `EventPublishStart` | Same events from SRT lifecycle |

### 2.4 Standard Library Availability

| Capability | Package | Status |
|------------|---------|--------|
| UDP sockets | `net` | ✅ Available |
| AES block cipher | `crypto/aes` | ✅ Available |
| AES-CTR stream cipher | `crypto/cipher` | ✅ Available |
| PBKDF2 key derivation | `crypto/pbkdf2` | ✅ Available (Go 1.24+) |
| HMAC | `crypto/hmac` | ✅ Available |
| SHA-1 | `crypto/sha1` | ✅ Available |
| Crypto random | `crypto/rand` | ✅ Available |
| Big-endian encoding | `encoding/binary` | ✅ Available |
| AES Key Wrap (RFC 3394) | — | ❌ Must implement (~100 LOC) |
| SRT protocol | — | ❌ Must implement (~6000+ LOC) |
| MPEG-TS demuxer | — | ❌ Must implement (~600+ LOC) |
| H.264 Annex B parser | — | ❌ Must implement (~200 LOC) |

---

## 3. Architecture Overview

### 3.1 High-Level Data Flow

```
                         ┌─────────────────────────────────────────────┐
                         │              go-rtmp Server                 │
                         │                                             │
  RTMP/TCP ─────────────►│  ┌──────────┐                               │
                         │  │  RTMP    │                               │
  RTMPS/TLS ────────────►│  │  Accept  │──► chunk.Message ──┐          │
                         │  │  Loop    │                    │          │
                         │  └──────────┘                    ▼          │
                         │                            ┌──────────┐     │
                         │                            │ Ingress  │     │
  SRT/UDP ──────────────►│  ┌──────────┐              │ Manager  │     │
                         │  │  SRT     │──► TS Demux  │          │     │
                         │  │  Accept  │   ──► AVCC   │ • auth   │     │
                         │  │  Loop    │   ──► chunk  │ • hooks  │     │
                         │  └──────────┘      .Message│ • metrics│     │
                         │                            │ • record │     │
                         │                            │ • relay  │     │
                         │                            └────┬─────┘     │
                         │                                 │           │
                         │                                 ▼           │
                         │                          ┌──────────┐       │
                         │                          │ Stream   │       │
                         │                          │ Registry │       │
                         │                          │          │       │
                         │                          │ pub/sub  │       │
                         │                          └────┬─────┘       │
                         │                               │             │
                         │              ┌────────────────┼──────┐      │
                         │              ▼                ▼      ▼      │
                         │         Subscriber      Subscriber  Relay   │
                         │         (RTMP)          (RTMP)      Dest    │
                         └─────────────────────────────────────────────┘
```

### 3.2 Package Layout

```
internal/
├── errors/
│   └── errors.go                  # EXISTING — add SRTError, TSError types
│
├── srt/                           # NEW — SRT protocol implementation
│   ├── doc.go                     # Package documentation
│   │
│   ├── packet/                    # Wire format (marshal/unmarshal)
│   │   ├── header.go              # 16-byte SRT header (data + control)
│   │   ├── data.go                # Data packet structure
│   │   ├── control.go             # Control packet types
│   │   ├── handshake.go           # Handshake CIF + extensions
│   │   ├── ack.go                 # ACK CIF structure
│   │   ├── nak.go                 # NAK loss report encoding
│   │   └── *_test.go              # Wire format golden tests
│   │
│   ├── circular/                  # Sequence number arithmetic
│   │   ├── number.go              # 31-bit wraparound comparisons
│   │   └── number_test.go
│   │
│   ├── crypto/                    # Encryption primitives (Phase 13)
│   │   ├── keywrap.go             # RFC 3394 AES Key Wrap (stdlib only)
│   │   ├── keywrap_test.go
│   │   ├── pbkdf2.go              # PBKDF2 wrapper (uses crypto/pbkdf2)
│   │   └── ctr.go                 # AES-CTR encrypt/decrypt
│   │
│   ├── conn/                      # Connection state machine
│   │   ├── conn.go                # SRT connection lifecycle
│   │   ├── sender.go              # Send-side buffer + retransmit
│   │   ├── receiver.go            # Receive-side buffer + TSBPD
│   │   ├── timers.go              # ACK/NAK/keepalive timers
│   │   └── *_test.go
│   │
│   ├── handshake/                 # Handshake protocol
│   │   ├── listener.go            # Server-side handshake FSM
│   │   ├── cookie.go              # SYN cookie generation/validation
│   │   ├── extensions.go          # HSREQ/HSRSP/SID/KM marshaling
│   │   └── *_test.go
│   │
│   ├── listener.go                # UDP multiplexed listener
│   ├── config.go                  # SRT configuration
│   ├── stream_id.go               # Stream ID parser (SRT Access Control)
│   └── stream_id_test.go
│
├── ts/                            # NEW — MPEG-TS demuxer
│   ├── doc.go
│   ├── packet.go                  # 188-byte TS packet parser
│   ├── psi.go                     # PAT/PMT table parser
│   ├── pes.go                     # PES reassembly + timestamp extraction
│   ├── demuxer.go                 # High-level demuxer (event-driven)
│   ├── stream_types.go            # Codec identification constants
│   └── *_test.go
│
├── codec/                         # NEW — elementary stream → RTMP conversion
│   ├── doc.go
│   ├── h264.go                    # Annex B → AVCC + sequence header
│   ├── aac.go                     # ADTS → AudioSpecificConfig + RTMP header
│   ├── nalu.go                    # NALU splitter (start code detection)
│   └── *_test.go
│
├── ingress/                       # NEW — protocol-agnostic publish lifecycle
│   ├── doc.go
│   ├── manager.go                 # Ingress lifecycle manager
│   ├── publisher.go               # Virtual publisher interface
│   └── *_test.go
│
└── rtmp/                          # EXISTING — unchanged except minor additions
    ├── server/
    │   ├── server.go              # MODIFIED — add SRT listener startup
    │   ├── srt_accept.go          # NEW — SRT accept loop + bridge wiring
    │   └── ...
    └── ...

cmd/rtmp-server/
├── flags.go                       # MODIFIED — add SRT CLI flags
└── main.go                        # MODIFIED — pass SRT config to server
```

### 3.3 Key Type: chunk.Message (Existing — The Internal Media Unit)

```go
// internal/rtmp/chunk/stub.go — UNCHANGED
type Message struct {
    CSID            uint32 // Chunk Stream ID
    Timestamp       uint32 // Message timestamp in milliseconds
    MessageLength   uint32 // Total payload length
    TypeID          uint8  // 8=audio, 9=video, 20=AMF0 command
    MessageStreamID uint32 // 0=control, 1+=media
    Payload         []byte // Raw FLV tag body
}
```

The SRT bridge's job is to produce `chunk.Message` values with correct RTMP-format payloads
from MPEG-TS elementary streams. This is the protocol boundary.

---

## 4. Implementation Phases

| Phase | Name | Commits | Est. LOC | Dependencies |
|-------|------|---------|----------|--------------|
| 1 | SRT Packet Types | 3 | ~900 | None |
| 2 | Circular Sequence Numbers | 1 | ~200 | None |
| 3 | Crypto Primitives | 2 | ~250 | None |
| 4 | UDP Multiplexed Listener | 2 | ~500 | None |
| 5 | SRT Handshake v5 | 3 | ~800 | 1, 2, 4 |
| 6 | Connection State Machine | 3 | ~1000 | 1, 2, 5 |
| 7 | Reliability (ARQ/NAK/TSBPD) | 4 | ~1500 | 2, 6 |
| 8 | MPEG-TS Demuxer | 3 | ~700 | None |
| 9 | Ingress Lifecycle Abstraction | 2 | ~400 | None |
| 10 | SRT-to-RTMP Bridge | 3 | ~800 | 7, 8, 9 |
| 11 | Server Integration | 3 | ~500 | 10 |
| 12 | E2E Testing & Documentation | 4 | ~600 | 11 |
| 13 | (Future) Encryption | 3 | ~600 | 3, 5 |
| 14 | (Future) HEVC/AV1 via SRT | 2 | ~400 | 10, E-RTMP |
| **Total MVP (1-12)** | | **~31** | **~7,900** | |

---

## Phase 1: SRT Packet Types

### Goal
Implement binary marshal/unmarshal for all SRT packet types needed for live streaming.

### Files
```
internal/srt/packet/header.go
internal/srt/packet/data.go
internal/srt/packet/control.go
internal/srt/packet/handshake.go
internal/srt/packet/ack.go
internal/srt/packet/nak.go
internal/srt/packet/header_test.go
internal/srt/packet/data_test.go
internal/srt/packet/control_test.go
internal/srt/packet/handshake_test.go
internal/srt/doc.go
internal/errors/errors.go          # Add SRTError, TSError
```

### Design

#### 1a. SRT Header (header.go)

```go
package packet

import "encoding/binary"

// HeaderSize is the fixed size of an SRT packet header in bytes.
const HeaderSize = 16

// PacketType distinguishes data from control packets.
type PacketType uint8

const (
    PacketTypeData    PacketType = 0 // F=0
    PacketTypeControl PacketType = 1 // F=1
)

// Header represents the common 16-byte SRT packet header.
// The first bit (F) determines whether the remaining fields
// are interpreted as data or control packet fields.
type Header struct {
    IsControl       bool   // F bit: false=data, true=control
    Timestamp       uint32 // Microseconds relative to connection start
    DestSocketID    uint32 // Peer's socket ID (0 during initial handshake)
}

// ParseHeader reads the F bit, timestamp (bytes 8-11), and
// destination socket ID (bytes 12-15) from a 16-byte buffer.
func ParseHeader(buf []byte) Header { ... }
```

#### 1b. Data Packet (data.go)

```go
// PacketPosition indicates where this packet sits in a message.
type PacketPosition uint8

const (
    PositionMiddle PacketPosition = 0b00 // Middle of message
    PositionLast   PacketPosition = 0b01 // Last packet of message
    PositionFirst  PacketPosition = 0b10 // First packet of message
    PositionSolo   PacketPosition = 0b11 // Single-packet message
)

// EncryptionFlag indicates the encryption key status.
type EncryptionFlag uint8

const (
    EncryptionNone EncryptionFlag = 0b00
    EncryptionEven EncryptionFlag = 0b01
    EncryptionOdd  EncryptionFlag = 0b10
)

// DataPacket represents an SRT data packet.
type DataPacket struct {
    Header
    SequenceNumber uint32         // 31 bits [0, 2^31-1]
    Position       PacketPosition // PP: 2 bits
    InOrder        bool           // O: 1 bit
    Encryption     EncryptionFlag // KK: 2 bits
    Retransmitted  bool           // R: 1 bit
    MessageNumber  uint32         // 26 bits
    Payload        []byte         // Variable length
}

// MarshalBinary serializes the data packet to wire format.
func (d *DataPacket) MarshalBinary() ([]byte, error) { ... }

// UnmarshalDataPacket parses a data packet from raw UDP payload.
func UnmarshalDataPacket(buf []byte) (*DataPacket, error) { ... }
```

#### 1c. Control Packet Types (control.go)

```go
// ControlType identifies the type of SRT control packet.
type ControlType uint16

const (
    CtrlHandshake  ControlType = 0x0000
    CtrlKeepAlive  ControlType = 0x0001
    CtrlACK        ControlType = 0x0002
    CtrlNAK        ControlType = 0x0003
    CtrlCongestion ControlType = 0x0004
    CtrlShutdown   ControlType = 0x0005
    CtrlACKACK     ControlType = 0x0006
    CtrlDropReq    ControlType = 0x0007
    CtrlPeerError  ControlType = 0x0008
)

// ControlPacket is the generic control packet envelope.
type ControlPacket struct {
    Header
    Type         ControlType // 15 bits
    Subtype      uint16      // 16 bits (usually 0)
    TypeSpecific uint32      // 32 bits (meaning varies by type)
    CIF          []byte      // Control Information Field (variable)
}

// MarshalBinary serializes the control packet.
func (c *ControlPacket) MarshalBinary() ([]byte, error) { ... }

// UnmarshalControlPacket parses a control packet from raw UDP payload.
func UnmarshalControlPacket(buf []byte) (*ControlPacket, error) { ... }
```

#### 1d. Handshake CIF (handshake.go)

```go
// HandshakeType identifies the handshake phase.
type HandshakeType uint32

const (
    HSTypeWaveAHand  HandshakeType = 0x00000000
    HSTypeInduction  HandshakeType = 0x00000001
    HSTypeConclusion HandshakeType = 0xFFFFFFFF
    HSTypeAgreement  HandshakeType = 0xFFFFFFFE
    HSTypeDone       HandshakeType = 0xFFFFFFFD
)

// HandshakeCIF is the Control Information Field for handshake packets.
// Fixed 48 bytes + variable extensions.
type HandshakeCIF struct {
    Version          uint32        // 4 or 5
    EncryptionField  uint16        // 0=none, 2=AES-128, 3=AES-192, 4=AES-256
    ExtensionField   uint16        // Bitmask: HSREQ=1, KMREQ=2, CONFIG=4
    InitialSeqNumber uint32        // 31 bits
    MTU              uint32        // Max transmission unit (typically 1500)
    FlowWindow       uint32        // Max in-flight packets
    Type             HandshakeType // Handshake phase
    SocketID         uint32        // Sender's socket ID
    SYNCookie        uint32        // Anti-amplification cookie
    PeerIP           [16]byte      // IPv4 (4 bytes + 12 zeros) or IPv6
    Extensions       []HSExtension // Variable-length extensions (v5)
}

// HSExtension represents a single handshake extension.
type HSExtension struct {
    Type    uint16 // Extension type (HSREQ=1, HSRSP=2, KMREQ=3, etc.)
    Length  uint16 // Length in 4-byte blocks
    Content []byte // Extension payload
}

// MarshalBinary serializes the handshake CIF including extensions.
func (h *HandshakeCIF) MarshalBinary() ([]byte, error) { ... }

// UnmarshalHandshakeCIF parses a handshake CIF from raw bytes.
func UnmarshalHandshakeCIF(buf []byte) (*HandshakeCIF, error) { ... }
```

#### 1e. ACK CIF (ack.go)

```go
// ACKCIF carries acknowledgement information.
type ACKCIF struct {
    LastACKPacketSeq uint32 // Last acknowledged packet sequence number
    RTT              uint32 // Round-trip time (microseconds)
    RTTVariance      uint32 // RTT variance (microseconds)
    AvailableBuffer  uint32 // Available buffer size (packets)
    PacketsReceiving uint32 // Receiving rate (packets/second)
    EstBandwidth     uint32 // Estimated link bandwidth (packets/second)
    ReceivingRate    uint32 // Receiving rate (bytes/second)
}

func (a *ACKCIF) MarshalBinary() ([]byte, error) { ... }
func UnmarshalACKCIF(buf []byte) (*ACKCIF, error) { ... }
```

#### 1f. NAK Loss Report (nak.go)

```go
// NAK encodes lost packet sequence numbers as ranges.
// Format: single loss = [seqno], range = [seqno|0x80000000, seqno_end]
// This matches the SRT RFC Appendix A "Packet Sequence List Coding".

// EncodeLossRanges serializes a list of lost sequence number ranges.
func EncodeLossRanges(ranges [][2]uint32) []byte { ... }

// DecodeLossRanges parses a NAK CIF into sequence number ranges.
func DecodeLossRanges(buf []byte) [][2]uint32 { ... }
```

#### 1g. Error Types (errors.go addition)

```go
// SRTError wraps errors from the SRT protocol layer.
type SRTError struct {
    Op  string
    Err error
}

// TSError wraps errors from the MPEG-TS demuxer.
type TSError struct {
    Op  string
    Err error
}
```

### Commits

| # | Message | Files |
|---|---------|-------|
| 1 | `feat(srt): add SRT packet header and data packet types` | `packet/header.go`, `packet/data.go`, `packet/*_test.go`, `srt/doc.go` |
| 2 | `feat(srt): add SRT control packet types and handshake CIF` | `packet/control.go`, `packet/handshake.go`, `packet/ack.go`, `packet/nak.go`, `packet/*_test.go` |
| 3 | `feat(errors): add SRTError and TSError domain error types` | `errors/errors.go`, `errors/errors_test.go` |

### Testing Strategy
- Golden binary vectors for each packet type (similar to AMF0 golden tests)
- Round-trip: marshal → unmarshal → compare
- Known FFmpeg/OBS handshake captures as test vectors
- Edge cases: maximum sequence numbers, empty CIF, extension boundary

---

## Phase 2: Circular Sequence Numbers

### Goal
Implement 31-bit circular arithmetic for SRT sequence number comparison, distance, and range operations.

### Files
```
internal/srt/circular/number.go
internal/srt/circular/number_test.go
```

### Design

```go
package circular

// Number represents a 31-bit SRT sequence number with wraparound arithmetic.
// Range: [0, 2^31 - 1]. Comparisons account for the circular number space.
type Number uint32

const (
    MaxVal  Number = 0x7FFFFFFF // 2^31 - 1
    HalfMax Number = 0x40000000 // 2^30 — the midpoint for "is A before B"
)

// New creates a circular number from a raw uint32, masking to 31 bits.
func New(v uint32) Number { return Number(v & uint32(MaxVal)) }

// Inc returns n + 1 (with wraparound).
func (n Number) Inc() Number { return New(uint32(n) + 1) }

// Add returns n + delta (with wraparound).
func (n Number) Add(delta uint32) Number { return New(uint32(n) + delta) }

// Distance returns the forward distance from n to other in the circular space.
// Always non-negative. If other == n, returns 0.
func (n Number) Distance(other Number) uint32 {
    if other >= n {
        return uint32(other - n)
    }
    return uint32(MaxVal) - uint32(n) + uint32(other) + 1
}

// Before returns true if n comes before other in the circular space.
// Uses the "half-space" rule: n is before other if the forward
// distance from n to other is less than half the number space.
func (n Number) Before(other Number) bool {
    diff := New(uint32(other) - uint32(n))
    return diff > 0 && diff < HalfMax
}

// After returns true if n comes after other in the circular space.
func (n Number) After(other Number) bool { return other.Before(n) }

// InRange returns true if n is in the range [lo, hi] (inclusive, circular).
func (n Number) InRange(lo, hi Number) bool {
    if lo.Before(hi) || lo == hi {
        return (n == lo || lo.Before(n)) && (n == hi || n.Before(hi))
    }
    // Wraps around: [lo..MaxVal] ∪ [0..hi]
    return n == lo || lo.Before(n) || n == hi || n.Before(hi)
}
```

### Commits

| # | Message | Files |
|---|---------|-------|
| 4 | `feat(srt): add 31-bit circular sequence number arithmetic` | `circular/number.go`, `circular/number_test.go` |

### Testing Strategy
- Wraparound boundary: `MaxVal - 1`, `MaxVal`, `0`, `1`
- Distance across wrap: `Distance(MaxVal - 5, 3)` = 9
- Before/After across wrap
- InRange with wrapping range

---

## Phase 3: Crypto Primitives

### Goal
Implement RFC 3394 AES Key Wrap using only `crypto/aes` (stdlib). Prepare PBKDF2 wrapper
using `crypto/pbkdf2` (stdlib since Go 1.24). These are needed for Phase 13 (encryption)
but are self-contained and can be built/tested independently.

### Files
```
internal/srt/crypto/keywrap.go
internal/srt/crypto/keywrap_test.go
internal/srt/crypto/pbkdf2.go
```

### Design: AES Key Wrap (RFC 3394)

```go
package crypto

import (
    "crypto/aes"
    "encoding/binary"
    "errors"
)

// DefaultIV is the default Initial Value defined in RFC 3394 Section 2.2.3.1.
var DefaultIV = [8]byte{0xA6, 0xA6, 0xA6, 0xA6, 0xA6, 0xA6, 0xA6, 0xA6}

// ErrIntegrityCheck is returned when unwrapping detects integrity failure.
var ErrIntegrityCheck = errors.New("aes key wrap: integrity check failed")

// Wrap performs AES Key Wrap per RFC 3394.
// kek must be 16, 24, or 32 bytes (AES-128/192/256).
// plaintext must be a multiple of 8 bytes, minimum 16 bytes.
func Wrap(kek, plaintext []byte) ([]byte, error) {
    block, err := aes.NewCipher(kek)
    if err != nil {
        return nil, err
    }
    n := len(plaintext) / 8
    // ... RFC 3394 Section 2.2.1 algorithm ...
    // 6 rounds, n iterations each
    // Uses AES ECB (single-block encrypt) on [A || R[i]]
    return ciphertext, nil
}

// Unwrap performs AES Key Unwrap per RFC 3394.
func Unwrap(kek, ciphertext []byte) ([]byte, error) {
    block, err := aes.NewCipher(kek)
    if err != nil {
        return nil, err
    }
    // ... RFC 3394 Section 2.2.2 algorithm ...
    // Verify A == DefaultIV after unwrapping
    if !bytes.Equal(a[:], DefaultIV[:]) {
        return nil, ErrIntegrityCheck
    }
    return plaintext, nil
}
```

### Design: PBKDF2 Wrapper

```go
package crypto

import (
    "crypto/pbkdf2"
    "crypto/sha1"
)

// DeriveKey uses PBKDF2-HMAC-SHA1 to derive a key from a passphrase.
// SRT uses 2048 iterations by default.
func DeriveKey(passphrase string, salt []byte, keyLen int) []byte {
    return pbkdf2.Key(sha1.New, passphrase, salt, 2048, keyLen)
}
```

### Commits

| # | Message | Files |
|---|---------|-------|
| 5 | `feat(srt/crypto): implement RFC 3394 AES Key Wrap with stdlib` | `crypto/keywrap.go`, `crypto/keywrap_test.go` |
| 6 | `feat(srt/crypto): add PBKDF2 key derivation wrapper` | `crypto/pbkdf2.go` |

### Testing Strategy
- RFC 3394 test vectors from the RFC itself (Section 4)
- Round-trip: Wrap → Unwrap == original
- Invalid KEK lengths
- Tampered ciphertext → ErrIntegrityCheck

---

## Phase 4: UDP Multiplexed Listener

### Goal
Build a UDP listener that multiplexes multiple SRT connections on a single UDP port,
routing packets to the correct connection by `(remoteAddr, socketID)` pair.

### Files
```
internal/srt/listener.go
internal/srt/listener_test.go
internal/srt/config.go
```

### Design

```go
package srt

import (
    "net"
    "sync"
)

// connKey uniquely identifies an SRT connection by its remote address
// and SRT socket ID. During pre-handshake (socketID=0), only the
// remote address is used for matching.
type connKey struct {
    addr     string // Remote IP:port
    socketID uint32 // Peer's socket ID (0 = handshake phase)
}

// Listener accepts SRT connections on a single UDP port.
// Unlike TCP, all connections share one UDP socket. The listener
// demultiplexes incoming packets by examining the source address
// and SRT destination socket ID header field.
type Listener struct {
    udpConn  *net.UDPConn
    mu       sync.RWMutex
    sessions map[connKey]*pendingConn // Pre-handshake sessions
    conns    map[connKey]*Conn        // Established connections
    config   Config
    closing  bool
    log      *slog.Logger
}

// Config holds SRT-specific listener configuration.
type Config struct {
    ListenAddr string // UDP bind address (e.g. ":10080")
    Latency    int    // TSBPD latency in milliseconds (default 120)
    MTU        int    // Maximum transmission unit (default 1500)
    FlowWindow int   // Max in-flight packets (default 8192)
    Passphrase string // Encryption passphrase (empty = plaintext)
    PbKeyLen   int    // AES key size: 0, 16, 24, 32 (default 0 = no encryption)
}

// Listen creates and starts an SRT listener on the given UDP address.
func Listen(addr string, cfg Config) (*Listener, error) {
    udpAddr, err := net.ResolveUDPAddr("udp", addr)
    if err != nil { return nil, err }
    udpConn, err := net.ListenUDP("udp", udpAddr)
    if err != nil { return nil, err }
    // ...
    return l, nil
}

// readLoop runs forever, reading UDP datagrams and dispatching them.
func (l *Listener) readLoop() {
    buf := make([]byte, l.config.MTU)
    for {
        n, remoteAddr, err := l.udpConn.ReadFromUDP(buf)
        if err != nil {
            if l.closing { return }
            continue
        }
        l.dispatch(buf[:n], remoteAddr)
    }
}

// dispatch routes an incoming UDP packet to the correct handler:
// 1. If it's a handshake packet (socketID=0), route to handshake handler
// 2. If socketID matches an established connection, route to that conn
// 3. Otherwise, discard (unknown peer)
func (l *Listener) dispatch(data []byte, from *net.UDPAddr) { ... }

// Accept blocks until a new SRT connection completes its handshake.
// Returns a ConnRequest that can be inspected (stream ID, etc.)
// and accepted or rejected, following the gosrt pattern.
func (l *Listener) Accept() (*ConnRequest, error) { ... }

// Close shuts down the listener and all active connections.
func (l *Listener) Close() error { ... }
```

### Key Design Decision: Packet Dispatch

```
UDP datagram arrives
  ├─ Parse SRT header (16 bytes minimum)
  ├─ Extract: F bit, destSocketID, source address
  │
  ├─ destSocketID == 0 (pre-handshake)?
  │   └─ Route to handshake handler (keyed by remoteAddr only)
  │
  ├─ Lookup connKey{addr, destSocketID} in established conns?
  │   └─ Route to conn.recvPacket(data)
  │
  └─ Unknown → discard (or log at debug level)
```

### Commits

| # | Message | Files |
|---|---------|-------|
| 7 | `feat(srt): add SRT configuration types` | `config.go` |
| 8 | `feat(srt): implement UDP multiplexed listener with packet dispatch` | `listener.go`, `listener_test.go` |

---

## Phase 5: SRT Handshake v5

### Goal
Implement the Caller-Listener handshake protocol with v5 extensions (HSREQ/HSRSP, Stream ID).
This is the server-side (listener) implementation — we receive handshakes from callers (FFmpeg/OBS).

### Files
```
internal/srt/handshake/listener.go
internal/srt/handshake/cookie.go
internal/srt/handshake/extensions.go
internal/srt/handshake/listener_test.go
internal/srt/handshake/extensions_test.go
internal/srt/stream_id.go
internal/srt/stream_id_test.go
```

### Design: Handshake Flow (Listener Side)

```
Caller                              Listener (us)
  │                                      │
  │  INDUCTION (v4 format, cookie=0)     │
  │─────────────────────────────────────►│
  │                                      │  Generate SYN cookie
  │  INDUCTION response (v5, cookie=X)   │  Set extension=0x0005 (HSREQ|CONFIG)
  │◄─────────────────────────────────────│
  │                                      │
  │  CONCLUSION (v5, cookie=X,           │
  │    extensions: HSREQ + SID)          │
  │─────────────────────────────────────►│
  │                                      │  Validate cookie
  │                                      │  Parse HSREQ (SRT version, flags, delays)
  │                                      │  Parse SID (stream ID for routing)
  │  CONCLUSION response                 │  Negotiate TSBPD, flags
  │    (extensions: HSRSP)               │
  │◄─────────────────────────────────────│
  │                                      │
  │  ══════ Connection Established ══════│
```

### Design: SYN Cookie (cookie.go)

```go
package handshake

import (
    "crypto/hmac"
    "crypto/sha1"
    "encoding/binary"
    "time"
)

// SYN cookies prevent connection-state exhaustion attacks.
// Cookie = HMAC-SHA1(secret, remoteIP || remotePort || timestamp_bucket)
// truncated to 32 bits.

var cookieSecret []byte // Randomly generated at listener startup

// GenerateCookie creates a SYN cookie for the given remote address.
func GenerateCookie(remoteAddr *net.UDPAddr) uint32 { ... }

// ValidateCookie checks if a cookie is valid for the given address.
// Checks current and previous time buckets (30s each) for clock skew.
func ValidateCookie(cookie uint32, remoteAddr *net.UDPAddr) bool { ... }
```

### Design: HSREQ/HSRSP Extensions (extensions.go)

```go
// HSReqData contains the HSREQ extension payload (12 bytes).
type HSReqData struct {
    SRTVersion   uint32 // major*0x10000 + minor*0x100 + patch
    SRTFlags     uint32 // Bitmask: TSBPDSND, TSBPDRCV, CRYPT, TLPKTDROP, etc.
    RecvTSBPD    uint16 // Receiver TSBPD delay (ms)
    SenderTSBPD  uint16 // Sender TSBPD delay (ms)
}

// SRT Flag constants
const (
    FlagTSBPDSND    uint32 = 0x00000001
    FlagTSBPDRCV    uint32 = 0x00000002
    FlagCRYPT       uint32 = 0x00000004
    FlagTLPKTDROP   uint32 = 0x00000008
    FlagPERIODICNAK uint32 = 0x00000010
    FlagREXMITFLG   uint32 = 0x00000020
    FlagSTREAM      uint32 = 0x00000040
)

// ParseHSReq parses an HSREQ extension from raw bytes.
func ParseHSReq(data []byte) (*HSReqData, error) { ... }

// BuildHSRsp constructs an HSRSP response.
func BuildHSRsp(localVersion uint32, flags uint32, recvDelay, sendDelay uint16) []byte { ... }

// ParseStreamID extracts a Stream ID string from an SID extension.
// Note: SRT stores stream ID as 32-bit little-endian words (per RFC).
func ParseStreamID(data []byte) string { ... }
```

### Design: Stream ID Parser (stream_id.go)

```go
package srt

// StreamIDInfo holds parsed SRT Access Control fields.
// Convention: "#!::r=<resource>,m=<mode>,s=<session>,u=<user>,h=<host>,t=<type>"
type StreamIDInfo struct {
    Resource string // Stream path (e.g. "live/mystream") — maps to RTMP stream key
    Mode     string // "publish" or "request" (subscribe)
    Session  string // Optional session identifier
    User     string // Optional username
    Host     string // Optional hostname
    Type     string // "stream", "file", "auth"
    Raw      string // Original stream ID string
}

// ParseStreamID parses an SRT Access Control formatted stream ID.
// Falls back to treating the entire string as a resource name if not
// in the "#!::" format.
func ParseStreamID(raw string) StreamIDInfo {
    if !strings.HasPrefix(raw, "#!::") {
        // Simple mode: entire string is the resource (stream key)
        // Determine mode by checking for "publish:" prefix (gosrt convention)
        if strings.HasPrefix(raw, "publish:") {
            return StreamIDInfo{
                Resource: strings.TrimPrefix(raw, "publish:"),
                Mode:     "publish",
                Raw:      raw,
            }
        }
        return StreamIDInfo{Resource: raw, Mode: "request", Raw: raw}
    }
    // Parse key=value pairs after "#!::"
    // ...
}

// StreamKey returns the RTMP-style stream key derived from the resource.
// Strips leading "/" and ensures "app/stream" format.
func (s StreamIDInfo) StreamKey() string {
    key := strings.TrimPrefix(s.Resource, "/")
    if key == "" { key = "live/default" }
    return key
}

// IsPublish returns true if the caller wants to publish media.
func (s StreamIDInfo) IsPublish() bool {
    return s.Mode == "publish"
}
```

### Commits

| # | Message | Files |
|---|---------|-------|
| 9 | `feat(srt): add SYN cookie generation and Stream ID parser` | `handshake/cookie.go`, `stream_id.go`, `*_test.go` |
| 10 | `feat(srt): implement HSREQ/HSRSP/SID handshake extensions` | `handshake/extensions.go`, `*_test.go` |
| 11 | `feat(srt): implement listener-side handshake v5 state machine` | `handshake/listener.go`, `*_test.go` |

### Testing Strategy
- Simulate Induction → Conclusion exchange with mock UDP
- Verify SYN cookie validation across time buckets
- Parse real FFmpeg stream IDs (both `#!::` and simple formats)
- Extension round-trip (marshal → unmarshal)
- Reject invalid cookies, invalid versions, malformed extensions

---

## Phase 6: Connection State Machine

### Goal
Implement the SRT connection lifecycle: established state, packet queues,
read/write APIs, and timer management.

### Files
```
internal/srt/conn/conn.go
internal/srt/conn/sender.go
internal/srt/conn/receiver.go
internal/srt/conn/timers.go
internal/srt/conn/conn_test.go
```

### Design

```go
package conn

// State represents the lifecycle state of an SRT connection.
type State uint8

const (
    StateHandshake  State = iota // Handshake in progress
    StateConnected               // Established, exchanging data
    StateClosing                 // Shutdown initiated
    StateClosed                  // Fully closed
)

// Conn represents an established SRT connection.
// Unlike TCP, SRT connections share a single UDP socket managed by the Listener.
// The Conn sends packets by writing to the shared UDP socket with the peer's address.
type Conn struct {
    // Identity (immutable after handshake)
    localSocketID  uint32
    peerSocketID   uint32
    peerAddr       *net.UDPAddr
    udpConn        *net.UDPConn   // Shared UDP socket (owned by Listener)
    streamID       string

    // State
    mu    sync.RWMutex
    state State

    // Lifecycle
    ctx    context.Context
    cancel context.CancelFunc

    // Send-side (sender.go)
    sender *Sender

    // Receive-side (receiver.go)
    receiver *Receiver

    // Timers (timers.go)
    timers *TimerManager

    // Configuration
    config ConnConfig

    // Callbacks
    onDisconnect func()

    log *slog.Logger
}

// ConnConfig holds negotiated connection parameters.
type ConnConfig struct {
    MTU           uint32 // Negotiated MTU (typically 1500)
    FlowWindow    uint32 // Max in-flight packets
    TSBPDDelay    uint32 // TSBPD latency in microseconds
    PeerTSBPDDelay uint32
    InitialSeqNum uint32 // Starting sequence number
    PayloadSize   uint32 // Max payload per data packet (MTU - 16 header)
}

// Read reads the next delivered message payload into buf.
// Blocks until data is available or context is cancelled.
// Implements io.Reader semantics.
func (c *Conn) Read(buf []byte) (int, error) { ... }

// Write sends data as an SRT data packet.
// Implements io.Writer semantics.
func (c *Conn) Write(data []byte) (int, error) { ... }

// Close initiates graceful shutdown (sends SHUTDOWN control packet).
func (c *Conn) Close() error { ... }

// recvPacket is called by the Listener when a UDP packet arrives for this conn.
func (c *Conn) recvPacket(data []byte) { ... }
```

### Design: Sender (sender.go)

```go
// Sender manages the send-side buffer and retransmission queue.
type Sender struct {
    mu             sync.Mutex
    nextSeqNum     circular.Number
    sendBuffer     *list.List     // In-flight packets awaiting ACK
    lossBuffer     *list.List     // Packets to retransmit (from NAK)
    lastACK        circular.Number
    rtt            uint32          // Estimated RTT (microseconds)
    rttVar         uint32          // RTT variance
}

// Send enqueues a data packet for transmission.
func (s *Sender) Send(payload []byte, timestamp uint32) error { ... }

// OnACK processes an ACK: removes acknowledged packets from send buffer.
func (s *Sender) OnACK(ackSeq circular.Number) { ... }

// OnNAK processes a NAK: moves lost packets to retransmit queue.
func (s *Sender) OnNAK(ranges [][2]uint32) { ... }

// GetRetransmit returns the next packet to retransmit, if any.
func (s *Sender) GetRetransmit() *packet.DataPacket { ... }
```

### Design: Receiver (receiver.go)

```go
// Receiver manages the receive-side buffer and TSBPD delivery.
type Receiver struct {
    mu              sync.Mutex
    lastDelivered   circular.Number
    lastACKed       circular.Number
    receiveBuffer   map[circular.Number]*packet.DataPacket
    tsbpdBase       uint64  // Microsecond base for timestamp conversion
    tsbpdDelay      uint64  // Configured TSBPD delay (microseconds)
    deliveryChan    chan []byte // Channel for delivering data to Read()
}

// OnData processes an incoming data packet: buffer and schedule delivery.
func (r *Receiver) OnData(pkt *packet.DataPacket) { ... }

// DeliverReady delivers all packets whose TSBPD time has passed.
func (r *Receiver) DeliverReady(now uint64) { ... }

// GetLossReport returns sequence numbers of detected gaps for NAK.
func (r *Receiver) GetLossReport() [][2]uint32 { ... }

// GetACKSequence returns the last contiguous sequence number for ACK.
func (r *Receiver) GetACKSequence() circular.Number { ... }
```

### Commits

| # | Message | Files |
|---|---------|-------|
| 12 | `feat(srt/conn): implement SRT connection state machine and config` | `conn/conn.go`, `conn/conn_test.go` |
| 13 | `feat(srt/conn): implement sender and receiver buffers` | `conn/sender.go`, `conn/receiver.go`, `*_test.go` |
| 14 | `feat(srt/conn): implement ACK/NAK/keepalive timer management` | `conn/timers.go`, `*_test.go` |

---

## Phase 7: Reliability Layer (ARQ/NAK/TSBPD)

### Goal
Wire up the full reliability mechanism: periodic ACK generation, NAK-based loss detection,
packet retransmission, TSBPD-scheduled delivery, and too-late packet drop.

### Files
```
internal/srt/conn/reliability.go
internal/srt/conn/tsbpd.go
internal/srt/conn/ack_handler.go
internal/srt/conn/nak_handler.go
internal/srt/conn/reliability_test.go
internal/srt/conn/tsbpd_test.go
```

### Design: ACK Generation (ack_handler.go)

```go
// ACK is sent periodically by receiver (every 10ms or every 64 packets).
// Contains: last contiguous sequence number, RTT estimate, buffer status.

// ACK triggering rules (from SRT RFC):
// 1. Every SYN interval (10ms)
// 2. Every 64 received data packets (whichever comes first)

const (
    ackIntervalMs    = 10
    ackIntervalPkts  = 64
)

// GenerateACK creates an ACK control packet from current receiver state.
func (c *Conn) GenerateACK() *packet.ControlPacket {
    ackSeq := c.receiver.GetACKSequence()
    cif := &packet.ACKCIF{
        LastACKPacketSeq: uint32(ackSeq),
        RTT:              c.sender.rtt,
        RTTVariance:      c.sender.rttVar,
        AvailableBuffer:  c.receiver.AvailableBuffer(),
        // ...
    }
    // ...
}
```

### Design: NAK Generation (nak_handler.go)

```go
// NAK is sent when the receiver detects a gap in sequence numbers.
// Periodic NAK (PERIODICNAK flag) sends NAK every NAK interval.

const nakIntervalRTT = 20 // NAK interval = 20 * RTT

// GenerateNAK creates a NAK packet with lost sequence ranges.
func (c *Conn) GenerateNAK() *packet.ControlPacket {
    ranges := c.receiver.GetLossReport()
    if len(ranges) == 0 {
        return nil
    }
    return &packet.ControlPacket{
        Type: packet.CtrlNAK,
        CIF:  packet.EncodeLossRanges(ranges),
    }
}
```

### Design: TSBPD (tsbpd.go)

```go
// TSBPD (Timestamp-Based Packet Delivery) ensures packets are delivered
// to the application at a constant end-to-end latency, regardless of
// network jitter. The receiver holds packets until:
//   deliveryTime = packetTimestamp + tsbpdDelay
//
// The tsbpdDelay is negotiated during handshake:
//   delay = max(sender_requested_delay, receiver_requested_delay)
//
// Timestamp wraparound: SRT timestamps are 32-bit microsecond counters
// that wrap at ~71 minutes. We track a base offset to convert to 64-bit.

const tsWrapPeriod = uint64(0x100000000) // 2^32 microseconds (~71.6 min)

// TSBPDManager handles timestamp-based packet delivery scheduling.
type TSBPDManager struct {
    baseTime   uint64 // Wall clock at connection start (microseconds)
    tsBase     uint32 // First packet's SRT timestamp
    tsBaseSet  bool
    delay      uint64 // Negotiated TSBPD delay (microseconds)
    wrapCount  int    // Number of timestamp wraps detected
}

// DeliveryTime returns the wall-clock time at which a packet should
// be delivered, given its SRT timestamp.
func (t *TSBPDManager) DeliveryTime(pktTimestamp uint32) uint64 {
    // Convert packet timestamp to 64-bit absolute:
    absTS := t.toAbsoluteTS(pktTimestamp)
    return t.baseTime + absTS + t.delay
}

// toAbsoluteTS converts a 32-bit SRT timestamp to a 64-bit absolute
// timestamp, accounting for wraparound.
func (t *TSBPDManager) toAbsoluteTS(ts uint32) uint64 {
    if !t.tsBaseSet {
        t.tsBase = ts
        t.tsBaseSet = true
    }
    // Detect wraparound
    relative := uint64(ts) - uint64(t.tsBase) + uint64(t.wrapCount)*tsWrapPeriod
    // ... wraparound detection logic ...
    return relative
}

// TooLate returns true if a packet has passed its delivery deadline.
// Used for TLPKTDROP (Too-Late Packet Drop).
func (t *TSBPDManager) TooLate(pktTimestamp uint32, now uint64) bool {
    return now > t.DeliveryTime(pktTimestamp)
}
```

### Design: Reliability Loop (reliability.go)

```go
// reliabilityLoop runs as a goroutine per connection, managing:
// 1. Periodic ACK generation (every 10ms)
// 2. TSBPD delivery scheduling
// 3. Periodic NAK (if enabled)
// 4. Keepalive (every 1s if no data sent)
// 5. Too-late packet drop

func (c *Conn) reliabilityLoop() {
    ackTicker := time.NewTicker(10 * time.Millisecond)
    deliveryTicker := time.NewTicker(1 * time.Millisecond)
    keepaliveTicker := time.NewTicker(1 * time.Second)
    defer ackTicker.Stop()
    defer deliveryTicker.Stop()
    defer keepaliveTicker.Stop()

    for {
        select {
        case <-c.ctx.Done():
            return

        case <-ackTicker.C:
            if ack := c.GenerateACK(); ack != nil {
                c.sendControl(ack)
            }

        case <-deliveryTicker.C:
            c.receiver.DeliverReady(microsecondNow())

        case <-keepaliveTicker.C:
            c.sendKeepalive()
        }
    }
}
```

### Commits

| # | Message | Files |
|---|---------|-------|
| 15 | `feat(srt/conn): implement TSBPD timestamp-based delivery scheduling` | `conn/tsbpd.go`, `conn/tsbpd_test.go` |
| 16 | `feat(srt/conn): implement ACK generation and ACKACK handling` | `conn/ack_handler.go`, `conn/ack_handler_test.go` |
| 17 | `feat(srt/conn): implement NAK loss detection and retransmission` | `conn/nak_handler.go`, `conn/nak_handler_test.go` |
| 18 | `feat(srt/conn): implement reliability loop with keepalive and TLPKTDROP` | `conn/reliability.go`, `conn/reliability_test.go` |

---

## Phase 8: MPEG-TS Demuxer

### Goal
Implement a minimal MPEG-TS demuxer that extracts H.264 and AAC elementary streams from
transport stream packets. This is needed because SRT carries MPEG-TS as its payload.

### Files
```
internal/ts/doc.go
internal/ts/packet.go
internal/ts/psi.go
internal/ts/pes.go
internal/ts/demuxer.go
internal/ts/stream_types.go
internal/ts/packet_test.go
internal/ts/psi_test.go
internal/ts/pes_test.go
internal/ts/demuxer_test.go
```

### Design: TS Packet (packet.go)

```go
package ts

import "encoding/binary"

const (
    PacketSize = 188        // Fixed MPEG-TS packet size
    SyncByte   = 0x47       // Sync byte at start of each packet
    NullPID    = 0x1FFF     // Null/padding PID
    PATPID     = 0x0000     // Program Association Table PID
)

// Packet represents a parsed 188-byte MPEG-TS packet.
type Packet struct {
    TEI               bool   // Transport Error Indicator
    PayloadUnitStart  bool   // Payload Unit Start Indicator (PES/PSI start)
    Priority          bool   // Transport Priority
    PID               uint16 // 13-bit Packet Identifier
    Scrambling        uint8  // 2-bit Transport Scrambling Control
    HasAdaptation     bool   // Adaptation field present
    HasPayload        bool   // Payload present
    ContinuityCounter uint8  // 4-bit continuity counter (0-15)
    AdaptationField   *AdaptationField
    Payload           []byte // Payload data (variable length, up to 184 bytes)
}

// AdaptationField carries timing and control information.
type AdaptationField struct {
    Length            uint8
    Discontinuity     bool
    RandomAccess      bool  // Set on keyframes (RAP)
    PCR               int64 // Program Clock Reference (-1 if not present)
}

// ParsePacket parses a 188-byte MPEG-TS packet.
func ParsePacket(data [PacketSize]byte) (*Packet, error) {
    if data[0] != SyncByte {
        return nil, fmt.Errorf("invalid sync byte: 0x%02x", data[0])
    }
    // Parse 3-byte header: TEI, PUSI, Priority, PID, Scrambling, AFC, CC
    // Parse optional adaptation field
    // Extract payload
    return pkt, nil
}
```

### Design: PSI Tables (psi.go)

```go
// StreamType constants for codec identification in PMT.
const (
    StreamTypeMPEG2Video uint8 = 0x02
    StreamTypeMPEG1Audio uint8 = 0x03
    StreamTypeMPEG2Audio uint8 = 0x04
    StreamTypeAAC_ADTS   uint8 = 0x0F // ISO/IEC 13818-7 Audio with ADTS
    StreamTypeAAC_LATM   uint8 = 0x11 // ISO/IEC 14496-3 Audio with LATM
    StreamTypeH264       uint8 = 0x1B // ITU-T H.264 | ISO/IEC 14496-10
    StreamTypeH265       uint8 = 0x24 // ITU-T H.265 | ISO/IEC 23008-2
)

// PATEntry maps a program number to its PMT PID.
type PATEntry struct {
    ProgramNumber uint16
    PMTPID        uint16
}

// ParsePAT parses a Program Association Table section.
func ParsePAT(payload []byte) ([]PATEntry, error) { ... }

// PMTStream describes a single elementary stream in a program.
type PMTStream struct {
    StreamType uint8  // Codec (see StreamType* constants)
    PID        uint16 // PID carrying this elementary stream
}

// PMT holds parsed Program Map Table information.
type PMT struct {
    PCRPID  uint16      // PID carrying PCR for this program
    Streams []PMTStream // Elementary streams
}

// ParsePMT parses a Program Map Table section.
func ParsePMT(payload []byte) (*PMT, error) { ... }
```

### Design: PES Reassembly (pes.go)

```go
// PESPacket represents a reassembled Packetized Elementary Stream packet.
type PESPacket struct {
    StreamID uint8  // Audio/video stream identifier
    PTS      int64  // Presentation Timestamp (90kHz, -1 if not present)
    DTS      int64  // Decode Timestamp (90kHz, -1 if not present)
    Data     []byte // Elementary stream data (H.264 NALUs, AAC frames, etc.)
}

// PESAssembler reassembles PES packets from TS packet payloads.
// Handles PES packets that span multiple TS packets.
type PESAssembler struct {
    buffer    bytes.Buffer
    hasPESStart bool
    streamID  uint8
}

// Feed processes a TS packet payload for this PID.
// Returns a complete PES packet if one is fully assembled, nil otherwise.
func (a *PESAssembler) Feed(payload []byte, payloadUnitStart bool) *PESPacket {
    if payloadUnitStart {
        // Emit any pending PES packet
        var prev *PESPacket
        if a.hasPESStart && a.buffer.Len() > 0 {
            prev = parsePESPacket(a.buffer.Bytes())
        }
        a.buffer.Reset()
        a.hasPESStart = true
        a.buffer.Write(payload)
        return prev
    }
    if a.hasPESStart {
        a.buffer.Write(payload)
    }
    return nil
}

// parsePESPacket parses PES header to extract PTS/DTS and raw ES data.
func parsePESPacket(data []byte) *PESPacket {
    // PES start code: 0x00 0x00 0x01
    // Stream ID (1 byte)
    // PES packet length (2 bytes)
    // Optional: PTS/DTS flags, header data length, PTS/DTS values
    // Payload = elementary stream data
    // ...
}

// parsePTS extracts a 33-bit PTS/DTS timestamp from 5 bytes of PES header.
func parsePTS(data []byte) int64 {
    // Bits: [4] xxx1 [32:30] 1 [29:15] 1 [14:0] 1
    // Assemble 33-bit value from 5 bytes with marker bits
    // ...
}
```

### Design: High-Level Demuxer (demuxer.go)

```go
// ElementaryStream represents a detected audio or video stream.
type ElementaryStream struct {
    PID        uint16
    StreamType uint8
    Codec      string // "H264", "H265", "AAC", etc.
}

// MediaFrame is a single decoded media frame from the TS stream.
type MediaFrame struct {
    Stream  *ElementaryStream
    PTS     int64  // 90kHz presentation timestamp (-1 if absent)
    DTS     int64  // 90kHz decode timestamp (-1 if absent)
    Data    []byte // Raw elementary stream data
    IsKey   bool   // Random Access Point (keyframe)
}

// FrameHandler is called for each complete media frame.
type FrameHandler func(frame *MediaFrame)

// Demuxer processes MPEG-TS packets and emits elementary stream frames.
type Demuxer struct {
    patParsed   bool
    pmtPID      uint16
    pmtParsed   bool
    streams     map[uint16]*ElementaryStream
    assemblers  map[uint16]*PESAssembler
    ccCounters  map[uint16]uint8 // Continuity counter tracking
    handler     FrameHandler
}

// NewDemuxer creates a demuxer that calls handler for each complete frame.
func NewDemuxer(handler FrameHandler) *Demuxer { ... }

// Feed processes raw data from the SRT connection.
// It syncs to 188-byte packet boundaries, parses each packet,
// and routes to the appropriate handler (PAT/PMT/PES).
func (d *Demuxer) Feed(data []byte) error {
    for len(data) >= PacketSize {
        // Find sync byte
        if data[0] != SyncByte {
            data = resync(data)
            continue
        }
        var pktBuf [PacketSize]byte
        copy(pktBuf[:], data[:PacketSize])
        data = data[PacketSize:]

        pkt, err := ParsePacket(pktBuf)
        if err != nil { continue }

        d.processPacket(pkt)
    }
    return nil
}

// processPacket routes a parsed TS packet to the correct handler.
func (d *Demuxer) processPacket(pkt *Packet) {
    switch {
    case pkt.PID == PATPID:
        d.handlePAT(pkt)
    case pkt.PID == d.pmtPID:
        d.handlePMT(pkt)
    case d.streams[pkt.PID] != nil:
        d.handlePES(pkt)
    }
    // Null PIDs and unknown PIDs are silently ignored
}
```

### Commits

| # | Message | Files |
|---|---------|-------|
| 19 | `feat(ts): implement MPEG-TS 188-byte packet parser` | `ts/packet.go`, `ts/packet_test.go`, `ts/doc.go` |
| 20 | `feat(ts): implement PAT/PMT parsing and PES reassembly` | `ts/psi.go`, `ts/pes.go`, `ts/stream_types.go`, `*_test.go` |
| 21 | `feat(ts): implement high-level MPEG-TS demuxer` | `ts/demuxer.go`, `ts/demuxer_test.go` |

### Testing Strategy
- Generate test TS files with FFmpeg: `ffmpeg -f lavfi -i testsrc2 -f mpegts test.ts`
- Parse known TS files and verify PAT/PMT content
- Verify PES reassembly across multiple TS packets
- Test continuity counter error detection
- Test packet resync after byte offset

---

## Phase 9: Ingress Lifecycle Abstraction

### Goal
Create a protocol-agnostic ingress manager that handles the publish lifecycle:
authentication, hook events, metrics, recording, and registry interaction.
Both RTMP and SRT publishers use this abstraction.

### Files
```
internal/ingress/doc.go
internal/ingress/manager.go
internal/ingress/publisher.go
internal/ingress/manager_test.go
```

### Design

```go
package ingress

import (
    "github.com/alxayo/go-rtmp/internal/rtmp/chunk"
    "github.com/alxayo/go-rtmp/internal/rtmp/server/auth"
    "github.com/alxayo/go-rtmp/internal/rtmp/server/hooks"
)

// Publisher represents a media source that pushes audio/video into the registry.
// Both RTMP connections and SRT connections implement this interface.
type Publisher interface {
    ID() string          // Unique identifier (e.g. "rtmp-abc123" or "srt-xyz789")
    Protocol() string    // "rtmp" or "srt"
    RemoteAddr() string  // Peer address
    StreamKey() string   // Stream key (e.g. "live/mystream")
    Close() error        // Disconnect the publisher
}

// IngressEvent describes lifecycle events for publish sessions.
type IngressEvent int

const (
    EventPublishStart IngressEvent = iota
    EventPublishStop
    EventMediaReceived
)

// Manager coordinates the publish lifecycle across protocols.
// It wraps the Registry, auth, hooks, metrics, and recording
// so that protocol-specific code only needs to:
// 1. Call BeginPublish() with auth context
// 2. Call PushMedia() for each media frame
// 3. Call EndPublish() on disconnect
type Manager struct {
    registry    RegistryInterface
    auth        auth.Validator
    hookManager *hooks.HookManager
    config      ManagerConfig
    log         *slog.Logger
}

// RegistryInterface is the subset of server.Registry needed by ingress.
type RegistryInterface interface {
    CreateStream(key string) (StreamInterface, bool)
    GetStream(key string) StreamInterface
    DeleteStream(key string) bool
}

// BeginPublish authenticates and registers a new publisher.
// Returns a PublishSession handle for pushing media.
func (m *Manager) BeginPublish(pub Publisher, authCtx map[string]interface{}) (*PublishSession, error) {
    // 1. Authenticate
    if m.auth != nil {
        if err := m.auth.ValidatePublish(pub.StreamKey(), authCtx); err != nil {
            return nil, fmt.Errorf("auth denied: %w", err)
        }
    }

    // 2. Create/get stream in registry
    stream, _ := m.registry.CreateStream(pub.StreamKey())

    // 3. Set publisher (fails if already publishing)
    if err := stream.SetPublisher(pub); err != nil {
        return nil, err
    }

    // 4. Start recording if enabled
    // 5. Fire hook event
    // 6. Update metrics

    return &PublishSession{
        publisher: pub,
        stream:    stream,
        manager:   m,
    }, nil
}

// PublishSession manages an active publish session.
type PublishSession struct {
    publisher Publisher
    stream    StreamInterface
    manager   *Manager
    detector  *media.CodecDetector
}

// PushMedia routes a single media message through the pipeline:
// codec detection → sequence header caching → recording → broadcast → relay
func (s *PublishSession) PushMedia(msg *chunk.Message) {
    // Same pipeline as current dispatchMedia() but protocol-agnostic
    s.detector.Process(msg.TypeID, msg.Payload, s.stream, s.manager.log)
    if s.stream.Recorder() != nil {
        s.stream.Recorder().WriteMessage(msg)
    }
    s.stream.BroadcastMessage(s.detector, msg, s.manager.log)
}

// EndPublish cleans up the publish session.
func (s *PublishSession) EndPublish() {
    // 1. Clear publisher from registry
    // 2. Stop recording
    // 3. Fire hook event
    // 4. Update metrics
}
```

### Important Note
This phase introduces the abstraction but does **not** refactor the existing RTMP code to use it.
The RTMP path continues using `command_integration.go` / `media_dispatch.go` unchanged.
Only SRT uses the new `ingress.Manager`. In a future cleanup, RTMP can be migrated to use
the same abstraction, but that is out of scope for the SRT MVP.

### Commits

| # | Message | Files |
|---|---------|-------|
| 22 | `feat(ingress): add protocol-agnostic publisher interface` | `ingress/doc.go`, `ingress/publisher.go` |
| 23 | `feat(ingress): implement ingress lifecycle manager` | `ingress/manager.go`, `ingress/manager_test.go` |

---

## Phase 10: SRT-to-RTMP Bridge

### Goal
Build the codec conversion layer that transforms MPEG-TS elementary streams into
RTMP-format `chunk.Message` values, suitable for the existing registry/broadcast pipeline.

### Critical Design Decisions

#### Timestamp Conversion (90kHz PTS/DTS → 1kHz RTMP)
- **RTMP timestamp = DTS / 90** (not PTS!)
- **Composition Time Offset = (PTS - DTS) / 90** (stored in AVCC header)
- Use integer arithmetic with remainder tracking to avoid drift

#### H.264: Annex B → AVCC Conversion
- FFmpeg sends H.264 in **Annex B** format (start codes: 0x00000001 or 0x000001)
- RTMP expects **AVCC** format (length-prefixed NALUs)
- Sequence header: construct `AVCDecoderConfigurationRecord` from SPS/PPS NALUs
- Frame data: replace start codes with 4-byte NALU lengths

#### AAC: ADTS → Raw AAC Conversion
- FFmpeg sends AAC with **ADTS headers** (7 or 9 bytes per frame)
- RTMP expects raw AAC frames with a separate `AudioSpecificConfig` sequence header
- Strip ADTS header, extract config from first frame

### Files
```
internal/codec/doc.go
internal/codec/nalu.go
internal/codec/h264.go
internal/codec/aac.go
internal/codec/nalu_test.go
internal/codec/h264_test.go
internal/codec/aac_test.go
internal/srt/bridge.go
internal/srt/bridge_test.go
```

### Design: NALU Splitter (nalu.go)

```go
package codec

// SplitAnnexB splits an H.264/H.265 Annex B byte stream into individual NALUs.
// Detects start codes: 0x000001 (3-byte) and 0x00000001 (4-byte).
// Returns slices into the original data (no copy).
func SplitAnnexB(data []byte) [][]byte {
    // Scan for start codes, collect NALU boundaries
    // ...
}

// NALUType returns the H.264 NALU type (lower 5 bits of first byte).
func NALUType(nalu []byte) uint8 {
    if len(nalu) == 0 { return 0 }
    return nalu[0] & 0x1F
}

// H.264 NALU type constants
const (
    NALUTypeSlice    uint8 = 1  // Non-IDR slice
    NALUTypeDPA      uint8 = 2
    NALUTypeIDR      uint8 = 5  // IDR slice (keyframe)
    NALUTypeSEI      uint8 = 6  // Supplemental Enhancement Info
    NALUTypeSPS      uint8 = 7  // Sequence Parameter Set
    NALUTypePPS      uint8 = 8  // Picture Parameter Set
    NALUTypeAUD      uint8 = 9  // Access Unit Delimiter
)

// ToAVCC converts Annex B NALUs to AVCC format (4-byte length prefix).
func ToAVCC(nalus [][]byte) []byte {
    var buf bytes.Buffer
    for _, nalu := range nalus {
        binary.Write(&buf, binary.BigEndian, uint32(len(nalu)))
        buf.Write(nalu)
    }
    return buf.Bytes()
}
```

### Design: H.264 Converter (h264.go)

```go
package codec

// AVCSequenceHeader builds an RTMP video tag payload containing
// the AVCDecoderConfigurationRecord (AVCC sequence header).
//
// RTMP video tag format for H.264 sequence header:
//   Byte 0: FrameType(1=key)<<4 | CodecID(7=AVC) = 0x17
//   Byte 1: AVCPacketType = 0 (sequence header)
//   Bytes 2-4: CompositionTime = 0x000000
//   Remaining: AVCDecoderConfigurationRecord
//
// AVCDecoderConfigurationRecord:
//   Version(1), Profile(1), Compat(1), Level(1),
//   LenSizeMinusOne(0xFF=4-byte), NumSPS(0xE1=1),
//   SPSLen(2), SPS data, NumPPS(1), PPSLen(2), PPS data
func BuildAVCSequenceHeader(sps, pps []byte) []byte { ... }

// AVCVideoFrame builds an RTMP video tag payload for an H.264 frame.
//
// RTMP video tag format for H.264 NALU:
//   Byte 0: FrameType<<4 | CodecID(7) — FrameType: 1=key, 2=inter
//   Byte 1: AVCPacketType = 1 (NALU)
//   Bytes 2-4: CompositionTimeOffset (CTS) in milliseconds (signed 24-bit)
//   Remaining: AVCC-format NALUs (4-byte length prefix)
func BuildAVCVideoFrame(nalus [][]byte, isKeyframe bool, cts int32) []byte { ... }

// ExtractSPSPPS scans NALUs for SPS and PPS.
func ExtractSPSPPS(nalus [][]byte) (sps, pps []byte, found bool) {
    for _, nalu := range nalus {
        switch NALUType(nalu) {
        case NALUTypeSPS:
            sps = nalu
        case NALUTypePPS:
            pps = nalu
        }
    }
    return sps, pps, sps != nil && pps != nil
}
```

### Design: AAC Converter (aac.go)

```go
package codec

// ADTSHeader represents a parsed ADTS frame header.
type ADTSHeader struct {
    Profile        uint8  // 0=Main, 1=LC, 2=SSR
    SamplingFreqIdx uint8 // 0-12 index
    ChannelConfig  uint8  // 1=mono, 2=stereo, etc.
    FrameLength    uint16 // Total ADTS frame length including header
    HeaderSize     int    // 7 or 9 bytes
}

// ParseADTSHeader parses a 7-byte ADTS fixed header.
func ParseADTSHeader(data []byte) (*ADTSHeader, error) { ... }

// BuildAudioSpecificConfig creates the AAC AudioSpecificConfig from ADTS info.
// This is the RTMP AAC sequence header payload.
//
// AudioSpecificConfig (2 bytes for LC profile):
//   Bits [4:0] = AudioObjectType (2=AAC-LC)
//   Bits [3:0] = SamplingFrequencyIndex
//   Bits [3:0] = ChannelConfiguration
//   Remaining bits = 0
func BuildAudioSpecificConfig(h *ADTSHeader) []byte { ... }

// BuildAACSequenceHeader wraps AudioSpecificConfig in RTMP audio tag format.
//
// RTMP audio tag for AAC sequence header:
//   Byte 0: SoundFormat(10=AAC)<<4 | Rate(3=44kHz)<<2 | Size(1=16-bit)<<1 | Type(1=stereo) = 0xAF
//   Byte 1: AACPacketType = 0 (sequence header)
//   Remaining: AudioSpecificConfig
func BuildAACSequenceHeader(h *ADTSHeader) []byte { ... }

// BuildAACFrame wraps raw AAC frame data in RTMP audio tag format.
//
// RTMP audio tag for AAC raw frame:
//   Byte 0: 0xAF (same as sequence header)
//   Byte 1: AACPacketType = 1 (raw)
//   Remaining: Raw AAC frame data (ADTS header stripped)
func BuildAACFrame(rawFrame []byte) []byte { ... }

// StripADTS removes the ADTS header from an AAC frame, returning raw data.
func StripADTS(data []byte) ([]byte, *ADTSHeader, error) { ... }
```

### Design: SRT Bridge (bridge.go)

```go
package srt

import (
    "github.com/alxayo/go-rtmp/internal/codec"
    "github.com/alxayo/go-rtmp/internal/rtmp/chunk"
    "github.com/alxayo/go-rtmp/internal/ts"
)

// Bridge converts MPEG-TS elementary streams from an SRT connection
// into RTMP-format chunk.Messages and pushes them through the ingress manager.
type Bridge struct {
    conn       *Conn
    demuxer    *ts.Demuxer
    session    *ingress.PublishSession

    // Timestamp tracking (per-track)
    videoTSBase int64  // First video DTS (90kHz)
    audioTSBase int64  // First audio PTS (90kHz)
    videoTSSet  bool
    audioTSSet  bool

    // Codec state
    sps, pps      []byte // Cached H.264 SPS/PPS
    seqHeaderSent bool   // Whether we've sent the video sequence header
    aacConfigSent bool   // Whether we've sent the AAC sequence header

    log *slog.Logger
}

// NewBridge creates a bridge for the given SRT connection and ingress session.
func NewBridge(conn *Conn, session *ingress.PublishSession, log *slog.Logger) *Bridge {
    b := &Bridge{conn: conn, session: session, log: log}
    b.demuxer = ts.NewDemuxer(b.onFrame)
    return b
}

// Run reads from the SRT connection and feeds data to the TS demuxer.
// Blocks until the connection is closed.
func (b *Bridge) Run() error {
    buf := make([]byte, 1500)
    for {
        n, err := b.conn.Read(buf)
        if err != nil { return err }
        if err := b.demuxer.Feed(buf[:n]); err != nil {
            b.log.Warn("TS demux error", "error", err)
        }
    }
}

// onFrame handles a complete media frame from the TS demuxer.
func (b *Bridge) onFrame(frame *ts.MediaFrame) {
    switch {
    case frame.Stream.StreamType == ts.StreamTypeH264:
        b.handleH264Frame(frame)
    case frame.Stream.StreamType == ts.StreamTypeAAC_ADTS:
        b.handleAACFrame(frame)
    }
}

// handleH264Frame converts an H.264 access unit to RTMP video messages.
func (b *Bridge) handleH264Frame(frame *ts.MediaFrame) {
    nalus := codec.SplitAnnexB(frame.Data)
    if len(nalus) == 0 { return }

    // Check for SPS/PPS → send sequence header
    sps, pps, found := codec.ExtractSPSPPS(nalus)
    if found && !b.seqHeaderSent {
        b.sps, b.pps = sps, pps
        seqHeader := codec.BuildAVCSequenceHeader(sps, pps)
        b.pushVideo(seqHeader, 0, true)
        b.seqHeaderSent = true
    }

    // Convert timestamps: 90kHz → milliseconds
    dts := frame.DTS
    if dts < 0 { dts = frame.PTS } // Fallback if DTS not present
    if !b.videoTSSet {
        b.videoTSBase = dts
        b.videoTSSet = true
    }
    rtmpTS := uint32((dts - b.videoTSBase) / 90) // 90kHz → ms

    // Composition time offset (for B-frames)
    cts := int32(0)
    if frame.PTS >= 0 && frame.DTS >= 0 {
        cts = int32((frame.PTS - frame.DTS) / 90)
    }

    // Filter out SPS/PPS/AUD NALUs from frame data
    var frameNalus [][]byte
    for _, nalu := range nalus {
        switch codec.NALUType(nalu) {
        case codec.NALUTypeSPS, codec.NALUTypePPS, codec.NALUTypeAUD:
            continue // Skip non-VCL NALUs
        default:
            frameNalus = append(frameNalus, nalu)
        }
    }
    if len(frameNalus) == 0 { return }

    isKey := codec.NALUType(frameNalus[0]) == codec.NALUTypeIDR
    payload := codec.BuildAVCVideoFrame(frameNalus, isKey, cts)
    b.pushVideo(payload, rtmpTS, false)
}

// handleAACFrame converts an AAC ADTS frame to RTMP audio messages.
func (b *Bridge) handleAACFrame(frame *ts.MediaFrame) {
    rawFrame, adts, err := codec.StripADTS(frame.Data)
    if err != nil { return }

    // Send AudioSpecificConfig on first frame
    if !b.aacConfigSent {
        seqHeader := codec.BuildAACSequenceHeader(adts)
        b.pushAudio(seqHeader, 0)
        b.aacConfigSent = true
    }

    // Convert timestamp: 90kHz → ms
    pts := frame.PTS
    if !b.audioTSSet {
        b.audioTSBase = pts
        b.audioTSSet = true
    }
    rtmpTS := uint32((pts - b.audioTSBase) / 90)

    payload := codec.BuildAACFrame(rawFrame)
    b.pushAudio(payload, rtmpTS)
}

// pushVideo constructs a chunk.Message for video and pushes to ingress.
func (b *Bridge) pushVideo(payload []byte, timestamp uint32, isSeqHeader bool) {
    msg := &chunk.Message{
        CSID:            6, // Video CSID (convention)
        Timestamp:       timestamp,
        MessageLength:   uint32(len(payload)),
        TypeID:          9, // Video
        MessageStreamID: 1,
        Payload:         payload,
    }
    b.session.PushMedia(msg)
}

// pushAudio constructs a chunk.Message for audio and pushes to ingress.
func (b *Bridge) pushAudio(payload []byte, timestamp uint32) {
    msg := &chunk.Message{
        CSID:            4, // Audio CSID (convention)
        Timestamp:       timestamp,
        MessageLength:   uint32(len(payload)),
        TypeID:          8, // Audio
        MessageStreamID: 1,
        Payload:         payload,
    }
    b.session.PushMedia(msg)
}
```

### Commits

| # | Message | Files |
|---|---------|-------|
| 24 | `feat(codec): implement H.264 Annex B→AVCC and NALU parser` | `codec/doc.go`, `codec/nalu.go`, `codec/h264.go`, `*_test.go` |
| 25 | `feat(codec): implement AAC ADTS→AudioSpecificConfig converter` | `codec/aac.go`, `codec/aac_test.go` |
| 26 | `feat(srt): implement SRT-to-RTMP bridge with TS demux and codec conversion` | `srt/bridge.go`, `srt/bridge_test.go` |

---

## Phase 11: Server Integration

### Goal
Wire SRT into the existing server: add CLI flags, start optional SRT listener
alongside RTMP/TLS, and connect the SRT accept loop to the ingress manager.

### Files
```
cmd/rtmp-server/flags.go           # MODIFIED — add SRT flags
cmd/rtmp-server/main.go            # MODIFIED — pass SRT config
internal/rtmp/server/server.go     # MODIFIED — add SRT listener lifecycle
internal/rtmp/server/srt_accept.go # NEW — SRT accept loop
internal/rtmp/metrics/metrics.go   # MODIFIED — add SRT metrics
```

### Design: CLI Flags (flags.go additions)

```go
// SRT configuration
srtListenAddr string // SRT UDP listen address (e.g. ":10080"). Empty = disabled
srtLatency    int    // SRT latency in milliseconds (default 120)
srtPassphrase string // SRT encryption passphrase (empty = no encryption)
srtPbKeyLen   int    // AES key length: 16, 24, or 32 (default 16)

// Flag registration:
fs.StringVar(&cfg.srtListenAddr, "srt-listen", "", "SRT UDP listen address (e.g. :10080). Empty = disabled")
fs.IntVar(&cfg.srtLatency, "srt-latency", 120, "SRT buffer latency in milliseconds")
fs.StringVar(&cfg.srtPassphrase, "srt-passphrase", "", "SRT encryption passphrase (empty = no encryption)")
fs.IntVar(&cfg.srtPbKeyLen, "srt-pbkeylen", 16, "SRT AES key length: 16, 24, or 32")
```

### Design: Server Config (server.go additions)

```go
type Config struct {
    // ... existing fields ...

    // SRT configuration (all optional). When SRTListenAddr is non-empty,
    // the server starts a UDP listener for SRT ingest alongside RTMP.
    SRTListenAddr string // SRT UDP listen address (e.g. ":10080"). Empty = disabled
    SRTLatency    int    // SRT buffer latency in milliseconds (default 120)
    SRTPassphrase string // SRT encryption passphrase (empty = plaintext)
    SRTPbKeyLen   int    // AES key length: 16, 24, or 32 (default 16)
}
```

### Design: SRT Accept Loop (srt_accept.go)

```go
package server

import (
    "github.com/alxayo/go-rtmp/internal/srt"
    "github.com/alxayo/go-rtmp/internal/ingress"
)

// startSRTListener creates and starts the SRT UDP listener.
func (s *Server) startSRTListener() error {
    cfg := srt.Config{
        ListenAddr: s.cfg.SRTListenAddr,
        Latency:    s.cfg.SRTLatency,
        Passphrase: s.cfg.SRTPassphrase,
        PbKeyLen:   s.cfg.SRTPbKeyLen,
    }
    ln, err := srt.Listen(cfg.ListenAddr, cfg)
    if err != nil {
        return fmt.Errorf("srt listen: %w", err)
    }
    s.srtListener = ln
    s.acceptingWg.Add(1)
    go s.srtAcceptLoop()
    return nil
}

// srtAcceptLoop accepts incoming SRT connections and wires them
// to the ingress manager for media routing.
func (s *Server) srtAcceptLoop() {
    defer s.acceptingWg.Done()
    for {
        req, err := s.srtListener.Accept()
        if err != nil {
            if s.closing { return }
            s.log.Warn("SRT accept error", "error", err)
            continue
        }

        go s.handleSRTConnection(req)
    }
}

// handleSRTConnection processes a single SRT ingest session.
func (s *Server) handleSRTConnection(req *srt.ConnRequest) {
    // Parse Stream ID for routing info
    info := srt.ParseStreamID(req.StreamID())

    if !info.IsPublish() {
        // SRT playback is not supported in MVP
        req.Reject(srt.RejectBadRequest)
        return
    }

    // Accept the SRT connection
    conn, err := req.Accept()
    if err != nil {
        s.log.Error("SRT accept failed", "error", err)
        return
    }

    // Track connection
    connID := fmt.Sprintf("srt-%s", conn.LocalSocketID())
    metrics.SRTConnectionsActive.Add(1)
    metrics.SRTConnectionsTotal.Add(1)
    s.log.Info("SRT connection accepted",
        "conn_id", connID,
        "remote", conn.PeerAddr(),
        "stream_key", info.StreamKey(),
    )

    // Fire hook
    s.triggerHookEvent(hooks.EventConnectionAccept, connID, info.StreamKey(), map[string]interface{}{
        "remote_addr": conn.PeerAddr().String(),
        "protocol":    "srt",
    })

    // Create virtual publisher
    pub := &srtPublisher{
        id:        connID,
        conn:      conn,
        streamKey: info.StreamKey(),
    }

    // Begin publish via ingress manager
    session, err := s.ingressManager.BeginPublish(pub, nil)
    if err != nil {
        s.log.Error("SRT publish rejected", "error", err, "stream_key", info.StreamKey())
        conn.Close()
        return
    }

    // Start bridge: reads SRT → TS demux → codec conversion → chunk.Message → registry
    bridge := srt.NewBridge(conn, session, s.log)
    err = bridge.Run() // Blocks until connection closes

    // Cleanup
    session.EndPublish()
    conn.Close()
    metrics.SRTConnectionsActive.Add(-1)
    s.log.Info("SRT connection closed", "conn_id", connID)
}

// srtPublisher implements ingress.Publisher for SRT connections.
type srtPublisher struct {
    id        string
    conn      *srt.Conn
    streamKey string
}

func (p *srtPublisher) ID() string         { return p.id }
func (p *srtPublisher) Protocol() string   { return "srt" }
func (p *srtPublisher) RemoteAddr() string { return p.conn.PeerAddr().String() }
func (p *srtPublisher) StreamKey() string  { return p.streamKey }
func (p *srtPublisher) Close() error       { return p.conn.Close() }
```

### Design: SRT Metrics (metrics.go additions)

```go
var (
    SRTConnectionsActive = expvar.NewInt("srt_connections_active")
    SRTConnectionsTotal  = expvar.NewInt("srt_connections_total")
    SRTBytesReceived     = expvar.NewInt("srt_bytes_received")
    SRTPacketsReceived   = expvar.NewInt("srt_packets_received")
    SRTPacketsRetransmit = expvar.NewInt("srt_packets_retransmit")
    SRTPacketsDropped    = expvar.NewInt("srt_packets_dropped")
)
```

### Commits

| # | Message | Files |
|---|---------|-------|
| 27 | `feat(server): add SRT CLI flags and server configuration` | `flags.go`, `main.go`, `server/server.go` (Config fields) |
| 28 | `feat(metrics): add SRT connection and packet metrics` | `metrics/metrics.go` |
| 29 | `feat(server): implement SRT accept loop and bridge wiring` | `server/srt_accept.go`, `server/server.go` (Start/Stop) |

---

## Phase 12: E2E Testing & Documentation

### Goal
Validate end-to-end SRT ingest with FFmpeg and document the feature.

### Files
```
scripts/test-srt-ingest.sh
scripts/test-srt-ingest.ps1
scripts/README.md                  # MODIFIED
tests/integration/srt_test.go      # NEW
README.md                          # MODIFIED
CHANGELOG.md                       # MODIFIED
quick-start.md                     # MODIFIED
docs/srt-protocol.md               # NEW
site/content/...                   # Updated pages
```

### E2E Test Flow

```bash
# 1. Start go-rtmp with SRT enabled
./rtmp-server -listen :1935 -srt-listen :10080 -srt-latency 200 -record-all true

# 2. Publish via SRT (FFmpeg → go-rtmp)
ffmpeg -re -f lavfi -i "testsrc2=rate=25:size=640x360" \
    -f lavfi -i "sine=frequency=440:sample_rate=44100" \
    -c:v libx264 -preset ultrafast -b:v 1000k -g 50 \
    -c:a aac -b:a 128k \
    -f mpegts \
    "srt://127.0.0.1:10080?streamid=#!::r=live/srt-test,m=publish"

# 3. Subscribe via RTMP (ffplay)
ffplay rtmp://127.0.0.1:1935/live/srt-test

# 4. Verify recording
ffprobe recordings/live_srt-test_*.flv
```

### Commits

| # | Message | Files |
|---|---------|-------|
| 30 | `test(srt): add SRT ingest integration test` | `tests/integration/srt_test.go` |
| 31 | `test(srt): add SRT E2E test scripts for bash and PowerShell` | `scripts/test-srt-ingest.sh`, `scripts/test-srt-ingest.ps1`, `scripts/README.md` |
| 32 | `docs: add SRT protocol documentation` | `docs/srt-protocol.md` |
| 33 | `docs: update README, CHANGELOG, quick-start for SRT support` | `README.md`, `CHANGELOG.md`, `quick-start.md`, site content |

---

## Phase 13 (Future): Encryption

### Goal
Add optional AES encryption for SRT connections.

### Scope
- PBKDF2-HMAC-SHA1 key derivation from passphrase (stdlib `crypto/pbkdf2`)
- AES Key Wrap (RFC 3394) for wrapping/unwrapping SEK (Phase 3 implementation)
- AES-CTR encryption/decryption of data packet payloads
- KM extension in handshake (KMREQ/KMRSP)
- Even/odd key rotation

### Dependencies
Phase 3 (crypto primitives), Phase 5 (handshake extensions)

### Commits

| # | Message | Files |
|---|---------|-------|
| 34 | `feat(srt/crypto): implement AES-CTR payload encryption/decryption` | `crypto/ctr.go`, `crypto/ctr_test.go` |
| 35 | `feat(srt): implement Key Material handshake extension` | `handshake/km.go`, `handshake/km_test.go` |
| 36 | `feat(srt): wire encryption into connection and config` | `conn/conn.go`, `config.go`, integration |

---

## Phase 14 (Future): HEVC/AV1 via SRT

### Goal
Support H.265 and AV1 codecs ingested via SRT, bridging to Enhanced RTMP payloads.

### Scope
- H.265 Annex B → HVCC conversion
- Enhanced RTMP video tag generation (IsExHeader + FourCC)
- Subscriber capability gating (only Enhanced RTMP subscribers get HEVC)
- AV1 OBU parsing (future)

### Dependencies
Phase 10 (bridge), existing Enhanced RTMP support (v0.1.4)

### Commits

| # | Message | Files |
|---|---------|-------|
| 37 | `feat(codec): implement H.265 Annex B→HVCC converter` | `codec/h265.go`, `codec/h265_test.go` |
| 38 | `feat(srt): extend bridge for HEVC and Enhanced RTMP payloads` | `srt/bridge.go`, tests |

---

## Task Sequence & Dependencies

```
Phase 1 ──┐
           ├── Phase 5 ──── Phase 6 ──── Phase 7 ──┐
Phase 2 ──┤                                         ├── Phase 10 ── Phase 11 ── Phase 12
Phase 3    │                                         │
           │                         Phase 8 ────────┤
Phase 4 ──┘                                         │
                                     Phase 9 ────────┘

Phase 13 (Future) ← Phase 3 + Phase 5
Phase 14 (Future) ← Phase 10
```

### Dependency Matrix

| Phase | Depends On | Can Parallel With |
|-------|-----------|-------------------|
| 1 (Packets) | — | 2, 3, 4, 8, 9 |
| 2 (Circular) | — | 1, 3, 4, 8, 9 |
| 3 (Crypto) | — | 1, 2, 4, 8, 9 |
| 4 (UDP Listener) | — | 1, 2, 3, 8, 9 |
| 5 (Handshake) | 1, 2, 4 | 8, 9 |
| 6 (Connection) | 1, 2, 5 | 8, 9 |
| 7 (Reliability) | 2, 6 | 8, 9 |
| 8 (TS Demuxer) | — | 1-7, 9 |
| 9 (Ingress) | — | 1-8 |
| 10 (Bridge) | 7, 8, 9 | — |
| 11 (Integration) | 10 | — |
| 12 (E2E/Docs) | 11 | — |

### Parallelism Strategy
- **Track A** (SRT protocol): Phases 1 → 2 → 4 → 5 → 6 → 7
- **Track B** (Media): Phases 8 → (codec conversion in 10)
- **Track C** (Architecture): Phase 9
- **Track D** (Crypto): Phase 3 (can be done anytime, used in Phase 13)
- **Merge**: Phase 10 merges Track A + B + C

---

## Risk Register

| # | Risk | Impact | Likelihood | Mitigation |
|---|------|--------|------------|------------|
| 1 | **FFmpeg/OBS interop failure** — handshake negotiation differences | Blocking | Medium | Test early with real FFmpeg; capture Wireshark traces of working gosrt sessions |
| 2 | **Timestamp drift** — 90kHz→1kHz conversion accumulates error over hours | Quality degradation | High | Track fractional remainder per-track; periodically re-sync from PTS |
| 3 | **MPEG-TS boundary issues** — PES spanning SRT packets with reordering | Data corruption | Medium | Ensure TS demuxer buffers handle arbitrary chunk boundaries |
| 4 | **ARQ complexity** — subtle bugs in retransmit/reorder logic | Reliability | High | Comprehensive tests; reference gosrt as gold standard; gradual bring-up |
| 5 | **Performance** — per-packet goroutine/timer overhead | Scalability | Low | Profile early; use ticker pools instead of per-connection timers |
| 6 | **Scope creep** — encryption/HEVC needed sooner than planned | Schedule | Medium | Hard boundary at Phase 12 for v0.2.0; encryption is separate release |
| 7 | **B-frame handling** — incorrect CTS causes A/V desync | Quality | Medium | Test with B-frame content; verify CTS = (PTS-DTS)/90 |
| 8 | **UDP port reuse** — firewall/NAT complications | Connectivity | Low | Document required UDP port openings; test with Docker |
| 9 | **Sequence number wrap** — 31-bit wraps at ~35 min at high packet rates | Crash/stall | Medium | Extensive circular math tests; long-running soak test |
| 10 | **Memory growth** — receive buffer grows under heavy loss | OOM | Medium | Implement TLPKTDROP; cap buffer size; monitor in metrics |

---

## References

1. **SRT IETF Draft**: https://haivision.github.io/srt-rfc/draft-sharabayko-srt.html
2. **Haivision SRT GitHub**: https://github.com/Haivision/srt
3. **SRT Handshake Docs**: https://github.com/Haivision/srt/blob/master/docs/features/handshake.md
4. **SRT Cookbook (FFmpeg)**: https://srtlab.github.io/srt-cookbook/apps/ffmpeg.html
5. **datarhei/gosrt** (reference Go impl): https://github.com/datarhei/gosrt
6. **RFC 3394** (AES Key Wrap): https://datatracker.ietf.org/doc/html/rfc3394
7. **ISO 13818-1** (MPEG-TS): Standard defining Transport Stream format
8. **ISO 14496-10** (H.264/AVC): Annex B byte stream format
9. **ISO 14496-3** (AAC): AudioSpecificConfig format
10. **SRS SRT Integration**: https://ossrs.net/lts/en-us/docs/v6/doc/srt
11. **OBS SRT Guide**: https://obsproject.com/kb/srt-protocol-streaming-guide

# SRT Protocol Support

go-rtmp supports SRT (Secure Reliable Transport) as an ingest protocol alongside RTMP. SRT streams are automatically converted to RTMP format, making them transparent to subscribers.

## Overview

SRT is a UDP-based protocol designed for reliable, low-latency live video transport over the public internet. Unlike RTMP (which runs over TCP), SRT uses UDP with its own reliability layer, allowing it to handle packet loss and jitter more gracefully.

### Data Flow

```
SRT Publisher (FFmpeg/OBS)
    │
    ▼ UDP + MPEG-TS
┌─────────────────────────────────┐
│  SRT Listener (:10080 UDP)      │
│    │                            │
│    ▼                            │
│  SRT Handshake v5               │
│  (cookie, extensions, stream ID)│
│    │                            │
│    ▼                            │
│  SRT Connection                 │
│  (reliability: ACK/NAK/TSBPD)  │
│    │                            │
│    ▼                            │
│  MPEG-TS Demuxer                │
│  (PAT/PMT → PES → H.264/AAC)   │
│    │                            │
│    ▼                            │
│  Codec Converter                │
│  H.264: Annex B → AVCC          │
│  AAC:   ADTS → Raw              │
│    │                            │
│    ▼                            │
│  chunk.Message (RTMP format)    │
│    │                            │
│    ▼                            │
│  Stream Registry (shared)       │
│    │                            │
│    ▼                            │
│  RTMP Subscribers / Recording   │
└─────────────────────────────────┘
```

## Quick Start

### Enable SRT Ingest

```bash
# Run with both RTMP and SRT
./rtmp-server -listen :1935 -srt-listen :10080

# With SRT encryption (AES-256)
./rtmp-server -listen :1935 -srt-listen :10080 -srt-passphrase "mysecret"

# With custom latency (default 120ms)
./rtmp-server -listen :1935 -srt-listen :10080 -srt-latency 200
```

### Publish via SRT

```bash
# FFmpeg with SRT output (publish mode)
ffmpeg -re -i test.mp4 -c copy -f mpegts \
  "srt://localhost:10080?streamid=publish:live/test"

# With structured stream ID
ffmpeg -re -i test.mp4 -c copy -f mpegts \
  "srt://localhost:10080?streamid=#!::r=live/test,m=publish"

# With SRT encryption
ffmpeg -re -i test.mp4 -c copy -f mpegts \
  "srt://localhost:10080?streamid=publish:live/test&passphrase=mysecret"
```

### Subscribe via RTMP

SRT streams appear as regular RTMP streams — subscribe with any RTMP client:

```bash
# Watch via RTMP (same as any RTMP stream)
ffplay rtmp://localhost:1935/live/test

# Or via RTMPS
ffplay rtmps://localhost:443/live/test
```

## CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-srt-listen` | (disabled) | SRT UDP listen address (e.g., `:10080`) |
| `-srt-latency` | `120` | TSBPD buffer latency in milliseconds |
| `-srt-passphrase` | (none) | AES encryption passphrase (10-79 chars; empty = no encryption) |
| `-srt-pbkeylen` | `16` | AES key length: 16 (AES-128), 24 (AES-192), or 32 (AES-256) |

### SRT Encryption

SRT encryption provides end-to-end AES-CTR encryption of media data using a shared passphrase. The implementation follows the [Haivision SRT encryption specification](https://github.com/Haivision/srt/blob/master/docs/features/encryption.md).

**Key Exchange Flow:**
1. Client generates a random 16-byte salt and Stream Encrypting Key (SEK)
2. Client derives a Key Encrypting Key (KEK) via PBKDF2-HMAC-SHA1 from passphrase + LSB 64 bits of salt (2048 iterations)
3. Client wraps SEK with KEK using AES Key Wrap (RFC 3394)
4. Client sends wrapped key in KMREQ extension during Conclusion handshake
5. Server derives same KEK, unwraps SEK, sends KMRSP to confirm
6. All data packets are encrypted/decrypted with AES-CTR using the SEK

**Key Rotation:**
For long-running streams, the sender periodically rotates the SEK (typically every ~2^24 packets / ~6-7 hours). SRT uses an even/odd dual-key model:
- Both even and odd key slots are maintained simultaneously
- The sender pre-announces the new key via a post-handshake KMREQ control packet
- Data packets carry a KK flag (2 bits) indicating which key encrypted the payload
- The receiver installs the new key and acknowledges with KMRSP
- Transition is hitless — no packets are lost during rekeying

**Supported configurations:**
| Key Length | Flag Value | Security Level |
|-----------|-----------|----------------|
| AES-128 | `-srt-pbkeylen 16` | Standard |
| AES-192 | `-srt-pbkeylen 24` | Enhanced |
| AES-256 | `-srt-pbkeylen 32` | Maximum (recommended) |

**Crypto profile requirements:** Only AES-CTR cipher, no authentication (beyond key wrap integrity), MPEG-TS/SRT encapsulation, and passphrase-derived KEK (KEKI=0) are supported. Unsupported configurations are rejected with clear error messages.

## Stream ID Format

SRT uses Stream IDs to identify what resource the client wants to access and whether they want to publish or subscribe. Three formats are supported:

### 1. Simple Format
```
live/mystream
```
The entire string is treated as the resource name. Mode defaults to "request" (subscribe).

### 2. Prefixed Format
```
publish:live/mystream
```
The prefix specifies the mode (`publish` or `request`).

### 3. Structured Format (SRT Access Control)
```
#!::r=live/mystream,m=publish,u=user1
```
Key-value pairs with single-letter keys:
- `r` — Resource (stream key)
- `m` — Mode (`publish` or `request`)
- `u` — Username
- `s` — Session ID
- `h` — Hostname
- `t` — Type (`stream`, `file`, `auth`)

## SRT Metrics

When the metrics endpoint is enabled (`-metrics-addr :8080`), SRT-specific counters are available at `/debug/vars`:

| Metric | Type | Description |
|--------|------|-------------|
| `srt_connections_active` | Gauge | Currently connected SRT publishers |
| `srt_connections_total` | Counter | Total SRT connections accepted |
| `srt_bytes_received` | Counter | Total bytes received over SRT |
| `srt_packets_received` | Counter | Total data packets received |
| `srt_packets_retransmit` | Counter | Retransmitted packets |
| `srt_packets_dropped` | Counter | Packets dropped (too late) |

## Architecture

### Package Structure

```
internal/srt/
├── packet/       SRT wire protocol: headers, data, control, ACK, NAK
├── circular/     31-bit sequence number arithmetic with wraparound
├── crypto/       AES-CTR cipher, KeySet (even/odd), KM parser, PBKDF2, AES Key Wrap
├── handshake/    SRT v5 handshake FSM with SYN cookies
├── conn/         Connection state machine with reliability
│   ├── sender    Send buffer, retransmission queue, RTT tracking
│   ├── receiver  Receive buffer, loss detection, reordering
│   ├── tsbpd     Timestamp-Based Packet Delivery scheduling
│   └── reliability  Background loop: ACK/NAK/keepalive tickers
├── bridge.go     SRT→RTMP conversion pipeline
├── listener.go   UDP socket multiplexer
├── stream_id.go  Stream ID parser (3 formats)
└── config.go     Configuration with defaults

internal/ts/        MPEG-TS demuxer (packet, PAT/PMT, PES, stream types)
internal/codec/     H.264 Annex B→AVCC, AAC ADTS→raw converters
internal/ingress/   Protocol-agnostic publish lifecycle manager
```

### Key Design Decisions

1. **UDP Multiplexing**: All SRT connections share a single UDP socket, demultiplexed by (remoteAddr, socketID) pairs.

2. **TSBPD**: Packets are held until their delivery time (timestamp + latency), creating a smooth jitter buffer. Too-late packets are dropped (TLPKTDROP).

3. **Protocol Transparency**: SRT streams are converted to standard `chunk.Message` format before entering the stream registry. Subscribers cannot distinguish SRT from RTMP sources.

4. **Zero Dependencies**: The entire SRT stack — including AES-CTR encryption, PBKDF2 key derivation, AES Key Wrap, KM parsing, MPEG-TS demuxing, and codec conversion — is implemented in pure Go using only the standard library.

## Codec Conversion

### H.264: Annex B → AVCC

MPEG-TS carries H.264 in **Annex B** format (start codes: `0x00000001`). RTMP expects **AVCC** format (4-byte length prefixes). The bridge:

1. Splits the bitstream at start codes to extract individual NALUs
2. Identifies SPS/PPS NALUs and builds an AVCDecoderConfigurationRecord (sequence header)
3. Replaces start codes with 4-byte big-endian lengths
4. Wraps in RTMP video tag format (FrameType + CodecID + AVCPacketType + CTS)

### AAC: ADTS → Raw

MPEG-TS wraps each AAC frame in a 7-byte ADTS header. RTMP expects raw AAC with a separate AudioSpecificConfig. The bridge:

1. Parses the ADTS header for profile, sample rate, and channel count
2. Builds a 2-byte AudioSpecificConfig (sent as sequence header)
3. Strips the ADTS header from subsequent frames
4. Wraps in RTMP audio tag format (SoundFormat + AACPacketType)

### Timestamp Conversion

MPEG-TS uses 90kHz clock units. RTMP uses milliseconds. The conversion:

- **RTMP timestamp** = DTS / 90 (decode timestamp, not presentation)
- **CTS** (Composition Time Offset) = (PTS - DTS) / 90 (for B-frame reordering)
- First timestamp is used as base (timestamps start at 0)

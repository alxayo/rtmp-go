# SRT Protocol Support

go-rtmp supports SRT (Secure Reliable Transport) as an ingest protocol alongside RTMP. SRT streams are automatically converted to RTMP format, making them transparent to subscribers.

## Overview

SRT is a UDP-based protocol designed for reliable, low-latency live video transport over the public internet. Unlike RTMP (which runs over TCP), SRT uses UDP with its own reliability layer, allowing it to handle packet loss and jitter more gracefully.

### Data Flow

```
SRT Publisher (FFmpeg/OBS)
    │
    ▼ UDP + MPEG-TS or Matroska/WebM
┌─────────────────────────────────────┐
│  SRT Listener (:10080 UDP)          │
│    │                                │
│    ▼                                │
│  SRT Handshake v5                   │
│  (cookie, extensions, stream ID)    │
│    │                                │
│    ▼                                │
│  SRT Connection                     │
│  (reliability: ACK/NAK/TSBPD)      │
│    │                                │
│    ▼                                │
│  Container Auto-Detection           │
│  (first 4 bytes: MKV or TS?)       │
│    │                                │
│    ├── 0x47 sync → MPEG-TS Demuxer  │
│    │   (PAT/PMT → PES → frames)    │
│    │                                │
│    └── 0x1A45DFA3 → MKV Demuxer    │
│        (EBML → Tracks → Clusters)   │
│    │                                │
│    ▼                                │
│  Codec Converter                    │
│  TS:  Annex B → AVCC, ADTS → Raw   │
│  MKV: Length-prefix → AVCC, raw AAC │
│    │                                │
│    ▼                                │
│  chunk.Message (RTMP format)        │
│    │                                │
│    ▼                                │
│  Stream Registry (shared)           │
│    │                                │
│    ▼                                │
│  RTMP Subscribers / Recording       │
└─────────────────────────────────────┘
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
# FFmpeg with MPEG-TS (H.264 + AAC)
ffmpeg -re -i test.mp4 -c copy -f mpegts \
  "srt://localhost:10080?streamid=publish:live/test"

# FFmpeg with Matroska (VP9 + Opus — not possible with MPEG-TS!)
ffmpeg -re -i test.mp4 -c:v libvpx-vp9 -c:a libopus -f matroska \
  "srt://localhost:10080?streamid=publish:live/vp9test"

# FFmpeg with Matroska (AV1 + Opus)
ffmpeg -re -i test.mp4 -c:v libsvtav1 -c:a libopus -f matroska \
  "srt://localhost:10080?streamid=publish:live/av1test"

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
├── bridge.go     SRT→RTMP conversion pipeline with container auto-detection
├── bridge_mkv.go Matroska/WebM frame handlers (VP8, VP9, AV1, Opus, FLAC, etc.)
├── listener.go   UDP socket multiplexer
├── stream_id.go  Stream ID parser (3 formats)
└── config.go     Configuration with defaults

internal/ts/        MPEG-TS demuxer (packet, PAT/PMT, PES, stream types)
internal/mkv/       Matroska/WebM demuxer (EBML parser, streaming state machine)
internal/codec/     Codec converters: Annex B→AVCC, ADTS→raw, MKV helpers,
                    Enhanced RTMP tag builders (VP8, VP9, AV1, Opus, FLAC, etc.)
internal/ingress/   Protocol-agnostic publish lifecycle manager
```

### Key Design Decisions

1. **UDP Multiplexing**: All SRT connections share a single UDP socket, demultiplexed by (remoteAddr, socketID) pairs.

2. **TSBPD**: Packets are held until their delivery time (timestamp + latency), creating a smooth jitter buffer. Too-late packets are dropped (TLPKTDROP).

3. **Protocol Transparency**: SRT streams are converted to standard `chunk.Message` format before entering the stream registry. Subscribers cannot distinguish SRT from RTMP sources.

4. **Zero Dependencies**: The entire SRT stack — including AES-CTR encryption, PBKDF2 key derivation, AES Key Wrap, KM parsing, MPEG-TS demuxing, Matroska/WebM demuxing, EBML parsing, and codec conversion — is implemented in pure Go using only the standard library.

### Supported Codecs

| Codec | Container | E-RTMP FourCC | Notes |
|-------|-----------|---------------|-------|
| H.264 (AVC) | TS, MKV | `avc1` | TS: Annex B→AVCC; MKV: length-prefix normalization |
| H.265 (HEVC) | TS, MKV | `hvc1` | TS: Annex B→HVCC; MKV: length-prefix normalization |
| VP8 | MKV | `vp08` | MKV only (no TS stream type) |
| VP9 | MKV | `vp09` | MKV only (no TS stream type) |
| AV1 | MKV | `av01` | MKV only (no standard TS binding) |
| AAC | TS, MKV | legacy | TS: ADTS→raw; MKV: raw with AudioSpecificConfig |
| Opus | MKV | `Opus` | MKV only (no TS stream type) |
| FLAC | MKV | `fLaC` | MKV only (no TS stream type) |
| AC-3 | TS, MKV | `ac-3` | Syncframe pass-through |
| E-AC-3 | TS, MKV | `ec-3` | Syncframe pass-through |

## Codec Conversion

The SRT bridge supports two container formats, each requiring different codec conversion strategies.

### MPEG-TS Container

#### H.264: Annex B → AVCC

MPEG-TS carries H.264 in **Annex B** format (start codes: `0x00000001`). RTMP expects **AVCC** format (4-byte length prefixes). The bridge:

1. Splits the bitstream at start codes to extract individual NALUs
2. Identifies SPS/PPS NALUs and builds an AVCDecoderConfigurationRecord (sequence header)
3. Replaces start codes with 4-byte big-endian lengths
4. Wraps in RTMP video tag format (FrameType + CodecID + AVCPacketType + CTS)

#### H.265: Annex B → HVCC

Same Annex B→length-prefix conversion as H.264, but with VPS/SPS/PPS NALUs. Builds HEVCDecoderConfigurationRecord for the sequence header.

#### AAC: ADTS → Raw

MPEG-TS wraps each AAC frame in a 7-byte ADTS header. RTMP expects raw AAC with a separate AudioSpecificConfig. The bridge:

1. Parses the ADTS header for profile, sample rate, and channel count
2. Builds a 2-byte AudioSpecificConfig (sent as sequence header)
3. Strips the ADTS header from subsequent frames
4. Wraps in RTMP audio tag format (SoundFormat + AACPacketType)

#### AC-3 / E-AC-3

Syncframes pass through directly — same format in both MPEG-TS and RTMP.

### Matroska/WebM Container

Matroska uses different internal formats than MPEG-TS, requiring separate codec handling:

#### H.264 (MKV): Length-Prefixed NALUs

MKV stores H.264 in **AVCC-like format** (length-prefixed NALUs, not Annex B). The CodecPrivate field IS the AVCDecoderConfigurationRecord. However, the NALU length field size may vary (1–4 bytes), so the bridge:

1. Parses the AVCDecoderConfigurationRecord to extract SPS, PPS, and lengthSizeMinusOne
2. Rebuilds the config record with 4-byte NALU lengths (RTMP standard)
3. Splits frame data using the source length size, re-wraps with 4-byte lengths
4. Wraps in Enhanced RTMP video tag format

#### H.265 (MKV): Length-Prefixed NALUs

Same approach as H.264 MKV. Parses HEVCDecoderConfigurationRecord for VPS/SPS/PPS arrays.

#### AAC (MKV): Raw Frames

MKV stores AAC as raw frames (no ADTS headers). The CodecPrivate field contains the AudioSpecificConfig (typically 2 bytes). The bridge wraps this config as an RTMP sequence header and passes frames through directly.

#### VP8, VP9, AV1 (MKV): Direct Mapping

These codecs store raw frames in MKV. The bridge wraps them in Enhanced RTMP video tags using their respective FourCC codes (`vp08`, `vp09`, `av01`). AV1 frames include the configOBUs from CodecPrivate as the sequence header.

#### Opus, FLAC (MKV): Direct Mapping

Opus and FLAC store raw frames in MKV. The bridge wraps them in Enhanced RTMP audio tags using FourCC codes (`Opus`, `fLaC`). FLAC includes the full stream info from CodecPrivate as the sequence header.

### Container Auto-Detection

The bridge automatically detects the container format from the first bytes of SRT payload:

| First Bytes | Container | Detection Method |
|-------------|-----------|-----------------|
| `0x1A 0x45 0xDF 0xA3` | Matroska/WebM | EBML header element ID |
| `0x47` at offset 0 and 188 | MPEG-TS | Dual sync byte validation |
| `0x47` at offset 0 only | MPEG-TS (fallback) | Single sync byte |
| Other | MPEG-TS (default) | Backward compatibility |

The first 189 bytes are buffered for detection, then replayed into the chosen demuxer. This is transparent to the publisher — no configuration or Stream ID hints are needed.

### Timestamp Conversion

**MPEG-TS** uses 90kHz clock units. **Matroska** uses milliseconds (scaled by TimecodeScale). RTMP uses milliseconds.

- **TS → RTMP**: timestamp = DTS / 90, CTS = (PTS - DTS) / 90
- **MKV → RTMP**: timestamp = (clusterTimecode + blockOffset) × timecodeScale / 1,000,000
- First timestamp is used as base (timestamps start at 0)
- MKV path uses CTS=0 (no B-frame reordering in live VP8/VP9/AV1 streams)

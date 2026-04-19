---
title: "SRT Ingest"
weight: 7
---

# SRT Ingest

go-rtmp accepts [SRT (Secure Reliable Transport)](https://github.com/Haivision/srt) streams over UDP alongside RTMP. SRT publishers are transparently converted to RTMP format — existing RTMP subscribers can watch SRT sources without any changes.

## Enabling SRT

Add the `-srt-listen` flag to start the UDP listener:

```bash
./rtmp-server -listen :1935 -srt-listen :4200 -log-level info
```

The server now accepts RTMP on TCP port 1935 and SRT on UDP port 4200 simultaneously.

## Publishing via SRT

### FFmpeg

```bash
ffmpeg -re -i test.mp4 -c copy -f mpegts \
  "srt://localhost:4200?streamid=live/test&pkt_size=1316"
```

### OBS Studio

1. Settings → Stream → Service: **Custom**
2. Server: `srt://your-server:4200`
3. Stream Key: `live/test`

### IP Cameras

Most IP cameras with SRT support can publish directly. Set the SRT destination to:

```
srt://your-server:4200?streamid=live/camera1
```

## Stream ID Formats

SRT uses the Stream ID to identify the target stream key. Three formats are supported:

| Format | Example | Stream Key |
|--------|---------|------------|
| Simple | `live/test` | `live/test` |
| Prefixed | `publish:live/test` | `live/test` |
| Structured | `#!::r=live/test,m=publish` | `live/test` |

## SRT Encryption

SRT encryption provides end-to-end AES-CTR encryption of all media data using a shared passphrase. When enabled, clients must provide the correct passphrase during the SRT handshake — connections with wrong or missing passphrases are rejected.

**Server setup:**

```bash
# AES-128 (default key length)
./rtmp-server -listen :1935 -srt-listen :4200 -srt-passphrase "my-secret-key"

# AES-256 (recommended for maximum security)
./rtmp-server -listen :1935 -srt-listen :4200 -srt-passphrase "my-secret-key" -srt-pbkeylen 32
```

**Publisher (FFmpeg):**

```bash
ffmpeg -re -i test.mp4 -c copy -f mpegts \
  "srt://localhost:4200?streamid=publish:live/test&passphrase=my-secret-key&pbkeylen=32"
```

**Publisher (OBS Studio):**

In OBS, set the Stream URL to `srt://server:4200?streamid=publish:live/test&passphrase=my-secret-key`.

| Flag | Default | Description |
|------|---------|-------------|
| `-srt-passphrase` | *(none)* | Shared secret for AES encryption (10-79 characters) |
| `-srt-pbkeylen` | `16` | Key length in bytes: 16 (AES-128), 24 (AES-192), or 32 (AES-256) |

#### How It Works

During the SRT handshake, the client and server exchange encryption keys:

1. The client generates a random Stream Encrypting Key (SEK) and salt
2. The SEK is wrapped using a Key Encrypting Key (KEK) derived from the shared passphrase via PBKDF2
3. The wrapped key is sent to the server in a KMREQ extension
4. The server derives the same KEK, unwraps the SEK, and confirms with KMRSP
5. All subsequent data packets are encrypted with AES-CTR

#### Key Rotation

For long-running streams, the client automatically rotates the encryption key (typically every ~6-7 hours). SRT uses a hitless dual-key model — both the old and new keys are active during the transition, so no packets are lost.

#### Supported Key Lengths

| Key Length | `-srt-pbkeylen` | Notes |
|-----------|-----------------|-------|
| AES-128 | `16` (default) | Standard security |
| AES-192 | `24` | Enhanced security |
| AES-256 | `32` | Maximum security (recommended) |

#### Security Notes

- The passphrase must be 10-79 characters (enforced by the SRT specification)
- Connections without a passphrase are rejected when encryption is configured
- Plaintext packets on encrypted connections are dropped
- The server validates all crypto parameters and rejects unsupported configurations

### Per-Stream Encryption

Instead of a single passphrase for all streams, you can assign each stream its own passphrase using a JSON file. This is useful when different publishers need independent credentials.

**1. Create a passphrase file** (e.g. `/etc/rtmp/srt-keys.json`):

```json
{
  "live/stream1": "passphrase-at-least-10-chars",
  "live/stream2": "another-secret-key-here"
}
```

Each key is a stream key and each value is the passphrase (10-79 characters per the SRT spec). Passphrases are validated at load time — the server refuses to start if any are out of range.

**2. Start the server with `-srt-passphrase-file`:**

```bash
./rtmp-server -srt-listen :10080 -srt-passphrase-file /etc/rtmp/srt-keys.json -srt-pbkeylen 16
```

**3. Publish with the stream's passphrase:**

```bash
ffmpeg -re -i input.mp4 -c copy -f mpegts \
  "srt://server:10080?streamid=publish:live/stream1&passphrase=passphrase-at-least-10-chars&pbkeylen=16"
```

Each client provides the passphrase assigned to its stream key — exactly the same syntax as single-passphrase mode.

**4. Hot reload via SIGHUP:**

You can update the passphrase file and apply changes without restarting the server:

```bash
kill -HUP $(pidof rtmp-server)
```

The server re-reads the file and swaps in the new passphrases. If the file contains errors (invalid JSON, out-of-range passphrases), the reload is rejected and the previous valid passphrases are preserved.

> **Note:** `-srt-passphrase` and `-srt-passphrase-file` are mutually exclusive. The server will refuse to start if both are set. Use `-srt-passphrase` for a single global passphrase or `-srt-passphrase-file` for per-stream passphrases.

## Latency Tuning

The `-srt-latency` flag controls the TSBPD (Timestamp-Based Packet Delivery) jitter buffer:

```bash
./rtmp-server -srt-listen :4200 -srt-latency 200ms
```

| Latency | Use Case |
|---------|----------|
| `50ms` | Low-latency LAN streaming |
| `120ms` | Default — good for most internet streams |
| `200ms–500ms` | Unreliable networks, high-jitter connections |

## Codec Support

SRT streams can carry either **MPEG-TS** or **Matroska/WebM** containers. The server auto-detects the format from the first bytes of each connection — no configuration needed.

### Supported Codecs

| Codec | Container | E-RTMP FourCC | RTMP Output |
|-------|-----------|---------------|-------------|
| H.264/AVC | TS, MKV | `avc1` | Standard or Enhanced RTMP video |
| H.265/HEVC | TS, MKV | `hvc1` | Enhanced RTMP video |
| VP8 | MKV only | `vp08` | Enhanced RTMP video |
| VP9 | MKV only | `vp09` | Enhanced RTMP video |
| AV1 | MKV only | `av01` | Enhanced RTMP video |
| AAC | TS, MKV | legacy | Standard RTMP audio |
| Opus | MKV only | `Opus` | Enhanced RTMP audio |
| FLAC | MKV only | `fLaC` | Enhanced RTMP audio |
| AC-3 | TS, MKV | `ac-3` | Enhanced RTMP audio |
| E-AC-3 | TS, MKV | `ec-3` | Enhanced RTMP audio |

### Container Selection Guide

| Use Case | Container | FFmpeg `-f` flag |
|----------|-----------|-----------------|
| H.264 + AAC (standard) | MPEG-TS | `-f mpegts` |
| H.265 + AAC | MPEG-TS | `-f mpegts` |
| VP8/VP9 video | Matroska | `-f matroska` |
| AV1 video | Matroska | `-f matroska` |
| Opus/FLAC audio | Matroska | `-f matroska` |
| Any codec mix | Matroska | `-f matroska` |

### Publishing with Matroska

```bash
# VP9 video + Opus audio
ffmpeg -re -i test.mp4 -c:v libvpx-vp9 -c:a libopus -f matroska \
  "srt://localhost:4200?streamid=publish:live/vp9test"

# AV1 video + Opus audio
ffmpeg -re -i test.mp4 -c:v libsvtav1 -c:a libopus -f matroska \
  "srt://localhost:4200?streamid=publish:live/av1test"

# VP8 video + FLAC audio
ffmpeg -re -i test.mp4 -c:v libvpx -c:a flac -f matroska \
  "srt://localhost:4200?streamid=publish:live/vp8test"

# H.264 in Matroska (also works — MKV supports all codecs)
ffmpeg -re -i test.mp4 -c copy -f matroska \
  "srt://localhost:4200?streamid=publish:live/h264mkv"
```

### How Auto-Detection Works

When a new SRT connection starts sending data, the server inspects the first bytes:

- **`0x1A 0x45 0xDF 0xA3`** (EBML header) → Matroska/WebM demuxer
- **`0x47` sync byte** at expected positions → MPEG-TS demuxer

The detection is transparent — publishers simply use `-f mpegts` or `-f matroska` in FFmpeg (or equivalent in OBS). All codecs are converted to Enhanced RTMP for delivery to RTMP subscribers.

## Recording SRT Streams

SRT streams are recorded just like RTMP streams when `-record-all` is enabled. The server automatically selects the container format based on the video codec:

- **H.264 streams** → FLV recording
- **H.265/VP8/VP9/AV1 streams** → MP4 recording

```bash
./rtmp-server -srt-listen :4200 -record-all true -record-dir ./recordings
```

## SRT Metrics

When metrics are enabled (`-metrics-addr`), SRT adds 6 counters:

| Counter | Description |
|---------|-------------|
| `srt_connections_active` | Currently connected SRT publishers |
| `srt_connections_total` | Total SRT connections since startup |
| `srt_bytes_received` | Total bytes received over SRT |
| `srt_packets_received` | Total SRT data packets received |
| `srt_packets_retransmit` | Packets retransmitted (NAK recovery) |
| `srt_packets_dropped` | Packets dropped (too late for TSBPD) |

## Architecture

```
SRT Publisher → UDP → SRT Handshake → TSBPD Buffer
    → Container Auto-Detection (MPEG-TS or Matroska/WebM)
    → Codec Convert → chunk.Message → Stream Registry
    → RTMP Subscribers / Recording / Relay
```

The conversion is transparent: RTMP subscribers see the SRT source as a regular RTMP publisher, regardless of whether the SRT stream uses MPEG-TS or Matroska/WebM.

## Example: Full Setup

```bash
# Accept RTMP, SRT, and RTMPS — record everything, expose metrics
./rtmp-server \
  -listen :1935 \
  -tls-listen :1936 \
  -tls-cert cert.pem \
  -tls-key key.pem \
  -srt-listen :4200 \
  -srt-latency 120ms \
  -record-all true \
  -record-dir /data/recordings \
  -metrics-addr :8080 \
  -log-level info
```

Publish via SRT and watch via RTMP:

```bash
# Publish H.265 via SRT (MPEG-TS)
ffmpeg -re -i test.mp4 -c:v libx265 -c:a aac -f mpegts \
  "srt://localhost:4200?streamid=live/test"

# Publish VP9 + Opus via SRT (Matroska)
ffmpeg -re -i test.mp4 -c:v libvpx-vp9 -c:a libopus -f matroska \
  "srt://localhost:4200?streamid=live/vp9"

# Publish AV1 via SRT (Matroska)
ffmpeg -re -i test.mp4 -c:v libsvtav1 -c:a libopus -f matroska \
  "srt://localhost:4200?streamid=live/av1"

# Subscribe via RTMP (works for any SRT publisher above)
ffplay rtmp://localhost:1935/live/test
```

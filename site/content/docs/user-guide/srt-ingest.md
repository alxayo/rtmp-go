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

> **Status: Not Yet Functional** — The encryption infrastructure (PBKDF2 key derivation, AES Key Wrap) is implemented, but the key exchange (KMREQ/KMRSP) is not yet integrated into the SRT handshake. The `-srt-passphrase` and `-srt-pbkeylen` flags are accepted but have no effect.
>
> **Workaround**: Use [RTMPS (TLS)]({{< relref "rtmps" >}}) for encrypted transport. RTMPS encrypts the entire connection including all media data.

The following flags are reserved for future SRT encryption support:

| Flag | Default | Description |
|------|---------|-------------|
| `-srt-passphrase` | *(none)* | Shared secret for AES encryption (not yet enforced) |
| `-srt-pbkeylen` | `16` | Key length in bytes: 16 (AES-128), 24 (AES-192), or 32 (AES-256) |

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

SRT streams carry MPEG-TS containers. The server automatically detects and converts:

| Codec | Support | RTMP Output |
|-------|---------|-------------|
| H.264/AVC | ✅ Full | Standard RTMP video (TypeID 9) |
| H.265/HEVC | ✅ Full | Enhanced RTMP with FourCC `hvc1` |
| AAC | ✅ Full | Standard RTMP audio (TypeID 8) |

H.265 streams from SRT are automatically converted to Enhanced RTMP format, allowing modern players (FFmpeg 6.1+, OBS 29.1+) to subscribe.

## Recording SRT Streams

SRT streams are recorded just like RTMP streams when `-record-all` is enabled. The server automatically selects the container format:

- **H.264 streams** → FLV recording
- **H.265 streams** → MP4 recording

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
SRT Publisher → UDP → SRT Handshake → TSBPD Buffer → MPEG-TS Demux
    → Codec Convert (Annex B→AVCC, ADTS→raw) → chunk.Message → Stream Registry
    → RTMP Subscribers / Recording / Relay
```

The conversion is transparent: RTMP subscribers see the SRT source as a regular RTMP publisher.

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
# Publish H.265 via SRT
ffmpeg -re -i test.mp4 -c:v libx265 -c:a aac -f mpegts \
  "srt://localhost:4200?streamid=live/test"

# Subscribe via RTMP
ffplay rtmp://localhost:1935/live/test
```

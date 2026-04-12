---
title: "Roadmap"
weight: 2
---

# Roadmap

## Completed

- ✅ **Core RTMP protocol** — handshake, chunks, AMF0, commands, media relay *(v0.1.0)*
- ✅ **FLV recording** — automatic recording with timestamped filenames *(v0.1.0)*
- ✅ **Multi-destination relay** — forward to YouTube, Twitch, and custom servers *(v0.1.0)*
- ✅ **Event hooks** — webhooks, shell scripts, stdio *(v0.1.0)*
- ✅ **Authentication** — token, file, callback, and open backends *(v0.1.1)*
- ✅ **Expvar metrics** — live counters via HTTP endpoint *(v0.1.2)*
- ✅ **TCP deadline enforcement** — zombie detection and cleanup *(v0.1.2)*
- ✅ **Performance optimizations** — AMF0 decode, chunk writer, RPC lazy-init *(v0.1.2)*
- ✅ **RTMPS (TLS)** — encrypted RTMP connections via TLS termination *(v0.1.3)*
- ✅ **E2E testing scripts** — cross-platform Bash + PowerShell test suite for RTMP/RTMPS/HLS/auth *(v0.1.3)*
- ✅ **Enhanced RTMP (E-RTMP v2)** — H.265, AV1, VP9 codec support via FourCC signaling *(v0.1.4)*
- ✅ **SRT ingest** — accept SRT streams over UDP with transparent RTMP conversion *(v0.2.0)*
- ✅ **MPEG-TS demuxer** — full transport stream parser for SRT-to-RTMP bridge *(v0.2.0)*
- ✅ **Codec converters** — H.264/H.265/AAC format converters for protocol bridging *(v0.2.0)*
- ✅ **Codec-aware recording** — automatic FLV/MP4 container selection based on codec *(v0.2.0)*
- ✅ **Comprehensive E2E test suite** — 25+ tests covering all features with cross-platform runners *(v0.2.0)*

## In Progress

- 🔄 **Fuzz testing** — fuzz testing for AMF0 and chunk parsing to find edge cases and crashes

## Planned

- 📋 **Configurable backpressure** — tunable queue sizes, drop policies, and subscriber eviction strategies
- 📋 **Clustering & HA** — multi-node stream distribution with failover
- 📋 **DVR / Time-shift** — seek-back into recorded streams for live rewind
- 📋 **Transcoding** — on-the-fly quality adaptation (ABR) for multi-bitrate delivery *(note: H.265/HEVC is now natively supported for passthrough via E-RTMP v2 and SRT ingest)*

## How to Contribute

If you're interested in working on any planned feature, open an issue to discuss the approach before starting. See the [Contributing Guide]({{< relref "/docs/developer/contributing" >}}) for workflow details.

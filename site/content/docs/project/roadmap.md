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

## In Progress

- 🔄 **Fuzz testing** — fuzz testing for AMF0 and chunk parsing to find edge cases and crashes

## Planned

- 📋 **Configurable backpressure** — tunable queue sizes, drop policies, and subscriber eviction strategies
- 📋 **Clustering & HA** — multi-node stream distribution with failover
- 📋 **DVR / Time-shift** — seek-back into recorded streams for live rewind
- 📋 **Transcoding** — on-the-fly quality adaptation (ABR) for multi-bitrate delivery

## How to Contribute

If you're interested in working on any planned feature, open an issue to discuss the approach before starting. See the [Contributing Guide]({{< relref "/docs/developer/contributing" >}}) for workflow details.

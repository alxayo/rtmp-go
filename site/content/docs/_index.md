---
title: "go-rtmp Documentation"
type: docs
---

# go-rtmp

**A production-ready RTMP server in pure Go. Zero external dependencies.**

Stream from OBS/FFmpeg → go-rtmp server → multiple viewers + FLV recording + multi-destination relay.

---

## What is go-rtmp?

go-rtmp is a lightweight, high-performance RTMP server built entirely on Go's standard library. It receives live audio/video streams from tools like OBS Studio or FFmpeg, and can simultaneously:

- **Relay** streams to unlimited subscribers in real-time
- **Record** streams to FLV files on disk
- **Forward** streams to external RTMP servers (YouTube, Twitch, custom CDNs)
- **Notify** external services via webhooks, shell scripts, or stdio hooks
- **Authenticate** publishers and subscribers with pluggable token-based validation
- **Monitor** live metrics via HTTP `/debug/vars` endpoint

## Key Features

| Feature | Description |
|---------|-------------|
| **Zero Dependencies** | Built entirely on Go's standard library — no vendor lock-in |
| **RTMP v3 Protocol** | Full handshake, chunk streaming, AMF0 commands |
| **Live Relay** | Transparent pub/sub forwarding to unlimited subscribers |
| **FLV Recording** | Automatic recording with timestamped filenames |
| **Late-Join Support** | H.264 SPS/PPS + AAC config caching for instant playback |
| **Multi-Destination Relay** | Forward to external RTMP servers (`-relay-to`) |
| **Authentication** | Static tokens, JSON file, or webhook callback |
| **Event Hooks** | Webhooks, shell scripts, stdio for all lifecycle events |
| **Expvar Metrics** | Live counters via HTTP endpoint |
| **Zombie Detection** | TCP deadline enforcement (read 90s, write 30s) |

## Get Started

{{< columns >}}

- ### 🚀 [Quick Start]({{< relref "/docs/quick-start" >}})

  Build, run, and stream in **under 5 minutes**. No configuration needed.

- ### 📖 [User Guide]({{< relref "/docs/user-guide" >}})

  Configure recording, relay, authentication, hooks, metrics, and more.

- ### 🔧 [Developer Guide]({{< relref "/docs/developer" >}})

  Architecture, RTMP protocol reference, code walkthrough, and testing.

{{< /columns >}}

## Supported Platforms

| Platform | Architecture | Binary |
|----------|-------------|--------|
| Linux | x86_64, ARM64 | `rtmp-server-linux-amd64` |
| macOS | Intel, Apple Silicon | `rtmp-server-darwin-arm64` |
| Windows | x86_64 | `rtmp-server-windows-amd64.exe` |

## Requirements

- **Go 1.21+** (to build from source)
- **FFmpeg** (optional, for testing with `ffmpeg`/`ffplay`)
- **OBS Studio** (optional, for live streaming from camera/screen)

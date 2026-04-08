---
title: "go-rtmp"
type: page
---

<div style="text-align: center; padding: 4rem 0 2rem 0;">
  <h1 style="font-size: 3rem; margin-bottom: 0.5rem;">go-rtmp</h1>
  <p style="font-size: 1.4rem; color: #666; margin-bottom: 2rem;">
    Production-ready RTMP server in pure Go.<br>
    Zero external dependencies.
  </p>
  <p>
    <a href="/go-rtmp/docs/quick-start/" style="display: inline-block; padding: 0.75rem 2rem; background: #0366d6; color: white; text-decoration: none; border-radius: 6px; font-weight: bold; margin: 0.25rem;">🚀 Quick Start</a>
    <a href="https://github.com/alxayo/go-rtmp" style="display: inline-block; padding: 0.75rem 2rem; background: #24292e; color: white; text-decoration: none; border-radius: 6px; font-weight: bold; margin: 0.25rem;">⭐ GitHub</a>
    <a href="/go-rtmp/docs/" style="display: inline-block; padding: 0.75rem 2rem; background: #28a745; color: white; text-decoration: none; border-radius: 6px; font-weight: bold; margin: 0.25rem;">📖 Documentation</a>
  </p>
</div>

---

## What Can go-rtmp Do?

```
OBS / FFmpeg (Publisher)
        │
        ▼ RTMP
┌─────────────────────────────────┐
│         go-rtmp server          │
│                                 │
│  ┌───────────┐  ┌───────────┐  │
│  │  Record   │  │  Relay    │  │
│  │  to FLV   │  │  to CDN   │  │
│  └───────────┘  └───────────┘  │
│                                 │
│  ┌───────────┐  ┌───────────┐  │
│  │  Auth     │  │  Hooks    │  │
│  │  Tokens   │  │  Webhooks │  │
│  └───────────┘  └───────────┘  │
└────────┬──────────┬─────────────┘
         │          │
    ┌────▼───┐ ┌───▼────┐
    │ ffplay │ │  VLC   │ ... (unlimited subscribers)
    └────────┘ └────────┘
```

### Stream from any RTMP client → go-rtmp → multiple viewers + recording + relay

---

## Feature Highlights

| | Feature | Description |
|---|---------|-------------|
| 📡 | **Live Streaming** | Accept streams from OBS, FFmpeg, or any RTMP client |
| 👥 | **Multi-Subscriber** | Unlimited concurrent viewers with independent connections |
| 💾 | **FLV Recording** | Automatic recording with H.264 + AAC |
| ⏩ | **Late-Join** | Sequence header caching — subscribers see video instantly |
| 🔄 | **Multi-Relay** | Forward to YouTube, Twitch, or any RTMP server |
| 🔐 | **Authentication** | Token-based auth with 4 pluggable backends |
| 🔔 | **Event Hooks** | Webhooks, shell scripts, stdio on every lifecycle event |
| 📊 | **Metrics** | Live expvar counters via HTTP endpoint |
| 🛡️ | **Zombie Detection** | TCP deadlines kill stale connections automatically |
| 📦 | **Zero Dependencies** | Standard library only — no vendor lock-in |

---

## Quick Example

```bash
# Build
go build -o rtmp-server ./cmd/rtmp-server

# Run with recording
./rtmp-server -listen :1935 -record-all true

# Publish (another terminal)
ffmpeg -re -i video.mp4 -c copy -f flv rtmp://localhost:1935/live/test

# Watch (another terminal)
ffplay rtmp://localhost:1935/live/test
```

That's it. No config files, no databases, no containers. Just build and run.

**[→ Full Quick Start Guide](/go-rtmp/docs/quick-start/)**

---

## Documentation

| Section | Description |
|---------|-------------|
| [**Quick Start**](/go-rtmp/docs/quick-start/) | Up and running in 5 minutes |
| [**Installation**](/go-rtmp/docs/installation/) | Download binaries or build from source |
| [**User Guide**](/go-rtmp/docs/user-guide/) | Recording, relay, auth, hooks, metrics |
| [**CLI Reference**](/go-rtmp/docs/configuration/) | Every command-line flag explained |
| [**Developer Guide**](/go-rtmp/docs/developer/) | Architecture, protocol, testing, contributing |
| [**Changelog**](/go-rtmp/docs/project/changelog/) | Release history |

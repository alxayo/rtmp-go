---
title: "User Guide"
weight: 3
bookCollapseSection: true
---

# User Guide

The go-rtmp server is designed around a simple philosophy: **zero configuration by default, with opt-in features via CLI flags**. Out of the box, running `./rtmp-server` gives you a fully functional RTMP relay server on port 1935 — no config files, no YAML, no environment variables required. Every feature in this guide is activated by adding a flag to the command line.

**Recording** lets you capture every published stream to FLV files on disk. Enable it with `-record-all true` and point it at a directory with `-record-dir`. Files are named with the stream key and timestamp, and can be converted to MP4 with a single FFmpeg command. Recording runs alongside live relay — subscribers see the stream in real-time while the server simultaneously writes to disk.

**Live Relay** is the core of go-rtmp. When a publisher sends a stream to a key like `live/test`, any number of subscribers can play that same key and receive the media in real-time. The server caches sequence headers for all supported codecs (H.264, H.265/HEVC, AV1, VP9, AAC) so that subscribers who join mid-stream get instant decoder initialization — no waiting for the next keyframe.

**Multi-Destination Relay** extends the server's reach by forwarding published streams to external RTMP servers. Use the `-relay-to` flag (repeatable) to simulcast to YouTube, Twitch, a CDN origin, or a backup recording server. Media messages are forwarded exactly as received — no transcoding, no quality loss.

**Authentication** protects your streams with pluggable token-based validation. Choose from static tokens for simple setups, a JSON file with live reload for medium deployments, or a webhook callback for full integration with your existing auth infrastructure. Auth is enforced at the publish/play command level, and the default mode is `none` for full backward compatibility.

**RTMPS (TLS Encryption)** secures your RTMP connections with TLS, preventing eavesdropping on stream data in transit. Enable it with `-tls-listen` to run a TLS-encrypted listener alongside (or instead of) the plain RTMP listener. The server handles TLS termination at the transport layer — all protocol features (relay, recording, hooks, auth) work identically over both plain and encrypted connections. Generate self-signed certificates for development with the included helper scripts, or use Let's Encrypt certificates for production.

**Event Hooks** notify external systems when things happen in your RTMP server. Webhooks, shell scripts, and stdio output cover lifecycle events like publish start/stop, subscriber count changes, and authentication failures. Hooks execute asynchronously — they never block RTMP message processing, so a slow webhook won't affect your stream.

**Metrics & Monitoring** expose live server statistics via an HTTP endpoint using Go's built-in `expvar` package. When enabled with `-metrics-addr`, you get real-time gauges (active connections, publishers, subscribers) and counters (total messages, bytes ingested, relay stats) in JSON format — ready for Prometheus, Grafana, or custom monitoring scripts.

Every feature follows the principle of **graceful degradation**. If recording fails, streaming continues. If a relay destination goes down, other destinations and local subscribers are unaffected. If a hook times out, the RTMP connection carries on. The server is built to keep streams flowing even when optional subsystems encounter errors.

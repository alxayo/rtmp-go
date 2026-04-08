---
title: "CLI Reference"
weight: 5
bookCollapseSection: false
---

# CLI Reference

go-rtmp is configured entirely through command-line flags. There is no configuration file — all options are passed directly to the binary.

```bash
./rtmp-server [flags]
```

## Server

| Flag | Default | Description |
|------|---------|-------------|
| `-listen` | `:1935` | TCP address to listen on |
| `-log-level` | `info` | Log verbosity: `debug`, `info`, `warn`, `error` |
| `-chunk-size` | `4096` | Outbound chunk payload size (1–65536 bytes) |
| `-version` | | Print version and exit |

## Recording

| Flag | Default | Description |
|------|---------|-------------|
| `-record-all` | `false` | Record all published streams to FLV |
| `-record-dir` | `recordings` | Directory for FLV files |

## Relay

| Flag | Default | Description |
|------|---------|-------------|
| `-relay-to` | *(none)* | RTMP URL to relay streams to (repeatable) |

## Authentication

| Flag | Default | Description |
|------|---------|-------------|
| `-auth-mode` | `none` | Auth mode: `none`, `token`, `file`, `callback` |
| `-auth-token` | *(none)* | Stream token: `streamKey=token` (repeatable) |
| `-auth-file` | *(none)* | Path to JSON token file |
| `-auth-callback` | *(none)* | Webhook URL for auth validation |
| `-auth-callback-timeout` | `5s` | Auth callback HTTP timeout |

## Hooks

| Flag | Default | Description |
|------|---------|-------------|
| `-hook-script` | *(none)* | Shell hook: `event_type=/path/to/script` (repeatable) |
| `-hook-webhook` | *(none)* | Webhook: `event_type=https://url` (repeatable) |
| `-hook-stdio-format` | *(disabled)* | Stdio hook output: `json` or `env` |
| `-hook-timeout` | `30s` | Hook execution timeout |
| `-hook-concurrency` | `10` | Max concurrent hook executions |

## Metrics

| Flag | Default | Description |
|------|---------|-------------|
| `-metrics-addr` | *(disabled)* | HTTP address for metrics endpoint |

---

## Example Configurations

### 1. Basic Server

The simplest possible setup — no recording, no relay, no auth:

```bash
./rtmp-server
```

This listens on `:1935` with default settings. Any client can publish and subscribe.

### 2. Recording Server

Record all streams to FLV files:

```bash
./rtmp-server \
  -record-all true \
  -record-dir /data/recordings
```

Files are saved as `{record-dir}/{streamKey}_{timestamp}.flv`.

### 3. Relay Server (Simulcast to YouTube + Twitch)

Forward all streams to multiple destinations:

```bash
./rtmp-server \
  -relay-to rtmp://a.rtmp.youtube.com/live2/YOUR_YOUTUBE_KEY \
  -relay-to rtmp://live.twitch.tv/app/YOUR_TWITCH_KEY
```

The server accepts the stream once and relays it to both YouTube and Twitch simultaneously.

### 4. Authenticated Server with Webhooks

Require tokens for publishing and notify an external service:

```bash
./rtmp-server \
  -auth-mode token \
  -auth-token "live/mystream=secret123" \
  -auth-token "live/backup=another456" \
  -hook-webhook "publish_start=https://api.example.com/hooks/stream" \
  -hook-webhook "publish_stop=https://api.example.com/hooks/stream" \
  -hook-webhook "connection_accept=https://api.example.com/hooks/connect"
```

Publishers must include the token in their stream key:
```
rtmp://server:1935/live/mystream?token=secret123
```

### 5. Full Production Setup

All features enabled:

```bash
./rtmp-server \
  -listen 0.0.0.0:1935 \
  -log-level info \
  -chunk-size 4096 \
  -record-all true \
  -record-dir /data/recordings \
  -relay-to rtmp://a.rtmp.youtube.com/live2/YOUTUBE_KEY \
  -relay-to rtmp://live.twitch.tv/app/TWITCH_KEY \
  -auth-mode callback \
  -auth-callback https://api.example.com/auth/rtmp \
  -auth-callback-timeout 3s \
  -hook-webhook "publish_start=https://api.example.com/hooks/stream" \
  -hook-webhook "publish_stop=https://api.example.com/hooks/stream" \
  -hook-webhook "connection_accept=https://api.example.com/hooks/connect" \
  -hook-webhook "connection_close=https://api.example.com/hooks/connect" \
  -hook-script "publish_start=/opt/scripts/notify-slack.sh" \
  -hook-timeout 10s \
  -hook-concurrency 20 \
  -metrics-addr :8080
```

This configuration:
- Listens on all interfaces
- Records all streams to `/data/recordings`
- Simulcasts to YouTube and Twitch
- Validates tokens via webhook callback
- Notifies an API on stream start/stop and connection events
- Runs a Slack notification script on publish
- Exposes metrics at `http://localhost:8080/debug/vars`

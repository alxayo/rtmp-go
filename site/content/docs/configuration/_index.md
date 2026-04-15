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

## TLS (RTMPS)

| Flag | Default | Description |
|------|---------|-------------|
| `-tls-listen` | *(disabled)* | TCP address for RTMPS (TLS-encrypted RTMP) connections |
| `-tls-cert` | *(none)* | Path to TLS certificate file (PEM format) |
| `-tls-key` | *(none)* | Path to TLS private key file (PEM format) |

When `-tls-listen` is set, the server runs a second listener for encrypted RTMP connections. Both plain RTMP (`-listen`) and RTMPS (`-tls-listen`) can run simultaneously. TLS requires both `-tls-cert` and `-tls-key` to be provided.

The minimum TLS version is 1.2.

## Recording

| Flag | Default | Description |
|------|---------|-------------|
| `-record-all` | `false` | Record all published streams to FLV |
| `-record-dir` | `recordings` | Directory for FLV files |

## Relay

| Flag | Default | Description |
|------|---------|-------------|
| `-relay-to` | *(none)* | RTMP/RTMPS URL to relay streams to (repeatable) |

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

## SRT Ingest

| Flag | Default | Description |
|------|---------|-------------|
| `-srt-listen` | *(disabled)* | UDP address for SRT ingest connections |
| `-srt-latency` | `120ms` | TSBPD jitter buffer latency |
| `-srt-passphrase` | *(none)* | Shared secret for AES encryption (10-79 chars) |
| `-srt-passphrase-file` | `""` | Path to JSON file mapping stream keys to passphrases for per-stream SRT encryption. Mutually exclusive with `-srt-passphrase`. Supports hot reload via SIGHUP. |
| `-srt-pbkeylen` | `16` | AES key length in bytes: 16, 24, or 32 |

When `-srt-listen` is set, the server starts a UDP listener for SRT publishers. SRT streams are automatically converted to RTMP format and injected into the stream registry — existing RTMP subscribers can watch SRT sources transparently.

When `-srt-passphrase` is set, all SRT connections require AES encryption. Clients must provide the matching passphrase. Connections with wrong or missing passphrases are rejected during the handshake.

When `-srt-passphrase-file` is set, each stream key can have its own passphrase loaded from a JSON file. The file maps stream keys to passphrases (e.g. `{"live/stream1": "secret1"}`). Send SIGHUP to reload the file without restarting. Mutually exclusive with `-srt-passphrase`.

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

### 5. Authenticated Server with Webhooks and Relay

All RTMP features — relay, auth, and hooks:

```bash
./rtmp-server \
  -listen 0.0.0.0:1935 \
  -log-level info \
  -record-all true \
  -record-dir /data/recordings \
  -relay-to rtmp://a.rtmp.youtube.com/live2/YOUTUBE_KEY \
  -relay-to rtmp://live.twitch.tv/app/TWITCH_KEY \
  -auth-mode token \
  -auth-token "live/mystream=secret123" \
  -hook-webhook "publish_start=https://api.example.com/hooks/stream" \
  -hook-webhook "publish_stop=https://api.example.com/hooks/stream" \
  -metrics-addr :8080
```

### 6. RTMPS (TLS-Encrypted) Server

Serve encrypted RTMP connections:

```bash
./rtmp-server \
  -tls-listen :1936 \
  -tls-cert /path/to/cert.pem \
  -tls-key /path/to/key.pem
```

This listens for TLS-encrypted RTMP on port 1936. Clients connect with `rtmps://server:1936/live/test`.

### 7. Dual Listener (RTMP + RTMPS)

Run both plain and encrypted listeners simultaneously:

```bash
./rtmp-server \
  -listen :1935 \
  -tls-listen :1936 \
  -tls-cert /path/to/cert.pem \
  -tls-key /path/to/key.pem \
  -log-level info
```

Plain RTMP on port 1935 and encrypted RTMPS on port 1936. Useful during migration or when supporting both legacy and modern clients.

### 8. SRT Ingest

Accept SRT streams alongside RTMP:

```bash
./rtmp-server \
  -listen :1935 \
  -srt-listen :4200 \
  -srt-latency 120ms \
  -log-level info
```

Publishers can stream via SRT and subscribers watch via RTMP:

```bash
# Publish via SRT
ffmpeg -re -i test.mp4 -c copy -f mpegts "srt://localhost:4200?streamid=live/test"

# Subscribe via RTMP
ffplay rtmp://localhost:1935/live/test
```

### 9. SRT with Encryption

Encrypted SRT ingest with AES-256:

```bash
./rtmp-server \
  -srt-listen :4200 \
  -srt-passphrase "my-secret-key" \
  -srt-pbkeylen 32 \
  -record-all true
```

### 10. Full Production Setup (RTMP + RTMPS + SRT)

All features enabled:

```bash
./rtmp-server \
  -listen 0.0.0.0:1935 \
  -tls-listen :1936 \
  -tls-cert /path/to/cert.pem \
  -tls-key /path/to/key.pem \
  -srt-listen :4200 \
  -srt-latency 120ms \
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
  -hook-script "publish_start=/opt/scripts/notify-slack.sh" \
  -hook-timeout 10s \
  -hook-concurrency 20 \
  -metrics-addr :8080
```

This configuration accepts RTMP (port 1935), RTMPS (port 1936), and SRT (port 4200) simultaneously with recording, relay, authentication, hooks, and metrics.

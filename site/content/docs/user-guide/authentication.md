---
title: "Authentication"
weight: 4
---

# Authentication

go-rtmp supports pluggable token-based authentication to control who can publish and subscribe to streams.

## Overview

| Mode | Flag | Best For |
|------|------|----------|
| `none` | `-auth-mode none` (default) | Open access, backward compatible |
| `token` | `-auth-mode token` | Small setups, static configuration |
| `file` | `-auth-mode file` | Medium deployments, live reload |
| `callback` | `-auth-mode callback` | Full integration with existing auth systems |

Authentication is enforced at the **publish/play command level** — not at connect or handshake. This means the RTMP connection is established first, then auth is checked when the client issues a publish or play command.

## Token Passing

Clients pass tokens as URL query parameters appended to the stream name:

```
rtmp://server:1935/app/streamName?token=secret123
```

The server parses the query string, extracts the `token` parameter, and validates it against the configured backend. The clean stream name (without query params) is used for stream key matching.

## Static Tokens

For simple setups with a small number of streams:

```bash
./rtmp-server -auth-mode token \
  -auth-token "live/stream1=secret123" \
  -auth-token "live/camera1=cam_token"
```

The `-auth-token` flag is repeatable. Each value maps a full stream key (`app/streamName`) to a token. The server maintains these as an in-memory map.

## File-Based Tokens

For larger deployments where tokens change over time:

```bash
./rtmp-server -auth-mode file -auth-file tokens.json
```

The JSON file maps stream keys to tokens:

```json
{
  "live/stream1": "secret123",
  "live/camera1": "cam_token",
  "live/event": "event_2024_key"
}
```

**Live reload**: Send `SIGHUP` to the server process to reload the token file without restarting:

```bash
kill -HUP $(pidof rtmp-server)
```

The reload is thread-safe — active streams are not interrupted.

## Webhook Callback

For full integration with external authentication systems:

```bash
./rtmp-server -auth-mode callback \
  -auth-callback https://auth.example.com/validate \
  -auth-callback-timeout 5s
```

When a client attempts to publish or play, the server sends an HTTP POST to the callback URL:

**Request:**

```http
POST /validate HTTP/1.1
Content-Type: application/json

{
  "action": "publish",
  "app": "live",
  "stream_name": "stream1",
  "stream_key": "live/stream1",
  "token": "secret123",
  "remote_addr": "192.168.1.100:54321"
}
```

**Response:**

| Status | Result |
|--------|--------|
| `200 OK` | Authentication **passes** — publish/play proceeds |
| Any other status | Authentication **fails** — connection closed |

The `-auth-callback-timeout` flag (default `5s`) controls the HTTP request timeout.

## Client Configuration

### FFmpeg (Publish)

```bash
ffmpeg -re -i test.mp4 -c copy -f flv \
  "rtmp://localhost:1935/live/stream1?token=secret123"
```

### ffplay (Subscribe)

```bash
ffplay "rtmp://localhost:1935/live/stream1?token=secret123"
```

### OBS Studio

In OBS settings:

| Field | Value |
|-------|-------|
| **Server** | `rtmp://localhost:1935/live` |
| **Stream Key** | `stream1?token=secret123` |

OBS appends the stream key to the server URL, so the token is included as a query parameter on the stream name.

## Auth Failure Behavior

When authentication fails:

1. The server sends a `NetStream.Publish.Unauthorized` or `NetStream.Play.Unauthorized` status to the client
2. The connection is closed
3. An `auth_failed` hook event is triggered with the action (`publish` or `play`) and error details

**Sentinel errors:**

| Error | Meaning |
|-------|---------|
| `authentication failed: invalid credentials` | Token provided but doesn't match |
| `authentication failed: token missing` | No token in the URL query params |

## Example: Full Auth Setup

```bash
# Start server with file-based auth and a webhook for auth failures
./rtmp-server -listen :1935 \
  -auth-mode file \
  -auth-file tokens.json \
  -hook-webhook "auth_failed=https://alerts.example.com/auth-failure" \
  -log-level info

# Publish with valid token (succeeds)
ffmpeg -re -i test.mp4 -c copy -f flv \
  "rtmp://localhost:1935/live/stream1?token=secret123"

# Publish without token (fails)
ffmpeg -re -i test.mp4 -c copy -f flv \
  "rtmp://localhost:1935/live/stream1"
# => connection closed, auth_failed hook fires
```

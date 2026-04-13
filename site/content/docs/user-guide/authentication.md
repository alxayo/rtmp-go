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

`SIGHUP` **only reloads the token file** — it does not stop the server, close connections, or interrupt any active streams. The server atomically swaps its in-memory token map so that **new** publish/play requests validate against the updated file immediately. Streams that were already authenticated continue unaffected.

| Signal | Effect |
|--------|--------|
| `SIGHUP` | Reloads token file. Server keeps running, all active streams stay connected |
| `SIGINT` / `SIGTERM` | Graceful shutdown — stops accepting connections and terminates the server |

> **Note**: SIGHUP-based reload is available on Linux and macOS only (`syscall.SIGHUP` is a Unix signal). On Windows, a server restart is required to pick up token file changes.

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

### Payload Field Reference

| Field | Type | Description |
|-------|------|-------------|
| `action` | string | `"publish"` or `"play"` — lets you authorize publishing and viewing independently |
| `app` | string | Application name from the RTMP connect command (e.g. `"live"`) |
| `stream_name` | string | Clean stream name without query params (e.g. `"cam1"`) |
| `stream_key` | string | Full key: `app/streamName` (e.g. `"live/cam1"`) — use this for per-stream authorization |
| `token` | string | Value of `?token=` query param from the client URL. Empty string if not provided |
| `remote_addr` | string | Client IP and port (e.g. `"192.168.1.100:54321"`) — use for IP-based rules |

### Separate Publish/Play Callbacks

Every publish and play attempt triggers a **separate** HTTP POST to your callback. This means you can apply completely different authorization logic per action:

- Allow a publisher but require a different token for viewers
- Allow all viewers but restrict who can publish
- Apply different rate limits or IP restrictions per action

For example, a viewer and a publisher hitting the same stream will result in two independent calls — one with `"action": "publish"` and one with `"action": "play"`.

### Timeout & Error Behavior

The callback auth mode is **fail-closed**: if your auth service is unavailable, no streams can start.

| Scenario | Result |
|----------|--------|
| Webhook returns non-200 | Connection closed, `auth_failed` hook event fires |
| Webhook times out (default 5s) | Treated as failure — connection denied. This is a transport error, not `ErrUnauthorized` |
| Webhook is unreachable | Same as timeout — connection denied |

> **Important**: If your auth service goes down, all new publish and play attempts will be denied. Monitor your auth endpoint and consider the timeout value carefully.

### Security Considerations

- **Always use `https://`** for the callback URL — the token is sent in the JSON body in plain text
- The token is also visible in the plain RTMP stream. Use RTMPS (`-tls-listen`) to encrypt the client→server connection, and HTTPS for the server→webhook connection
- The callback does **not** receive `ConnectParams` beyond what's listed in the field reference. For custom data, encode it in the token field (e.g. as a JWT or other structured token)

### Why Callback Is the Only Dynamic Auth Mode

| Mode | Behavior |
|------|----------|
| `token` | Static — set at startup via flags, requires restart to change |
| `file` | Loaded at startup, live reload via `SIGHUP` signal (Linux/macOS) |
| `callback` | Fresh HTTP call on every publish/play — fully dynamic, no server interaction needed |

`callback` is the most flexible: no signal or file editing required. `file` with SIGHUP is a good middle ground for teams that manage tokens as config files.

### Example: Minimal Webhook Server

A simple Node.js webhook that validates tokens:

```javascript
const express = require('express');
const app = express();
app.use(express.json());

app.post('/rtmp/auth', (req, res) => {
  const { action, stream_key, token } = req.body;

  // Your logic: check database, validate JWT, etc.
  const isValid = checkToken(stream_key, token);

  res.sendStatus(isValid ? 200 : 403);
});

app.listen(3000);
```

Point the RTMP server at it:

```bash
./rtmp-server -auth-mode callback \
  -auth-callback http://localhost:3000/rtmp/auth
```

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

---
title: "Quick Start"
weight: 1
---

# Quick Start

Get from zero to streaming in under 5 minutes. This guide assumes you have **Go 1.21+** and **FFmpeg** installed.

## 1. Build the Server

```bash
go build -o rtmp-server ./cmd/rtmp-server
```

> On Windows this produces `rtmp-server.exe` automatically.

## 2. Start the Server with Recording

```bash
./rtmp-server -listen :1935 -record-all true -log-level info
```

| Flag | Description |
|------|-------------|
| `-listen :1935` | Listen on all interfaces, port 1935 (the standard RTMP port). |
| `-record-all true` | Automatically record every published stream to an FLV file. |
| `-log-level info` | Show informational log messages (use `debug` for more detail). |

You should see output like:

```json
{"level":"INFO","msg":"server started","addr":":1935","version":"dev"}
```

## 3. Publish a Stream

Open a **second terminal** and push a video file:

```bash
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test
```

**Don't have a video file?** Generate a test pattern with synthetic audio:

```bash
ffmpeg -re -f lavfi -i testsrc=size=640x480:rate=30 \
  -f lavfi -i sine=frequency=440:sample_rate=44100 \
  -c:v libx264 -preset ultrafast -tune zerolatency \
  -c:a aac -f flv rtmp://localhost:1935/live/test
```

## 4. Watch the Stream

Open a **third terminal**:

```bash
ffplay rtmp://localhost:1935/live/test
```

A player window will appear showing the live stream.

## 5. Verify the Recording

Once you stop the publisher (Ctrl+C in the FFmpeg terminal), check the recordings directory:

```bash
ls recordings/
```

Play back the recorded file:

```bash
ffplay recordings/live_test_*.flv
```

## 6. Add More Viewers

Open additional terminals and run the same `ffplay` command — each viewer connects independently and receives the stream in real time:

```bash
ffplay rtmp://localhost:1935/live/test
```

---

## What Just Happened?

Here is what happened under the hood during the session above:

1. **RTMP Handshake** — The server accepted the TCP connection and completed the RTMP v3 handshake (C0/C1/C2 ↔ S0/S1/S2), establishing a reliable bidirectional channel.
2. **Sequence Header Caching** — The publisher's H.264 SPS/PPS (video decoder configuration) and AAC AudioSpecificConfig (audio decoder configuration) were cached as "sequence headers." These are essential for any new viewer to initialize their decoders.
3. **FLV Recording** — The recording subsystem wrote each incoming audio and video message as FLV tags to a file on disk, producing a standard `.flv` file playable by any media player.
4. **Live Media Relay** — Each subscriber first received the cached sequence headers (so their player could initialize immediately), then received live audio/video messages relayed from the publisher in real time.
5. **Zombie Detection** — TCP deadlines (read 90s, write 30s) are reset on every I/O operation. If a connection goes silent, the server automatically closes it — no zombie connections accumulate.

## Next Steps

- [Installation]({{< relref "/docs/installation" >}}) — Download pre-built binaries or cross-compile for other platforms.
- [User Guide]({{< relref "/docs/user-guide" >}}) — Configure recording, relay, authentication, and event hooks.
- [CLI Reference]({{< relref "/docs/configuration" >}}) — See every command-line flag and its default value.

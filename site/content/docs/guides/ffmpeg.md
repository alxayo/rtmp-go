---
title: "FFmpeg Commands"
weight: 2
---

# FFmpeg Commands

Common FFmpeg commands for publishing, subscribing, recording, and converting with go-rtmp.

## Publishing

### From a File

Re-stream a video file at its original frame rate:

```bash
ffmpeg -re -i video.mp4 -c copy -f flv rtmp://localhost:1935/live/test
```

- `-re` reads the file at its native frame rate (real-time). Without this flag, FFmpeg sends frames as fast as possible.
- `-c copy` copies the codec without re-encoding (fast, no quality loss).

### Test Pattern (No File Needed)

Generate a synthetic video + audio stream for testing:

```bash
ffmpeg -re \
  -f lavfi -i testsrc=size=640x480:rate=30 \
  -f lavfi -i sine=frequency=440:sample_rate=48000 \
  -c:v libx264 -preset ultrafast -tune zerolatency \
  -c:a aac -b:a 128k \
  -f flv rtmp://localhost:1935/live/test
```

This creates a color bar pattern with a 440Hz tone — useful for verifying the server works without any source media.

### From Webcam

**Windows:**
```bash
ffmpeg -f dshow -i video="Integrated Camera":audio="Microphone" \
  -c:v libx264 -preset veryfast -c:a aac \
  -f flv rtmp://localhost:1935/live/webcam
```

**macOS:**
```bash
ffmpeg -f avfoundation -i "0:0" \
  -c:v libx264 -preset veryfast -c:a aac \
  -f flv rtmp://localhost:1935/live/webcam
```

**Linux:**
```bash
ffmpeg -f v4l2 -i /dev/video0 -f pulse -i default \
  -c:v libx264 -preset veryfast -c:a aac \
  -f flv rtmp://localhost:1935/live/webcam
```

> **Tip**: List available devices with `ffmpeg -list_devices true -f dshow -i dummy` (Windows) or `ffmpeg -f avfoundation -list_devices true -i ""` (macOS).

## Subscribing

### Basic Playback

```bash
ffplay rtmp://localhost:1935/live/test
```

### Low-Latency Playback

Minimize buffering for the lowest possible latency:

```bash
ffplay -fflags nobuffer -flags low_delay -framedrop rtmp://localhost:1935/live/test
```

- `-fflags nobuffer` disables input buffering
- `-flags low_delay` enables low-delay decoding
- `-framedrop` drops frames if the decoder falls behind

## Recording

### Record from Server to File

```bash
ffmpeg -i rtmp://localhost:1935/live/test -c copy output.flv
```

This records the stream without re-encoding. Press `q` to stop recording.

### Convert FLV to MP4

Convert a recorded FLV file to MP4 (widely compatible):

```bash
ffmpeg -i recording.flv -c copy output.mp4
```

The `-c copy` flag remuxes without re-encoding — instant and lossless.

## With Authentication

When the server requires a token, wrap the URL in quotes to prevent shell interpretation of the `?` character:

```bash
# Publish with token
ffmpeg -re -i video.mp4 -c copy -f flv "rtmp://localhost:1935/live/test?token=secret123"

# Subscribe with token
ffplay "rtmp://localhost:1935/live/test?token=secret123"
```

> **Important**: The quotes are required. Without them, the shell may interpret `?` as a glob pattern.

## With RTMPS (TLS)

FFmpeg does not natively support `rtmps://` as an output format. To publish to an RTMPS-enabled go-rtmp server, use the plain RTMP listener or the Go client:

### Publishing

```bash
# Option 1: Use the plain RTMP listener (if dual-listener is enabled)
ffmpeg -re -i video.mp4 -c copy -f flv rtmp://localhost:1935/live/test

# Option 2: Use the Go client for true RTMPS publishing
go run ./cmd/rtmp-client -url rtmps://localhost:1936/live/test -publish video.flv
```

### Subscribing

```bash
# Subscribe via plain RTMP
ffplay rtmp://localhost:1935/live/test
```

### Testing TLS Connections

The E2E test suite validates RTMPS using the Go client:

```bash
./scripts/test-e2e.sh --test "RTMPS Publish + Capture"
```

> **Note**: Even though FFmpeg connects via plain RTMP, the TLS listener can be verified independently via the Go client. All streams are shared between both listeners — a publisher on one listener is visible to subscribers on the other.

## Useful Flags

| Flag | Description |
|------|-------------|
| `-re` | Read input at native frame rate |
| `-c copy` | Copy codec without re-encoding |
| `-f flv` | Force FLV output format (required for RTMP) |
| `-preset ultrafast` | Fastest x264 encoding (lowest quality) |
| `-tune zerolatency` | Optimize x264 for low latency |
| `-b:v 2500k` | Set video bitrate |
| `-b:a 128k` | Set audio bitrate |
| `-t 60` | Limit to 60 seconds |
| `-loglevel warning` | Reduce FFmpeg output noise |

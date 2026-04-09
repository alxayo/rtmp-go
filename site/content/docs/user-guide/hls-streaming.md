---
title: "HLS Streaming"
weight: 7
---

# HLS Streaming

go-rtmp outputs RTMP natively. To deliver streams to web browsers, convert to HLS (HTTP Live Streaming) using FFmpeg as a sidecar process.

## Architecture

```
Publisher (OBS/FFmpeg)
    │
    ▼
go-rtmp server (RTMP on :1935)
    │
    ▼
FFmpeg (subscribes via RTMP, outputs HLS segments)
    │
    ▼
HTTP server (serves .m3u8 + .ts files)
    │
    ▼
Browser (hls.js / native HLS player)
```

## Basic Setup

### Step 1: Start go-rtmp

```bash
./rtmp-server -listen :1935
```

### Step 2: Publish a stream

From OBS, FFmpeg, or any RTMP client:

```bash
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test
```

### Step 3: Convert to HLS with FFmpeg

Subscribe to the RTMP stream and output HLS segments:

```bash
mkdir -p hls-output
ffmpeg -i rtmp://localhost:1935/live/test \
  -c copy -f hls \
  -hls_time 4 -hls_list_size 6 \
  -hls_flags delete_segments \
  hls-output/playlist.m3u8
```

| Flag | Purpose |
|------|---------|
| `-c copy` | No re-encoding — remux only (fast, no quality loss) |
| `-hls_time 4` | Each segment is ~4 seconds |
| `-hls_list_size 6` | Playlist contains the 6 most recent segments |
| `-hls_flags delete_segments` | Remove old `.ts` files to save disk space |

### Step 4: Serve with HTTP

```bash
python3 -m http.server 8080
```

Or use any static file server (nginx, caddy, etc.).

### Step 5: Play in browser

Open `http://localhost:8080/hls-output/playlist.m3u8` in:

- **VLC**: File → Open Network Stream
- **Safari**: Native HLS support, paste URL directly
- **Chrome/Firefox**: Use [hls.js](https://github.com/video-dev/hls.js) — see the included player below

## Adaptive Bitrate Streaming

For multi-quality output with automatic quality switching:

```bash
ffmpeg -i rtmp://localhost:1935/live/test \
  -map 0:v -map 0:a -map 0:v -map 0:a \
  -c:v:0 libx264 -b:v:0 5000k -s:v:0 1920x1080 \
  -c:v:1 libx264 -b:v:1 2500k -s:v:1 1280x720 \
  -c:a aac -b:a 128k \
  -f hls -hls_time 4 -hls_list_size 6 \
  -master_pl_name master.m3u8 \
  -var_stream_map "v:0,a:0 v:1,a:1" \
  hls-output/stream_%v.m3u8
```

This produces:

| File | Content |
|------|---------|
| `master.m3u8` | Master playlist referencing quality variants |
| `stream_0.m3u8` | 1080p @ 5 Mbps playlist |
| `stream_1.m3u8` | 720p @ 2.5 Mbps playlist |
| `stream_0_*.ts` | 1080p segments |
| `stream_1_*.ts` | 720p segments |

The browser player (hls.js or native) automatically switches quality based on bandwidth.

## Low-Latency Tips

For the lowest possible HLS latency:

```bash
ffmpeg -i rtmp://localhost:1935/live/test \
  -c:v libx264 -tune zerolatency -preset ultrafast \
  -c:a aac -b:a 128k \
  -f hls -hls_time 2 -hls_list_size 3 \
  -hls_flags delete_segments \
  hls-output/playlist.m3u8
```

| Tuning | Effect |
|--------|--------|
| `-hls_time 2` | Shorter segments (2s instead of 4s) |
| `-hls_list_size 3` | Smaller playlist window |
| `-tune zerolatency` | Minimize encoder buffering |
| `-preset ultrafast` | Fastest encoding (at cost of compression efficiency) |

**Note**: Low-latency HLS still has inherent latency (typically 6–10 seconds). For sub-second latency, use direct RTMP playback.

## Included Tools

The go-rtmp project includes helper scripts in `tools-rtmp/` for common HLS workflows:

### rtmp-to-hls.sh

Convert RTMP streams to HLS with preset modes:

```bash
# Basic mode: codec copy, no re-encoding (fastest)
./tools-rtmp/rtmp-to-hls.sh basic test

# Quality mode: re-encode with libx264 + AAC
./tools-rtmp/rtmp-to-hls.sh quality test

# Adaptive mode: multi-bitrate with master playlist (3 quality levels)
./tools-rtmp/rtmp-to-hls.sh adaptive test
```

The adaptive mode generates three variants (5 Mbps, 2.5 Mbps, 1 Mbps) with a `master.m3u8` for automatic quality switching.

### hls-player.html

A ready-to-use HTML player built on hls.js:

```bash
# Serve the tools-rtmp directory
cd tools-rtmp && python3 -m http.server 8080

# Open http://localhost:8080/hls-player.html in your browser
```

### extract-rtmp-stream.sh

Extract specific media components from a live RTMP stream:

```bash
# Extract video only
./tools-rtmp/extract-rtmp-stream.sh video test

# Extract audio only
./tools-rtmp/extract-rtmp-stream.sh audio test

# Extract I-frames (keyframes) only
./tools-rtmp/extract-rtmp-stream.sh iframes test
```

Options: `--duration SEC`, `--output FILE`, `--format FORMAT`, `--quality PRESET`.

## Automated HLS via Publish Hook

Instead of manually starting FFmpeg for HLS conversion, use the included hook script to automatically convert every published stream to HLS:

```bash
./rtmp-server \
  -listen :1935 \
  -hook-script "publish_start=./scripts/on-publish-hls.sh" \
  -hook-timeout 30s
```

**Windows:**
```powershell
.\rtmp-server.exe `
  -listen :1935 `
  -hook-script "publish_start=.\scripts\on-publish-hls.ps1" `
  -hook-timeout 30s
```

When a publisher connects, the hook script:
1. Reads `RTMP_STREAM_KEY` from the environment (set by the hook system)
2. Starts FFmpeg in the background to subscribe to the RTMP stream
3. Outputs HLS segments to `hls-output/{stream_name}/playlist.m3u8`
4. Logs to `scripts/logs/hls-{stream_key}.log`

This works with both plain RTMP and RTMPS connections — hooks execute after TLS termination, so the transport layer is transparent.

See [E2E Testing Scripts]({{< relref "/docs/guides/e2e-testing" >}}) for automated testing of the hook-based HLS pipeline.

## Complete Example

End-to-end HLS streaming from OBS to browser:

```bash
# Terminal 1: Start RTMP server
./rtmp-server -listen :1935 -log-level info

# Terminal 2: Start HLS conversion
mkdir -p hls-output
ffmpeg -i rtmp://localhost:1935/live/test \
  -c copy -f hls \
  -hls_time 4 -hls_list_size 6 \
  -hls_flags delete_segments \
  hls-output/playlist.m3u8

# Terminal 3: Start HTTP server
python3 -m http.server 8080

# OBS: Set server to rtmp://localhost:1935/live, stream key to "test"
# Browser: Open http://localhost:8080/hls-output/playlist.m3u8
```

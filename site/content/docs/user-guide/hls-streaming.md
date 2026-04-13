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

## Parallel FFmpeg ABR

The single-FFmpeg approach above works well for small deployments. For fault isolation, multi-core scaling, or distributed encoding across machines, run **independent FFmpeg instances** — one per rendition — each subscribing to the same RTMP stream.

### Architecture

```
Publisher (OBS / FFmpeg / SRT)
     │
     ▼
go-rtmp server (:1935)
     │ (multiple RTMP subscribers on same key)
     ├───────────────────┬───────────────────┐
     ▼                   ▼                   ▼
FFmpeg #1 (1080p)   FFmpeg #2 (720p)    FFmpeg #3 (480p)
     │                   │                   │
     ▼                   ▼                   ▼
hls/1080p/index.m3u8 hls/720p/index.m3u8 hls/480p/index.m3u8
     │                   │                   │
     └───────────┬───────┘                   │
                 ▼                           │
        hls/master.m3u8 ◀───────────────────┘
                 │
                 ▼
        HTTP Server (:8080)
                 │
                 ▼
        Browser (hls.js)
```

This works because go-rtmp supports **unlimited concurrent subscribers** per stream key. Each FFmpeg instance is just another subscriber receiving independent copies of media data.

### Why Parallel

| Aspect | Single FFmpeg | Parallel FFmpeg |
|--------|--------------|-----------------|
| **Fault isolation** | One crash kills all renditions | One crash leaves others running |
| **CPU scaling** | Single process | Spreads across all cores/machines |
| **Distribution** | Same machine only | Each encoder on a different server |
| **Master playlist** | Auto-generated | Manual (static, create once) |
| **Segment alignment** | Guaranteed | Requires matching GOP params |

### Critical: Segment Alignment

For players to switch between renditions produced by different FFmpeg instances, keyframes must appear at the **exact same timestamps**. These flags must be identical across all instances:

| Flag | Purpose |
|------|---------|
| `-force_key_frames "expr:gte(t,n_forced*2)"` | Time-based keyframe every 2s (works at any fps) |
| `-sc_threshold 0` | Disable scene-change keyframes |
| `-hls_time 2` | Segment duration (must match keyframe interval) |
| `-r 30` | Normalize output fps across all renditions |

> **Why `-force_key_frames` over `-g`**: The `-g` flag sets keyframes by frame count (e.g. `-g 60` = every 60 frames), which only produces 2-second segments at exactly 30fps. With variable frame rate or other fps values, segments become misaligned. Time-based forcing works regardless of input fps.

Without `-sc_threshold 0`, FFmpeg inserts extra keyframes at scene changes. Different resolutions have different scene-detection sensitivity, which breaks alignment between renditions.

### Step-by-Step

**1. Start the server:**

```bash
./rtmp-server -listen :1935
```

**2. Publish a stream:**

```bash
ffmpeg -re -i source.mp4 -c copy -f flv rtmp://localhost:1935/live/test
```

**3. Launch 3 parallel transcoders** (each subscribes independently):

```bash
SEG=2  LIST=10  FPS=30

# 1080p @ 5 Mbps
mkdir -p hls/1080p
ffmpeg -i rtmp://localhost:1935/live/test \
  -c:v libx264 -s 1920x1080 -b:v 5000k -maxrate 5500k -bufsize 10000k \
  -preset veryfast -r $FPS \
  -force_key_frames "expr:gte(t,n_forced*${SEG})" -sc_threshold 0 \
  -c:a aac -b:a 192k -ar 48000 \
  -f hls -hls_time $SEG -hls_list_size $LIST \
  -hls_flags delete_segments+temp_file \
  -hls_segment_filename hls/1080p/seg_%05d.ts \
  hls/1080p/index.m3u8 &

# 720p @ 2.5 Mbps
mkdir -p hls/720p
ffmpeg -i rtmp://localhost:1935/live/test \
  -c:v libx264 -s 1280x720 -b:v 2500k -maxrate 2750k -bufsize 5000k \
  -preset veryfast -r $FPS \
  -force_key_frames "expr:gte(t,n_forced*${SEG})" -sc_threshold 0 \
  -c:a aac -b:a 128k -ar 48000 \
  -f hls -hls_time $SEG -hls_list_size $LIST \
  -hls_flags delete_segments+temp_file \
  -hls_segment_filename hls/720p/seg_%05d.ts \
  hls/720p/index.m3u8 &

# 480p @ 1 Mbps
mkdir -p hls/480p
ffmpeg -i rtmp://localhost:1935/live/test \
  -c:v libx264 -s 854x480 -b:v 1000k -maxrate 1100k -bufsize 2000k \
  -preset veryfast -r $FPS \
  -force_key_frames "expr:gte(t,n_forced*${SEG})" -sc_threshold 0 \
  -c:a aac -b:a 96k -ar 48000 \
  -f hls -hls_time $SEG -hls_list_size $LIST \
  -hls_flags delete_segments+temp_file \
  -hls_segment_filename hls/480p/seg_%05d.ts \
  hls/480p/index.m3u8 &
```

**4. Create master playlist** (one time — this file is static):

```bash
cat > hls/master.m3u8 << 'EOF'
#EXTM3U
#EXT-X-VERSION:3
#EXT-X-INDEPENDENT-SEGMENTS

#EXT-X-STREAM-INF:BANDWIDTH=5700000,RESOLUTION=1920x1080
1080p/index.m3u8

#EXT-X-STREAM-INF:BANDWIDTH=2900000,RESOLUTION=1280x720
720p/index.m3u8

#EXT-X-STREAM-INF:BANDWIDTH=1200000,RESOLUTION=854x480
480p/index.m3u8
EOF
```

**5. Serve and play:**

```bash
# Serve HLS files
cd hls && python3 -m http.server 8080

# Browser: open http://localhost:8080/master.m3u8
```

The `BANDWIDTH` value should include video + audio + overhead. Players read the master playlist once, then poll the sub-playlists for new segments.

### File Structure

```
hls/
├── master.m3u8            ← static, created once
├── 1080p/
│   ├── index.m3u8         ← updated by FFmpeg every segment
│   ├── seg_00001.ts
│   └── seg_00002.ts
├── 720p/
│   ├── index.m3u8
│   └── seg_*.ts
└── 480p/
    ├── index.m3u8
    └── seg_*.ts
```

### Automate with Publish Hook

Instead of starting transcoders manually, use the included hook script to launch all 3 renditions automatically when a publisher connects:

```bash
./rtmp-server -listen :1935 \
  -hook-script "publish_start=./scripts/on-publish-abr.sh" \
  -hook-timeout 30s
```

**Windows:**
```powershell
.\rtmp-server.exe `
  -listen :1935 `
  -hook-script "publish_start=.\scripts\on-publish-abr.ps1" `
  -hook-timeout 30s
```

The hook reads `RTMP_STREAM_KEY` from the environment, spawns 3 FFmpeg background processes with aligned GOP parameters, writes `master.m3u8`, and saves PIDs for cleanup. Logs go to `scripts/logs/abr-{key}-{rendition}.log`.

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

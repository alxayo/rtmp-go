# HLS Transcoder

Webhook-driven HLS transcoding service for the rtmp-go streaming platform.
Receives `publish_start`/`publish_stop` events from rtmp-server's webhook hook
system and manages FFmpeg processes that convert live RTMP streams into
multi-bitrate adaptive HLS output.

## Architecture

```
rtmp-server ──webhook──► hls-transcoder ──FFmpeg──► HLS segments
  (TCP 1935)              (HTTP 8090)               (Azure Files)
```

The transcoder subscribes to rtmp-server as an RTMP client (via FFmpeg) and
produces `.m3u8` playlists and `.ts` segment files. A separately-deployed HLS
server can mount the same Azure Files share to serve content to end users.

## Modes

### ABR Mode (default)

Produces 3 renditions with aligned keyframes for adaptive bitrate switching:

| Rendition | Resolution | Video Bitrate | Audio Bitrate |
|-----------|-----------|---------------|---------------|
| 1080p     | 1920×1080 | 5000 kbps     | 192 kbps      |
| 720p      | 1280×720  | 2500 kbps     | 128 kbps      |
| 480p      | 854×480   | 1000 kbps     | 96 kbps       |

Uses a single FFmpeg process with `-var_stream_map` for efficient multi-output.
Generates a `master.m3u8` with `#EXT-X-STREAM-INF` entries for each rendition.

**Resource requirements:** 4 vCPU / 8 GiB

### Copy Mode

Remuxes the RTMP stream to HLS without transcoding (`-c copy`). Produces a
single-bitrate HLS output at the original source quality.

**Resource requirements:** 0.5 vCPU / 1 GiB

## Configuration

| Flag | Default | Description |
|------|---------|-------------|
| `-listen-addr` | `:8090` | HTTP listen address for webhook events |
| `-hls-dir` | `/hls-output` | Root directory for HLS output files |
| `-rtmp-host` | `localhost` | RTMP server hostname (internal FQDN in Azure) |
| `-rtmp-port` | `1935` | RTMP server port |
| `-rtmp-token` | _(empty)_ | Auth token for RTMP subscribe (from secret) |
| `-mode` | `abr` | Transcoding mode: `abr` (multi-bitrate) or `copy` (remux) |
| `-log-level` | `info` | Log level: debug, info, warn, error |

## Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | `/events` | Receive webhook events from rtmp-server |
| GET | `/health` | Liveness/readiness probe (returns 200 OK) |

## Build

```bash
# Local binary
go build -o hls-transcoder .

# Docker image
docker build -t hls-transcoder:latest .
```

## Usage

```bash
# Local development
./hls-transcoder \
  -listen-addr :8090 \
  -hls-dir ./hls-output \
  -rtmp-host localhost \
  -rtmp-port 1935 \
  -mode abr \
  -log-level debug

# Trigger via webhook (simulating rtmp-server hook)
curl -X POST http://localhost:8090/events \
  -H 'Content-Type: application/json' \
  -d '{"type":"publish_start","stream_key":"live/test","data":{}}'
```

## HLS Output Structure

```
hls-output/
└── live_test/
    ├── master.m3u8          # ABR master playlist
    ├── stream_0/            # 1080p rendition
    │   ├── index.m3u8
    │   └── seg_00001.ts
    ├── stream_1/            # 720p rendition
    │   ├── index.m3u8
    │   └── seg_00001.ts
    └── stream_2/            # 480p rendition
        ├── index.m3u8
        └── seg_00001.ts
```

# 007 — HLS Transcoder Architecture

## Overview

The HLS transcoder is a Container App that converts live RTMP streams into
multi-bitrate adaptive HLS output using FFmpeg. It receives `publish_start`
and `publish_stop` webhook events from rtmp-server and manages FFmpeg process
lifecycles accordingly.

## Architecture

```
Publisher (OBS/FFmpeg)
    │
    │  RTMP (TCP 1935, external)
    ▼
┌───────────────────────────────────────────────────────────┐
│ Container Apps Environment (VNet 10.0.0.0/16)             │
│                                                           │
│  ┌─────────────┐    webhook     ┌──────────────────┐     │
│  │ rtmp-server  │──publish_start─▶ hls-transcoder   │     │
│  │ (TCP 1935)   │──publish_stop──▶ (HTTP 8090)      │     │
│  │              │                │                    │     │
│  │              │  segment_      │  FFmpeg subscribes │     │
│  │              │  complete      │  via RTMP (VNet)   │     │
│  │              │    │           │                    │     │
│  │              │    ▼           │  Writes:           │     │
│  │              │  ┌──────────┐ │  /hls-output/{key} │     │
│  │              │  │ blob-    │ │  ├── master.m3u8   │     │
│  │              │  │ sidecar  │ │  ├── stream_0/     │     │
│  │              │  │ (8080)   │ │  ├── stream_1/     │     │
│  │              │  └──────────┘ │  └── stream_2/     │     │
│  └─────────────┘                └──────────────────┘     │
│         │                              │                  │
│         ▼                              ▼                  │
│  Azure Files: recordings        Azure Files: hls-output   │
│         │                              │                  │
│         ▼                              ▼                  │
│  Azure Blob Storage            [Future] HLS Server        │
│  (segment archive)             mounts same share          │
└───────────────────────────────────────────────────────────┘
```

## Data Flow

1. Publisher connects to rtmp-server on TCP port 1935
2. rtmp-server fires `publish_start` webhook to hls-transcoder
3. hls-transcoder spawns FFmpeg process that subscribes to the RTMP stream
   over the internal VNet (no external network hop)
4. FFmpeg transcodes to 3 HLS renditions (1080p/720p/480p) with aligned
   keyframes and writes `.m3u8` playlists + `.ts` segments to the
   `hls-output` Azure Files share
5. When the publisher disconnects, rtmp-server fires `publish_stop` webhook
6. hls-transcoder sends SIGTERM to FFmpeg, which finalizes playlists and exits

## Transcoding Modes

### ABR Mode (Adaptive Bitrate)

Default mode. Single FFmpeg process with `-var_stream_map` producing 3
renditions:

| Rendition | Resolution | Video    | Max Rate | Audio  |
|-----------|-----------|----------|----------|--------|
| stream_0  | 1920×1080 | 5000 kbps | 5500 kbps | 192 kbps |
| stream_1  | 1280×720  | 2500 kbps | 2750 kbps | 128 kbps |
| stream_2  | 854×480   | 1000 kbps | 1100 kbps | 96 kbps  |

Key alignment parameters:
- `-force_key_frames "expr:gte(t,n_forced*2)"` — keyframe every 2 seconds
- `-sc_threshold 0` — disable scene-change detection for predictable segments
- `-hls_time 2` — 2-second HLS segments
- `-hls_list_size 10` — keep last 10 segments in playlist

**Resource requirements:** 4 vCPU / 8 GiB

### Copy Mode (Remux)

Flag: `-mode copy`. Remuxes the RTMP stream to HLS without transcoding.
Produces single-bitrate output at source quality.

**Resource requirements:** 0.5 vCPU / 1 GiB

## Configuration Reference

| Flag | Default | Description |
|------|---------|-------------|
| `-listen-addr` | `:8090` | HTTP listen address |
| `-hls-dir` | `/hls-output` | HLS output root directory (file mode only) |
| `-rtmp-host` | `localhost` | RTMP server hostname |
| `-rtmp-port` | `1935` | RTMP server port |
| `-rtmp-token` | _(empty)_ | Auth token for RTMP subscribe |
| `-mode` | `abr` | `abr` or `copy` |
| `-output-mode` | `file` | `file` (local filesystem) or `http` (HTTP ingest to blob-sidecar) |
| `-ingest-url` | _(empty)_ | HTTP ingest base URL (required for `-output-mode http`) |
| `-ingest-token` | _(empty)_ | Bearer token for HTTP ingest authentication |
| `-log-level` | `info` | `debug`, `info`, `warn`, `error` |

## Cost Analysis

| Mode | vCPU / Memory | Always-On (24/7) | Scheduled (5×2hr/wk) |
|------|--------------|------------------|----------------------|
| ABR  | 4 vCPU / 8 GiB | ~$60–80/month | ~$5/month |
| Copy | 0.5 vCPU / 1 GiB | ~$10–15/month | ~$1/month |

### Scale-to-Zero

The webhook-driven design supports Container Apps scale-to-zero. On incoming
HTTP request, the container cold-starts in ~10–30 seconds. This is acceptable
for scheduled broadcasts with 10-minute pre-spin buffers (see
`002-SCHEDULED-ORCHESTRATION.md`).

## Storage

### Azure Files (Phase 1 — file mode)

The `hls-output` Azure Files share (50 GiB) is mounted at `/hls-output` in
the hls-transcoder container. A future HLS server Container App can mount the
same share for direct serving.

**Capacity:** At 3 renditions × 2s segments × ~8 Mbps combined, keeping 10
segments per rendition ≈ 60 MB per active stream. The 50 GiB share supports
~800 concurrent streams.

### Azure Blob Storage via HTTP Ingest (Phase 2 — current)

In HTTP output mode (`-output-mode http`), FFmpeg uploads segments and variant
playlists directly to the hls-blob-sidecar via HTTP PUT. The sidecar buffers
each segment and uploads it to Azure Blob Storage. No Azure Files mount needed.

**Key details:**
- FFmpeg's `-master_pl_name` only writes to the local filesystem even in HTTP
  mode. The transcoder's `uploadMasterPlaylist()` function generates and uploads
  `master.m3u8` via HTTP PUT after a 2-second delay.
- Blob paths: `{eventId}/stream_N/index.m3u8`, `{eventId}/stream_N/seg_XXXXX.ts`
- Sidecar ingress must have `allowInsecure: true` (FFmpeg sends plain HTTP PUT).
  **Warning**: `az containerapp ingress update --transport http` resets
  `allowInsecure` to `false` — always re-apply `--allow-insecure` after changes.
- Transport must be `http`, never `tcp` — TCP transport breaks Container Apps
  internal HTTP routing.

### Azure Blob Storage + CDN (Phase 3 — future)

For multi-region distribution or CDN integration, Azure CDN can serve
directly from the Blob Storage origin.

## Troubleshooting

### FFmpeg not starting

- Check logs: `az containerapp logs show --name <hlsAppName> -g rg-rtmpgo`
- Verify rtmp-server is reachable: the `rtmp-host` flag must resolve to the
  rtmp-server internal FQDN
- Verify auth token matches between rtmp-server and hls-transcoder

### HLS segments not appearing

- Check the `hls-output` Azure Files share is mounted correctly
- Verify FFmpeg is running: logs should show "FFmpeg transcoder started"
- Check FFmpeg stderr output for codec/format errors

### High CPU usage

- ABR transcoding requires ~3.5 vCPU for 3 renditions at 30 fps
- If CPU is consistently > 90%, consider:
  - Reducing renditions (e.g., drop 1080p)
  - Lowering output frame rate (`-r 24`)
  - Switching to copy mode if source is already at target quality

### Stale segments after publisher reconnects

- FFmpeg `-hls_flags delete_segments` automatically cleans old segments
- The transcoder cleans up stale segments before starting a new FFmpeg process
  for the same stream key

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
┌───────────────────────────────────────────────────────────────┐
│ Container Apps Environment (VNet 10.0.0.0/16)                 │
│                                                               │
│  ┌─────────────┐    webhook     ┌───────────────────────────┐│
│  │ rtmp-server  │──publish_start─▶ hls-transcoder            ││
│  │ (TCP 1935)   │──publish_stop──▶ (multi-container app)     ││
│  │              │                │                             ││
│  │              │  segment_      │  ┌───────────┐ localhost  ││
│  │              │  complete      │  │ transcoder│───:8081──┐ ││
│  │              │    │           │  │ (FFmpeg)  │          │ ││
│  │              │    ▼           │  │ 3.5 vCPU  │          ▼ ││
│  │              │  ┌──────────┐ │  └───────────┘  ┌───────┐ ││
│  │              │  │ blob-    │ │                  │ blob- │ ││
│  │              │  │ sidecar  │ │                  │sidecar│ ││
│  │              │  │ (8080)   │ │                  │(8081) │ ││
│  │              │  └──────────┘ │                  │0.5 CPU│ ││
│  └─────────────┘                │                  └───┬───┘ ││
│         │                       └──────────────────────┼────┘│
│         ▼                                              │      │
│  Azure Files: recordings                               ▼      │
│         │                              Azure Blob Storage     │
│         ▼                              (hls-content container)│
│  Azure Blob Storage                           │               │
│  (recordings container)                       ▼               │
│                                        sg-hls (HLS server)    │
│                                        → proxies to viewer    │
└───────────────────────────────────────────────────────────────┘
```

> **Phase 4 — Co-located Sidecar:** The blob-sidecar for HLS runs as a second
> container in the same Container App as hls-transcoder. FFmpeg uploads segments
> via `localhost:8081`, bypassing the Envoy service mesh entirely. This eliminates
> the ~23% segment drop rate caused by the Envoy HTTP/2 CONNECT tunnel bug
> (envoyproxy/envoy#28329).

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
- `-hls_time 2` — 2-second HLS segments (configurable via Platform API)
- `-hls_list_size 6` — keep last 6 segments in playlist (configurable)
- `-tune zerolatency` — disable B-frames for lower encoding latency (configurable)
- `-preset ultrafast` — fastest encoding speed (configurable)

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
| `-platform-url` | _(empty)_ | Platform App URL for dynamic stream config |
| `-platform-api-key` | _(empty)_ | Internal API key for Platform App authentication |
| `-codec` | `h264` | Codec this transcoder handles |
| `-log-level` | `info` | `debug`, `info`, `warn`, `error` |

## Dynamic Stream Configuration

When `-platform-url` is configured, the transcoder fetches per-event stream
settings from the StreamGate Platform App instead of using hardcoded FFmpeg
arguments. This enables per-event tuning from the admin UI.

### Config Fetch Flow

1. **Startup**: Fetches system defaults from `GET /api/internal/stream-config/defaults`
   (cached, refreshed every 10 minutes)
2. **On `publish_start`**: Fetches per-event config from
   `GET /api/internal/events/:id/stream-config` (event ID from stream key)
3. **FFmpeg arg builder**: Constructs command-line args dynamically from config —
   rendition profile, segment duration, H.264 tune/preset, keyframe interval, etc.

### Failure Policy

| Fetch Result | Behavior |
|---|---|
| `200 OK` with valid config | Use per-event config |
| Timeout / `5xx` | Fall back to cached system defaults |
| `404 Not Found` | Do not transcode |
| `403 Forbidden` | Do not transcode |

### Publish Start/Stop Correlation

The transcoder uses connection ID correlation to prevent race conditions when
a new RTMP stream connects while the old stream's `publish_stop` is in flight.
Each `streamProcess` stores the `connID` from the webhook event; `Stop()` only
kills the FFmpeg process if the connID matches.

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

### Azure Files (Phase 1 — file mode, deprecated)

The original design mounted an `hls-output` Azure Files share. This was
replaced by HTTP ingest to Blob Storage in Phase 2.

### Azure Blob Storage via HTTP Ingest (Phase 2–3)

In HTTP output mode (`-output-mode http`), FFmpeg uploads segments and variant
playlists directly to a blob-sidecar via HTTP PUT. The sidecar buffers each
segment and uploads it to Azure Blob Storage. No Azure Files mount needed.

**Key details:**
- FFmpeg's `-master_pl_name` only writes to the local filesystem even in HTTP
  mode. The transcoder's `uploadMasterPlaylist()` function generates and uploads
  `master.m3u8` via HTTP PUT after a 2-second delay.
- Blob paths: `{eventId}/stream_N/index.m3u8`, `{eventId}/stream_N/seg_XXXXX.ts`

### Co-located Sidecar via localhost (Phase 4 — current)

Phase 3 used a separate `hls-blob-sidecar` Container App, but Azure Container
Apps routes inter-app traffic through an Envoy HTTP/2 CONNECT tunnel. A known
bug (envoyproxy/envoy#28329) causes RST_STREAM before DATA frames are flushed
with chunked transfer encoding, resulting in ~23% segment drops.

**Phase 4 solution:** The blob-sidecar runs as a second container in the same
Container App as hls-transcoder. FFmpeg sends HTTP PUT to `localhost:8081`,
bypassing Envoy entirely. Result: 0% segment drops.

**Key details:**
- Multi-container app: hls-transcoder (3.5 vCPU/7GiB) + blob-sidecar (0.5 vCPU/1GiB)
- Ingest URL: `http://localhost:8081/ingest/`
- The standalone `hls-blob-sidecar` Container App remains scaled to 0 for rollback
- No ingress configuration needed for the co-located sidecar (localhost only)

### Azure Blob Storage + CDN (future)

For multi-region distribution or CDN integration, Azure CDN can serve
directly from the Blob Storage origin.

## Troubleshooting

### FFmpeg not starting

- Check logs: `az containerapp logs show --name <hlsAppName> -g rg-rtmpgo`
- Verify rtmp-server is reachable: the `rtmp-host` flag must resolve to the
  rtmp-server internal FQDN
- Verify auth token matches between rtmp-server and hls-transcoder

### HLS segments not appearing in Blob Storage

- Verify FFmpeg is running: logs should show "FFmpeg transcoder started"
- Check co-located blob-sidecar container logs: `az containerapp logs show --name <hlsAppName> -g rg-rtmpgo --container blob-sidecar`
- Verify both containers are running: `az containerapp show --name <hlsAppName> -g rg-rtmpgo --query 'properties.template.containers[].name'`
- Check FFmpeg stderr output for HTTP PUT errors (connection refused = sidecar not ready)
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

# HLS Transcoder

Webhook-driven HLS transcoding service for the rtmp-go streaming platform.
Receives `publish_start`/`publish_stop` events from rtmp-server's webhook hook
system and manages FFmpeg processes that convert live RTMP streams into
multi-bitrate adaptive HLS output.

## Architecture

```
rtmp-server ──webhook──► hls-transcoder ──FFmpeg──► HLS segments
  (TCP 1935)              (HTTP 8090)               (Azure Files / HTTP ingest)
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

**Key FFmpeg flags:**

| Flag | Value | Purpose |
|------|-------|---------|
| `-async` | `1` | Resample audio to fix non-monotonic DTS timestamps |
| `-vsync` | `cfr` | Constant frame rate — prevents frame timing drift |
| `-hls_time` | `3` | 3-second segments (balanced for Azure upload pipeline) |
| `-hls_list_size` | `6` | 18-second playlist window (6 × 3s) |
| `-hls_flags` | `independent_segments` | Each segment independently decodable; avoids `delete_segments` which races with blob-sidecar on Azure Files SMB |
| `-force_key_frames` | `expr:gte(t,n_forced*2)` | Aligned keyframes every 2s across renditions |
| `-sc_threshold` | `0` | Disable scene-change keyframes (keeps GOP alignment) |

**Resource requirements:** 2 vCPU / 4 GiB (Azure deployment)

### Copy Mode

Remuxes the RTMP stream to HLS without transcoding (`-c copy`). Produces a
single-bitrate HLS output at the original source quality.

**Resource requirements:** 0.5 vCPU / 1 GiB

## Output Modes (Phase 2: HTTP Ingest)

The transcoder supports two output modes that determine where HLS segments are written:

### File Mode (default)

Writes HLS segments and playlists to the local filesystem (Azure Files SMB mount):

```bash
./hls-transcoder \
  -listen-addr :8090 \
  -hls-dir /hls-output \
  -rtmp-host rtmp-server \
  -mode abr \
  -output-mode file
```

**Characteristics:**
- Segments written to `/hls-output/{stream_key}/`
- Optional SegmentNotifier polls directory for new files and sends webhook events to blob-sidecar
- Suitable for deployments where Azure Files SMB mounts are directly accessible
- Master playlist (`master.m3u8`) is pre-written to ensure consistency on SMB mounts

### HTTP Mode (Phase 2)

Streams HLS segments and playlists directly to blob-sidecar's HTTP ingest endpoint via PUT requests:

```bash
./hls-transcoder \
  -listen-addr :8090 \
  -rtmp-host rtmp-server \
  -mode abr \
  -output-mode http \
  -ingest-url http://blob-sidecar:8081/ingest/ \
  -ingest-token "bearer-token-xyz"  # optional, for secure deployments
```

**Characteristics:**
- FFmpeg uses `-method PUT` to upload segments directly
- No local filesystem I/O; entirely HTTP-based
- SegmentNotifier is bypassed (HTTP mode doesn't poll local files)
- Master playlist is generated server-side via `-master_pl_name`
- URL structure: `http://blob-sidecar:8081/ingest/hls/{eventId}/stream_%v/index.m3u8`
- Optional bearer token passed via `Authorization` header on each PUT request (using FFmpeg `-headers` option)
- Requires FFmpeg 8.0+ (uses `-headers` instead of removed `-custom_http_headers`)
- FFmpeg's HLS HTTP muxer uses chunked transfer encoding (no Content-Length)
- Suitable for cloud-native deployments where blob-sidecar is a dedicated ingest gateway

**URL construction:**
- ABR mode: `http://blob-sidecar:8081/ingest/hls/mystream/stream_%v/index.m3u8` (FFmpeg replaces %v with variant 0, 1, 2)
- Copy mode: `http://blob-sidecar:8081/ingest/hls/mystream/index.m3u8` (no variants)

## Source Encoder Recommendations

The transcoder works best with a clean source stream. Avoid B-frames and ensure fixed keyframe intervals:

| Setting | Recommended | Why |
|---------|-------------|-----|
| H.264 Profile | Baseline | No B-frames = no reference frame errors |
| B-frames | 0 | Prevents non-monotonic DTS timestamps |
| Keyframe Interval | 2 seconds | Matches `-force_key_frames` alignment |
| Rate Control | CBR | Consistent bitrate avoids transcoder buffer underflows |
| Audio Sample Rate | 48000 Hz | Matches HLS rendition audio settings |

See [docs/obs-streaming-guide.md](../../docs/obs-streaming-guide.md) for detailed OBS Studio settings.

## Configuration

| Flag | Default | Description |
|------|---------|-------------|
| `-listen-addr` | `:8090` | HTTP listen address for webhook events |
| `-hls-dir` | `/hls-output` | Root directory for HLS output files (file mode only) |
| `-rtmp-host` | `localhost` | RTMP server hostname (internal FQDN in Azure) |
| `-rtmp-port` | `1935` | RTMP server port |
| `-rtmp-token` | _(empty)_ | Auth token for RTMP subscribe (from secret) |
| `-mode` | `abr` | Transcoding mode: `abr` (multi-bitrate) or `copy` (remux) |
| `-output-mode` | `file` | Output mode: `file` (local filesystem) or `http` (blob-sidecar HTTP ingest) |
| `-ingest-url` | _(empty)_ | HTTP ingest base URL for blob-sidecar (required if `-output-mode http`) |
| `-ingest-token` | _(empty)_ | Bearer token for HTTP ingest endpoint authentication (optional, for secure deployments) |
| `-blob-webhook-url` | _(empty)_ | Webhook URL for blob-sidecar segment upload (file mode only; empty = no blob upload) |
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

### Local Development (File Mode)

```bash
./hls-transcoder \
  -listen-addr :8090 \
  -hls-dir ./hls-output \
  -rtmp-host localhost \
  -rtmp-port 1935 \
  -mode abr \
  -output-mode file \
  -log-level debug
```

### Production with blob-sidecar (HTTP Mode)

```bash
./hls-transcoder \
  -listen-addr :8090 \
  -rtmp-host rtmp-server \
  -rtmp-port 1935 \
  -mode abr \
  -output-mode http \
  -ingest-url http://blob-sidecar:8081/ingest/ \
  -ingest-token "secret-bearer-token" \
  -log-level info
```

### Production with Azure Files and blob-sidecar (File Mode with Webhook)

```bash
./hls-transcoder \
  -listen-addr :8090 \
  -hls-dir /mnt/azure-files/hls \
  -rtmp-host rtmp-server \
  -rtmp-port 1935 \
  -mode copy \
  -output-mode file \
  -blob-webhook-url http://blob-sidecar:8090/webhook \
  -log-level info
```

### Trigger via Webhook

```bash
# Simulate rtmp-server webhook for publish_start event
curl -X POST http://localhost:8090/events \
  -H 'Content-Type: application/json' \
  -d '{"type":"publish_start","stream_key":"live/test","data":{}}'

# Simulate publish_stop event
curl -X POST http://localhost:8090/events \
  -H 'Content-Type: application/json' \
  -d '{"type":"publish_stop","stream_key":"live/test","data":{}}'
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

# File Transcoder — VOD Upload Pipeline

Standalone CLI program that transcodes uploaded video files into multi-rendition HLS output in fMP4/CMAF format. Designed to run as a one-shot container job (e.g., Azure Container Apps Job).

## Architecture

```
┌─────────────────┐     ┌──────────────────┐     ┌─────────────────┐
│  Platform App    │────▶│  File Transcoder  │────▶│  Blob Storage   │
│  (triggers job)  │     │  (FFmpeg encode)  │     │  (HLS output)   │
│                  │◀────│                   │     │                 │
│  (receives       │     └──────────────────┘     └─────────────────┘
│   callback)      │
└─────────────────┘
```

The Platform App creates a `TranscodeJob` record and launches this container with environment variables. The transcoder downloads the source, runs FFmpeg, and POSTs a callback when done.

## Pipeline

1. **Parse config** — Read environment variables (`config.go`)
2. **Resolve source** — Download from URL or use local file
3. **Probe duration** — FFmpeg determines total video length (for progress %)
4. **Transcode** — Multi-rendition HLS encode with fMP4/CMAF segments (`ffmpeg.go`)
5. **Report progress** — POST updates every 5s to Platform App (`progress.go`)
6. **Callback** — POST completion/failure result (`callback.go`)
7. **Exit** — Code 0 (success) or 1 (failure)

## Supported Codecs

| Codec | Encoder | Audio | Notes |
|-------|---------|-------|-------|
| `h264` | libx264 | AAC | Widest compatibility |
| `av1` | libsvtav1 / libaom-av1 | Opus | Best compression, SVT-AV1 preferred |
| `vp8` | libvpx | Opus | Legacy WebM |
| `vp9` | libvpx-vp9 | Opus | Good compression, wide browser support |

> **Note:** Alpine's FFmpeg package includes libaom-av1 but not SVT-AV1. The transcoder auto-detects and falls back. For SVT-AV1, build FFmpeg from source.

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `JOB_ID` | ✅ | — | Unique job ID (matches TranscodeJob in Platform DB) |
| `EVENT_ID` | — | — | Event UUID for output path construction |
| `CODEC` | ✅ | — | `h264`, `av1`, `vp8`, or `vp9` |
| `SOURCE_BLOB_URL` | ✅ | — | URL or local path to source video |
| `OUTPUT_BLOB_PREFIX` | — | — | Blob path prefix for uploads |
| `RENDITIONS` | ✅ | — | JSON array of rendition specs |
| `CODEC_CONFIG` | — | `{}` | JSON object with codec overrides |
| `HLS_TIME` | — | `4` | Segment duration in seconds |
| `FORCE_KEYFRAME_INTERVAL` | — | `4` | Keyframe interval in seconds |
| `CALLBACK_URL` | ✅ | — | Platform App callback URL |
| `PROGRESS_URL` | — | — | Platform App progress URL |
| `INTERNAL_API_KEY` | — | — | Auth key for callbacks |
| `AZURE_STORAGE_CONNECTION_STRING` | — | — | Blob access (omit for local dev) |
| `OUTPUT_DIR` | — | `/out/transcode-output` | Local output directory |

## Renditions Format

```json
[
  {"label": "1080p", "width": 1920, "height": 1080, "videoBitrate": "5000k", "audioBitrate": "192k"},
  {"label": "720p",  "width": 1280, "height": 720,  "videoBitrate": "2500k", "audioBitrate": "128k"},
  {"label": "480p",  "width": 854,  "height": 480,  "videoBitrate": "1000k", "audioBitrate": "96k"}
]
```

## Output Structure

```
{OUTPUT_DIR}/{EVENT_ID}/{CODEC}/
├── stream_0/           ← Highest quality (e.g., 1080p)
│   ├── init.mp4        ← fMP4 initialization segment
│   ├── seg_00000.m4s   ← Media segments
│   ├── seg_00001.m4s
│   └── index.m3u8      ← VOD playlist with #EXT-X-ENDLIST
├── stream_1/           ← Next quality (e.g., 720p)
│   ├── init.mp4
│   ├── seg_00000.m4s
│   └── index.m3u8
└── stream_2/           ← Lowest quality (e.g., 480p)
    ├── init.mp4
    ├── seg_00000.m4s
    └── index.m3u8
```

## Local Development

```bash
cd azure/file-transcoder
go build -o file-transcoder .

# Transcode a local file
JOB_ID=test-001 \
CODEC=h264 \
SOURCE_BLOB_URL=/path/to/video.mp4 \
OUTPUT_DIR=./output \
RENDITIONS='[{"label":"720p","width":1280,"height":720,"videoBitrate":"2500k","audioBitrate":"128k"}]' \
CALLBACK_URL=http://localhost:3000/api/internal/transcode/callback \
./file-transcoder
```

## Docker

```bash
# Build
docker build -t file-transcoder .

# Run
docker run --rm \
  -e JOB_ID=test-001 \
  -e CODEC=h264 \
  -e SOURCE_BLOB_URL=/input/video.mp4 \
  -e RENDITIONS='[{"label":"720p","width":1280,"height":720,"videoBitrate":"2500k","audioBitrate":"128k"}]' \
  -e CALLBACK_URL=http://host.docker.internal:3000/api/internal/transcode/callback \
  -v /path/to/input:/input:ro \
  -v /path/to/output:/out/transcode-output \
  file-transcoder
```

## Callback Payload

### Success
```json
{
  "jobId": "abc123",
  "codec": "h264",
  "status": "completed",
  "duration": 330.5,
  "variants": ["stream_0/index.m3u8", "stream_1/index.m3u8", "stream_2/index.m3u8"]
}
```

### Failure
```json
{
  "jobId": "abc123",
  "codec": "h264",
  "status": "failed",
  "error": "FFmpeg failed: exit status 1 — Error opening input file"
}
```

## Related

- **Live transcoder:** `azure/hls-transcoder/` — RTMP→HLS for live streams
- **Platform App:** `platform/` — Manages events, tokens, and transcode jobs

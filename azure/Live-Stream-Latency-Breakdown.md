# Live Stream Latency Breakdown

Investigated April 26, 2026. Observed ~20s end-to-end delay from RTMP ingest to HLS player.

> **Update (April 26, 2026):** All "Quick Wins" optimizations have been implemented and deployed. End-to-end latency reduced from ~20s to **8–10s**. See [Implemented Optimizations](#implemented-optimizations) below.

## The Pipeline

```
OBS/FFmpeg → RTMP Server → Transcoder (FFmpeg) → Blob Sidecar → Azure Blob Storage → HLS Server (proxy) → Browser (hls.js)
```

## Stage-by-Stage Analysis

| # | Stage | Latency | Evidence |
|---|-------|---------|----------|
| 1 | **FFmpeg segment accumulation** | **3–7s** | `hls_time 3` but stream_0 (copy mode) shows segments of 3.3–6.6s. Keyframes from source control segment boundaries — copy mode can't split on non-keyframes |
| 2 | **Blob sidecar upload** | **~1–2s** | HTTP PUT of ~2.4 MB segments to Azure Blob via managed identity. No stabilize-duration (HTTP ingest mode — upload starts immediately on PUT) |
| 3 | **Azure Blob Storage propagation** | **<0.5s** | Eventual consistency is negligible for write-then-read |
| 4 | **HLS server proxy fetch** | **0.4–2.0s** | Measured: playlist 0.38–0.42s, segment 1.97s (2.4 MB through proxy → blob → back). Every .m3u8 request goes upstream uncached |
| 5 | **hls.js playback buffer** | **9–18s** | `liveSyncDurationCount: 3` × ~3–5s avg segment = **9–15s behind live edge**. This is the dominant contributor |

## The Top 3 Contributors

### 1. hls.js `liveSyncDurationCount: 3` — ~9–15s (BIGGEST)

hls.js plays `liveSyncDurationCount × targetDuration` behind the live edge. With `liveSyncDurationCount: 3` and stream_0 having `TARGETDURATION: 7`, the player targets **21s behind live edge** in the worst case. Even with the average ~5s segments, it's ~15s behind.

This is a safety buffer to avoid rebuffering — the player intentionally sits behind live to have segments pre-loaded.

### 2. FFmpeg segment duration — 3–7s (MODERATE)

`hls_time 3` is the target, but stream_0 uses `-c:v copy` (no transcode). In copy mode, FFmpeg can only cut at keyframes. The source's keyframe interval determines actual segment size. The observed `TARGETDURATION: 7` and segments up to 6.6s mean the source GOP is likely longer than 3s.

The transcoded renditions (720p, 480p) use `force_key_frames "expr:gte(t,n_forced*2)"` — keyframe every 2s — and show tighter 2–4s segments with `TARGETDURATION: 4`.

### 3. HLS server double-proxy hop — 0.4–2.0s per request

Every playlist and segment request goes: **Browser → HLS Server (East US 2) → Azure Blob Storage → back**. No caching for live manifests (correct, but adds RTT). Segments are cached after first fetch, but each new live segment is a cold fetch.

## Current Configuration

### Transcoder (production ABR mode)

- `hls_time 2`, `hls_list_size 6` (12s playlist window) — configurable via Platform API
- `hls_flags independent_segments` (no `delete_segments`, no `omit_endlist`)
- stream_0: `-c:v copy` (passthrough, keyframes from source) — in `full-abr-1080p-720p-480p` profile
- stream_1/2: `ultrafast` preset, `force_key_frames "expr:gte(t,n_forced*2)"`, `-tune zerolatency`
- Output mode: HTTP PUT to blob sidecar (localhost:8081)
- Config source: Dynamic from Platform App (`-platform-url` + `-platform-api-key` flags)

### Player (hls.js)

- `lowLatencyMode: true` (when live) — configurable per-event
- `liveSyncDurationCount: 2` — configurable per-event
- `liveMaxLatencyDurationCount: 4` — configurable per-event
- `backBufferLength: 0` — configurable per-event
- Player config delivered via `playerConfig` field in token validation response

### Observed Playlist Data

- stream_0 (1080p copy): `TARGETDURATION: 7`, segments 3.3–6.6s, 6 segments in playlist
- stream_1 (720p transcode): `TARGETDURATION: 4`, segments 2–4s, 6 segments in playlist

## Optimization Plan

### Quick Wins (no architecture changes) — target ~8–10s ✅ IMPLEMENTED

| Change | Impact | Difficulty | Status |
|--------|--------|------------|--------|
| **Source encoder: keyframe interval 1s** (`keyint=30` at 30fps in OBS) | stream_0 copy-mode segments drop from 3–7s to ~1–2s. **Biggest server-side win** | OBS setting | ⚠️ Requires OBS config |
| **Transcoder: `hls_time 2`** | Smaller segments → less accumulation delay. ~1s saved per segment | Config change, redeploy transcoder | ✅ Done |
| **Transcoder: add `-tune zerolatency`** for 720p/480p | Reduces encoding latency on transcoded renditions | Add to transcoder args | ✅ Done |
| **Player: `liveSyncDurationCount: 2`** | Saves ~3–5s. Slightly higher rebuffer risk | One-line code change | ✅ Done |
| **Player: `liveMaxLatencyDurationCount: 4`** | Faster catch-up after stalls | One-line code change | ✅ Done |
| **Player: `liveBackBufferLength: 0`** | Reduces memory, slightly faster seeks to edge | One-line code change | ✅ Done |

### Medium Effort — target ~5–7s

| Change | Impact | Difficulty |
|--------|--------|------------|
| **Enable `EXT-X-PROGRAM-DATE-TIME`** in FFmpeg | Allows hls.js to calculate true live edge more accurately | FFmpeg flag + hls.js config |
| **HLS server: short-TTL cache for live playlists** (~1s) | Reduces upstream round-trips on concurrent viewers | Code change in HLS server |

### Major — target 2–4s (LL-HLS)

| Change | Impact | Difficulty |
|--------|--------|------------|
| **LL-HLS with CMAF partial segments** | Sub-segment delivery, 2–4s total latency | Requires FFmpeg `-hls_fmp4_init_filename`, `#EXT-X-PART`, `#EXT-X-PRELOAD-HINT`, architectural changes to blob sidecar and HLS server |

## Codec Compatibility

This analysis was performed against an H.264 pipeline, but most optimizations are **codec-agnostic**. Below is a breakdown of what works as-is, what needs syntax changes, and what is H.264-specific.

### Fully Codec-Agnostic

These optimizations are architecture/protocol-level and work identically with H.264, HEVC, VP9, and AV1:

- **Player tuning** — `liveSyncDurationCount`, `liveMaxLatencyDurationCount`, `liveBackBufferLength` (pure hls.js buffer math)
- **HLS manifest settings** — `hls_list_size`, playlist window size, `EXT-X-PROGRAM-DATE-TIME`
- **Infrastructure** — blob sidecar upload, HLS server playlist caching, proxy hop latency

### Codec-Agnostic in Principle, Syntax Differs

| Optimization | H.264 | VP9 (libvpx-vp9) | AV1 (SVT-AV1 / libaom) | Notes |
|---|---|---|---|---|
| **Source keyframe interval** | `keyint=30` | `-g 30` | `-g 30` / `--keyint 30` | VP9/AV1 keyframes are more expensive — shorter intervals hurt compression more than H.264 |
| **`hls_time 2`** | Works well | Works, but encode time adds latency | Works, but real-time encoding at 2s requires significant CPU or HW accel | AV1 especially — a 2s segment may take >2s to encode without fast presets |
| **`force_key_frames`** | Low cost | Moderate cost | High cost | Forced keyframes in AV1/VP9 produce larger I-frames with bigger quality penalties |
| **Low-latency encoder tuning** | `-tune zerolatency` | `-deadline realtime -cpu-used 8` | SVT-AV1: `--preset 12 --fast-decode 1`. libaom: too slow for real-time | **No direct `-tune zerolatency` equivalent** for VP9/AV1 |

### LL-HLS / CMAF

LL-HLS with CMAF partial segments (`EXT-X-PART`, fMP4 containers) supports H.264 and HEVC natively. VP9 and AV1 are valid in CMAF/fMP4 but **browser support is limited**:

- **Chromium** — hls.js + MSE can decode AV1/VP9 in fMP4
- **Safari** — HLS native playback supports H.264/HEVC only; no VP9/AV1

### Practical Constraint

VP9 and especially AV1 encode **significantly slower** than H.264. Real-time transcoding at the segment sizes proposed (2s) requires more CPU or hardware acceleration, which itself becomes a latency bottleneck. For live low-latency use cases, H.264 remains the most practical choice unless hardware AV1 encoding (e.g., NVENC AV1, QSV AV1) is available.

## Theoretical Minimum with Current Architecture

With 2s segments + `liveSyncDurationCount: 2`:
- Segment accumulation: 2s
- Upload + propagation: 1s
- Proxy fetch: 0.5s
- Player buffer: 2 × 2s = 4s
- **Total: ~7–8s**

To go below 5s requires LL-HLS (partial segments / CMAF chunks).

## Implemented Optimizations

All "Quick Wins" from the optimization plan above have been implemented and deployed to Azure Container Apps. The stream configuration is now **dynamic and per-event configurable** via the StreamGate admin UI.

### Changes Deployed

| Change | Before | After | Impact |
|--------|--------|-------|--------|
| **`hls_time`** | `3` (hardcoded) | `2` (configurable, default 2) | ~1s less accumulation per segment |
| **`-tune zerolatency`** | Not set | Applied to all transcoded renditions | ~0.5s less encoding latency |
| **`-preset`** | `ultrafast` (hardcoded) | `ultrafast` (configurable) | No change, but now tunable |
| **`liveSyncDurationCount`** | `3` (hardcoded) | `2` (configurable, default 2) | ~2–4s less player buffer delay |
| **`liveMaxLatencyDurationCount`** | `6` (hardcoded) | `4` (configurable, default 4) | Faster catch-up after stalls |
| **`backBufferLength`** | Not set | `0` (configurable) | Less memory, faster edge seeking |
| **`lowLatencyMode`** | `true` (hardcoded) | `true` (configurable, default true) | No change, but now tunable |

### How Dynamic Config Works

The transcoder no longer uses hardcoded FFmpeg arguments. Instead:

1. On startup, it fetches system-wide defaults from `GET /api/internal/stream-config/defaults`
2. On each `publish_start`, it fetches per-event config from `GET /api/internal/events/:id/stream-config`
3. FFmpeg arguments are built dynamically from the fetched config
4. The player receives its config via the `playerConfig` field in the token validation response

Admins can tune settings per-event via the "Advanced Stream Settings" section in the event edit form, or change system-wide defaults at `/admin/settings`.

### Current Production Configuration

```json
{
  "transcoder": {
    "codecs": ["h264"],
    "profile": "full-abr-1080p-720p-480p",
    "hlsTime": 2,
    "hlsListSize": 6,
    "forceKeyFrameInterval": 2,
    "h264": { "tune": "zerolatency", "preset": "ultrafast" }
  },
  "player": {
    "liveSyncDurationCount": 2,
    "liveMaxLatencyDurationCount": 4,
    "backBufferLength": 0,
    "lowLatencyMode": true
  }
}
```

### Measured Result

**~8–10 seconds** end-to-end latency (down from ~20s), matching the theoretical minimum for non-LL-HLS with 2s segments.

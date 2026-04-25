# Live Stream Latency Breakdown

Investigated April 26, 2026. Observed ~20s end-to-end delay from RTMP ingest to HLS player.

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

- `hls_time 3`, `hls_list_size 6` (18s playlist window)
- `hls_flags independent_segments` (no `delete_segments`, no `omit_endlist`)
- stream_0: `-c:v copy` (passthrough, keyframes from source)
- stream_1/2: `ultrafast` preset, `force_key_frames "expr:gte(t,n_forced*2)"`, no `-tune zerolatency`
- Output mode: HTTP PUT to blob sidecar (localhost:8081)

### Player (hls.js)

- `lowLatencyMode: true` (when live)
- `liveSyncDurationCount: 3`
- `liveMaxLatencyDurationCount: 6`
- No `liveBackBufferLength`, no `EXT-X-PROGRAM-DATE-TIME` support

### Observed Playlist Data

- stream_0 (1080p copy): `TARGETDURATION: 7`, segments 3.3–6.6s, 6 segments in playlist
- stream_1 (720p transcode): `TARGETDURATION: 4`, segments 2–4s, 6 segments in playlist

## Optimization Plan

### Quick Wins (no architecture changes) — target ~8–10s

| Change | Impact | Difficulty |
|--------|--------|------------|
| **Source encoder: keyframe interval 1s** (`keyint=30` at 30fps in OBS) | stream_0 copy-mode segments drop from 3–7s to ~1–2s. **Biggest server-side win** | OBS setting |
| **Transcoder: `hls_time 2`** | Smaller segments → less accumulation delay. ~1s saved per segment | Config change, redeploy transcoder |
| **Transcoder: add `-tune zerolatency`** for 720p/480p | Reduces encoding latency on transcoded renditions | Add to transcoder args |
| **Player: `liveSyncDurationCount: 2`** | Saves ~3–5s. Slightly higher rebuffer risk | One-line code change |
| **Player: `liveMaxLatencyDurationCount: 4`** | Faster catch-up after stalls | One-line code change |
| **Player: `liveBackBufferLength: 0`** | Reduces memory, slightly faster seeks to edge | One-line code change |

### Medium Effort — target ~5–7s

| Change | Impact | Difficulty |
|--------|--------|------------|
| **Enable `EXT-X-PROGRAM-DATE-TIME`** in FFmpeg | Allows hls.js to calculate true live edge more accurately | FFmpeg flag + hls.js config |
| **HLS server: short-TTL cache for live playlists** (~1s) | Reduces upstream round-trips on concurrent viewers | Code change in HLS server |

### Major — target 2–4s (LL-HLS)

| Change | Impact | Difficulty |
|--------|--------|------------|
| **LL-HLS with CMAF partial segments** | Sub-segment delivery, 2–4s total latency | Requires FFmpeg `-hls_fmp4_init_filename`, `#EXT-X-PART`, `#EXT-X-PRELOAD-HINT`, architectural changes to blob sidecar and HLS server |

## Theoretical Minimum with Current Architecture

With 2s segments + `liveSyncDurationCount: 2`:
- Segment accumulation: 2s
- Upload + propagation: 1s
- Proxy fetch: 0.5s
- Player buffer: 2 × 2s = 4s
- **Total: ~7–8s**

To go below 5s requires LL-HLS (partial segments / CMAF chunks).

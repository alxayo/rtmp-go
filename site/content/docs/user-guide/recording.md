---
title: "Recording"
weight: 1
---

# Recording

go-rtmp records published streams to disk automatically. The server selects the container format based on the video codec:

- **H.264/AVC streams** → **FLV** (Flash Video) container
- **H.265/HEVC streams** → **MP4** (ISO BMFF) container

Modern codecs including **AV1**, **VP8**, **VP9**, and **VVC** also record to MP4.

Recording runs alongside live relay — subscribers see the stream in real-time while the server simultaneously writes to disk.

## Enabling Recording

Add the `-record-all` and `-record-dir` flags:

```bash
./rtmp-server -record-all true -record-dir ./recordings
```

The directory is created automatically if it doesn't exist. By default, `-record-dir` points to `recordings` in the working directory.

## File Naming

Recordings follow this naming pattern:

```
{streamkey}_{YYYYMMDD}_{HHMMSS}.{flv,mp4}
```

Any forward slashes in the stream key are replaced with underscores. For example, publishing H.264 to `live/mystream` at 2:30:22 PM on January 15, 2024 produces:

```
recordings/live_mystream_20240115_143022.flv
```

An H.265 stream to the same key would produce:

```
recordings/live_mystream_20240115_143022.mp4
```

## FLV Recording (H.264)

FLV files contain video (H.264, AV1, VP9) and audio (AAC) in a standard Flash Video container. Enhanced RTMP tags are preserved transparently. The server writes:

- A 13-byte FLV header (signature `FLV`, version 1, audio+video flags)
- An `onMetaData` script tag (type 0x12) containing video dimensions, codec IDs, audio sample rate, and stereo flag
- FLV tags for each audio (type 0x08) and video (type 0x09) message
- Proper `PreviousTagSize` fields for seeking compatibility

On close, the `duration` and `filesize` fields in the `onMetaData` tag are patched via `WriteAt()` so that players can display accurate duration and seeking information.

## MP4 Recording (H.265)

When H.265/HEVC video is detected, the server writes an ISO BMFF (MP4) container:

- **Streaming write**: `mdat` (media data) is written to disk in real-time as frames arrive — no memory buffering
- **On close**: The `mdat` size is patched via `WriteAt()` and the `moov` atom (with `trak`, `stbl`, sample tables) is appended
- **File layout**: `ftyp` (32 bytes) → `mdat` (streamed media) → `moov` (metadata)
- **Codec detection**: Container format is decided lazily when the first video message arrives, ensuring correct codec identification

### Supported Audio Codecs in MP4

MP4 recording supports all Enhanced RTMP audio codecs with proper codec-specific sample entries:

| Audio Codec | FourCC | MP4 Sample Entry | Config Box | Notes |
|-------------|--------|------------------|------------|-------|
| AAC         | `mp4a` | `mp4a`           | `esds`     | Default, works with legacy and enhanced RTMP |
| Opus        | `Opus` | `Opus`           | `dOps`     | Always 48kHz timescale; parses OpusHead |
| FLAC        | `fLaC` | `fLaC`           | `dfLa`     | Parses STREAMINFO for sample rate/channels |
| AC-3        | `ac-3` | `ac-3`           | `dac3`     | Default 48kHz / 5.1 surround |
| E-AC-3      | `ec-3` | `ec-3`           | `dec3`     | Default 48kHz / 5.1 surround |
| MP3         | `.mp3` | `.mp3`           | `esds`     | Uses MPEG-1 Audio OTI (0x6B) |

## Verifying Recordings

Inspect a recording with FFprobe:

```bash
ffprobe recordings/live_mystream_20240115_143022.flv
ffprobe recordings/live_mystream_20240115_143022.mp4
```

Play it back directly:

```bash
ffplay recordings/live_mystream_20240115_143022.flv
ffplay recordings/live_mystream_20240115_143022.mp4
```

## Graceful Degradation

Recording errors **never affect live streaming**. If the recorder encounters a write error (disk full, permission denied, etc.), it disables itself and logs the failure. The publisher and all subscribers continue uninterrupted.

The recorder's `Disabled()` method returns true after a fatal write error, and all subsequent writes are silently ignored.

## Lifecycle

- **Start**: A new file is created when a publisher begins streaming (if `-record-all` is enabled).
- **Codec detection**: The container format (FLV or MP4) is determined when the first video message arrives.
- **During**: Each audio and video message is written in real-time.
- **Stop**: The file is finalized (MP4 moov atom appended, FLV closed) when the publisher disconnects.

## Limitations

| Limitation | Detail |
|------------|--------|
| All-or-nothing | `-record-all` records every stream — there is no per-stream control |
| No file rotation | Each publish session creates one file; there is no time-based or size-based splitting |
| Audio + video only | Data messages (AMF) are not recorded |

## Example Session

```bash
# Terminal 1: Start server with recording
./rtmp-server -record-all true -record-dir ./recordings -log-level info

# Terminal 2: Publish H.264 (records as FLV)
ffmpeg -re -i test.mp4 -c:v libx264 -c:a aac -f flv rtmp://localhost:1935/live/h264

# Terminal 3: Publish H.265 (records as MP4)
ffmpeg -re -i test.mp4 -c:v libx265 -c:a aac -f flv rtmp://localhost:1935/live/h265

# Check recordings
ls recordings/
# live_h264_20240115_143022.flv
# live_h265_20240115_143025.mp4
```

## Segmented Recording

For long-running streams, go-rtmp can split recordings into multiple segment files of configurable duration. Each segment is **independently playable** — players can open any segment without needing previous ones.

### Enabling Segments

Add `-segment-duration` to activate segmentation:

```bash
./rtmp-server -record-all true -record-dir ./recordings -segment-duration 30s
```

Segments rotate at the next **video keyframe** after the target duration elapses. This ensures each segment starts with a decodable frame. For audio-only streams, rotation occurs immediately at the duration boundary.

> **Tip:** Your segment duration should be significantly longer than the encoder's keyframe interval (GOP size). For example, with a 2-second GOP, a 30-second segment duration produces segments of approximately 30–32 seconds.

### Filename Patterns

The `-segment-pattern` flag controls segment naming using FFmpeg-inspired placeholders:

```bash
./rtmp-server -record-all true \
  -segment-duration 5m \
  -segment-pattern "%s/%Y-%m-%D/seg%03d"
```

| Placeholder | Expansion | Example |
|---|---|---|
| `%s` | Stream key (slashes → underscores) | `live_mystream` |
| `%d` | Segment number (1-based) | `1`, `2`, `3` |
| `%03d` | Zero-padded segment number | `001`, `002` |
| `%T` | Timestamp `YYYYMMDD_HHMMSS` | `20260419_221050` |
| `%Y` | Year (4-digit) | `2026` |
| `%m` | Month (2-digit) | `04` |
| `%D` | Day (2-digit) | `19` |
| `%H` | Hour (24h) | `22` |
| `%M` | Minute | `10` |
| `%S` | Second | `50` |
| `%%` | Literal `%` | `%` |

The default pattern is `%s_%T_seg%03d`, producing files like:
```
recordings/live_mystream_20260419_221050_seg001.flv
recordings/live_mystream_20260419_221050_seg002.flv
```

If the pattern contains `/`, subdirectories are created automatically.

### How Segments Work

1. **Sequence headers are cached** — Video (SPS/PPS) and audio (AudioSpecificConfig) init data is stored
2. **Duration is tracked** — Each message's RTMP timestamp is compared to the segment start
3. **Keyframe alignment** — When duration exceeds the target, the recorder waits for the next video keyframe
4. **Rotation** — The current segment is finalized (FLV duration patched / MP4 moov written), then a new file is opened
5. **Re-injection** — Cached sequence headers are written into the new segment so decoders can initialize

### Examples

**30-second segments (default pattern):**
```bash
./rtmp-server -record-all true -segment-duration 30s
# Output: recordings/live_mystream_20260419_221050_seg001.flv
```

**5-minute segments in date-organized subdirectories:**
```bash
./rtmp-server -record-all true \
  -segment-duration 5m \
  -segment-pattern "%s/%Y-%m-%D/seg%03d"
# Output: recordings/live_mystream/2026-04-19/seg001.mp4
```

**1-hour segments for archival:**
```bash
./rtmp-server -record-all true \
  -segment-duration 1h \
  -segment-pattern "%s_%Y%m%D_%H%M%S_%04d"
# Output: recordings/live_mystream_20260419_220000_0001.mp4
```

### Container Format

Segmented recording uses the same automatic format selection as single-file recording:

| Video Codec | Segment Format |
|---|---|
| H.264/AVC | `.flv` |
| H.265, AV1, VP9, VP8, VVC | `.mp4` |

All segments in a stream use the same format.

### Limitations

- **No HLS/DASH playlists**: Segments are standalone files. Use FFmpeg or a packaging tool to generate manifests if needed.
- **No automatic cleanup**: Old segments are not deleted. Use an external script or cron job for retention management.
- **No maximum segment count**: Disk space management is the user's responsibility.
- **Segment length variance**: Actual segments may slightly exceed the target duration because rotation waits for the next keyframe.

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

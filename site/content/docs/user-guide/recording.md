---
title: "FLV Recording"
weight: 1
---

# FLV Recording

go-rtmp can record every published stream to FLV files on disk. Recording runs alongside live relay — subscribers see the stream in real-time while the server simultaneously writes to disk.

## Enabling Recording

Add the `-record-all` and `-record-dir` flags:

```bash
./rtmp-server -record-all true -record-dir ./recordings
```

The directory is created automatically if it doesn't exist. By default, `-record-dir` points to `recordings` in the working directory.

## File Naming

Recordings follow this naming pattern:

```
{streamkey}_{YYYYMMDD}_{HHMMSS}.flv
```

Any forward slashes in the stream key are replaced with underscores. For example, publishing to `live/mystream` at 2:30:22 PM on January 15, 2024 produces:

```
recordings/live_mystream_20240115_143022.flv
```

## FLV Format

FLV files contain video (H.264, H.265/HEVC, AV1, VP9) and audio (AAC) in a standard Flash Video container. Enhanced RTMP tags are preserved transparently. The server writes:

- A 13-byte FLV header (signature `FLV`, version 1, audio+video flags)
- FLV tags for each audio (type 0x08) and video (type 0x09) message
- Proper `PreviousTagSize` fields for seeking compatibility

## Verifying Recordings

Inspect a recording with FFprobe:

```bash
ffprobe recordings/live_mystream_20240115_143022.flv
```

Play it back directly:

```bash
ffplay recordings/live_mystream_20240115_143022.flv
```

## Converting to MP4

Since the FLV file already contains the original codec data, you can remux to MP4 without re-encoding:

```bash
ffmpeg -i recordings/live_mystream_20240115_143022.flv -c copy output.mp4
```

This is nearly instantaneous regardless of file size.

## Graceful Degradation

Recording errors **never affect live streaming**. If the recorder encounters a write error (disk full, permission denied, etc.), it disables itself and logs the failure. The publisher and all subscribers continue uninterrupted.

The recorder's `Disabled()` method returns true after a fatal write error, and all subsequent writes are silently ignored.

## Lifecycle

- **Start**: A new FLV file is created when a publisher begins streaming (if `-record-all` is enabled). The FLV header is written immediately.
- **During**: Each audio and video message is written as an FLV tag in real-time.
- **Stop**: The file is closed cleanly when the publisher disconnects or the server shuts down.

## Limitations

| Limitation | Detail |
|------------|--------|
| All-or-nothing | `-record-all` records every stream — there is no per-stream control |
| No file rotation | Each publish session creates one file; there is no time-based or size-based splitting |
| No metadata tag | FLV `onMetaData` script tags are not written |
| Audio + video only | Data messages (AMF) are not recorded |

## Example Session

```bash
# Terminal 1: Start server with recording
./rtmp-server -record-all true -record-dir ./recordings -log-level info

# Terminal 2: Publish a stream
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test

# Terminal 3: Check recordings directory
ls -la recordings/
# live_test_20240115_143022.flv  (grows as stream continues)

# After publisher disconnects, convert to MP4
ffmpeg -i recordings/live_test_20240115_143022.flv -c copy output.mp4
```

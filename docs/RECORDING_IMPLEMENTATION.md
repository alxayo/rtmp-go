# RTMP Stream Recording Implementation

## Overview

This document describes the FLV recording implementation for the RTMP server. Recording allows capturing live streams to disk in FLV format for later playback or archival purposes.

## Implementation Date
October 11, 2025

## Architecture

### Components

1. **Recorder** (`internal/rtmp/media/recorder.go`)
   - Core FLV file writer
   - Writes FLV header and tags
   - Thread-safe with graceful error handling

2. **Command Integration** (`internal/rtmp/server/command_integration.go`)
   - Creates recorder when publishing starts
   - Routes media packets to recorder
   - Manages recorder lifecycle

3. **Server Configuration** (`internal/rtmp/server/server.go`)
   - `RecordAll bool` - Enable/disable recording
   - `RecordDir string` - Directory for FLV files
   - Cleanup on server shutdown

4. **Stream Registry** (`internal/rtmp/server/registry.go`)
   - Stores `*media.Recorder` per stream
   - Thread-safe access to recorder

## Data Flow

```
Publisher (OBS/FFmpeg)
    ↓
Connection.readLoop (internal/rtmp/conn/conn.go)
    ↓
MessageHandler (internal/rtmp/server/command_integration.go)
    ↓
├─→ MediaLogger.ProcessMessage() [Statistics/Logging]
│
└─→ stream.Recorder.WriteMessage() [FLV Recording]
```

## File Naming Convention

Recorded files follow this pattern:
```
{stream_key}_{timestamp}.flv
```

Examples:
- `live_test_20251011_143052.flv`
- `app_stream_20251011_143052.flv`

Notes:
- Forward slashes in stream keys are replaced with underscores
- Timestamp format: `YYYYMMDD_HHMMSS`
- All files written to configured `RecordDir`

## Usage

### Command Line

```powershell
# Enable recording with default directory (./recordings)
.\rtmp-server.exe -record-all true

# Specify custom recording directory
.\rtmp-server.exe -record-all true -record-dir C:\recordings

# With debug logging to see recording events
.\rtmp-server.exe -record-all true -log-level debug
```

### Publishing a Stream

```powershell
# Using FFmpeg
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test

# Using OBS
# Set Server: rtmp://localhost:1935/live
# Set Stream Key: test
```

### Verifying Recordings

```powershell
# List recorded files
ls ./recordings

# Check file metadata
ffprobe recordings\live_test_20251011_143052.flv

# Play back recording
ffplay recordings\live_test_20251011_143052.flv
```

## FLV File Format

### Header (13 bytes)
```
Bytes 0-2:  "FLV" signature
Byte 3:     0x01 (version)
Byte 4:     0x05 (flags: audio + video)
Bytes 5-8:  0x00000009 (header length, big-endian)
Bytes 9-12: 0x00000000 (PreviousTagSize0)
```

### Tag Structure (per audio/video packet)
```
Byte 0:     Tag Type (0x08=audio, 0x09=video)
Bytes 1-3:  Data Size (24-bit big-endian)
Bytes 4-6:  Timestamp lower 24 bits
Byte 7:     Timestamp extended (upper 8 bits)
Bytes 8-10: Stream ID (always 0x000000)
Bytes 11+:  Tag Data (audio/video payload)
Last 4:     PreviousTagSize (11 + data size)
```

## Lifecycle

### Recording Start (OnPublish)
1. Client sends `publish` command
2. `HandlePublish` registers publisher in registry
3. If `RecordAll` enabled:
   - Create recording directory (if needed)
   - Generate unique filename
   - Create `media.NewRecorder()`
   - Store in `stream.Recorder`
4. Log: `"recording started"`

### Recording Active (Media Messages)
1. Audio/Video messages arrive (TypeID 8 or 9)
2. Message handler processes packet:
   - Log stats via `MediaLogger`
   - Write to FLV via `stream.Recorder.WriteMessage()`
3. Recorder writes:
   - FLV tag header (11 bytes)
   - Payload data
   - PreviousTagSize (4 bytes)

### Recording Stop
Triggered by:
- Publisher disconnects
- Server shutdown (`Stop()` method)

Actions:
1. Call `recorder.Close()`
2. Flush and close file handle
3. Set `stream.Recorder = nil`
4. Log: `"recorder closed"`

## Error Handling

### Graceful Degradation
If recorder encounters errors:
- Error logged but NOT propagated
- Live stream continues unaffected
- Recorder disabled (set to nil)
- Future packets not written

Example scenarios:
- Disk full
- Permission denied
- I/O errors

### Directory Creation
- `MkdirAll()` creates parent directories as needed
- Fails if insufficient permissions
- Error logged, recording disabled for that stream

## Thread Safety

### Recorder
- Uses `sync.Mutex` internally
- Safe for single-goroutine use (message handler)
- WriteMessage() is non-blocking

### Stream Registry
- Uses `sync.RWMutex` for stream map access
- Per-stream `mu sync.RWMutex` for recorder field
- Safe concurrent access across connections

## Configuration Options

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-record-all` | bool | false | Enable recording for all streams |
| `-record-dir` | string | "recordings" | Directory for FLV files |
| `-log-level` | string | "info" | Set to "debug" for recording logs |

## Log Messages

### Info Level
```
recording started | stream_key=live/test record_dir=./recordings
recorder initialized | stream_key=live/test file=./recordings/live_test_20251011_143052.flv
recorder closed | stream_key=live/test
```

### Error Level
```
failed to create recorder | error="permission denied" stream_key=live/test
recorder close error | error="write error" stream_key=live/test
```

### Debug Level
```
Media packet | type=audio csid=6 msid=1 timestamp=1000 length=128
Media packet | type=video csid=7 msid=1 timestamp=1033 length=4096
```

## Testing

### Manual Test
```powershell
# Terminal 1: Start server with recording
.\rtmp-server.exe -record-all true -log-level info

# Terminal 2: Publish test stream
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test

# Terminal 3: Verify recording
ffprobe recordings\live_test_*.flv
```

### Automated Test (Future)
Create test in `internal/rtmp/server/recording_test.go`:
- Start server with recording enabled
- Simulate publish command
- Send mock audio/video messages
- Verify FLV file created
- Parse FLV header and tags
- Verify tag count and timestamps

## Performance Considerations

### Disk I/O
- Synchronous writes to file
- No buffering beyond OS page cache
- May impact high-bitrate streams (>10 Mbps)

### Memory
- Minimal overhead (~200 bytes per recorder)
- No message buffering in recorder
- Payload written directly to file

### Recommendations
- Use SSD for recording directory
- Monitor disk space
- Implement rotation/cleanup policy (future enhancement)

## Future Enhancements

### Planned
1. **Per-Stream Recording** - Record specific streams via API/config
2. **Disk Space Monitoring** - Auto-stop on low disk space
3. **File Rotation** - Max file size limits
4. **Metadata Tags** - Write script data tag with stream info
5. **Recording API** - Start/stop recording via admin interface

### Considered
1. **HLS Output** - Generate HLS playlist alongside FLV
2. **MP4 Muxing** - Optionally write MP4 instead of FLV
3. **Cloud Upload** - Automatic upload to S3/Azure/GCS
4. **Thumbnail Generation** - Extract keyframes as thumbnails

## Known Limitations

1. **No Metadata Tag** - FLV files lack onMetaData script tag (playback works but metadata missing)
2. **No Fragmentation** - Single file per stream (no auto-rotation)
3. **No Resume** - Recording cannot resume after error
4. **Filename Collisions** - Possible if same stream published within same second (unlikely)

## References

- **FLV Specification**: Adobe Flash Video File Format Specification v10.1
- **RTMP Specification**: RTMP v1.0 Specification (Section 7 - Audio/Video Messages)
- **Implementation Plan**: `specs/001-rtmp-server-implementation/plan.md`
- **Task T045**: `specs/001-rtmp-server-implementation/tasks.md` (FLV Recorder)

## Code Locations

```
internal/rtmp/media/recorder.go          # Core FLV writer
internal/rtmp/media/recorder_test.go     # Recorder unit tests
internal/rtmp/server/command_integration.go  # Recording integration
internal/rtmp/server/server.go           # Server configuration
internal/rtmp/server/registry.go         # Stream recorder storage
cmd/rtmp-server/flags.go                 # CLI flags
cmd/rtmp-server/main.go                  # Server initialization
```

## Changelog

### 2025-10-11: Initial Implementation
- Added recorder initialization on publish
- Wired media packets to recorder.WriteMessage()
- Added cleanup on server shutdown
- File naming with timestamp
- Directory creation with error handling
- Graceful degradation on write errors

---

**Status**: ✅ Complete and ready for testing
**Version**: 1.0.0
**Author**: Implementation based on Task T045 specification

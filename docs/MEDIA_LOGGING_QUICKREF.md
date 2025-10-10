# Media Logging Quick Reference

## Quick Start

```powershell
# Start server with INFO logging (production)
.\rtmp-server.exe -log-level info

# Start server with DEBUG logging (development)
.\rtmp-server.exe -log-level debug

# Stream to server
ffmpeg -re -i video.mp4 -c copy -f flv rtmp://localhost:1935/live/test
```

## Log Levels at a Glance

| Level   | Shows                                      | Volume    | Use Case        |
|---------|-------------------------------------------|-----------|-----------------|
| `debug` | Every packet + codec + stats              | Very High | Development     |
| `info`  | Codec detection + periodic stats          | Low       | Production      |
| `warn`  | Warnings only                             | Very Low  | Production      |
| `error` | Errors only                               | Very Low  | Production      |

## What You'll See

### Connection Lifecycle
```
INFO Connection accepted (handshake_ms=15)
INFO connection registered (conn_id=c000001)
INFO First media packet received (type=video)
INFO Video codec detected (codec=H264)
INFO Audio codec detected (codec=AAC)
INFO Media statistics (every 30s)
```

### Per-Packet Details (DEBUG only)
```
DEBUG Media packet (type=video, csid=6, timestamp=33, length=15432)
DEBUG Media packet (type=audio, csid=4, timestamp=23, length=256)
```

## Key Fields in Logs

| Field           | Description                                |
|----------------|--------------------------------------------|
| `conn_id`      | Connection identifier (e.g., c000001)      |
| `type`         | Media type: "audio" or "video"             |
| `codec`        | Detected codec: AAC, MP3, H264, H265       |
| `audio_packets`| Total audio packets received               |
| `video_packets`| Total video packets received               |
| `total_bytes`  | Total media bytes received                 |
| `bitrate_kbps` | Current bitrate in kilobits/second         |
| `duration_sec` | Seconds since first packet                 |
| `timestamp`    | RTMP timestamp (milliseconds)              |
| `csid`         | Chunk Stream ID (4=audio, 6=video typical) |
| `msid`         | Message Stream ID                          |

## Common Commands

### View Only Statistics
```powershell
.\rtmp-server.exe -log-level info 2>&1 | Select-String "Media statistics"
```

### View Only Codec Detection
```powershell
.\rtmp-server.exe -log-level info 2>&1 | Select-String "codec detected"
```

### Save Logs to File
```powershell
.\rtmp-server.exe -log-level debug > server.log 2>&1
```

### Count Packets (PowerShell)
```powershell
Get-Content server.log | ConvertFrom-Json | Where-Object { $_.msg -eq "Media packet" } | Measure-Object
```

### Analyze Bitrate Over Time
```powershell
Get-Content server.log | ConvertFrom-Json | Where-Object { $_.msg -eq "Media statistics" } | Select-Object time, bitrate_kbps
```

## Supported Codecs

### Audio
- ✅ **AAC** (Advanced Audio Coding) - Most common
- ✅ **MP3** (MPEG-1 Audio Layer 3)
- ✅ **Speex** (Speech codec)

### Video
- ✅ **H.264 (AVC)** - Most common
- ✅ **H.265 (HEVC)** - High efficiency

## FFmpeg Test Commands

### Video + Audio (Typical)
```powershell
ffmpeg -re -i video.mp4 -c copy -f flv rtmp://localhost:1935/live/test
```

### Test Pattern (No File Needed)
```powershell
ffmpeg -f lavfi -i testsrc=duration=30:size=1280x720:rate=30 -c:v libx264 -f flv rtmp://localhost:1935/live/pattern
```

### Video Only
```powershell
ffmpeg -f lavfi -i testsrc -c:v libx264 -an -f flv rtmp://localhost:1935/live/videoonly
```

### Audio Only
```powershell
ffmpeg -f lavfi -i sine=frequency=1000 -c:a aac -vn -f flv rtmp://localhost:1935/live/audioonly
```

### Webcam (Windows)
```powershell
ffmpeg -f dshow -i video="Integrated Camera" -c:v libx264 -f flv rtmp://localhost:1935/live/webcam
```

## Troubleshooting Quick Fixes

| Problem                      | Solution                                    |
|------------------------------|---------------------------------------------|
| No logs appear               | Check FFmpeg is running, try `-log-level debug` |
| Codec not detected           | Verify codec is supported (AAC/H264)        |
| Too many logs                | Switch to `-log-level info`                 |
| Bitrate shows 0              | Wait for at least one media packet          |
| Stats not appearing          | Wait 30 seconds after first packet          |

## Statistics Calculation

```
bitrate_kbps = (total_bytes * 8) / duration_sec / 1000
duration_sec = time_since_first_packet
```

## Example Session Output

```json
{"time":"2025-10-10T17:05:17Z","level":"INFO","msg":"RTMP server listening","addr":"[::]:1935"}
{"time":"2025-10-10T17:05:18Z","level":"INFO","msg":"Connection accepted","conn_id":"c000001","handshake_ms":15}
{"time":"2025-10-10T17:05:18Z","level":"INFO","msg":"First media packet received","conn_id":"c000001","type":"video","timestamp":0}
{"time":"2025-10-10T17:05:18Z","level":"INFO","msg":"Video codec detected","conn_id":"c000001","codec":"H264","frame_type":"keyframe"}
{"time":"2025-10-10T17:05:18Z","level":"INFO","msg":"Audio codec detected","conn_id":"c000001","codec":"AAC","packet_type":"sequence_header"}
{"time":"2025-10-10T17:05:48Z","level":"INFO","msg":"Media statistics","conn_id":"c000001","audio_packets":1250,"video_packets":900,"total_bytes":5242880,"bitrate_kbps":1398,"duration_sec":30}
```

## Files to Review

- **Implementation**: `internal/rtmp/server/media_logger.go`
- **Integration**: `internal/rtmp/server/command_integration.go`
- **Tests**: `internal/rtmp/server/media_logger_test.go`
- **Full Docs**: `docs/media_packet_logging.md`
- **Testing Guide**: `docs/testing_media_logging.md`

## Need Help?

1. Check full documentation: `docs/media_packet_logging.md`
2. Review troubleshooting: `docs/testing_media_logging.md`
3. Enable debug logging: `.\rtmp-server.exe -log-level debug`
4. Check FFmpeg output for connection errors

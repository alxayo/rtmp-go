# RTMP Recording Quick Reference

## Enable Recording

```powershell
.\rtmp-server.exe -record-all true -record-dir ./recordings -log-level info
```

## Publish & Record

```powershell
# FFmpeg
ffmpeg -re -i video.mp4 -c copy -f flv rtmp://localhost:1935/live/mystream

# Recorded file: ./recordings/live_mystream_20251011_143052.flv
```

## Verify Recording

```powershell
# List files
ls ./recordings

# Check metadata
ffprobe recordings\live_mystream_20251011_143052.flv

# Playback
ffplay recordings\live_mystream_20251011_143052.flv
```

## Log Messages

### Recording Started
```
INFO recording started | stream_key=live/mystream record_dir=./recordings
INFO recorder initialized | file=./recordings/live_mystream_20251011_143052.flv
```

### Active Recording
```
DEBUG Media packet | type=audio timestamp=1000 length=128
DEBUG Media packet | type=video timestamp=1033 length=4096
```

### Recording Stopped
```
INFO recorder closed | stream_key=live/mystream
```

## File Format

```
{stream_key}_{timestamp}.flv

Examples:
  live_test_20251011_143052.flv
  app_stream_20251011_150230.flv
```

## Troubleshooting

### Recording Not Working
1. Check flag: `-record-all true`
2. Verify directory permissions
3. Check disk space
4. Enable debug: `-log-level debug`

### File Not Found
- Check `record-dir` path
- Default: `./recordings`
- Use absolute path if needed

### Disk Full
- Recording stops gracefully
- Stream continues live
- Check logs for errors

## Options

| Flag | Default | Description |
|------|---------|-------------|
| `-record-all` | false | Enable recording |
| `-record-dir` | "recordings" | Output directory |
| `-log-level` | "info" | Logging level |

## Complete Example

```powershell
# Terminal 1: Start server
.\rtmp-server.exe -listen localhost:1935 -record-all true -record-dir C:\recordings -log-level info

# Terminal 2: Publish stream
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test

# Terminal 3: Monitor recordings
ls C:\recordings
ffprobe C:\recordings\live_test_*.flv

# Terminal 4: Playback
ffplay C:\recordings\live_test_20251011_143052.flv
```

## See Also

- Full documentation: `docs/RECORDING_IMPLEMENTATION.md`
- Media logging: `docs/MEDIA_LOGGING_QUICKREF.md`

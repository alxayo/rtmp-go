# Testing Media Packet Logging

This guide demonstrates how to test the media packet logging feature.

## Quick Test

### 1. Start the Server with Debug Logging

```powershell
# From the go-rtmp directory
.\rtmp-server.exe -log-level debug -listen :1935
```

Expected output:
```
{"time":"2025-10-10T17:05:17Z","level":"INFO","msg":"RTMP server listening","component":"rtmp_server","addr":"[::]:1935"}
{"time":"2025-10-10T17:05:17Z","level":"INFO","msg":"server started","component":"cli","addr":"[::]:1935"}
```

### 2. Stream Video to the Server

In another terminal, use FFmpeg to stream a test video:

```powershell
# Use a test video file
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test

# Or use webcam (Windows)
ffmpeg -f dshow -i video="Integrated Camera" -c:v libx264 -f flv rtmp://localhost:1935/live/webcam

# Or generate a test pattern
ffmpeg -f lavfi -i testsrc=duration=30:size=1280x720:rate=30 -c:v libx264 -f flv rtmp://localhost:1935/live/pattern
```

### 3. Expected Log Output

#### Connection and Handshake
```json
{"time":"2025-10-10T17:05:18Z","level":"INFO","msg":"Connection accepted","conn_id":"c000001","handshake_ms":15}
{"time":"2025-10-10T17:05:18Z","level":"INFO","msg":"connection registered","component":"rtmp_server","conn_id":"c000001"}
```

#### First Media Packets
```json
{"time":"2025-10-10T17:05:18.100Z","level":"INFO","msg":"First media packet received","component":"media_logger","conn_id":"c000001","type":"video","timestamp":0}
{"time":"2025-10-10T17:05:18.101Z","level":"INFO","msg":"Video codec detected","component":"media_logger","conn_id":"c000001","codec":"H264","frame_type":"keyframe","packet_type":"sequence_header"}
{"time":"2025-10-10T17:05:18.120Z","level":"INFO","msg":"First media packet received","component":"media_logger","conn_id":"c000001","type":"audio","timestamp":0}
{"time":"2025-10-10T17:05:18.121Z","level":"INFO","msg":"Audio codec detected","component":"media_logger","conn_id":"c000001","codec":"AAC","packet_type":"sequence_header"}
```

#### Debug-Level Packet Logs (with -log-level debug)
```json
{"time":"2025-10-10T17:05:18.150Z","level":"DEBUG","msg":"Media packet","component":"media_logger","conn_id":"c000001","type":"video","csid":6,"msid":1,"timestamp":33,"length":15432,"payload_size":15432}
{"time":"2025-10-10T17:05:18.160Z","level":"DEBUG","msg":"Media packet","component":"media_logger","conn_id":"c000001","type":"audio","csid":4,"msid":1,"timestamp":23,"length":256,"payload_size":256}
```

#### Periodic Statistics (every 30 seconds)
```json
{"time":"2025-10-10T17:05:48Z","level":"INFO","msg":"Media statistics","component":"media_logger","conn_id":"c000001","audio_packets":1250,"video_packets":900,"total_bytes":5242880,"bitrate_kbps":1398,"audio_codec":"AAC","video_codec":"H264","duration_sec":30}
```

## Verification Checklist

Use this checklist to verify the media logging feature is working correctly:

- [ ] Server starts and listens on port 1935
- [ ] FFmpeg successfully connects and publishes stream
- [ ] "First media packet received" log appears for video
- [ ] "Video codec detected" log shows correct codec (H264)
- [ ] "First media packet received" log appears for audio  
- [ ] "Audio codec detected" log shows correct codec (AAC)
- [ ] With debug logging, individual packet logs appear
- [ ] Statistics log appears after 30 seconds
- [ ] Statistics show increasing packet counts
- [ ] Bitrate calculation is reasonable (>0 kbps)
- [ ] On stream stop, final statistics are logged

## Log Level Comparison

### INFO Level (Production)
```powershell
.\rtmp-server.exe -log-level info
```

**Pros:**
- Clean, readable output
- Shows key events (codec detection, statistics)
- Low overhead
- Suitable for production

**Output volume:** ~10 lines per connection (startup + periodic stats)

### DEBUG Level (Development)
```powershell
.\rtmp-server.exe -log-level debug
```

**Pros:**
- Shows every media packet
- Detailed protocol information
- Useful for troubleshooting
- Helps understand packet flow

**Output volume:** Hundreds to thousands of lines per second (depending on bitrate)

**Cons:**
- Can overwhelm console
- Higher CPU/disk usage
- Not suitable for production

## Common Test Scenarios

### Test 1: Video Only Stream
```powershell
ffmpeg -f lavfi -i testsrc=duration=10:size=640x480:rate=30 -c:v libx264 -an -f flv rtmp://localhost:1935/live/videoonly
```

Expected: Only video codec detected, audio_packets=0

### Test 2: Audio Only Stream
```powershell
ffmpeg -f lavfi -i "sine=frequency=1000:duration=10" -c:a aac -vn -f flv rtmp://localhost:1935/live/audioonly
```

Expected: Only audio codec detected, video_packets=0

### Test 3: High Bitrate Stream
```powershell
ffmpeg -f lavfi -i testsrc=duration=10:size=1920x1080:rate=60 -c:v libx264 -b:v 5M -f flv rtmp://localhost:1935/live/highbitrate
```

Expected: Higher bitrate_kbps value in statistics

### Test 4: Multiple Connections
```powershell
# Terminal 1
ffmpeg -f lavfi -i testsrc -c:v libx264 -f flv rtmp://localhost:1935/live/stream1

# Terminal 2
ffmpeg -f lavfi -i testsrc -c:v libx264 -f flv rtmp://localhost:1935/live/stream2
```

Expected: Separate logs for each conn_id (c000001, c000002)

## Filtering Logs

### Show Only Media Statistics
```powershell
.\rtmp-server.exe -log-level info 2>&1 | Select-String "Media statistics"
```

### Show Only Codec Detection
```powershell
.\rtmp-server.exe -log-level info 2>&1 | Select-String "codec detected"
```

### Count Media Packets (Debug Level)
```powershell
.\rtmp-server.exe -log-level debug 2>&1 | Select-String "Media packet" | Measure-Object
```

### Export to JSON File
```powershell
.\rtmp-server.exe -log-level debug 2> media-packets.json
```

Then analyze with PowerShell:
```powershell
Get-Content media-packets.json | ConvertFrom-Json | Where-Object { $_.msg -eq "Media packet" } | Group-Object type | Select-Object Name, Count
```

## Performance Testing

### Measure Logging Overhead

**Without debug logging:**
```powershell
Measure-Command { .\rtmp-server.exe -log-level info }
```

**With debug logging:**
```powershell
Measure-Command { .\rtmp-server.exe -log-level debug }
```

**With debug logging to file:**
```powershell
Measure-Command { .\rtmp-server.exe -log-level debug > debug.log 2>&1 }
```

### Stress Test
```powershell
# Stream for 5 minutes and analyze stats
.\rtmp-server.exe -log-level info > stats.log 2>&1

# In another terminal
ffmpeg -re -i test.mp4 -stream_loop -1 -c copy -f flv rtmp://localhost:1935/live/stress

# Wait 5 minutes, then analyze
Get-Content stats.log | ConvertFrom-Json | Where-Object { $_.msg -eq "Media statistics" } | Format-Table time, audio_packets, video_packets, bitrate_kbps
```

## Troubleshooting

### Problem: No logs appear

**Check 1:** Is the server running?
```powershell
Get-Process rtmp-server
```

**Check 2:** Is FFmpeg connecting?
Look for FFmpeg output showing:
```
Stream #0:0: Video: h264 ...
Stream #0:1: Audio: aac ...
```

**Check 3:** Is the stream key correct?
Default is `live/test`, verify FFmpeg URL matches

### Problem: Codec not detected

**Check 1:** Is the stream using a supported codec?
Supported: AAC, MP3, H.264, H.265

**Check 2:** Is the first packet complete?
Enable debug logging and check payload_size > 0

**Check 3:** Is it a raw stream without codec headers?
Some streams need `-c:v libx264` instead of `-c copy`

### Problem: Log output is corrupted

**Solution:** Redirect stderr separately:
```powershell
.\rtmp-server.exe -log-level debug 1> stdout.log 2> stderr.log
```

## Automated Testing

Create a test script (`test-media-logging.ps1`):

```powershell
# Start server in background
Start-Process -FilePath ".\rtmp-server.exe" -ArgumentList "-log-level debug" -RedirectStandardOutput "test-output.log" -RedirectStandardError "test-errors.log"

# Wait for server to start
Start-Sleep -Seconds 2

# Stream test video for 10 seconds
ffmpeg -f lavfi -i testsrc=duration=10:size=640x480:rate=30 -f lavfi -i sine=frequency=1000:duration=10 -c:v libx264 -c:a aac -f flv rtmp://localhost:1935/live/test

# Wait for processing
Start-Sleep -Seconds 2

# Check logs
$logs = Get-Content test-output.log | ConvertFrom-Json

$codecDetected = $logs | Where-Object { $_.msg -like "*codec detected*" }
Write-Host "✓ Codec detection logs: $($codecDetected.Count)"

$mediaStats = $logs | Where-Object { $_.msg -eq "Media statistics" }
Write-Host "✓ Statistics logs: $($mediaStats.Count)"

# Stop server
Stop-Process -Name rtmp-server
```

Run the test:
```powershell
.\test-media-logging.ps1
```

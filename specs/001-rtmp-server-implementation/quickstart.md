# Quickstart: RTMP Server Implementation

**Feature**: 001-rtmp-server-implementation  
**Date**: 2025-10-01  
**Purpose**: End-to-end validation scenario for FFmpeg publish + ffplay playback

---

## Prerequisites

### Required Software

1. **Go 1.21+**
   ```powershell
   go version  # Verify Go installation
   ```

2. **FFmpeg with RTMP support**
   ```powershell
   ffmpeg -version  # Verify FFmpeg installation
   ffplay -version  # Verify ffplay installation
   ```

3. **Sample Media File**
   - Use any H.264/AAC encoded video file (e.g., test.mp4)
   - Recommended: 10-30 seconds, 1280x720 or lower resolution
   - Generate test file if needed:
     ```powershell
     ffmpeg -f lavfi -i testsrc=duration=10:size=1280x720:rate=30 `
            -f lavfi -i sine=frequency=1000:duration=10 `
            -c:v libx264 -preset fast -c:a aac -b:a 128k test.mp4
     ```

---

## Build Instructions

### Step 1: Clone Repository (if not already done)

```powershell
cd c:\code\alxayo
git clone <repo-url> go-rtmp
cd go-rtmp
git checkout 001-rtmp-server-implementation
```

### Step 2: Build Server

```powershell
# From repository root
go build -o bin\rtmp-server.exe .\cmd\rtmp-server

# Verify build
.\bin\rtmp-server.exe -version
```

**Expected Output**:
```
go-rtmp server version 0.1.0
```

### Step 3: Build Client (Optional)

```powershell
go build -o bin\rtmp-client.exe .\cmd\rtmp-client
```

---

## Quickstart Scenario: FFmpeg Publish + ffplay Playback

### Terminal 1: Start RTMP Server

```powershell
# Start server with info-level logging
.\bin\rtmp-server.exe -listen :1935 -log-level info

# Alternative: Debug mode for protocol details
.\bin\rtmp-server.exe -listen :1935 -log-level debug
```

**Expected Output**:
```
{"time":"2025-10-01T12:00:00Z","level":"INFO","msg":"RTMP server starting","listen":":1935"}
{"time":"2025-10-01T12:00:00Z","level":"INFO","msg":"Server listening","addr":"[::]:1935"}
```

**Server Flags**:
- `-listen`: TCP listen address (default ":1935")
- `-log-level`: Logging level (debug/info/warn/error, default "info")
- `-record-all`: Enable recording for all streams (default false)
- `-record-dir`: Recording output directory (default "recordings")
- `-chunk-size`: Default send chunk size (default 4096)

### Terminal 2: Publish Stream with FFmpeg

```powershell
# Publish test.mp4 to stream key "live/test"
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test
```

**FFmpeg Flags**:
- `-re`: Read input at native frame rate (real-time simulation)
- `-i test.mp4`: Input file
- `-c copy`: Copy codecs without re-encoding (fast)
- `-f flv`: Force FLV container format
- `rtmp://localhost:1935/live/test`: RTMP URL (app=live, streamname=test)

**Expected FFmpeg Output**:
```
Input #0, mov,mp4,m4a,3gp,3g2,mj2, from 'test.mp4':
  Duration: 00:00:10.00, start: 0.000000, bitrate: 1234 kb/s
  Stream #0:0: Video: h264 (High), yuv420p, 1280x720, 30 fps
  Stream #0:1: Audio: aac (LC), 48000 Hz, stereo, 128 kb/s
Output #0, flv, to 'rtmp://localhost:1935/live/test':
  Stream #0:0: Video: h264, 1280x720, 30 fps
  Stream #0:1: Audio: aac, 48000 Hz, stereo, 128 kb/s
frame=  300 fps= 30 q=-1.0 Lsize=    1234kB time=00:00:10.00 bitrate=1234.0kbits/s speed=1.00x
```

**Expected Server Logs** (Terminal 1):
```
{"time":"2025-10-01T12:00:01Z","level":"INFO","msg":"Connection accepted","conn_id":"c1a2b3c4","peer_addr":"127.0.0.1:54321"}
{"time":"2025-10-01T12:00:01Z","level":"INFO","msg":"Handshake completed","conn_id":"c1a2b3c4","duration_ms":5}
{"time":"2025-10-01T12:00:01Z","level":"INFO","msg":"connect command","conn_id":"c1a2b3c4","app":"live","tcUrl":"rtmp://localhost:1935/live"}
{"time":"2025-10-01T12:00:01Z","level":"INFO","msg":"createStream","conn_id":"c1a2b3c4","stream_id":1}
{"time":"2025-10-01T12:00:01Z","level":"INFO","msg":"publish command","conn_id":"c1a2b3c4","stream_key":"live/test","type":"live"}
{"time":"2025-10-01T12:00:02Z","level":"INFO","msg":"Codec detected","stream_key":"live/test","video":"H.264 AVC","audio":"AAC"}
```

### Terminal 3: Play Stream with ffplay

```powershell
# Play stream from server
ffplay rtmp://localhost:1935/live/test
```

**Expected ffplay Behavior**:
- Video playback window opens within 3-5 seconds
- Video plays smoothly (30 fps)
- Audio synchronized with video
- No buffering warnings or errors

**Expected Server Logs** (Terminal 1):
```
{"time":"2025-10-01T12:00:03Z","level":"INFO","msg":"Connection accepted","conn_id":"p2b3c4d5","peer_addr":"127.0.0.1:54322"}
{"time":"2025-10-01T12:00:03Z","level":"INFO","msg":"Handshake completed","conn_id":"p2b3c4d5","duration_ms":4}
{"time":"2025-10-01T12:00:03Z","level":"INFO","msg":"connect command","conn_id":"p2b3c4d5","app":"live"}
{"time":"2025-10-01T12:00:03Z","level":"INFO","msg":"createStream","conn_id":"p2b3c4d5","stream_id":1}
{"time":"2025-10-01T12:00:03Z","level":"INFO","msg":"play command","conn_id":"p2b3c4d5","stream_key":"live/test"}
{"time":"2025-10-01T12:00:03Z","level":"INFO","msg":"Subscriber added","stream_key":"live/test","conn_id":"p2b3c4d5","total_subscribers":1}
```

### Step 4: Verify Playback

**Validation Checklist**:
- [ ] Video displays in ffplay window
- [ ] Audio is audible and synchronized
- [ ] No buffering or stuttering
- [ ] Latency is 3-5 seconds (acceptable for target)
- [ ] Server logs show codec detection (H.264 AVC, AAC)

### Step 5: Test Graceful Disconnect

**Action**: Press `Ctrl+C` in FFmpeg terminal (Terminal 2)

**Expected Server Logs**:
```
{"time":"2025-10-01T12:00:15Z","level":"INFO","msg":"Publisher disconnected","conn_id":"c1a2b3c4","stream_key":"live/test"}
{"time":"2025-10-01T12:00:15Z","level":"INFO","msg":"Stream ended","stream_key":"live/test","duration_sec":12}
{"time":"2025-10-01T12:00:15Z","level":"INFO","msg":"Connection closed","conn_id":"c1a2b3c4","bytes_received":1234567,"bytes_sent":98765}
```

**Expected ffplay Behavior**:
- Playback stops (stream EOF)
- ffplay closes or displays "End of file" message

**Action**: Press `q` or close ffplay window (Terminal 3)

**Expected Server Logs**:
```
{"time":"2025-10-01T12:00:16Z","level":"INFO","msg":"Subscriber disconnected","conn_id":"p2b3c4d5","stream_key":"live/test"}
{"time":"2025-10-01T12:00:16Z","level":"INFO","msg":"Connection closed","conn_id":"p2b3c4d5","bytes_received":5678,"bytes_sent":1234567}
```

---

## Optional: Test Recording

### Enable Recording

**Restart server with recording enabled**:
```powershell
.\bin\rtmp-server.exe -listen :1935 -record-all -record-dir recordings
```

### Publish and Verify Recording

```powershell
# Terminal 2: Publish
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test

# Wait 10 seconds, then Ctrl+C to stop
```

**Expected Server Logs**:
```
{"time":"2025-10-01T12:05:00Z","level":"INFO","msg":"Recording started","stream_key":"live/test","file":"recordings\\live_test_20251001_120500.flv"}
...
{"time":"2025-10-01T12:05:10Z","level":"INFO","msg":"Recording stopped","stream_key":"live/test","duration_sec":10,"bytes_written":1234567,"video_frames":300,"audio_frames":480}
```

### Play Recorded File

```powershell
ffplay recordings\live_test_20251001_120500.flv
```

**Expected**: Video plays from recorded FLV file (same as live playback).

---

## Validation Criteria

### Success Criteria (FR-001 through FR-054)

| ID | Criterion | Validation Method |
|----|-----------|-------------------|
| ✅ | Handshake completes successfully | Server logs "Handshake completed" |
| ✅ | FFmpeg publishes without errors | No errors in FFmpeg output |
| ✅ | ffplay receives and plays stream | Video visible in ffplay window |
| ✅ | Latency within 3-5 seconds | Timestamp comparison (publisher vs player) |
| ✅ | Codec detection works | Server logs "Codec detected: H.264 AVC, AAC" |
| ✅ | Graceful disconnect | No server crashes, clean connection close logs |
| ✅ | Recording works (optional) | FLV file playable with ffplay |
| ✅ | No memory leaks | Run for 10 minutes, monitor memory (should be stable) |

### Performance Validation

**Memory Usage**:
```powershell
# Monitor server memory (PowerShell)
while ($true) {
    Get-Process rtmp-server | Select-Object WorkingSet64
    Start-Sleep -Seconds 5
}
```

**Expected**: WorkingSet64 (RSS) stable after initial ramp-up (<50MB for 1 publisher + 1 player).

**CPU Usage**:
```powershell
Get-Process rtmp-server | Select-Object CPU
```

**Expected**: <10% CPU on modern hardware for 1 stream.

---

## Troubleshooting

### Issue: "Connection refused"

**Symptom**: FFmpeg error `Connection to tcp://localhost:1935 failed`  
**Cause**: Server not running or listening on wrong port  
**Solution**:
1. Verify server is running: `netstat -an | findstr 1935`
2. Check server logs for "Server listening" message
3. Ensure no firewall blocking port 1935

### Issue: "Handshake timeout"

**Symptom**: Server logs "Handshake timeout reading C1"  
**Cause**: Network issue or client sending incomplete data  
**Solution**:
1. Check network connectivity (localhost should work)
2. Verify FFmpeg version supports RTMP
3. Try different RTMP URL format

### Issue: "Codec not detected"

**Symptom**: Server logs missing "Codec detected" message  
**Cause**: Input file not H.264/AAC or metadata missing  
**Solution**:
1. Verify input file codecs: `ffmpeg -i test.mp4`
2. Re-encode if needed: `ffmpeg -i input.mp4 -c:v libx264 -c:a aac output.mp4`

### Issue: "Playback stutters or buffers"

**Symptom**: ffplay video freezes or shows buffering warnings  
**Cause**: Network congestion, slow publisher, or player buffer too small  
**Solution**:
1. Reduce input bitrate: `ffmpeg -re -i test.mp4 -b:v 1000k -b:a 128k -f flv rtmp://...`
2. Increase ffplay buffer: `ffplay -fflags nobuffer rtmp://...` (reduces latency but may stutter)
3. Check server logs for slow consumer warnings

### Issue: "Server crashes or panics"

**Symptom**: Server process terminates unexpectedly  
**Cause**: Unhandled error, malformed input, or protocol violation  
**Solution**:
1. Run server in debug mode: `-log-level debug`
2. Check for panic stack trace in logs
3. Report issue with logs and reproduction steps

---

## Multi-Client Testing (Optional)

### Test Concurrent Publishers (Different Streams)

**Terminal 2**: Publish to `live/stream1`
```powershell
ffmpeg -re -i test1.mp4 -c copy -f flv rtmp://localhost:1935/live/stream1
```

**Terminal 3**: Publish to `live/stream2`
```powershell
ffmpeg -re -i test2.mp4 -c copy -f flv rtmp://localhost:1935/live/stream2
```

**Terminal 4**: Play `live/stream1`
```powershell
ffplay rtmp://localhost:1935/live/stream1
```

**Terminal 5**: Play `live/stream2`
```powershell
ffplay rtmp://localhost:1935/live/stream2
```

**Expected**: Both streams play independently without interference.

### Test Multiple Players (Same Stream)

**Terminal 2**: Publish to `live/test`
```powershell
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test
```

**Terminal 3, 4, 5**: Play `live/test`
```powershell
ffplay rtmp://localhost:1935/live/test
```

**Expected**: All 3 players receive and play the stream simultaneously.

---

## Next Steps

After completing the quickstart scenario:

1. **Run Unit Tests**:
   ```powershell
   go test ./...
   ```

2. **Run Integration Tests**:
   ```powershell
   go test -tags=integration ./tests/integration/...
   ```

3. **Run Fuzz Tests** (for parsers):
   ```powershell
   go test -fuzz=FuzzChunkHeader ./internal/rtmp/chunk/
   ```

4. **Profile Performance**:
   ```powershell
   go test -bench=. -cpuprofile=cpu.prof ./internal/rtmp/chunk/
   go tool pprof cpu.prof
   ```

5. **Generate Coverage Report**:
   ```powershell
   go test -coverprofile=coverage.out ./...
   go tool cover -html=coverage.out
   ```

---

## References

- FFmpeg RTMP Documentation: https://trac.ffmpeg.org/wiki/StreamingGuide
- OBS Studio RTMP Setup: https://obsproject.com/kb/rtmp-streaming-guide
- Adobe RTMP Specification: https://rtmp.veriskope.com/docs/spec/

---

**Status**: Quickstart scenario complete. Ready for implementation validation and interoperability testing.

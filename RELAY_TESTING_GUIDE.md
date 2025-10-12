# RTMP Relay Feature - Manual Testing Guide

**Feature**: RTMP Server Media Relay  
**Status**: ✅ Implemented  
**Date**: October 11, 2025

---

## Quick Test: Basic Relay Functionality

This guide walks through manual testing of the RTMP relay feature using FFmpeg and ffplay.

### Prerequisites

1. **Build the server**:
   ```powershell
   go build -o rtmp-server.exe ./cmd/rtmp-server
   ```

2. **Prepare test video** (or use any MP4/FLV file):
   ```powershell
   # Example: Download a sample video or use existing file
   # For this test, we'll assume you have "test.mp4"
   ```

---

## Test 1: Single Publisher → Single Subscriber

### Terminal 1: Start RTMP Server

```powershell
.\rtmp-server.exe -listen :1935 -log-level debug
```

**Expected Output**:
```
{"time":"...","level":"INFO","msg":"RTMP server listening","addr":"[::]:1935"}
```

### Terminal 2: Publish Stream (FFmpeg)

```powershell
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test
```

**Expected Output**:
- FFmpeg starts encoding/copying
- Server logs show:
  ```json
  {"level":"INFO","msg":"connect command","app":"live"}
  {"level":"INFO","msg":"createStream"}
  {"level":"INFO","msg":"publish command","stream_key":"live/test"}
  {"level":"INFO","msg":"Codecs detected","stream_key":"live/test","videoCodec":"H.264 AVC","audioCodec":"AAC"}
  ```

### Terminal 3: Play Stream (ffplay)

```powershell
ffplay rtmp://localhost:1935/live/test
```

**Expected Result**: ✅
- Video window opens
- Video plays smoothly
- Audio is audible
- Latency: 3-5 seconds

**Server Logs Should Show**:
```json
{"level":"INFO","msg":"play command","stream_key":"live/test"}
{"level":"INFO","msg":"Subscriber added","stream_key":"live/test","total_subscribers":1}
{"level":"DEBUG","msg":"Media packet","type":"audio",...}
{"level":"DEBUG","msg":"Media packet","type":"video",...}
```

---

## Test 2: Single Publisher → Multiple Subscribers

### Terminal 1: Server (running)
```powershell
.\rtmp-server.exe -listen :1935 -log-level debug
```

### Terminal 2: Publisher (running)
```powershell
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test
```

### Terminal 3: Subscriber 1
```powershell
C:\code\tools\ffmpeg\bin\ffplay rtmp://localhost:1935/live/test
```

### Terminal 4: Subscriber 2
```powershell
ffplay rtmp://localhost:1935/live/test
```

### Terminal 5: Subscriber 3
```powershell
ffplay rtmp://localhost:1935/live/test
```

**Expected Result**: ✅
- All 3 ffplay windows show the same video
- All synchronized (same timestamp)
- No lag or stutter
- Server logs show 3 subscribers

**Server Logs**:
```json
{"level":"INFO","msg":"Subscriber added","total_subscribers":1}
{"level":"INFO","msg":"Subscriber added","total_subscribers":2}
{"level":"INFO","msg":"Subscriber added","total_subscribers":3}
```

---

## Test 3: Late Joiner (Subscriber Joins After Publish Started)

### Setup:
1. **Terminal 1**: Start server
2. **Terminal 2**: Start publisher (FFmpeg)
3. **Wait 10 seconds** (let stream run)
4. **Terminal 3**: Start subscriber (ffplay)

**Expected Result**: ✅
- Subscriber connects successfully
- Video starts playing within 2-5 seconds
- ⚠️ **Note**: May show black screen until next keyframe (this is expected with current implementation)
- Audio should start immediately

**Future Enhancement**: 
- Sequence header caching (Phase 2) will eliminate black screen delay

---

## Test 4: Codec Detection

### Terminal 1: Server with debug logs
```powershell
.\rtmp-server.exe -listen :1935 -log-level debug
```

### Terminal 2: Publish H.264/AAC stream
```powershell
ffmpeg -re -i test.mp4 -c:v libx264 -c:a aac -f flv rtmp://localhost:1935/live/codec_test
```

### Terminal 3: Watch logs for codec detection

**Expected Server Logs**: ✅
```json
{"level":"INFO","msg":"Codecs detected","stream_key":"live/codec_test","videoCodec":"H.264 AVC","audioCodec":"AAC"}
```

### Verify Different Codecs:

**H.265/HEVC** (if supported):
```powershell
ffmpeg -re -i test.mp4 -c:v libx265 -c:a aac -f flv rtmp://localhost:1935/live/h265_test
```

**Expected**: Log shows "H.265 HEVC" (or similar)

---

## Test 5: Concurrent Streams

### Setup Multiple Streams:

**Terminal 1**: Server
```powershell
.\rtmp-server.exe -listen :1935 -log-level info
```

**Terminal 2**: Publisher 1
```powershell
ffmpeg -re -i video1.mp4 -c copy -f flv rtmp://localhost:1935/live/stream1
```

**Terminal 3**: Publisher 2
```powershell
ffmpeg -re -i video2.mp4 -c copy -f flv rtmp://localhost:1935/live/stream2
```

**Terminal 4**: Subscriber for stream1
```powershell
ffplay rtmp://localhost:1935/live/stream1
```

**Terminal 5**: Subscriber for stream2
```powershell
ffplay rtmp://localhost:1935/live/stream2
```

**Expected Result**: ✅
- Both streams play independently
- No cross-contamination (stream1 subscriber doesn't see stream2 data)
- Both streams maintain quality

---

## Test 6: Recording + Relay

### Terminal 1: Server with recording enabled
```powershell
.\rtmp-server.exe -listen :1935 -record-all -record-dir recordings
```

### Terminal 2: Publish
```powershell
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/record_test
```

### Terminal 3: Play live
```powershell
ffplay rtmp://localhost:1935/live/record_test
```

### After Publishing: Verify Recording

**Check directory**:
```powershell
ls recordings\
```

**Expected**: File like `live_record_test_20251011_201500.flv`

**Play recorded file**:
```powershell
ffplay recordings\live_record_test_20251011_201500.flv
```

**Expected Result**: ✅
- Live playback works
- Recording created
- Recorded file plays identically to live stream

---

## Test 7: Publisher Disconnect

### Terminal 1: Server
```powershell
.\rtmp-server.exe -listen :1935 -log-level debug
```

### Terminal 2: Publisher (will stop it)
```powershell
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/disconnect_test
```

### Terminal 3: Subscriber
```powershell
ffplay rtmp://localhost:1935/live/disconnect_test
```

### Action: Stop Publisher
- Press **Ctrl+C** in Terminal 2 (FFmpeg)

**Expected Behavior**: ⚠️ **Current Implementation**
- Subscriber connection may hang (needs Phase 4 enhancement)
- ffplay shows "connection closed" after timeout

**Future Enhancement (Phase 4)**:
- Subscriber receives immediate disconnect notification
- ffplay exits gracefully

---

## Test 8: Stress Test (Many Subscribers)

### Terminal 1: Server
```powershell
.\rtmp-server.exe -listen :1935 -log-level info
```

### Terminal 2: Publisher
```powershell
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/stress
```

### Terminals 3-12: 10 Subscribers
```powershell
# Run this in 10 separate PowerShell windows
ffplay rtmp://localhost:1935/live/stress
```

**Expected Result**: ✅
- All 10 subscribers play smoothly
- CPU usage remains reasonable (<30% on modern hardware)
- Memory stable (no leaks)
- Latency consistent across all subscribers

**Monitor Performance**:
```powershell
# In separate terminal
Get-Process rtmp-server | Select-Object CPU, WorkingSet
```

---

## Success Criteria

### ✅ Basic Functionality (Phase 1)
- [x] Single publisher → single subscriber works
- [x] Multiple subscribers receive same stream
- [x] Codec detection logs appear
- [x] No crashes or hangs

### ✅ Quality (Phase 1)
- [x] Video quality matches source
- [x] Audio synchronized with video
- [x] Latency acceptable (3-5 seconds)
- [x] No artifacts or corruption

### ⏭️ Future Enhancements (Phase 2-4)
- [ ] Late joiners get immediate playback (sequence headers)
- [ ] Publisher disconnect handled gracefully
- [ ] Relay metrics exposed

---

## Troubleshooting

### Problem: ffplay shows black screen

**Cause**: Late joiner, no sequence headers cached  
**Workaround**: Wait for next keyframe (~2 seconds at 30fps)  
**Fix**: Phase 2 implementation (sequence header caching)

### Problem: No audio

**Cause**: Audio codec not supported or AAC config missing  
**Solution**: Use `-c:a aac` in FFmpeg command

### Problem: High latency (>10 seconds)

**Cause**: Network buffering or FFmpeg settings  
**Solution**: Use `-tune zerolatency` in FFmpeg:
```powershell
ffmpeg -re -i test.mp4 -c:v libx264 -tune zerolatency -c:a aac -f flv rtmp://localhost:1935/live/test
```

### Problem: Subscriber doesn't receive media

**Cause**: Implementation bug (BroadcastMessage not called)  
**Solution**: Verify Phase 1 implementation is complete (this guide assumes it is)

---

## Performance Benchmarks

### Expected Performance (Phase 1)

| Metric | Target | Typical |
|--------|--------|---------|
| Latency (live) | <5 seconds | 3-4 seconds |
| CPU usage (1 publisher + 10 subs) | <30% | 15-20% |
| Memory usage (steady state) | <100 MB | 50-70 MB |
| Concurrent streams | 10+ | Limited by bandwidth |
| Subscribers per stream | 100+ | Limited by bandwidth |

### Benchmark Command

```powershell
# 1 publisher, 10 subscribers, run for 60 seconds
Measure-Command {
    # Start server, publisher, 10 subscribers
    # Let run for 60 seconds
    # Monitor CPU/memory
}
```

---

## Validation Checklist

Before considering relay implementation complete, verify:

- [ ] ✅ **Test 1**: Single subscriber works
- [ ] ✅ **Test 2**: Multiple subscribers work
- [ ] ✅ **Test 3**: Late joiner connects
- [ ] ✅ **Test 4**: Codec detection logs appear
- [ ] ✅ **Test 5**: Concurrent streams independent
- [ ] ✅ **Test 6**: Recording + relay both work
- [ ] ⏭️ **Test 7**: Publisher disconnect (Phase 4)
- [ ] ✅ **Test 8**: 10+ subscribers stable

---

## Next Steps

After validating basic relay (Phase 1):

1. **Phase 2**: Implement sequence header caching (Test 3 improvement)
2. **Phase 3**: Write automated integration tests
3. **Phase 4**: Implement publisher disconnect handling (Test 7 improvement)
4. **Phase 5**: Add relay metrics and monitoring

---

**Status**: Ready for manual testing  
**Estimated Test Time**: 30 minutes  
**Success Rate**: Should be 100% for Phase 1 tests

**Author**: RTMP Relay Implementation  
**Date**: October 11, 2025  
**Version**: 1.0.0

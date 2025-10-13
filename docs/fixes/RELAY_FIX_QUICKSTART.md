# Quick Test Guide: RTMP Relay Sequence Header Fix

## The Fix
**Problem**: ffplay got H.264 decoder errors because late-joining subscribers never received codec initialization packets (SPS/PPS for H.264, AudioSpecificConfig for AAC).

**Solution**: Cache sequence headers when publisher sends them, then send cached headers to new subscribers before they receive media packets.

---

## Test Steps

### 1. Start rtmp-server
```powershell
.\rtmp-server.exe -listen localhost:1935 -log-level info -record-all true -record-dir ./recordings
```

### 2. Start OBS
- Stream to: `rtmp://localhost:1935/live/test`
- Wait 2-3 seconds for stream to initialize

### 3. Start ffplay (subscriber)
```powershell
ffplay rtmp://localhost:1935/live/test
```

---

## Expected Results

### ✅ Success Indicators

**Server logs should show**:
```
INFO: Cached audio sequence header | stream_key=live/test | size=7
INFO: Cached video sequence header | stream_key=live/test | size=52
INFO: Subscriber added | stream_key=live/test | total_subscribers=1
INFO: Sent cached audio sequence header to subscriber | stream_key=live/test
INFO: Sent cached video sequence header to subscriber | stream_key=live/test
```

**ffplay should**:
- ✅ Play video smoothly
- ✅ Play audio correctly
- ✅ NO H.264 errors
- ✅ NO "No start code is found" messages
- ✅ NO "Error splitting the input into NAL units" messages

### ❌ Failure Indicators (if fix didn't work)
- H.264 decoder errors in ffplay console
- Video doesn't play or shows corruption
- Missing "Cached ... sequence header" or "Sent cached ... sequence header" logs

---

## What Changed

### Before Fix
1. OBS sends sequence headers at stream start (timestamp 0)
2. ffplay connects 43 seconds later
3. ffplay receives media packets but **missed** sequence headers
4. H.264 decoder fails: "No start code is found"

### After Fix
1. OBS sends sequence headers → **server caches them**
2. ffplay connects 43 seconds later
3. Server sends **cached sequence headers** to ffplay first
4. Then server relays live media packets
5. ffplay decoder has initialization data → **decoding works!**

---

## Verify Both Features Work

### Recording (should still work)
```powershell
# Play recorded file
ffplay recordings\live_test_20251013_121100.flv
```

### Relay (should now work)
```powershell
# Play live relay while OBS is streaming
ffplay rtmp://localhost:1935/live/test
```

**Both should work simultaneously now!**

---

## Troubleshooting

### If relay still fails:
1. Check server logs for "Cached ... sequence header" messages
   - If missing: Sequence headers not being detected/cached properly
2. Check server logs for "Sent cached ... sequence header" messages
   - If missing: Headers not being sent to subscriber
3. Check OBS codec settings:
   - Video: H.264 (x264 encoder)
   - Audio: AAC
4. Try stopping OBS, restarting server, then start OBS → ffplay

### Debug mode:
```powershell
.\rtmp-server.exe -listen localhost:1935 -log-level debug -record-all true -record-dir ./recordings > debug.log
```

Then search `debug.log` for:
- `"Cached audio sequence header"`
- `"Cached video sequence header"`
- `"Sent cached audio sequence header to subscriber"`
- `"Sent cached video sequence header to subscriber"`

---

## Technical Summary

**Files Modified**:
1. `internal/rtmp/server/registry.go` - Added sequence header caching
2. `internal/rtmp/server/play_handler.go` - Send cached headers to new subscribers

**Key Insight**: RTMP subscribers joining mid-stream need codec initialization packets that were sent at stream start. The fix caches these and replays them for late joiners.

**See**: `RELAY_FIX_SEQUENCE_HEADERS.md` for detailed technical documentation.

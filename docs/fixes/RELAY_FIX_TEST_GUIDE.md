# RTMP Relay Fix - Test Guide

**Date**: October 13, 2025  
**Fix Applied**: Payload cloning in `Stream.BroadcastMessage()`  
**Expected Outcome**: ffplay plays relayed stream without H.264 errors

---

## What Was Fixed

### Problem
- Relay was forwarding video packets correctly at RTMP chunk level
- BUT: Subscriber received corrupted H.264 NALUs
- ffplay showed 100+ errors: "No start code is found", "Error splitting NAL units"

### Root Cause
**Shared slice corruption**: Publisher and subscriber connections shared the same `msg.Payload` slice, causing race conditions or buffer reuse issues.

### Solution
**Payload cloning**: Each subscriber now receives an independent copy:
```go
relayMsg := &chunk.Message{
    // ... copy fields ...
    Payload: make([]byte, len(msg.Payload)),
}
copy(relayMsg.Payload, msg.Payload)
```

---

## Test Procedure

### Step 1: Start RTMP Server

**Terminal 1**:
```powershell
.\rtmp-server.exe -listen localhost:1935 -log-level debug -record-all true -record-dir ./recordings
```

**Expected**:
```
{"level":"INFO","msg":"RTMP server listening","addr":"[::]:1935"}
```

---

### Step 2: Publish from OBS

**OBS Settings**:
- Stream Server: `rtmp://localhost:1935/live`
- Stream Key: `test`
- Start Streaming

**Expected Server Logs**:
```json
{"level":"INFO","msg":"connect command","app":"live"}
{"level":"INFO","msg":"createStream"}
{"level":"INFO","msg":"publish command","stream_key":"live/test"}
{"level":"INFO","msg":"Codecs detected","videoCodec":"H.264 AVC","audioCodec":"AAC"}
```

---

### Step 3: Subscribe with ffplay

**Terminal 2**:
```powershell
ffplay rtmp://localhost:1935/live/test
```

**Expected Results** ✅:
1. **Video window opens** and shows live stream
2. **No H.264 decoder errors** (previously saw 100+ errors)
3. **Smooth playback** with audio synchronized
4. **Latency**: 3-5 seconds (acceptable)

**Previous Errors (should NOT appear)**:
```
[h264 @ ...] missing picture in access unit with size XXXX
[h264 @ ...] No start code is found.
[h264 @ ...] Error splitting the input into NAL units.
```

---

### Step 4: Verify Diagnostic Logs

**Look for** in server logs (Terminal 1):

**First video packet (sequence header)**:
```json
{"level":"DEBUG","msg":"Video packet structure before relay",
 "frame_type":1,"codec_id":7,"avc_packet_type":0,
 "payload_len":47,
 "first_10_bytes":"17 00 00 00 00 01 64 00 1F FF"}
```

**Regular video frames (NALUs)**:
```json
{"level":"DEBUG","msg":"Video packet structure before relay",
 "frame_type":2,"codec_id":7,"avc_packet_type":1,
 "payload_len":13150,
 "first_10_bytes":"27 01 00 00 00 00 00 0C 41 9E"}
```

**Hex Dump Analysis**:
- Byte 0: `0x17` = keyframe + AVC, `0x27` = inter frame + AVC
- Byte 1: `0x00` = sequence header, `0x01` = NALU
- Bytes 2-4: Composition time offset (usually `00 00 00`)

**Warning (should NOT appear)**:
```json
{"level":"WARN","msg":"Invalid AVC codec ID in video packet","codec_id":X,"expected":7}
```

---

## Success Criteria

### ✅ Primary Goal
- [ ] ffplay plays without H.264 decoder errors
- [ ] Video displays correctly (1280x720 @ 30fps)
- [ ] Audio synchronized with video

### ✅ Secondary Validation
- [ ] Server logs show valid FLV structure:
  - `frame_type`: 1 or 2
  - `codec_id`: 7
  - `avc_packet_type`: 0 or 1
- [ ] First 10 bytes hex dump looks correct (starts with `17` or `27`)
- [ ] Recording also works (already verified: `live_test_20251013_105440.flv`)

### ✅ Performance
- [ ] Latency: 3-5 seconds (acceptable for Phase 1)
- [ ] CPU usage: <30% (1 publisher + 1 subscriber)
- [ ] No crashes or hangs

---

## Troubleshooting

### Issue: Still seeing H.264 errors in ffplay

**Possible Causes**:
1. Old binary (rebuild didn't work)
2. Payload cloning not sufficient (deeper issue)
3. OBS encoding settings incompatible

**Actions**:
1. Verify binary timestamp: `ls -l rtmp-server.exe` (should be recent)
2. Check hex dumps in logs - are they valid FLV structure?
3. Try different OBS encoder: Settings → Output → Encoder → x264 (not NVENC)

---

### Issue: "Invalid AVC codec ID" warning

**Cause**: Payload first byte doesn't have codec ID = 7

**Actions**:
1. Check if OBS is sending H.264 (not H.265 or VP9)
2. Verify chunk reader isn't corrupting payload on receive
3. Add logging before codec detection to see raw payload

---

### Issue: No diagnostic logs appearing

**Cause**: Log level not set to debug, or no video packets received

**Actions**:
1. Confirm server started with `-log-level debug`
2. Check OBS is actually streaming (watch bitrate in OBS)
3. Verify publish succeeded (look for "publish command" log)

---

## Performance Benchmarks

### Expected (1 publisher + 1 subscriber)

| Metric | Target | Previous | After Fix |
|--------|--------|----------|-----------|
| ffplay errors | 0 | 100+ | ? |
| Video playback | Smooth | Black screen | ? |
| Latency | 3-5s | N/A | ? |
| CPU usage | <30% | ~15% | ? |

Fill in "After Fix" column after test.

---

## Next Steps After Success

1. **Test multiple subscribers** (10+ concurrent ffplay instances)
2. **Test late joiner** (start subscriber after publish)
3. **Long-duration test** (30+ minutes streaming)
4. **Stress test** (many streams, many subscribers)
5. **Update `RELAY_TESTING_GUIDE.md`** with successful results

---

## Rollback Plan (If Fix Fails)

**Git revert**:
```powershell
git diff internal/rtmp/server/registry.go
git checkout internal/rtmp/server/registry.go
go build -o rtmp-server.exe ./cmd/rtmp-server
```

**Temporary workaround**: Use recording + playback instead of live relay

---

## Documentation Updates After Success

Files to update:
- [x] `RELAY_TEST_ANALYSIS.md` - Mark fix as successful
- [ ] `RELAY_TESTING_GUIDE.md` - Update Test 1 with "WORKS" status
- [ ] `RELAY_IMPLEMENTATION_COMPLETE.md` - Update Phase 1 status
- [ ] Create `Fix_RTMP_Relay_Video_Corruption_20251013.md` - Document fix

---

**Test Guide Version**: 1.0  
**Last Updated**: October 13, 2025  
**Tester**: alxayo  
**Expected Test Duration**: 5 minutes

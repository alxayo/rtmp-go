# Gap Analysis: RTMP Relay Feature

**Date**: October 11, 2025  
**Feature**: Feature 002 - RTMP Server Relay  
**Status**: 🔴 **NOT FUNCTIONAL** - Critical implementation gaps prevent relay from working

---

## Executive Summary

### Current State: ⚠️ **RELAY BROKEN**

**The RTMP relay feature is NOT working in production despite having ~70% of the code implemented.**

**Root Cause**: `BroadcastMessage()` is never called, so media messages from publishers never reach subscribers.

### Quick Fix Required

**File**: `internal/rtmp/server/command_integration.go:146-157`

**Add ONE line**:
```go
stream.BroadcastMessage(st.codecDetector, m, log)
```

**This single line will enable basic relay functionality.**

---

## Gap Matrix

| Component | Implemented | Tested | Working | Gap |
|-----------|-------------|--------|---------|-----|
| Stream Registry | ✅ 100% | ✅ Yes | ✅ Yes | None |
| Subscriber Mgmt | ✅ 100% | ✅ Yes | ✅ Yes | None |
| Publish Handler | ✅ 100% | ✅ Yes | ✅ Yes | None |
| Play Handler | ✅ 100% | ✅ Yes | ✅ Yes | None |
| Broadcast Logic | ✅ 100% | ✅ Yes | ❌ **NEVER CALLED** | **Critical** |
| Codec Detection | ✅ 100% | ✅ Yes | ❌ No instance | **Critical** |
| Sequence Caching | ❌ 0% | ❌ No | ❌ No | **High Priority** |
| Disconnect Notify | ⚠️ 30% | ❌ No | ❌ No | **Medium** |
| Integration Tests | ❌ 0% | ❌ No | ❌ No | **Critical** |
| Observability | ❌ 0% | ❌ No | ❌ No | **Low** |

**Overall Completeness**: **~70% code, 0% functional**

---

## Critical Gaps (Blocking Relay)

### 1. BroadcastMessage() Never Called 🔴

**Location**: `internal/rtmp/server/command_integration.go:146-157`

**Current Code**:
```go
if m.TypeID == 8 || m.TypeID == 9 {
    st.mediaLogger.ProcessMessage(m)

    if st.streamKey != "" {
        stream := reg.GetStream(st.streamKey)
        if stream != nil && stream.Recorder != nil {
            stream.Recorder.WriteMessage(m)
        }
    }

    return // ⚠️ PROBLEM: Returns without broadcasting!
}
```

**What's Missing**:
```go
// AFTER recorder write, BEFORE return:
stream.BroadcastMessage(st.codecDetector, m, log)
```

**Impact**: 
- Publishers send media ✅
- Server receives media ✅  
- Subscribers connected ✅
- **Media NEVER forwarded** ❌

**Fix Complexity**: 5 minutes, 3 lines of code

---

### 2. No CodecDetector Instance 🔴

**Location**: `internal/rtmp/server/command_integration.go:46-50`

**Current Code**:
```go
type commandState struct {
    app         string
    streamKey   string
    allocator   *rpc.StreamIDAllocator
    mediaLogger *MediaLogger
    // ⚠️ MISSING: codecDetector
}
```

**What's Missing**:
```go
type commandState struct {
    app           string
    streamKey     string
    allocator     *rpc.StreamIDAllocator
    mediaLogger   *MediaLogger
    codecDetector *media.CodecDetector  // ADD THIS
}

// In attachCommandHandling():
st := &commandState{
    allocator:     rpc.NewStreamIDAllocator(),
    mediaLogger:   NewMediaLogger(c.ID(), log, 30*time.Second),
    codecDetector: &media.CodecDetector{},  // ADD THIS
}
```

**Impact**: Codec detection logs never appear

**Fix Complexity**: 10 minutes, 5 lines of code

---

### 3. Stream Doesn't Implement CodecStore 🔴

**Location**: `internal/rtmp/server/registry.go`

**Current**: `server.Stream` struct exists but missing codec interface methods

**What's Missing**:
```go
// Add to registry.go:
func (s *Stream) SetAudioCodec(c string) {
    s.mu.Lock()
    s.AudioCodec = c
    s.mu.Unlock()
}

func (s *Stream) SetVideoCodec(c string) {
    s.mu.Lock()
    s.VideoCodec = c
    s.mu.Unlock()
}

func (s *Stream) GetAudioCodec() string {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return s.AudioCodec
}

func (s *Stream) GetVideoCodec() string {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return s.VideoCodec
}

func (s *Stream) StreamKey() string {
    return s.Key
}
```

**Impact**: Type assertion fails when BroadcastMessage tries to set codecs

**Fix Complexity**: 15 minutes, 25 lines of code

---

## High Priority Gaps (Degraded Experience)

### 4. No Sequence Header Caching 🟡

**Problem**: New subscribers joining mid-stream can't decode video/audio

**Current Behavior**:
1. Publisher sends sequence headers at start
2. Early subscribers receive them ✅
3. Late joiner connects 30 seconds later
4. Late joiner misses sequence headers ❌
5. ffplay shows errors, can't decode ❌

**Expected Behavior**:
1. Server caches first audio/video sequence headers
2. Late joiner connects
3. Server immediately sends cached headers
4. Late joiner can decode immediately ✅

**What's Missing**:
```go
// In Stream struct:
AudioSeqHeader []byte  // Cache AAC AudioSpecificConfig
VideoSeqHeader []byte  // Cache H.264 SPS/PPS

// In command_integration.go - detect and cache:
if isAudioSeqHeader(m) {
    stream.AudioSeqHeader = clonePayload(m.Payload)
}

// In play_handler.go - send on play:
if stream.AudioSeqHeader != nil {
    conn.SendMessage(makeAudioMsg(stream.AudioSeqHeader))
}
```

**Impact**: 
- ❌ Late joiners see black screen
- ❌ ffplay logs decoder errors
- ❌ Poor user experience

**Fix Complexity**: 2-3 hours (detection + caching + sending)

---

### 5. No Integration Tests 🟡

**Current Tests**:
- ✅ Unit tests for BroadcastMessage logic (`relay_test.go`)
- ✅ Unit tests for handlers
- ❌ **NO end-to-end relay tests**

**Missing Test Scenarios**:
1. Publish → Relay → Play (basic flow)
2. 1 publisher → 3 subscribers (multi-subscriber)
3. Late joiner gets cached headers
4. Slow subscriber doesn't block others
5. Publisher disconnect notifies subscribers

**Impact**: 
- No validation that relay actually works
- Bugs not caught until manual testing
- Risk of regressions

**Fix Complexity**: 1-2 days (write 5 integration tests)

---

## Medium Priority Gaps (Nice-to-Have)

### 6. Publisher Disconnect Not Handled 🟢

**Current**: When publisher disconnects:
- ✅ Publisher removed from registry
- ❌ Subscribers NOT notified
- ❌ Players keep waiting indefinitely

**Expected**: Send `NetStream.Play.UnpublishNotify` to all subscribers

**Fix Complexity**: 1-2 hours

---

### 7. No Relay Metrics 🟢

**Missing**:
- Message relay count
- Bytes forwarded
- Subscribers per stream
- Drop rate (backpressure)

**Fix Complexity**: 2-3 hours

---

## Comparison: Unit Tests vs Production

| Scenario | Unit Test | Production Code |
|----------|-----------|-----------------|
| BroadcastMessage called | ✅ `relay_test.go:42` | ❌ Never called |
| CodecDetector passed | ✅ `&CodecDetector{}` | ❌ No instance |
| Multiple subscribers | ✅ Tests 3 subscribers | ❌ Never tested |
| Slow subscriber handling | ✅ Tests backpressure | ❌ Never tested |

**Conclusion**: Unit tests prove the relay logic works, but production code never invokes it!

---

## Why Relay Doesn't Work: Call Chain Analysis

### Expected Flow:
```
Publisher sends audio frame
  ↓
Connection.readLoop() receives chunks
  ↓
Dechunker reassembles Message (Type 8)
  ↓
attachCommandHandling() messageHandler invoked
  ↓
Handler detects m.TypeID == 8
  ↓
⚠️ SHOULD call: stream.BroadcastMessage(detector, m, log)
  ↓
BroadcastMessage loops subscribers
  ↓
Each subscriber.SendMessage(m) called
  ↓
Connection.SendMessage() enqueues to outboundQueue
  ↓
Connection.writeLoop() chunks and sends
  ↓
Subscriber receives frame ✅
```

### Actual Flow:
```
Publisher sends audio frame ✅
  ↓
readLoop receives chunks ✅
  ↓
Dechunker reassembles Message ✅
  ↓
messageHandler invoked ✅
  ↓
Handler detects m.TypeID == 8 ✅
  ↓
st.mediaLogger.ProcessMessage(m) ✅
  ↓
stream.Recorder.WriteMessage(m) ✅ (if recording enabled)
  ↓
return ❌ STOPS HERE - never broadcasts!
  ↓
Subscribers never notified ❌
```

**The ONE line missing** breaks the entire relay feature.

---

## Impact Analysis

### What Works Today:
- ✅ Server startup
- ✅ Connection acceptance
- ✅ Handshake
- ✅ Command processing (connect, createStream, publish, play)
- ✅ Publisher registration
- ✅ Subscriber registration
- ✅ Recording to FLV files
- ✅ Codec detection (in tests)
- ✅ Broadcast logic (in tests)

### What Doesn't Work:
- ❌ **Media relay (core feature)**
- ❌ Playback (subscribers receive nothing)
- ❌ Codec detection in production
- ❌ Late joiner support
- ❌ Publisher disconnect notification
- ❌ Integration validation

### User Impact:
```
User Action: ffmpeg publishes → ffplay tries to play
Expected: Video plays ✅
Actual: ffplay hangs, no data received ❌

Error: "Connection timeout" or "No data"
Root Cause: BroadcastMessage never called
```

---

## Quick Win: Minimal Viable Fix

**Goal**: Make basic relay work in 30 minutes

**Changes Required**: 3 files, ~40 lines total

### File 1: `internal/rtmp/server/command_integration.go`

```diff
 type commandState struct {
     app         string
     streamKey   string
     allocator   *rpc.StreamIDAllocator
     mediaLogger *MediaLogger
+    codecDetector *media.CodecDetector
 }

 func attachCommandHandling(c *Connection, reg *Registry, cfg *Config, log *slog.Logger) {
     st := &commandState{
         allocator:   rpc.NewStreamIDAllocator(),
         mediaLogger: NewMediaLogger(c.ID(), log, 30*time.Second),
+        codecDetector: &media.CodecDetector{},
     }

     // ... later in messageHandler ...
     
     if m.TypeID == 8 || m.TypeID == 9 {
         st.mediaLogger.ProcessMessage(m)

         if st.streamKey != "" {
             stream := reg.GetStream(st.streamKey)
             if stream != nil {
                 if stream.Recorder != nil {
                     stream.Recorder.WriteMessage(m)
                 }
+                
+                // BROADCAST TO SUBSCRIBERS
+                stream.BroadcastMessage(st.codecDetector, m, log)
             }
         }

         return
     }
```

### File 2: `internal/rtmp/server/registry.go`

```go
// Add CodecStore interface implementation
func (s *Stream) SetAudioCodec(c string) {
    s.mu.Lock()
    s.AudioCodec = c
    s.mu.Unlock()
}

func (s *Stream) SetVideoCodec(c string) {
    s.mu.Lock()
    s.VideoCodec = c
    s.mu.Unlock()
}

func (s *Stream) GetAudioCodec() string {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return s.AudioCodec
}

func (s *Stream) GetVideoCodec() string {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return s.VideoCodec
}

func (s *Stream) StreamKey() string {
    return s.Key
}
```

### File 3: `internal/rtmp/server/registry.go` (modify Stream)

```go
// Ensure Stream implements media.Subscriber via duck typing
// (Already works - Connection implements SendMessage)
```

**Test**:
```powershell
# Rebuild
go build -o rtmp-server.exe ./cmd/rtmp-server

# Terminal 1
.\rtmp-server.exe

# Terminal 2
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test

# Terminal 3 - SHOULD NOW WORK!
ffplay rtmp://localhost:1935/live/test
```

**Expected Result**: Video plays in ffplay ✅

---

## Recommended Implementation Order

### Phase 0: Quick Win (30 minutes)
1. Add BroadcastMessage() call
2. Add CodecDetector instance
3. Add CodecStore methods
4. **Test with FFmpeg/ffplay** ✅

### Phase 1: Validation (2 days)
5. Write integration test: publish → play
6. Write integration test: multiple subscribers
7. Run with `-race` flag
8. Validate no memory leaks

### Phase 2: Polish (3 days)
9. Add sequence header caching
10. Test late joiner scenario
11. Add publisher disconnect handling
12. Update documentation

### Phase 3: Observability (1 day)
13. Add relay metrics
14. Add performance logging
15. Benchmark suite

**Total Estimate**: 1 week (1 developer)

---

## Success Criteria

### Minimal (Phase 0):
- ✅ ffplay can watch published stream
- ✅ Multiple ffplay instances work simultaneously
- ✅ Codec detection logs appear

### Complete (Phase 2):
- ✅ Late joiners get immediate playback
- ✅ Publisher disconnect handled gracefully
- ✅ All integration tests pass
- ✅ No race conditions (`go test -race`)

### Production Ready (Phase 3):
- ✅ Relay metrics exposed
- ✅ Documentation updated
- ✅ Performance validated (10 concurrent streams)

---

## Conclusion

**The relay feature is 70% complete but 0% functional due to a missing function call.**

**Priority**: **CRITICAL** - This is a core feature blocking usability

**Effort**: **Low** - Quick win achievable in 30 minutes, full polish in 1 week

**Risk**: **Low** - Most code already written and tested, just needs wiring

**Recommendation**: Start with Phase 0 (quick win) today, validate immediately with FFmpeg/ffplay.

---

**Next Steps**:
1. ✅ Review this gap analysis
2. ⏭️ Implement quick win (30 min)
3. ⏭️ Test with FFmpeg/ffplay
4. ⏭️ Create GitHub issues for remaining gaps
5. ⏭️ Proceed with Phase 1-3 implementation

---

**Document**: `specs/002-rtmp-relay-feature/gap-analysis.md`  
**Related**: `specs/002-rtmp-relay-feature/implementation-plan.md`  
**Author**: System Analysis  
**Date**: October 11, 2025

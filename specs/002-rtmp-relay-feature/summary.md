# Feature 002: RTMP Relay Implementation Plan - Summary

**Date**: October 11, 2025  
**Status**: 🔴 **Critical Issue Found** - Relay Not Functional  
**Priority**: P0 - Blocking Core Functionality

---

## TL;DR

**Problem**: RTMP relay feature is **NOT working** despite 70% of code being implemented.

**Root Cause**: `BroadcastMessage()` is never called, so publishers can send but subscribers never receive.

**Quick Fix**: Add **ONE function call** to make it work.

**Full Solution**: 1 week effort to complete, test, and polish the relay feature.

---

## Documents Created

### 1. **Gap Analysis** 
**File**: `specs/002-rtmp-relay-feature/gap-analysis.md`

**Contents**:
- Critical gaps preventing relay from working
- What exists vs what's missing
- Impact analysis
- Quick win (30-minute fix)
- Recommended implementation order

**Key Finding**: Missing one `stream.BroadcastMessage()` call breaks entire feature

---

### 2. **Implementation Plan**
**File**: `specs/002-rtmp-relay-feature/implementation-plan.md`

**Contents**:
- Detailed task breakdown by phase
- Code examples for each fix
- Integration test specifications
- Acceptance criteria
- Risk assessment
- 3-week implementation timeline

**Phases**:
- **Phase 1 (P0)**: Enable basic relay - 2 days
- **Phase 2 (P1)**: Sequence header caching - 2 days  
- **Phase 3 (P0)**: Integration tests - 2 days
- **Phase 4 (P1)**: Disconnect handling - 1 day
- **Phase 5 (P2)**: Observability - 1 day

---

### 3. **Feature Documentation**
**File**: `feature002-rtmp-relay.md`

**Contents**:
- How relay architecture works
- How ffplay replays streams (live and recorded)
- Complete data flow diagrams
- Example usage scenarios
- Implementation file references

**Purpose**: Technical documentation for understanding the relay system

---

## Critical Gaps Summary

| Gap | File | Impact | Fix Time |
|-----|------|--------|----------|
| **BroadcastMessage not called** | `command_integration.go:146` | 🔴 **Blocks relay** | 5 min |
| **No CodecDetector instance** | `command_integration.go:46` | 🔴 **No codec detection** | 10 min |
| **Stream missing CodecStore** | `registry.go` | 🔴 **Type errors** | 15 min |
| **No sequence header cache** | `registry.go` + `play_handler.go` | 🟡 Late joiners fail | 3 hours |
| **No integration tests** | `tests/integration/relay_test.go` | 🟡 No validation | 2 days |
| **No disconnect handling** | `publish_handler.go` | 🟢 Poor UX | 2 hours |

**Total Quick Win**: 30 minutes to make relay work  
**Total Complete Solution**: 5-7 days to production-ready

---

## What Exists (Already Implemented)

✅ **Infrastructure (100%)**:
- Stream registry with thread-safe operations
- Subscriber/publisher management
- Connection read/write loops
- Message routing by type
- FLV recording

✅ **Handlers (100%)**:
- Publish handler (registers publisher)
- Play handler (adds subscriber)
- Disconnect handlers (partial)

✅ **Core Logic (100%)**:
- `BroadcastMessage()` implementation
- Backpressure handling (TrySendMessage)
- Codec detection logic
- Unit tests (3 tests, all passing)

---

## What's Missing (Gaps)

❌ **Integration (0%)**:
- BroadcastMessage never called in production
- No CodecDetector instantiation
- Stream doesn't implement CodecStore interface

❌ **Features (0%)**:
- Sequence header caching
- Late joiner support  
- Publisher disconnect notification

❌ **Tests (0%)**:
- No integration tests
- No FFmpeg/ffplay validation
- No concurrent subscriber tests

---

## Quick Win: 30-Minute Fix

### Step 1: Add BroadcastMessage Call (5 min)

**File**: `internal/rtmp/server/command_integration.go:146`

```go
// BEFORE:
if m.TypeID == 8 || m.TypeID == 9 {
    st.mediaLogger.ProcessMessage(m)
    if st.streamKey != "" {
        stream := reg.GetStream(st.streamKey)
        if stream != nil && stream.Recorder != nil {
            stream.Recorder.WriteMessage(m)
        }
    }
    return
}

// AFTER:
if m.TypeID == 8 || m.TypeID == 9 {
    st.mediaLogger.ProcessMessage(m)
    if st.streamKey != "" {
        stream := reg.GetStream(st.streamKey)
        if stream != nil {
            if stream.Recorder != nil {
                stream.Recorder.WriteMessage(m)
            }
            // 🆕 BROADCAST TO SUBSCRIBERS
            stream.BroadcastMessage(st.codecDetector, m, log)
        }
    }
    return
}
```

### Step 2: Add CodecDetector Instance (10 min)

**File**: `internal/rtmp/server/command_integration.go:46-50`

```go
type commandState struct {
    app           string
    streamKey     string
    allocator     *rpc.StreamIDAllocator
    mediaLogger   *MediaLogger
    codecDetector *media.CodecDetector  // 🆕 ADD
}

// In attachCommandHandling():
st := &commandState{
    allocator:     rpc.NewStreamIDAllocator(),
    mediaLogger:   NewMediaLogger(c.ID(), log, 30*time.Second),
    codecDetector: &media.CodecDetector{},  // 🆕 INITIALIZE
}
```

### Step 3: Implement CodecStore Interface (15 min)

**File**: `internal/rtmp/server/registry.go` (add methods)

```go
// Add after Stream struct definition:
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

### Step 4: Test (5 min)

```powershell
# Rebuild
go build -o rtmp-server.exe ./cmd/rtmp-server

# Terminal 1: Start server
.\rtmp-server.exe -listen :1935 -log-level debug

# Terminal 2: Publish
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test

# Terminal 3: Play - SHOULD NOW WORK! ✅
ffplay rtmp://localhost:1935/live/test
```

**Expected**: 
- ✅ Video plays in ffplay
- ✅ Logs show: `"Codec detected","video":"H.264 AVC","audio":"AAC"`
- ✅ Multiple ffplay instances work

---

## Full Implementation Plan

### Week 1: Critical Path (P0)

**Days 1-2**: Enable basic relay (Tasks 1.1-1.3)
- Add BroadcastMessage() call
- Add CodecDetector instance  
- Implement CodecStore interface
- **Deliverable**: Basic relay works with FFmpeg/ffplay

**Days 3-4**: Integration tests (Tasks 3.1-3.3)
- Write publish → play test
- Write multiple subscribers test
- Write late joiner test
- **Deliverable**: Automated validation suite

**Day 5**: Testing & validation
- Run with `-race` flag
- Memory leak testing
- Performance baseline
- **Deliverable**: Stability confirmed

### Week 2: Polish (P1)

**Days 1-2**: Sequence header caching (Tasks 2.1-2.3)
- Add header storage to Stream
- Cache on first frame
- Send to new subscribers
- **Deliverable**: Late joiners work immediately

**Days 3-4**: Disconnect handling (Task 4.1)
- Send UnpublishNotify on disconnect
- Clean up streams
- Integration test
- **Deliverable**: Graceful publisher exit

**Day 5**: End-to-end validation
- FFmpeg/ffplay interop tests
- Concurrent streams test
- Documentation updates
- **Deliverable**: Production-ready relay

### Week 3: Observability (P2)

**Days 1-2**: Metrics (Task 5.1)
- Relay counters
- Performance metrics
- Monitoring endpoints
- **Deliverable**: Observable relay

**Days 3-5**: Final polish
- Performance optimization
- Documentation completion
- Deployment guide
- **Deliverable**: Complete feature

---

## Success Metrics

### Immediate (Week 1)
- ✅ ffplay can watch published stream
- ✅ Multiple players work simultaneously
- ✅ No race conditions detected
- ✅ Integration tests pass

### Complete (Week 2)
- ✅ Late joiners get immediate playback
- ✅ Publisher disconnect handled gracefully
- ✅ 10 concurrent streams supported
- ✅ Latency < 5 seconds

### Production (Week 3)
- ✅ Relay metrics exposed
- ✅ Documentation complete
- ✅ Performance validated
- ✅ Deployment guide ready

---

## Testing Strategy

### Unit Tests (Existing)
- ✅ `relay_test.go`: BroadcastMessage logic (3 tests)
- ✅ `publish_handler_test.go`: Publisher registration
- ✅ `play_handler_test.go`: Subscriber registration

### Integration Tests (New)
- ⏭️ `relay_test.go`: Publish → relay → play
- ⏭️ `relay_test.go`: Multiple subscribers
- ⏭️ `relay_test.go`: Late joiner
- ⏭️ `relay_test.go`: Slow subscriber backpressure
- ⏭️ `relay_test.go`: Publisher disconnect

### Interop Tests (Existing)
- ✅ `tests/interop/ffmpeg_test.ps1`: FFmpeg/ffplay validation
- Need to verify after relay fix

---

## Risk Assessment

| Risk | Probability | Impact | Mitigation |
|------|------------|--------|------------|
| Quick fix breaks existing code | Low | Medium | Comprehensive unit tests already exist |
| Performance regression | Low | Medium | Benchmark before/after |
| Race conditions | Low | High | Run all tests with `-race` flag |
| Backward compatibility | Very Low | Low | No API changes required |

---

## Effort Estimate

### Quick Win (Phase 0)
- **Development**: 30 minutes
- **Testing**: 15 minutes  
- **Total**: 45 minutes
- **Developer**: 1 person

### Full Implementation
- **Development**: 7-10 days
- **Testing**: 3-5 days
- **Documentation**: 1-2 days
- **Total**: 2-3 weeks
- **Developer**: 1 person

---

## Recommended Approach

### Option A: Quick Win First (Recommended)
1. **Today**: Implement 30-minute quick win
2. **Today**: Test with FFmpeg/ffplay
3. **Tomorrow**: Start integration tests
4. **Week 2**: Polish features
5. **Week 3**: Observability

**Pros**: 
- ✅ Immediate functionality
- ✅ Unblocks development
- ✅ Validates approach

**Cons**:
- ⚠️ Missing polish features initially

### Option B: Complete Implementation
1. **Week 1**: All P0 tasks
2. **Week 2**: All P1 tasks
3. **Week 3**: P2 + validation

**Pros**:
- ✅ Production-ready on completion
- ✅ No interim releases

**Cons**:
- ⚠️ 3 weeks before any functionality
- ⚠️ Higher risk (no incremental validation)

**Recommendation**: **Option A** - Quick win establishes value immediately

---

## Next Steps

### Immediate (Today)
1. ✅ Review this summary
2. ⏭️ Implement quick win (30 min)
3. ⏭️ Test with FFmpeg/ffplay (15 min)
4. ⏭️ Create GitHub issues for remaining work

### This Week
5. ⏭️ Write integration tests
6. ⏭️ Run race detector
7. ⏭️ Validate stability

### Next Week
8. ⏭️ Implement sequence header caching
9. ⏭️ Add disconnect handling
10. ⏭️ Complete documentation

---

## Files Created

### Documentation
- ✅ `feature002-rtmp-relay.md` - Architecture documentation
- ✅ `specs/002-rtmp-relay-feature/gap-analysis.md` - Gap analysis
- ✅ `specs/002-rtmp-relay-feature/implementation-plan.md` - Detailed plan
- ✅ `specs/002-rtmp-relay-feature/summary.md` - This file

### To Be Created
- ⏭️ `tests/integration/relay_test.go` - Integration tests
- ⏭️ `internal/rtmp/server/metrics.go` - Relay metrics
- ⏭️ `docs/relay-architecture.md` - Developer guide

---

## Conclusion

**The RTMP relay feature is 70% complete but not functional due to missing integration code.**

**Immediate Action**: Implement the 30-minute quick win to enable basic relay functionality.

**Long-term Plan**: Follow the 3-week implementation plan to achieve production-ready relay with full polish.

**Risk Level**: **Low** - Most code exists and is tested, just needs proper wiring.

**Value**: **High** - Enables core streaming functionality (publish → relay → play).

---

**Status**: Ready for implementation  
**Priority**: P0 - Critical  
**Effort**: 30 minutes (quick win) → 3 weeks (complete)  
**Next**: Review with team, then implement quick win

---

**Author**: System Analysis  
**Date**: October 11, 2025  
**Last Updated**: October 11, 2025

# Feature 002: RTMP Relay - Documentation Index

**Feature**: RTMP Server Media Relay  
**Status**: 🔴 **Implementation Incomplete** - Core functionality not working  
**Created**: October 11, 2025

---

## Quick Links

| Document | Purpose | Audience |
|----------|---------|----------|
| **[Summary](summary.md)** | Executive overview, quick win guide | Team leads, developers |
| **[Gap Analysis](gap-analysis.md)** | What's missing, why it doesn't work | Developers, QA |
| **[Implementation Plan](implementation-plan.md)** | Detailed tasks, code examples, tests | Developers |
| **[Architecture](../feature002-rtmp-relay.md)** | How relay works (design doc) | All team members |

---

## Problem Statement

**Issue**: RTMP relay feature is **NOT working** in production despite having ~70% of the code implemented.

**Root Cause**: `BroadcastMessage()` method is never called, so media messages from publishers don't reach subscribers.

**Impact**: 
- ❌ Publishers can send streams
- ❌ Players can connect
- ❌ **But players receive NO media data**
- ❌ ffplay hangs indefinitely

---

## Document Overview

### 1. Summary ([summary.md](summary.md))

**Purpose**: High-level overview for quick understanding

**Contents**:
- TL;DR problem and solution
- Quick win (30-minute fix)
- Full implementation timeline
- Success metrics
- Recommended approach

**Read this first if**: You need to understand the situation quickly or decide on next steps.

---

### 2. Gap Analysis ([gap-analysis.md](gap-analysis.md))

**Purpose**: Detailed analysis of what exists vs what's missing

**Contents**:
- Component-by-component gap matrix
- Critical gaps blocking functionality
- Call chain analysis (expected vs actual)
- Impact analysis
- Quick win code snippets
- Comparison: unit tests vs production

**Read this if**: You need to understand exactly what's broken and why.

**Key Findings**:
- ✅ 70% of code exists and is tested
- ❌ Missing ONE function call breaks entire feature
- ❌ No integration tests validate end-to-end flow
- ❌ Sequence headers not cached (late joiners fail)

---

### 3. Implementation Plan ([implementation-plan.md](implementation-plan.md))

**Purpose**: Detailed task breakdown with code examples

**Contents**:
- Phase-by-phase task list (5 phases)
- Code examples for each fix
- Integration test specifications
- Acceptance criteria per task
- Risk assessment
- 3-week timeline

**Read this if**: You're implementing the fixes and need detailed guidance.

**Phases**:
1. **Phase 1 (P0)**: Enable basic relay - 2 days
2. **Phase 2 (P1)**: Sequence header caching - 2 days
3. **Phase 3 (P0)**: Integration tests - 2 days
4. **Phase 4 (P1)**: Disconnect handling - 1 day
5. **Phase 5 (P2)**: Observability - 1 day

---

### 4. Architecture Documentation ([../feature002-rtmp-relay.md](../feature002-rtmp-relay.md))

**Purpose**: Technical documentation of how the relay system works

**Contents**:
- Complete relay architecture
- How ffplay replays streams (live and recorded)
- Data flow diagrams
- Example usage scenarios
- Implementation file references
- Performance characteristics

**Read this if**: You need to understand how the relay system is designed to work.

**Topics Covered**:
- Publisher → Server → Subscriber flow
- BroadcastMessage() implementation
- Backpressure handling
- Recording integration
- Multi-subscriber support
- Concurrency model

---

## Quick Start Guide

### For Developers

**Goal**: Understand the problem and implement the quick win

1. **Read**: [Summary](summary.md) (5 minutes)
2. **Read**: [Gap Analysis - Quick Win section](gap-analysis.md#quick-win-minimal-viable-fix) (10 minutes)
3. **Implement**: 3 code changes (30 minutes)
4. **Test**: FFmpeg publish → ffplay (5 minutes)
5. **Next**: Read [Implementation Plan](implementation-plan.md) for full solution

### For Team Leads

**Goal**: Understand scope and prioritize work

1. **Read**: [Summary](summary.md) (5 minutes)
2. **Review**: [Gap Analysis - Impact Analysis](gap-analysis.md#impact-analysis) (10 minutes)
3. **Decide**: Quick win vs full implementation
4. **Plan**: Assign tasks from [Implementation Plan](implementation-plan.md)

### For QA/Testing

**Goal**: Understand what needs validation

1. **Read**: [Summary - Success Metrics](summary.md#success-metrics) (5 minutes)
2. **Review**: [Implementation Plan - Phase 3](implementation-plan.md#phase-3-integration-tests-p0) (15 minutes)
3. **Prepare**: Test scenarios and FFmpeg/ffplay commands
4. **Validate**: Run tests after each implementation phase

---

## Quick Win: 30-Minute Fix

**Problem**: Media not forwarded to subscribers

**Solution**: Add ONE function call

**Files to Edit**: 3 files, ~40 lines total

### Step 1: Add BroadcastMessage Call (5 min)

**File**: `internal/rtmp/server/command_integration.go:146`

```go
// Add after recording:
stream.BroadcastMessage(st.codecDetector, m, log)
```

### Step 2: Add CodecDetector (10 min)

**File**: `internal/rtmp/server/command_integration.go:46-50`

```go
type commandState struct {
    // ... existing fields ...
    codecDetector *media.CodecDetector  // ADD THIS
}
```

### Step 3: Implement CodecStore (15 min)

**File**: `internal/rtmp/server/registry.go`

```go
// Add 5 methods: SetAudioCodec, SetVideoCodec, GetAudioCodec, GetVideoCodec, StreamKey
```

**Full code in**: [Gap Analysis - Quick Win](gap-analysis.md#quick-win-minimal-viable-fix)

### Test

```powershell
go build -o rtmp-server.exe ./cmd/rtmp-server
.\rtmp-server.exe

# Terminal 2: ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test
# Terminal 3: ffplay rtmp://localhost:1935/live/test  ✅ SHOULD WORK!
```

---

## Implementation Status

### ✅ Completed (Existing Code)

| Component | Status | File | Tests |
|-----------|--------|------|-------|
| Stream Registry | ✅ | `server/registry.go` | ✅ |
| Publisher Handler | ✅ | `server/publish_handler.go` | ✅ |
| Play Handler | ✅ | `server/play_handler.go` | ✅ |
| Broadcast Logic | ✅ | `media/relay.go` | ✅ |
| Codec Detection | ✅ | `media/codec.go` | ✅ |
| Recording | ✅ | `media/recorder.go` | ✅ |
| Connection Loops | ✅ | `conn/conn.go` | ✅ |

### ❌ Missing (Gaps)

| Component | Priority | Effort | Impact |
|-----------|----------|--------|--------|
| BroadcastMessage call | P0 | 5 min | **Blocks relay** |
| CodecDetector instance | P0 | 10 min | No codec detection |
| CodecStore interface | P0 | 15 min | Type errors |
| Integration tests | P0 | 2 days | No validation |
| Sequence header cache | P1 | 3 hours | Late joiners fail |
| Disconnect handling | P1 | 2 hours | Poor UX |
| Metrics | P2 | 3 hours | No monitoring |

---

## Testing Strategy

### Unit Tests (Existing ✅)
- `internal/rtmp/media/relay_test.go` (3 tests)
  - Single subscriber relay
  - Multiple subscribers relay  
  - Slow subscriber backpressure

### Integration Tests (Missing ❌)
- `tests/integration/relay_test.go` (5 tests needed)
  - Publish → relay → play (basic)
  - Multiple subscribers
  - Late joiner
  - Publisher disconnect
  - Concurrent streams

### Interop Tests (Existing ✅)
- `tests/interop/ffmpeg_test.ps1`
  - FFmpeg publish test
  - ffplay playback test
  - Recording validation

**Status**: Need to run after quick win implementation

---

## Success Criteria

### Phase 0: Quick Win (30 minutes)
- ✅ ffplay receives video/audio
- ✅ Multiple players work simultaneously
- ✅ Codec detection logs appear

### Phase 1-2: Core Features (1 week)
- ✅ Late joiners get immediate playback
- ✅ Integration tests pass
- ✅ No race conditions
- ✅ Memory stable

### Phase 3: Production Ready (2 weeks)
- ✅ Publisher disconnect handled
- ✅ Relay metrics exposed
- ✅ Documentation complete
- ✅ Performance validated

---

## Timeline

### Option A: Quick Win First (Recommended) ⭐

```
Day 1:   Quick win (30 min) → Test with FFmpeg/ffplay → Create issues
Week 1:  Integration tests + basic validation
Week 2:  Sequence headers + disconnect handling
Week 3:  Metrics + performance + documentation
```

**Pros**: Immediate functionality, validates approach  
**Cons**: Missing polish features initially

### Option B: Complete Implementation

```
Week 1:  All P0 tasks (relay + tests)
Week 2:  All P1 tasks (headers + disconnect)
Week 3:  All P2 tasks (metrics + docs)
```

**Pros**: Production-ready on completion  
**Cons**: 3 weeks before any functionality

**Recommendation**: **Option A** - Quick win provides immediate value

---

## Common Questions

### Q: Why doesn't the relay work if the code exists?

**A**: The `BroadcastMessage()` method is fully implemented and tested, but the production code never calls it. Media messages are received, logged, and recorded, but never forwarded to subscribers.

### Q: How hard is it to fix?

**A**: **30 minutes** for basic functionality (quick win), **2-3 weeks** for production-ready with all features.

### Q: Will the fix break anything?

**A**: **Very unlikely**. The broadcast logic is already tested in unit tests. We're just wiring it into the existing message flow.

### Q: What about late joiners?

**A**: Quick win enables relay but late joiners need sequence headers (Phase 2). They'll see black screen until next keyframe (~2 seconds for 30fps video).

### Q: Can we test it easily?

**A**: **Yes**. Simple FFmpeg publish + ffplay command validates the fix immediately.

---

## References

### Internal
- **Constitution**: `docs/000-constitution.md`
- **Original Spec**: `specs/001-rtmp-server-implementation/spec.md`
- **Data Model**: `specs/001-rtmp-server-implementation/data-model.md`
- **Tasks**: `specs/001-rtmp-server-implementation/tasks.md`

### Code
- **Relay Logic**: `internal/rtmp/media/relay.go`
- **Integration Point**: `internal/rtmp/server/command_integration.go`
- **Stream Registry**: `internal/rtmp/server/registry.go`
- **Tests**: `internal/rtmp/media/relay_test.go`

### External
- **RTMP Spec**: Adobe RTMP Specification 1.0
- **FLV Format**: Adobe FLV File Format Specification v10.1

---

## Contact & Support

**Questions?** Refer to the appropriate document:
- **"How does relay work?"** → [Architecture Doc](../feature002-rtmp-relay.md)
- **"What's broken?"** → [Gap Analysis](gap-analysis.md)
- **"How do I fix it?"** → [Implementation Plan](implementation-plan.md)
- **"What's the priority?"** → [Summary](summary.md)

**Need Help?** Check existing issues or create a new one with:
- Link to relevant document
- Specific section/task
- Question or blocker

---

**Created**: October 11, 2025  
**Last Updated**: October 11, 2025  
**Status**: Ready for implementation  
**Next Action**: Implement quick win (30 minutes)

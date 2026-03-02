# Implementation Plan: RTMP Relay Feature (Feature 002)

**Created**: October 11, 2025  
**Status**: Planning  
**Priority**: High (Core functionality incomplete)

---

## Executive Summary

The RTMP server currently has **partial relay implementation** with critical gaps preventing actual media forwarding from publishers to subscribers. This plan addresses the missing pieces to make the relay feature fully functional.

### Current Status: ⚠️ **RELAY NOT WORKING**

**Problem**: Media messages from publishers are **NOT being broadcast** to subscribers. The `BroadcastMessage()` method is never called in the production code path.

---

## Gap Analysis

### ✅ What Exists (Already Implemented)

| Component | Status | File | Notes |
|-----------|--------|------|-------|
| **Stream Registry** | ✅ Complete | `internal/rtmp/server/registry.go` | Thread-safe stream management |
| **Subscriber Management** | ✅ Complete | `internal/rtmp/server/registry.go` | `AddSubscriber()`, `RemoveSubscriber()` |
| **Publisher Registration** | ✅ Complete | `internal/rtmp/server/publish_handler.go` | `HandlePublish()` registers publisher |
| **Play Handler** | ✅ Complete | `internal/rtmp/server/play_handler.go` | `HandlePlay()` adds subscribers |
| **Broadcast Logic** | ✅ Complete | `internal/rtmp/media/relay.go` | `BroadcastMessage()` implementation |
| **Codec Detection** | ✅ Complete | `internal/rtmp/media/codec.go` | AAC/H.264 detection |
| **Connection Loops** | ✅ Complete | `internal/rtmp/conn/conn.go` | readLoop + writeLoop |
| **Message Handler** | ✅ Complete | `internal/rtmp/server/command_integration.go` | Routes messages by type |
| **Recording** | ✅ Complete | `internal/rtmp/media/recorder.go` | FLV file writing |

### ❌ What's Missing (Critical Gaps)

| Component | Status | Impact | Priority |
|-----------|--------|--------|----------|
| **BroadcastMessage() Call** | ❌ MISSING | **CRITICAL** - Relay doesn't work | P0 |
| **CodecDetector Instance** | ❌ MISSING | Codec info not detected/logged | P0 |
| **Sequence Header Caching** | ❌ MISSING | New subscribers don't get init data | P1 |
| **Publisher Disconnect Cleanup** | ⚠️ PARTIAL | Subscribers not notified on EOF | P1 |
| **Integration Tests** | ❌ MISSING | No end-to-end relay validation | P0 |
| **Performance Metrics** | ❌ MISSING | No monitoring/observability | P2 |
| **Connection State Tracking** | ⚠️ PARTIAL | commandState not linked to stream | P1 |

---

## Detailed Gap Analysis

### **Gap 1: BroadcastMessage() Never Called (CRITICAL)** 🔴

**Location**: `internal/rtmp/server/command_integration.go:146-157`

**Current Code**:
```go
// Process media packets (audio/video) through MediaLogger
if m.TypeID == 8 || m.TypeID == 9 {
    st.mediaLogger.ProcessMessage(m)

    // Write to recorder if recording is active
    if st.streamKey != "" {
        stream := reg.GetStream(st.streamKey)
        if stream != nil && stream.Recorder != nil {
            stream.Recorder.WriteMessage(m)
        }
    }

    return // ⚠️ Returns without broadcasting to subscribers!
}
```

**Problem**: 
- Media messages are logged and recorded
- **But never broadcast to subscribers** 
- `stream.BroadcastMessage()` is never called
- Result: Subscribers receive nothing

**Impact**: **Relay feature completely non-functional**

---

### **Gap 2: No CodecDetector Instance** 🔴

**Problem**: `BroadcastMessage(detector *CodecDetector, ...)` requires a detector, but none exists in production code.

**Current**: 
- Tests use `&CodecDetector{}` 
- Production code has no detector instantiation

**Impact**: Codec detection doesn't work in production

---

### **Gap 3: Sequence Headers Not Cached** 🟡

**Background**: When a new subscriber joins an active stream, they need:
1. **Audio Sequence Header** (AAC AudioSpecificConfig)
2. **Video Sequence Header** (H.264 SPS/PPS in AVCDecoderConfigurationRecord)

**Current Behavior**: 
- First frames detected and logged
- But NOT cached for late joiners

**Problem**: New subscribers joining mid-stream:
- Miss sequence headers
- Can't initialize decoders
- Get corrupted video/audio

**Expected Behavior**: Server should:
1. Cache first audio/video sequence headers
2. Send cached headers to new subscribers immediately on play
3. Then forward live frames

---

### **Gap 4: Publisher Disconnect Not Handled** 🟡

**Current**: 
- `PublisherDisconnected()` exists but only clears publisher reference
- Subscribers not notified
- No EOF or stream end message sent

**Expected**: On publisher disconnect:
1. Send `StreamEOF` User Control message to all subscribers
2. Send `onStatus` NetStream.Play.UnpublishNotify
3. Remove stream from registry (optional, configurable)
4. Close recorder if active

---

### **Gap 5: No Integration Tests for Relay** 🔴

**Current Tests**:
- ✅ Unit tests: `internal/rtmp/media/relay_test.go` (3 tests)
  - `TestRelaySingleSubscriber`
  - `TestRelayMultipleSubscribers`
  - `TestRelaySlowSubscriberDropped`
- ❌ No integration tests for full publish → relay → play flow

**Missing**:
- End-to-end test: FFmpeg publish → Server → ffplay
- Concurrent subscribers test
- Late joiner test (join mid-stream)
- Publisher disconnect test
- Codec detection validation test

---

### **Gap 6: Connection State Not Linked to Stream** 🟡

**Problem**: `commandState` tracks `streamKey`, but no reverse link from Stream to publisher connection.

**Current**:
```go
type commandState struct {
    app         string
    streamKey   string  // Tracks publishing stream
    allocator   *rpc.StreamIDAllocator
    mediaLogger *MediaLogger
    // ⚠️ Missing: codecDetector, sequenceHeaders
}
```

**Impact**: 
- Can't track publisher connection properly
- Cleanup on disconnect incomplete

---

## Implementation Plan

### Phase 1: Critical Fixes (P0) - Enable Basic Relay

**Goal**: Make relay functional for basic publish → play scenarios

#### Task 1.1: Add BroadcastMessage() Call

**File**: `internal/rtmp/server/command_integration.go`

**Changes**:
```go
// Process media packets (audio/video)
if m.TypeID == 8 || m.TypeID == 9 {
    st.mediaLogger.ProcessMessage(m)

    // Get stream and broadcast to subscribers
    if st.streamKey != "" {
        stream := reg.GetStream(st.streamKey)
        if stream != nil {
            // Write to recorder if active
            if stream.Recorder != nil {
                stream.Recorder.WriteMessage(m)
            }
            
            // 🆕 BROADCAST TO SUBSCRIBERS
            if st.codecDetector == nil {
                st.codecDetector = &media.CodecDetector{}
            }
            stream.BroadcastMessage(st.codecDetector, m, log)
        }
    }

    return
}
```

**Test**:
```powershell
# Terminal 1: Start server
.\rtmp-server.exe -listen :1935

# Terminal 2: Publish
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test

# Terminal 3: Play (should now work!)
ffplay rtmp://localhost:1935/live/test
```

**Acceptance Criteria**:
- ✅ ffplay receives video/audio
- ✅ Playback starts within 5 seconds
- ✅ Multiple players can watch simultaneously

---

#### Task 1.2: Add CodecDetector to commandState

**File**: `internal/rtmp/server/command_integration.go`

**Changes**:
```go
type commandState struct {
    app           string
    streamKey     string
    allocator     *rpc.StreamIDAllocator
    mediaLogger   *MediaLogger
    codecDetector *media.CodecDetector  // 🆕 ADD THIS
}

func attachCommandHandling(...) {
    st := &commandState{
        allocator:     rpc.NewStreamIDAllocator(),
        mediaLogger:   NewMediaLogger(c.ID(), log, 30*time.Second),
        codecDetector: media.NewCodecDetector(),  // 🆕 INITIALIZE
    }
    // ...
}
```

**Acceptance Criteria**:
- ✅ Codec detection logs appear: `"Codec detected","video":"H.264 AVC","audio":"AAC"`
- ✅ Stream registry populates `VideoCodec` and `AudioCodec` fields

---

#### Task 1.3: Store Codec Info in Registry Stream

**File**: `internal/rtmp/server/command_integration.go`

**Changes**:
```go
// In BroadcastMessage, after codec detection:
stream.BroadcastMessage(st.codecDetector, m, log)

// 🆕 Sync codec info to registry stream
stream.mu.Lock()
stream.VideoCodec = stream.Recorder.GetVideoCodec()  // Wait, Recorder doesn't have this
stream.AudioCodec = stream.Recorder.GetAudioCodec()
stream.mu.Unlock()
```

**Actually, need to check**: Does `BroadcastMessage` already update the stream codecs? Let me check the relay.go implementation...

**From relay.go**: Uses `CodecStore` interface - `stream` implements:
- `SetAudioCodec(c string)`
- `SetVideoCodec(c string)`

**So codec detection already works IF we pass the stream correctly!**

**Issue**: `internal/rtmp/media/relay.go` has its own minimal `Stream` struct. The real `server.Stream` doesn't implement `CodecStore`!

**Fix Needed**: Make `server.Stream` implement `CodecStore` interface.

**File**: `internal/rtmp/server/registry.go`

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

**Acceptance Criteria**:
- ✅ `server.Stream` implements `media.CodecStore`
- ✅ Codec fields populated correctly
- ✅ Can query stream codecs via API

---

### Phase 2: Sequence Header Caching (P1)

#### Task 2.1: Add Sequence Header Storage

**File**: `internal/rtmp/server/registry.go`

```go
type Stream struct {
    Key              string
    Publisher        interface{}
    Subscribers      []media.Subscriber
    Metadata         map[string]interface{}
    VideoCodec       string
    AudioCodec       string
    StartTime        time.Time
    Recorder         *media.Recorder
    
    // 🆕 ADD THESE
    AudioSeqHeader   []byte  // AAC AudioSpecificConfig
    VideoSeqHeader   []byte  // H.264 AVCDecoderConfigurationRecord
    
    mu sync.RWMutex
}
```

---

#### Task 2.2: Cache Sequence Headers on First Frame

**File**: `internal/rtmp/server/command_integration.go`

```go
// In media message handler, before broadcast:
if m.TypeID == 8 || m.TypeID == 9 {
    st.mediaLogger.ProcessMessage(m)

    if st.streamKey != "" {
        stream := reg.GetStream(st.streamKey)
        if stream != nil {
            // 🆕 Cache sequence headers
            if m.TypeID == 8 && len(m.Payload) >= 2 {
                if m.Payload[0]&0xF0 == 0xA0 && m.Payload[1] == 0x00 {
                    // AAC Sequence Header
                    stream.mu.Lock()
                    if stream.AudioSeqHeader == nil {
                        stream.AudioSeqHeader = make([]byte, len(m.Payload))
                        copy(stream.AudioSeqHeader, m.Payload)
                        log.Info("cached audio sequence header", "stream_key", st.streamKey, "len", len(m.Payload))
                    }
                    stream.mu.Unlock()
                }
            }
            
            if m.TypeID == 9 && len(m.Payload) >= 2 {
                if m.Payload[0] == 0x17 && m.Payload[1] == 0x00 {
                    // AVC Sequence Header
                    stream.mu.Lock()
                    if stream.VideoSeqHeader == nil {
                        stream.VideoSeqHeader = make([]byte, len(m.Payload))
                        copy(stream.VideoSeqHeader, m.Payload)
                        log.Info("cached video sequence header", "stream_key", st.streamKey, "len", len(m.Payload))
                    }
                    stream.mu.Unlock()
                }
            }
            
            // Broadcast + record
            if stream.Recorder != nil {
                stream.Recorder.WriteMessage(m)
            }
            stream.BroadcastMessage(st.codecDetector, m, log)
        }
    }
    return
}
```

---

#### Task 2.3: Send Sequence Headers to New Subscribers

**File**: `internal/rtmp/server/play_handler.go`

```go
func HandlePlay(reg *Registry, conn sender, app string, msg *chunk.Message) (*chunk.Message, error) {
    // ... existing code ...
    
    stream.AddSubscriber(conn.(interface{ SendMessage(*chunk.Message) error }))
    
    // 🆕 Send cached sequence headers immediately
    stream.mu.RLock()
    audioSeq := stream.AudioSeqHeader
    videoSeq := stream.VideoSeqHeader
    stream.mu.RUnlock()
    
    // Send audio sequence header first
    if audioSeq != nil {
        audioMsg := &chunk.Message{
            TypeID:          8,
            CSID:            chunk.DefaultCSID_Audio,
            MessageStreamID: streamID,
            Timestamp:       0,
            Payload:         audioSeq,
            MessageLength:   uint32(len(audioSeq)),
        }
        if err := conn.SendMessage(audioMsg); err != nil {
            // Log but continue
        }
    }
    
    // Send video sequence header second
    if videoSeq != nil {
        videoMsg := &chunk.Message{
            TypeID:          9,
            CSID:            chunk.DefaultCSID_Video,
            MessageStreamID: streamID,
            Timestamp:       0,
            Payload:         videoSeq,
            MessageLength:   uint32(len(videoSeq)),
        }
        if err := conn.SendMessage(videoMsg); err != nil {
            // Log but continue
        }
    }
    
    return statusMsg, nil
}
```

**Acceptance Criteria**:
- ✅ Late joiner gets video immediately (no waiting for keyframe)
- ✅ ffplay starts playback within 1-2 seconds
- ✅ No decoder errors in ffplay logs

---

### Phase 3: Integration Tests (P0)

#### Task 3.1: Create Relay Integration Test

**File**: `tests/integration/relay_test.go` (NEW)

```go
package integration

import (
    "testing"
    "time"
    "net"
    // ... imports
)

// TestPublishToPlayRelay validates end-to-end relay:
// 1. Start server
// 2. Mock publisher sends audio/video
// 3. Mock player receives same messages
func TestPublishToPlayRelay(t *testing.T) {
    // Start server
    server := startTestServer(t, ":19350")
    defer server.Stop()
    
    // Connect publisher
    pubConn := dialAndHandshake(t, ":19350")
    defer pubConn.Close()
    
    // Send connect/createStream/publish commands
    sendConnect(t, pubConn, "live")
    streamID := sendCreateStream(t, pubConn)
    sendPublish(t, pubConn, streamID, "test")
    
    // Send sequence headers
    sendAudioSeqHeader(t, pubConn, streamID)
    sendVideoSeqHeader(t, pubConn, streamID)
    
    // Connect player
    playConn := dialAndHandshake(t, ":19350")
    defer playConn.Close()
    
    // Send connect/createStream/play commands
    sendConnect(t, playConn, "live")
    playStreamID := sendCreateStream(t, playConn)
    sendPlay(t, playConn, playStreamID, "test")
    
    // Player should receive sequence headers immediately
    audioMsg := readMessage(t, playConn, 2*time.Second)
    if audioMsg.TypeID != 8 {
        t.Fatalf("expected audio message, got type %d", audioMsg.TypeID)
    }
    
    videoMsg := readMessage(t, playConn, 2*time.Second)
    if videoMsg.TypeID != 9 {
        t.Fatalf("expected video message, got type %d", videoMsg.TypeID)
    }
    
    // Publisher sends more frames
    sendAudioFrame(t, pubConn, streamID, []byte{0xAF, 0x01, 0xAA, 0xBB})
    sendVideoFrame(t, pubConn, streamID, []byte{0x17, 0x01, 0x00, 0x00, 0x00, 0xCC, 0xDD})
    
    // Player should receive them
    frame1 := readMessage(t, playConn, 2*time.Second)
    frame2 := readMessage(t, playConn, 2*time.Second)
    
    // Validate
    if frame1.TypeID != 8 && frame1.TypeID != 9 {
        t.Fatalf("expected media frame, got type %d", frame1.TypeID)
    }
}
```

---

#### Task 3.2: Multiple Subscribers Test

**File**: `tests/integration/relay_test.go`

```go
func TestRelayMultipleSubscribers(t *testing.T) {
    server := startTestServer(t, ":19351")
    defer server.Stop()
    
    // 1 publisher
    pub := setupPublisher(t, ":19351", "live", "multi")
    defer pub.Close()
    
    // 3 players
    play1 := setupPlayer(t, ":19351", "live", "multi")
    play2 := setupPlayer(t, ":19351", "live", "multi")
    play3 := setupPlayer(t, ":19351", "live", "multi")
    defer play1.Close()
    defer play2.Close()
    defer play3.Close()
    
    // Publisher sends 10 frames
    for i := 0; i < 10; i++ {
        sendAudioFrame(t, pub, 1, []byte{0xAF, 0x01, byte(i)})
    }
    
    // Each player should receive all 10 frames
    for i := 1; i <= 3; i++ {
        var conn net.Conn
        switch i {
        case 1: conn = play1
        case 2: conn = play2
        case 3: conn = play3
        }
        
        for j := 0; j < 10; j++ {
            msg := readMessage(t, conn, 2*time.Second)
            if msg.TypeID != 8 {
                t.Fatalf("player %d frame %d: wrong type %d", i, j, msg.TypeID)
            }
        }
    }
}
```

---

#### Task 3.3: Late Joiner Test

**File**: `tests/integration/relay_test.go`

```go
func TestRelayLateJoiner(t *testing.T) {
    server := startTestServer(t, ":19352")
    defer server.Stop()
    
    pub := setupPublisher(t, ":19352", "live", "late")
    defer pub.Close()
    
    // Publisher sends sequence headers + 20 frames
    sendAudioSeqHeader(t, pub, 1)
    sendVideoSeqHeader(t, pub, 1)
    for i := 0; i < 20; i++ {
        sendVideoFrame(t, pub, 1, makeVideoFrame(i))
    }
    
    // Late joiner connects AFTER frames already sent
    latePlayer := setupPlayer(t, ":19352", "live", "late")
    defer latePlayer.Close()
    
    // Should immediately get sequence headers (cached)
    audioSeq := readMessage(t, latePlayer, 2*time.Second)
    if audioSeq.TypeID != 8 || len(audioSeq.Payload) < 2 || audioSeq.Payload[1] != 0x00 {
        t.Fatal("late joiner didn't receive audio sequence header")
    }
    
    videoSeq := readMessage(t, latePlayer, 2*time.Second)
    if videoSeq.TypeID != 9 || len(videoSeq.Payload) < 2 || videoSeq.Payload[1] != 0x00 {
        t.Fatal("late joiner didn't receive video sequence header")
    }
    
    // Then receive live frames
    liveFrame := readMessage(t, latePlayer, 2*time.Second)
    if liveFrame.TypeID != 9 {
        t.Fatal("late joiner didn't receive live frames")
    }
}
```

---

### Phase 4: Publisher Disconnect Handling (P1)

#### Task 4.1: Notify Subscribers on Publisher Disconnect

**File**: `internal/rtmp/server/publish_handler.go`

```go
func PublisherDisconnected(reg *Registry, streamKey string, pub sender, log *slog.Logger) {
    if reg == nil || streamKey == "" || pub == nil {
        return
    }
    s := reg.GetStream(streamKey)
    if s == nil {
        return
    }
    
    s.mu.Lock()
    if s.Publisher != pub {
        s.mu.Unlock()
        return  // Not this publisher
    }
    s.Publisher = nil
    
    // 🆕 Notify all subscribers
    subscribers := make([]media.Subscriber, len(s.Subscribers))
    copy(subscribers, s.Subscribers)
    s.mu.Unlock()
    
    // Send UnpublishNotify to each subscriber
    for _, sub := range subscribers {
        if sender, ok := sub.(sender); ok {
            statusMsg := buildUnpublishNotify(streamKey)
            if err := sender.SendMessage(statusMsg); err != nil {
                log.Warn("failed to send unpublish notify", "error", err)
            }
        }
    }
    
    log.Info("publisher disconnected, subscribers notified", "stream_key", streamKey, "subscriber_count", len(subscribers))
}

func buildUnpublishNotify(streamKey string) *chunk.Message {
    // Build onStatus NetStream.Play.UnpublishNotify
    // ...
}
```

---

### Phase 5: Observability & Metrics (P2)

#### Task 5.1: Add Relay Metrics

**File**: `internal/rtmp/server/metrics.go` (NEW)

```go
package server

type RelayMetrics struct {
    StreamCount         int64  // Active streams
    PublisherCount      int64  // Active publishers
    SubscriberCount     int64  // Total subscribers
    MessagesRelayed     int64  // Total messages broadcast
    MessagesDropped     int64  // Messages dropped (backpressure)
    BytesRelayed        int64  // Total bytes broadcast
}

func (s *Server) GetMetrics() RelayMetrics {
    // Collect from registry
}
```

---

## Testing Strategy

### Unit Tests (Per Task)

Each implementation task includes unit tests:
- `command_integration_test.go`: Test BroadcastMessage() call
- `registry_test.go`: Test sequence header caching
- `play_handler_test.go`: Test sequence header sending

### Integration Tests (Phase 3)

End-to-end scenarios:
1. **Basic Relay**: 1 publisher → 1 subscriber
2. **Multi-Subscriber**: 1 publisher → 3 subscribers
3. **Late Joiner**: Join mid-stream, get cached headers
4. **Publisher Disconnect**: Subscribers notified
5. **Concurrent**: Multiple streams simultaneously

### Interop Tests (FFmpeg/ffplay)

From `tests/interop/`:
```powershell
# Test with real tools
.\ffmpeg_test.ps1 -Tests PublishAndPlay,Concurrency,Recording
```

---

## Acceptance Criteria (Overall)

### Functional Requirements

✅ **FR-001**: Publisher can publish audio/video stream  
✅ **FR-002**: Multiple subscribers can watch same stream  
✅ **FR-003**: Late joiners receive immediate playback (seq headers)  
✅ **FR-004**: Slow subscribers don't block fast subscribers  
✅ **FR-005**: Publisher disconnect notifies subscribers  
✅ **FR-006**: Codec detection logs appear correctly  
✅ **FR-007**: Recording works alongside relay  

### Performance Requirements

✅ **PR-001**: Latency < 5 seconds (publisher → subscriber)  
✅ **PR-002**: Supports 10-50 concurrent connections  
✅ **PR-003**: CPU usage < 50% at 10 streams  
✅ **PR-004**: Memory stable (no leaks over 10 minutes)  

### Quality Requirements

✅ **QR-001**: Unit test coverage > 80%  
✅ **QR-002**: Integration tests pass with FFmpeg/ffplay  
✅ **QR-003**: No race conditions (verified with `-race`)  
✅ **QR-004**: Error handling for all failure modes  

---

## Execution Order

### Week 1: Critical Path (P0)

**Day 1-2**: Phase 1 (Tasks 1.1-1.3) - Enable basic relay  
**Day 3-4**: Phase 3 (Tasks 3.1-3.3) - Integration tests  
**Day 5**: Testing & validation with FFmpeg/ffplay  

### Week 2: Polish (P1)

**Day 1-2**: Phase 2 (Tasks 2.1-2.3) - Sequence header caching  
**Day 3-4**: Phase 4 (Task 4.1) - Disconnect handling  
**Day 5**: Performance testing & optimization  

### Week 3: Observability (P2)

**Day 1-2**: Phase 5 (Task 5.1) - Metrics  
**Day 3-5**: Documentation & final validation  

---

## Risk Assessment

| Risk | Probability | Impact | Mitigation |
|------|------------|--------|------------|
| **BroadcastMessage breaks chunking** | Low | High | Extensive unit tests, validate with tcpdump |
| **Sequence headers corrupt** | Medium | High | Golden test vectors, FFmpeg validation |
| **Race conditions in broadcast** | Low | High | Run all tests with `-race` flag |
| **Performance degradation** | Medium | Medium | Benchmark before/after, profile with pprof |
| **Backward compatibility** | Low | Low | Existing tests continue to pass |

---

## Definition of Done

✅ All P0 tasks completed  
✅ All integration tests pass  
✅ FFmpeg publish → ffplay works reliably  
✅ No race conditions detected  
✅ Code reviewed and documented  
✅ Performance validated (10 concurrent streams)  
✅ feature002-rtmp-relay.md updated  

---

## References

- **Feature Spec**: `specs/001-rtmp-server-implementation/spec.md`
- **Data Model**: `specs/001-rtmp-server-implementation/data-model.md`
- **Media Contracts**: `specs/001-rtmp-server-implementation/contracts/media.md`
- **Existing Tests**: `internal/rtmp/media/relay_test.go`
- **Documentation**: `feature002-rtmp-relay.md`

---

**Next Steps**: 
1. Review this plan with team
2. Create GitHub issues for each task
3. Start with Task 1.1 (highest priority)
4. Test incrementally after each task

---

**Last Updated**: October 11, 2025  
**Status**: Ready for implementation  
**Estimated Effort**: 2-3 weeks (1 developer)

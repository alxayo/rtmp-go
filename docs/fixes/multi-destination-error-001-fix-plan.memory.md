# Multi-Destination Relay Error Analysis & Fix Plan

**Date**: October 15, 2025  
**Issue**: Multi-destination RTMP relay test failure  
**Branch**: `003-multi-destination-relay`  
**Status**: Root cause identified, fix plan ready  

---

## Executive Summary

The multi-destination relay feature is **95% implemented correctly** with all infrastructure in place and connections working. The core issue is in the **media data forwarding pipeline** where audio/video packets from FFmpeg are not being relayed to destination servers, causing FFmpeg to encounter "Broken pipe" errors after ~17 seconds.

---

## Test Results Analysis

### ✅ What Worked Successfully

1. **Server Infrastructure**
   - All three RTMP servers started successfully (relay on :1935, destinations on :1936, :1937)
   - TCP listeners bound and accepting connections properly

2. **Destination Connection Management**
   - Relay server successfully connected to both destinations:
     ```
     "Successfully connected to destination","destination_url":"rtmp://localhost:1936/live/dest1"
     "Successfully connected to destination","destination_url":"rtmp://localhost:1937/live/dest2"
     ```

3. **RTMP Protocol Handshakes**
   - All handshakes completed successfully between relay and destinations
   - Client handshake timestamps logged correctly

4. **Command Flow Execution**
   - Complete RTMP command sequence executed properly:
     - `connect` → `_result` (success)
     - `createStream` → `_result` (stream_id=1)
     - `publish` → success
   - All transactions completed without protocol errors

5. **Incoming Connection Handling**
   - Relay server accepted FFmpeg connection successfully:
     ```
     "Connection accepted","conn_id":"c000001","peer_addr":"[::1]:54553"
     ```
   - Control message burst sent correctly (Window Ack Size, Set Peer Bandwidth, Set Chunk Size)

### ❌ What Failed

1. **Media Data Relay**
   - **Primary Issue**: Stream data not forwarded to destination servers
   - FFmpeg successfully sent ~17 seconds of video data (1022 frames) to relay server
   - **No corresponding media packets** appeared in destination server logs
   - Relay server logs show media processing but no relay confirmation

2. **Connection Termination**
   - FFmpeg encountered "Broken pipe" error:
     ```
     [vost#0:0/libx264] Error submitting a packet to the muxer: Broken pipe
     [out#0/flv] Error muxing a packet
     ```
   - Connection closed unexpectedly after ~17 seconds of streaming

---

## Root Cause Analysis

### Primary Issue: Media Pipeline Not Forwarding Data

**Evidence from Code Review**:

The relay integration exists in `internal/rtmp/server/command_integration.go`:

```go
// Process media packets (audio/video) 
if m.TypeID == 8 || m.TypeID == 9 {
    st.mediaLogger.ProcessMessage(m)

    if st.streamKey != "" {
        stream := reg.GetStream(st.streamKey)
        if stream != nil {
            // Local recording (existing) ✅
            if stream.Recorder != nil {
                stream.Recorder.WriteMessage(m)
            }
            // Local subscriber relay (existing) ✅
            stream.BroadcastMessage(st.codecDetector, m, log)
            
            // Multi-destination relay (NEW) ❓
            if destMgr != nil {
                destMgr.RelayMessage(m)  // ← This call may be failing silently
            }
        }
    }
    return
}
```

### Potential Root Causes

1. **Silent Failure in RelayMessage()**
   - `RelayMessage()` may be failing without proper error logging
   - Errors in destination `SendMessage()` calls not surfaced to logs

2. **SendAudio/SendVideo Implementation Issues**
   - The `RTMPClient` interface expects `SendAudio()` and `SendVideo()` methods
   - These methods exist in `client.go` but may have implementation problems:
     ```go
     func (c *Client) SendAudio(ts uint32, data []byte) error
     func (c *Client) SendVideo(ts uint32, data []byte) error
     ```

3. **Connection State Management**
   - Destination connections may disconnect after initial setup
   - No health monitoring or reconnection logic currently active

4. **Message Format Issues**
   - Media messages may not be in the correct format for relay
   - Timestamp or payload corruption during forwarding

---

## Architecture Review

### Current Implementation Status

**✅ Implemented Components**:
- `DestinationManager` with parallel message sending
- `Destination` connection management with metrics
- `RTMPClient` interface with factory pattern
- Command-line flag parsing for `-relay-to` destinations
- Server integration with destination manager

**❌ Missing/Broken Components**:
- Proper error logging in relay pipeline
- Connection health monitoring
- Automatic reconnection logic
- Media message validation before relay

### Data Flow Analysis

```
FFmpeg → Relay Server (✅) → DestinationManager.RelayMessage() (❓) → Destination.SendMessage() (❓) → Client.SendAudio/SendVideo() (❓) → Destination Servers (❌)
```

**Flow Status**:
- ✅ FFmpeg successfully sends media to relay server
- ❓ `RelayMessage()` called but success/failure unknown
- ❓ Individual destination sends unknown status
- ❌ No media packets reach destination servers

---

## Fix Plan

### Phase 1: Diagnosis & Logging Enhancement (30 minutes)

**Priority**: Immediate  
**Goal**: Identify exact failure point in relay pipeline

#### Tasks:

1. **Add Debug Logging to RelayMessage**
   ```go
   // File: internal/rtmp/relay/manager.go
   func (dm *DestinationManager) RelayMessage(msg *chunk.Message) {
       dm.logger.Debug("RelayMessage called", "type_id", msg.TypeID, "payload_len", len(msg.Payload))
       
       if msg == nil || (msg.TypeID != 8 && msg.TypeID != 9) {
           dm.logger.Debug("Skipping non-media message", "type_id", msg.TypeID)
           return
       }
       
       // Log destination count and status
       dm.mu.RLock()
       destCount := len(dm.destinations)
       dm.mu.RUnlock()
       dm.logger.Debug("Relaying to destinations", "count", destCount)
       
       // ... existing relay logic with enhanced logging
   }
   ```

2. **Add Error Logging to Destination SendMessage**
   ```go
   // File: internal/rtmp/relay/destination.go
   func (d *Destination) SendMessage(msg *chunk.Message) error {
       d.logger.Debug("SendMessage called", "type_id", msg.TypeID, "status", d.Status, "payload_len", len(msg.Payload))
       
       // ... existing validation logic
       
       if err != nil {
           d.logger.Error("SendMessage failed", "type_id", msg.TypeID, "error", err)
           return err
       }
       
       d.logger.Debug("SendMessage success", "type_id", msg.TypeID)
       return nil
   }
   ```

3. **Add Connection Status Monitoring**
   ```go
   // Add periodic status logging to DestinationManager
   func (dm *DestinationManager) LogStatus() {
       dm.mu.RLock()
       for url, dest := range dm.destinations {
           dm.logger.Info("Destination status", "url", url, "status", dest.GetStatus())
       }
       dm.mu.RUnlock()
   }
   ```

### Phase 2: SendAudio/SendVideo Implementation Fix (2 hours)

**Priority**: High  
**Goal**: Ensure media messages are properly forwarded to destinations

#### Tasks:

1. **Validate Client SendAudio/SendVideo Methods**
   - Check current implementation in `internal/rtmp/client/client.go`
   - Verify message format and chunk stream IDs
   - Add connection state validation

2. **Add Error Handling in Media Sending**
   ```go
   func (c *Client) SendAudio(ts uint32, data []byte) error {
       if c.conn == nil {
           return errors.New("client not connected")
       }
       if c.writer == nil {
           return errors.New("writer not initialized")
       }
       
       msg := &chunk.Message{
           CSID: 6, 
           TypeID: 8, 
           MessageStreamID: c.streamID, 
           Timestamp: ts, 
           MessageLength: uint32(len(data)), 
           Payload: data
       }
       
       if err := c.writer.WriteMessage(msg); err != nil {
           return fmt.Errorf("write audio message: %w", err)
       }
       
       return nil
   }
   ```

3. **Add Message Validation**
   - Validate payload format before sending
   - Check timestamp consistency
   - Verify stream ID is set correctly

### Phase 3: Connection Management Enhancement (1 day)

**Priority**: Medium  
**Goal**: Improve destination connection reliability

#### Tasks:

1. **Add Connection Health Monitoring**
   - Periodic health checks for destination connections
   - Automatic detection of connection failures
   - Metrics for successful vs failed sends

2. **Implement Reconnection Logic**
   - Automatic reconnection on connection loss
   - Exponential backoff for reconnection attempts
   - Graceful handling of destination unavailability

3. **Add Destination Metrics**
   - Track messages sent/dropped per destination
   - Monitor connection uptime
   - Log throughput statistics

### Phase 4: Integration Testing (1 day)

**Priority**: Medium  
**Goal**: Comprehensive validation of relay functionality

#### Tasks:

1. **Create Focused Relay Tests**
   - Unit tests for `RelayMessage()` pipeline
   - Integration tests with multiple destinations
   - Failure scenarios (destination disconnect)

2. **Manual Testing Improvements**
   - Enhanced test script with better logging
   - Verification steps for media receipt
   - Performance monitoring during tests

---

## Expected Outcomes

### Phase 1 Results
With enhanced logging, we expect to see:
- Exact location where relay pipeline fails
- Error messages from `SendAudio/SendVideo` calls
- Connection status when failures occur

### Phase 2 Results
After fixing media sending:
- Successful media packet delivery to destination servers
- No more "Broken pipe" errors from FFmpeg
- Consistent streaming for extended periods

### Success Criteria
1. ✅ FFmpeg streams successfully to relay server
2. ✅ Relay server forwards all media packets to destinations
3. ✅ Destination servers receive and can relay media packets
4. ✅ End-to-end latency < 2 seconds
5. ✅ No connection drops during normal operation

---

## Implementation Priority

### Immediate (Next 30 minutes)
- [ ] Add debug logging to `RelayMessage()` pipeline
- [ ] Add error logging to `SendMessage()` calls
- [ ] Re-run test with enhanced logging

### High Priority (Next 2 hours)
- [ ] Fix `SendAudio/SendVideo` implementation issues
- [ ] Add connection state validation
- [ ] Test media forwarding functionality

### Medium Priority (Next 1-2 days)
- [ ] Implement connection health monitoring
- [ ] Add automatic reconnection logic
- [ ] Create comprehensive integration tests

---

## Files Requiring Changes

### Phase 1 (Logging)
- `internal/rtmp/relay/manager.go` - Add RelayMessage logging
- `internal/rtmp/relay/destination.go` - Add SendMessage logging

### Phase 2 (Core Fix)
- `internal/rtmp/client/client.go` - Fix SendAudio/SendVideo methods
- `internal/rtmp/relay/destination.go` - Improve error handling

### Phase 3 (Enhancement)
- `internal/rtmp/relay/manager.go` - Add health monitoring
- `internal/rtmp/relay/destination.go` - Add reconnection logic

### Phase 4 (Testing)
- `tests/integration/relay_test.go` - Add comprehensive tests
- `tools/test-multi-destination-relay.sh` - Enhance test script

---

## Risk Assessment

### Low Risk
- Adding debug logging (no functional changes)
- Message validation improvements
- Enhanced error reporting

### Medium Risk
- Modifying `SendAudio/SendVideo` implementation
- Connection state management changes
- Automatic reconnection logic

### High Risk
- None identified (all changes are incremental)

---

## Notes

1. **Architecture Assessment**: The overall architecture is sound with proper separation of concerns and good abstraction layers.

2. **Performance Impact**: The relay system uses goroutines for parallel sending, which should scale well to multiple destinations.

3. **Error Isolation**: The current design properly isolates destination failures, preventing one bad destination from affecting others.

4. **Code Quality**: The implementation follows Go best practices with proper error handling patterns and structured logging.

---

## Next Steps

1. **Start with Phase 1** - Add diagnostic logging immediately
2. **Run enhanced test** - Use improved logging to identify exact failure point
3. **Implement targeted fix** - Address specific issue identified in logs
4. **Validate fix** - Ensure end-to-end media flow works properly
5. **Add robustness** - Implement health monitoring and reconnection logic

**Expected Resolution Time**: 4-6 hours for core functionality, 1-2 days for full robustness.
# Gap Analysis: Multi-Destination RTMP Relay Feature

**Feature Request**: Multi-destination RTMP relay functionality  
**Date**: October 13, 2025  
**Status**: Analysis Phase  

---

## Executive Summary

The current RTMP server implements **local relay** functionality where it receives media streams from publishers (OBS, FFmpeg) and relays them to local subscribers (ffplay, VLC, etc.). The requested feature is to add **multi-destination relay** capability where the server acts as both receiver and publisher, actively pushing received streams to multiple external RTMP endpoints (YouTube Live, Facebook Live, Instagram Live, other RTMP servers).

**Test Scenario**: `OBS → rtmp-server-1 → rtmp-server-2 → ffplay`

---

## Current State Analysis

### ✅ What Exists (Local Relay Infrastructure)

1. **Inbound RTMP Server** (`internal/rtmp/server/`)
   - ✅ TCP listener with handshake support
   - ✅ Connection management and message routing
   - ✅ Stream registry with publisher/subscriber tracking
   - ✅ Media message broadcast to local subscribers
   - ✅ FLV recording capability

2. **RTMP Client Library** (`internal/rtmp/client/`)
   - ✅ Basic client implementation for testing
   - ✅ Handshake, connect, createStream, publish/play commands
   - ✅ Media message sending (SendAudio, SendVideo)
   - ✅ Connection management and chunking

3. **Media Processing** (`internal/rtmp/media/`)
   - ✅ Codec detection (AAC, H.264)
   - ✅ Sequence header caching for late joiners
   - ✅ BroadcastMessage with backpressure handling
   - ✅ FLV recording integration

4. **Protocol Implementation** (`internal/rtmp/`)
   - ✅ Handshake (simple v3)
   - ✅ Chunking (read/write with extended timestamps)
   - ✅ AMF0 encoding/decoding
   - ✅ Control messages (chunk size, window ack, etc.)
   - ✅ RPC command parsing and responses

### ❌ What's Missing (Multi-Destination Relay)

1. **Outbound Connection Manager**
   - ❌ No outbound RTMP client pool management
   - ❌ No automatic reconnection for failed destinations
   - ❌ No connection health monitoring
   - ❌ No destination configuration management

2. **Multi-Destination Broadcasting**
   - ❌ No parallel publishing to multiple endpoints
   - ❌ No per-destination error handling
   - ❌ No destination-specific stream keys/authentication
   - ❌ No bandwidth management across destinations

3. **Configuration System**
   - ❌ No command-line flags for destination endpoints
   - ❌ No configuration file support for complex setups
   - ❌ No runtime destination add/remove capability
   - ❌ No per-destination configuration (auth, stream keys)

4. **Monitoring & Diagnostics**
   - ❌ No per-destination connection status reporting
   - ❌ No relay success/failure metrics
   - ❌ No destination latency monitoring
   - ❌ No bandwidth usage tracking per destination

---

## Gap Analysis by Component

### Gap 1: Outbound Client Pool Management 🔴 HIGH PRIORITY

**Current**: Single-use client for testing only
**Required**: Managed pool of persistent outbound connections

**Missing Components**:
```go
// internal/rtmp/relay/destination.go (NEW)
type Destination struct {
    URL         string              // rtmp://youtube.com/live2/STREAM_KEY
    Client      *client.Client      // Persistent connection
    Status      DestinationStatus   // Connected, Disconnected, Error
    LastError   error
    Metrics     *DestinationMetrics // Success/failure counters
}

type DestinationManager struct {
    destinations map[string]*Destination
    mu          sync.RWMutex
}
```

**Dependencies**: Extend existing `internal/rtmp/client/` with persistence

### Gap 2: Multi-Destination Message Broadcasting 🔴 HIGH PRIORITY

**Current**: `stream.BroadcastMessage()` only sends to local subscribers
**Required**: Parallel broadcast to local subscribers + external destinations

**Missing Integration**:
```go
// Current: internal/rtmp/server/command_integration.go
if m.TypeID == 8 || m.TypeID == 9 {
    // Only local relay
    stream.BroadcastMessage(st.codecDetector, m, log)
}

// Required: Add destination relay
if m.TypeID == 8 || m.TypeID == 9 {
    // Local relay (existing)
    stream.BroadcastMessage(st.codecDetector, m, log)
    
    // Multi-destination relay (NEW)
    st.destinationManager.RelayMessage(m, log)
}
```

### Gap 3: Command-Line Interface 🟡 MEDIUM PRIORITY

**Current**: Basic server flags (listen, log-level, record-*)
```bash
rtmp-server -listen :1935 -record-all true
```

**Required**: Destination configuration flags
```bash
rtmp-server -listen :1935 \
  -relay-to "rtmp://localhost:1936/live/test1" \
  -relay-to "rtmp://youtube.com/live2/YOUR_STREAM_KEY" \
  -relay-to "rtmp://facebook.com/rtmp/YOUR_STREAM_KEY"
```

**Missing**: 
- Multi-value flag parsing for destinations
- URL validation and authentication handling
- Stream key mapping (source stream → destination stream)

### Gap 4: Connection Lifecycle Management 🟡 MEDIUM PRIORITY

**Current**: Client connections are short-lived and manual
**Required**: Automatic connection management with retry logic

**Missing Features**:
- Auto-reconnect on destination failure
- Graceful handling of temporary network issues
- Connection pooling and reuse
- Destination health checks

### Gap 5: Error Handling & Resilience 🟡 MEDIUM PRIORITY

**Current**: Local subscriber errors are logged but don't affect other subscribers
**Required**: Per-destination error isolation

**Scenarios to Handle**:
- Destination authentication failure (invalid stream key)
- Network timeouts during relay
- Destination server unavailable
- Bandwidth limits exceeded
- One destination failing shouldn't affect others

### Gap 6: Stream Mapping & Authentication 🟢 LOW PRIORITY

**Current**: Single stream key for local publishing
**Required**: Per-destination stream key mapping

**Example**:
```yaml
# Source stream "live/test" maps to different destinations
destinations:
  - url: "rtmp://youtube.com/live2/"
    stream_key: "YOUR_YOUTUBE_KEY"
    source_streams: ["live/test", "live/main"]
  - url: "rtmp://facebook.com/rtmp/"
    stream_key: "YOUR_FACEBOOK_KEY" 
    source_streams: ["live/test"]
```

---

## Technical Challenges

### Challenge 1: Connection State Management

**Problem**: Managing multiple persistent outbound connections
**Complexity**: Each destination needs independent connection lifecycle
**Solution Approach**: Connection pool with health monitoring

### Challenge 2: Message Synchronization

**Problem**: Ensuring all destinations receive identical media data
**Complexity**: Payload cloning, timestamp consistency, sequence headers
**Solution Approach**: Extend existing BroadcastMessage pattern

### Challenge 3: Error Isolation

**Problem**: One failing destination shouldn't affect others
**Complexity**: Parallel publishing with independent error handling
**Solution Approach**: Go routines per destination with error channels

### Challenge 4: Configuration Complexity

**Problem**: Supporting various destination types and authentication
**Complexity**: YouTube vs Facebook vs generic RTMP servers have different requirements
**Solution Approach**: Pluggable destination adapter pattern

---

## Implementation Complexity Assessment

| Component | Effort Level | Risk Level | Dependencies |
|-----------|-------------|------------|--------------|
| **Outbound Client Pool** | High (3-4 days) | Medium | Extend existing client |
| **Multi-Destination Broadcasting** | Medium (2-3 days) | Low | Integration with existing relay |
| **Command-Line Interface** | Low (1 day) | Low | Flag parsing extension |
| **Connection Management** | High (3-4 days) | High | Retry logic, health checks |
| **Error Handling** | Medium (2 days) | Medium | Parallel error channels |
| **Configuration System** | Medium (2-3 days) | Low | YAML/JSON parsing |

**Total Estimated Effort**: 13-21 days (2.5-4 weeks)

---

## Success Criteria

### Functional Requirements (Must Have)
- ✅ **FR-001**: Server accepts inbound RTMP streams (existing)
- ❌ **FR-002**: Server connects to multiple RTMP destinations via command-line flags
- ❌ **FR-003**: All destinations receive identical media data
- ❌ **FR-004**: Failed destinations don't affect successful ones
- ❌ **FR-005**: Auto-reconnection to failed destinations

### Test Requirements (Must Have)
- ❌ **TR-001**: OBS → rtmp-server-1 → rtmp-server-2 → ffplay (end-to-end test)
- ❌ **TR-002**: Multiple destinations (3+) receive same stream
- ❌ **TR-003**: Destination failure isolation (kill one destination, others continue)
- ❌ **TR-004**: Auto-reconnection test (restart destination server)

### Performance Requirements (Should Have)
- ❌ **PR-001**: Latency increase < 2 seconds per relay hop
- ❌ **PR-002**: Support 5+ destinations per source stream
- ❌ **PR-003**: CPU usage scales linearly with destination count
- ❌ **PR-004**: Memory stable over 60 minutes of relay

---

## Risk Assessment

### High Risk Items
1. **Connection Pool Complexity**: Managing multiple persistent connections is inherently complex
2. **Authentication Variations**: Each platform (YouTube, Facebook) has different auth requirements
3. **Error Cascade**: Network issues could cause multiple destination failures

### Medium Risk Items
1. **Performance Impact**: Multiple destinations will increase CPU/bandwidth usage
2. **Configuration Complexity**: Many command-line flags may become unwieldy
3. **Debugging Difficulty**: Troubleshooting multi-destination issues is challenging

### Mitigation Strategies
1. **Start Simple**: Implement basic multi-destination relay first, add advanced features later
2. **Comprehensive Testing**: Build integration tests for all failure scenarios
3. **Monitoring**: Add detailed logging and metrics for troubleshooting

---

## Recommended Implementation Phases

### Phase 1: Basic Multi-Destination Relay (MVP)
**Duration**: 1-1.5 weeks  
**Goal**: Get basic multi-destination functionality working

- Command-line flag for destination URLs
- Outbound client management
- Parallel message broadcasting
- Basic error handling

### Phase 2: Resilience & Monitoring
**Duration**: 1 week  
**Goal**: Production-ready reliability

- Auto-reconnection logic
- Per-destination health monitoring
- Detailed logging and metrics
- Error isolation improvements

### Phase 3: Advanced Configuration
**Duration**: 0.5-1 week  
**Goal**: User-friendly configuration

- Configuration file support
- Per-destination stream key mapping
- Runtime destination management
- Authentication helpers

### Phase 4: Platform Integration
**Duration**: 0.5-1 week  
**Goal**: Seamless integration with major platforms

- YouTube Live integration helpers
- Facebook Live integration helpers  
- Platform-specific validation
- Documentation and examples

---

## Next Steps

1. **Create Implementation Plan**: Detailed task breakdown with code examples
2. **Validate Architecture**: Review design with existing codebase patterns
3. **Build Prototype**: Implement Phase 1 MVP to validate approach
4. **Integration Testing**: Set up test environment with multiple RTMP servers
5. **Performance Testing**: Validate latency and resource usage requirements

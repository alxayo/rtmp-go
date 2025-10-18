# Advanced Event Hooks Specification

**Version:** 1.0  
**Date:** October 18, 2025  
**Author:** RTMP-GO Development Team  

## Table of Contents

- [Executive Summary](#executive-summary)
- [Current State Analysis](#current-state-analysis)
- [Gap Analysis](#gap-analysis)
- [Phase 2: Additional Event Triggers](#phase-2-additional-event-triggers)
- [Phase 3: Advanced Features](#phase-3-advanced-features)
- [Implementation Plan](#implementation-plan)
- [Task Breakdown Structure](#task-breakdown-structure)
- [Backward Compatibility Requirements](#backward-compatibility-requirements)
- [Testing Strategy](#testing-strategy)
- [Success Criteria](#success-criteria)

## Executive Summary

This specification defines the implementation of advanced event hook capabilities for the RTMP server, building upon the existing Phase 1 hook system. The implementation adds comprehensive monitoring, filtering, authentication, and reliability features while maintaining strict backward compatibility.

### Key Objectives

1. **Enhanced Observability**: Add codec detection, stream metadata, error events, and performance monitoring
2. **Production-Ready Features**: Implement filtering, authentication, retry logic, and delivery guarantees
3. **Enterprise Integration**: Enable seamless integration with monitoring, automation, and analytics systems
4. **Operational Excellence**: Provide comprehensive metrics, debugging capabilities, and failure recovery

## Current State Analysis

### Implemented Features (Phase 1)

The current hook system provides a solid foundation with the following capabilities:

#### Event Types
- ✅ `connection_accept` - New client connections
- ✅ `connection_close` - Client disconnections  
- ✅ `publish_start` - Stream publishing begins
- ✅ `play_start` - Stream playback begins

#### Hook Types
- ✅ **Shell Script Hooks** - Execute custom scripts with event data
- ✅ **HTTP Webhook Hooks** - Send POST requests to webhook URLs
- ✅ **Structured Stdio Output** - JSON/env formatted output to stdout

#### Infrastructure
- ✅ **Concurrent Execution** - Configurable concurrency with timeout support
- ✅ **Hook Manager** - Centralized registration and execution management
- ✅ **CLI Integration** - Command-line flags for configuration
- ✅ **Error Handling** - Basic error recovery and logging
- ✅ **Graceful Shutdown** - Proper cleanup and resource management

#### Configuration
```bash
# Current CLI flags
--hook-script "event_type=script_path"
--hook-webhook "event_type=webhook_url" 
--hook-stdio-format "json|env"
--hook-timeout "30s"
--hook-concurrency 10
```

### Architecture Assessment

**Strengths:**
- Clean interface-based design (`Hook` interface)
- Thread-safe concurrent execution 
- Proper context-based cancellation
- Comprehensive logging with structured fields
- Modular package structure

**Current Limitations:**
- Limited event types (missing media, error, performance events)
- No event filtering or routing capabilities
- Basic authentication (no signing, tokens, etc.)
- No retry logic or delivery guarantees
- Limited observability (no metrics, debugging tools)
- No configuration management beyond CLI flags

## Gap Analysis

### Phase 2 Gaps: Additional Event Triggers

| Category | Current State | Required | Gap |
|----------|---------------|----------|-----|
| **Media Events** | None | Codec detection, metadata, quality | Complete implementation needed |
| **Error Events** | None | Connection errors, protocol errors, performance alerts | Complete implementation needed |
| **Performance Events** | None | Resource usage, latency, throughput | Complete implementation needed |
| **Stream Events** | Basic (start only) | Complete lifecycle (create/delete/stop) | Extend existing events |
| **Event Data** | Basic metadata | Rich contextual data | Enhance event payloads |

### Phase 3 Gaps: Advanced Features

| Feature | Current State | Required | Gap |
|---------|---------------|----------|-----|
| **Filtering** | None | Event type, stream pattern, condition-based | Complete implementation needed |
| **Authentication** | None | API keys, HMAC signing, JWT tokens | Complete implementation needed |
| **Retry Logic** | None | Exponential backoff, dead letter queue | Complete implementation needed |
| **Delivery Guarantees** | Best effort | At-least-once with confirmation | Complete implementation needed |
| **Configuration** | CLI flags only | File-based, hot reload, validation | Extend configuration system |
| **Observability** | Basic logging | Metrics, tracing, debugging tools | Complete implementation needed |

### Integration Gaps

| System | Current State | Required | Gap |
|--------|---------------|----------|-----|
| **Monitoring** | Manual webhook setup | Prometheus, Grafana integration | Native exporters needed |
| **Analytics** | Raw event data | Structured data pipelines | Data transformation needed |
| **Automation** | Basic script hooks | Enterprise workflow integration | Enhanced APIs needed |
| **Security** | Basic HTTPS | Enterprise auth, audit trails | Security framework needed |

## Phase 2: Additional Event Triggers

### 2.1 Media Events

#### Codec Detection Events

**Event Type:** `codec_detected`

**Trigger Points:**
- First audio packet with codec information (message type 8)
- First video packet with codec information (message type 9)
- Codec change detection (streaming format switch)

**Event Data:**
```json
{
  "type": "codec_detected",
  "timestamp": 1634567890,
  "conn_id": "c000001",
  "stream_key": "live/stream1",
  "data": {
    "media_type": "video|audio",
    "codec_name": "H264|AAC|MP3|H265|VP8|VP9|AV1",
    "codec_profile": "baseline|main|high",
    "bitrate": 2048000,
    "resolution": "1920x1080",
    "framerate": 30.0,
    "sample_rate": 44100,
    "channels": 2,
    "packet_type": "sequence_header|raw",
    "frame_type": "keyframe|interframe"
  }
}
```

**Implementation Points:**
- Integrate with existing `CodecDetector` in `internal/rtmp/media/codec_detector.go`
- Hook into `MediaLogger.ProcessMessage()` in `internal/rtmp/server/media_logger.go`
- Extend codec detection to track format changes during stream

#### Stream Metadata Events

**Event Type:** `stream_metadata`

**Trigger Points:**
- onMetaData AMF object received (RTMP metadata command)
- Encoder information changes
- Stream configuration updates

**Event Data:**
```json
{
  "type": "stream_metadata",
  "timestamp": 1634567890,
  "conn_id": "c000001", 
  "stream_key": "live/stream1",
  "data": {
    "width": 1920,
    "height": 1080,
    "framerate": 30.0,
    "video_bitrate": 2048000,
    "audio_bitrate": 128000,
    "encoder": "OBS Studio 28.0.3",
    "creation_date": "2025-10-18T20:30:00Z",
    "title": "Live Stream Title",
    "duration": 0
  }
}
```

#### Quality Monitoring Events

**Event Types:**
- `bitrate_change` - Significant bitrate variations
- `resolution_change` - Video resolution changes
- `framerate_change` - Framerate variations
- `quality_alert` - Quality degradation detection

**Implementation Strategy:**
```go
// Quality monitoring in media pipeline
type QualityMonitor struct {
    thresholds QualityThresholds
    baseline   QualityBaseline
    window     time.Duration
}

type QualityThresholds struct {
    BitrateChangePercent  float64 // 20% = trigger event
    FramerateDropPercent  float64 // 10% = trigger event  
    ResolutionChange      bool    // any change = trigger
}
```

### 2.2 Error Events

#### Connection Error Events

**Event Type:** `connection_error`

**Trigger Points:**
- Handshake failures
- ReadLoop/WriteLoop errors
- Protocol errors (chunk parsing, AMF decoding)
- Network errors (timeouts, resets, EOF)

**Event Data:**
```json
{
  "type": "connection_error",
  "timestamp": 1634567890,
  "conn_id": "c000001",
  "stream_key": "live/stream1",
  "data": {
    "error_type": "handshake_failure|protocol_error|network_error|timeout",
    "error_message": "chunk error: reader.basic_header: read tcp: connection reset by peer",
    "error_code": "ECONNRESET",
    "phase": "handshake|chunking|command_processing|media_processing",
    "peer_addr": "192.168.1.100:54321",
    "connection_duration_ms": 45000,
    "bytes_received": 1048576,
    "bytes_sent": 524288,
    "retry_count": 0
  }
}
```

**Implementation Points:**
- Hook into error handling in `internal/rtmp/handshake/server.go`
- Monitor readLoop/writeLoop errors in `internal/rtmp/conn/conn.go`
- Classify errors using existing `internal/errors/` package

#### Stream Error Events

**Event Type:** `stream_error`

**Trigger Points:**
- Invalid stream keys
- Authentication failures  
- Codec mismatch errors
- Media processing failures

### 2.3 Performance Events

#### Server Performance Events

**Event Type:** `performance_alert`

**Trigger Points:**
- CPU usage > 80%
- Memory usage > 90% 
- Connection count > threshold
- Message queue backlog > threshold

**Event Data:**
```json
{
  "type": "performance_alert",
  "timestamp": 1634567890,
  "conn_id": "",
  "stream_key": "",
  "data": {
    "metric_type": "cpu|memory|connections|queue_depth",
    "current_value": 85.5,
    "threshold": 80.0,
    "severity": "warning|critical",
    "active_connections": 150,
    "total_streams": 75,
    "message_queue_depth": 1000,
    "goroutine_count": 500
  }
}
```

#### Latency Monitoring Events

**Event Type:** `latency_alert`

**Monitoring Points:**
- Handshake completion time
- Message processing latency
- Hook execution duration

### 2.4 Extended Stream Events

#### Stream Lifecycle Events

**New Event Types:**
- `stream_create` - Stream registry creation
- `stream_delete` - Stream cleanup  
- `publish_stop` - Publishing ends
- `play_stop` - Playback ends

**Implementation Points:**
- Integrate with stream registry in `internal/rtmp/server/registry.go`
- Hook into connection close handlers
- Track subscriber count changes

## Phase 3: Advanced Features

### 3.1 Event Filtering System

#### Filter Configuration Structure

```go
type FilterConfig struct {
    // Event type filtering
    EventTypes []EventType `json:"event_types,omitempty"`
    
    // Stream key pattern matching
    StreamPatterns []string `json:"stream_patterns,omitempty"`
    
    // Conditional filtering
    Conditions []FilterCondition `json:"conditions,omitempty"`
    
    // Rate limiting
    RateLimit *RateLimitConfig `json:"rate_limit,omitempty"`
    
    // Severity filtering
    MinSeverity string `json:"min_severity,omitempty"`
}

type FilterCondition struct {
    Field    string      `json:"field"`    // "data.bitrate", "conn_id", etc.
    Operator string      `json:"operator"` // "eq", "gt", "lt", "contains", "matches"
    Value    interface{} `json:"value"`    // Comparison value
}

type RateLimitConfig struct {
    MaxEvents   int           `json:"max_events"`   // Max events per window
    TimeWindow  time.Duration `json:"time_window"`  // Time window duration
    BurstSize   int          `json:"burst_size"`   // Burst allowance
}
```

#### CLI Configuration Extensions

```bash
# Event type filtering
--hook-filter-events "connection_accept,publish_start,codec_detected"

# Stream pattern filtering  
--hook-filter-streams "live/premium/*,live/vip/*"

# Conditional filtering
--hook-filter-condition "data.bitrate>1000000"
--hook-filter-condition "data.codec_name=H264"

# Rate limiting
--hook-rate-limit "10/1m"     # 10 events per minute
--hook-rate-burst 5           # Allow bursts of 5

# Severity filtering
--hook-min-severity "warning"
```

#### Implementation Architecture

```go
type FilterEngine struct {
    filters     map[string]*FilterConfig  // hookID -> filter config
    rateLimits  map[string]*rate.Limiter  // hookID -> rate limiter
    metrics     *FilterMetrics
    mu          sync.RWMutex
}

func (fe *FilterEngine) ShouldTrigger(event *Event, hookID string) (bool, string) {
    // Returns (shouldTrigger, reason)
}

func (fe *FilterEngine) UpdateFilter(hookID string, config *FilterConfig) error {
    // Hot reload filter configuration
}
```

### 3.2 Authentication & Security

#### Authentication Methods

**API Key Authentication:**
```go
type APIKeyAuth struct {
    Key    string `json:"key"`
    Header string `json:"header"` // Default: "X-API-Key"
}
```

**HMAC Signature Authentication:**
```go
type HMACAuth struct {
    Secret    string `json:"secret"`
    Algorithm string `json:"algorithm"` // "sha256", "sha1"
    Header    string `json:"header"`    // Default: "X-Signature"
}

// Implementation
func signPayload(payload []byte, secret string) string {
    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write(payload)
    return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
```

**JWT Token Authentication:**
```go
type JWTAuth struct {
    Secret   string            `json:"secret"`
    Claims   map[string]string `json:"claims"`
    Issuer   string            `json:"issuer"`
    Subject  string            `json:"subject"`
    Audience string            `json:"audience"`
    TTL      time.Duration     `json:"ttl"`
}
```

**OAuth2 Authentication:**
```go
type OAuth2Auth struct {
    ClientID     string   `json:"client_id"`
    ClientSecret string   `json:"client_secret"`
    TokenURL     string   `json:"token_url"`
    Scopes       []string `json:"scopes"`
}
```

#### Security Configuration

```bash
# API Key
--hook-auth-api-key "secret-key-123"
--hook-auth-header "X-API-Key"

# HMAC Signing
--hook-auth-hmac-secret "webhook-secret"
--hook-auth-hmac-algorithm "sha256"

# JWT Tokens
--hook-auth-jwt-secret "jwt-signing-key"
--hook-auth-jwt-issuer "rtmp-server"
--hook-auth-jwt-ttl "1h"

# Custom headers
--hook-header "Authorization: Bearer token123"
--hook-header "X-Source: rtmp-server"
```

### 3.3 Retry Logic & Delivery Guarantees

#### Retry Configuration

```go
type RetryConfig struct {
    MaxAttempts     int           `json:"max_attempts"`     // Max retry attempts
    InitialDelay    time.Duration `json:"initial_delay"`    // Initial delay
    MaxDelay        time.Duration `json:"max_delay"`        // Maximum delay
    BackoffStrategy string        `json:"backoff_strategy"` // "exponential", "linear", "fixed"
    Jitter          bool          `json:"jitter"`           // Add random jitter
    RetryableErrors []string      `json:"retryable_errors"` // Error patterns to retry
}

type DeliveryConfig struct {
    GuaranteeLevel string        `json:"guarantee_level"` // "none", "at-least-once"
    ConfirmTimeout time.Duration `json:"confirm_timeout"` // Wait for delivery confirmation
    DeadLetterURL  string        `json:"dead_letter_url"` // Fallback webhook URL
}
```

#### Implementation Architecture

```go
type DeliveryManager struct {
    retryQueue    chan *FailedDelivery
    deadLetterQ   chan *FailedDelivery  
    deliveryStore DeliveryStore         // Persistence for guarantees
    config        DeliveryConfig
    metrics       *DeliveryMetrics
}

type FailedDelivery struct {
    Event        *Event
    HookID       string
    AttemptCount int
    LastError    error
    NextAttempt  time.Time
    OriginalTime time.Time
}

func (dm *DeliveryManager) scheduleRetry(delivery *FailedDelivery) {
    delay := dm.calculateBackoff(delivery.AttemptCount)
    time.AfterFunc(delay, func() {
        dm.retryQueue <- delivery
    })
}
```

#### CLI Configuration

```bash
# Retry configuration
--hook-retry-max-attempts 3
--hook-retry-initial-delay "1s"
--hook-retry-max-delay "60s" 
--hook-retry-backoff "exponential"
--hook-retry-jitter

# Delivery guarantees
--hook-delivery-guarantee "at-least-once"
--hook-delivery-timeout "30s"
--hook-dead-letter-url "http://backup.example.com/failed-hooks"
```

### 3.4 Configuration Management

#### File-Based Configuration

```yaml
# hooks.yaml
hooks:
  default_config:
    timeout: "30s"
    concurrency: 10
    retry:
      max_attempts: 3
      initial_delay: "1s"
      backoff_strategy: "exponential"
  
  scripts:
    - event_types: ["connection_accept", "connection_close"]
      script: "/opt/scripts/connection-monitor.sh"
      filter:
        stream_patterns: ["live/*"]
        rate_limit:
          max_events: 100
          time_window: "1m"
    
  webhooks:
    - event_types: ["publish_start", "codec_detected"]
      url: "https://api.example.com/rtmp-events"
      auth:
        type: "hmac"
        secret: "${WEBHOOK_SECRET}"
      filter:
        conditions:
          - field: "data.bitrate"
            operator: "gt"
            value: 1000000

  stdio:
    format: "json"
    filter:
      min_severity: "info"
```

#### Hot Reload Support

```go
type ConfigWatcher struct {
    configPath string
    manager    *HookManager
    lastMod    time.Time
    mu         sync.Mutex
}

func (cw *ConfigWatcher) watchConfig() {
    ticker := time.NewTicker(5 * time.Second)
    for range ticker.C {
        if cw.hasChanged() {
            cw.reloadConfig()
        }
    }
}
```

### 3.5 Observability & Metrics

#### Prometheus Metrics

```go
var (
    // Hook execution metrics
    hookExecutionTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "rtmp_hook_executions_total",
            Help: "Total number of hook executions",
        },
        []string{"hook_type", "event_type", "status"},
    )
    
    hookExecutionDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name: "rtmp_hook_execution_duration_seconds",
            Help: "Hook execution duration",
        },
        []string{"hook_type", "event_type"},
    )
    
    // Event generation metrics
    eventsGenerated = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "rtmp_events_generated_total", 
            Help: "Total events generated by type",
        },
        []string{"event_type"},
    )
    
    // Filtering metrics
    eventsFiltered = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "rtmp_events_filtered_total",
            Help: "Events filtered by filter type",
        },
        []string{"filter_type", "reason"},
    )
)
```

#### Health Checks

```go
type HealthChecker struct {
    manager *HookManager
}

func (hc *HealthChecker) CheckHealth() map[string]interface{} {
    return map[string]interface{}{
        "hook_manager_status": "healthy",
        "active_hooks": hc.manager.GetActiveHookCount(),
        "failed_deliveries": hc.manager.GetFailedDeliveryCount(),
        "last_event_time": hc.manager.GetLastEventTime(),
    }
}
```

## Implementation Plan

### Phase 2: Additional Event Triggers (4 weeks)

#### Week 1: Media Events Foundation
- **Task 2.1:** Extend `EventType` constants with new media events
- **Task 2.2:** Implement codec detection event triggers
- **Task 2.3:** Add stream metadata event parsing
- **Task 2.4:** Create quality monitoring framework

#### Week 2: Error Events Implementation  
- **Task 2.5:** Implement connection error event triggers
- **Task 2.6:** Add protocol error classification
- **Task 2.7:** Create stream error event handling
- **Task 2.8:** Integrate error events with existing error handling

#### Week 3: Performance Events
- **Task 2.9:** Implement performance monitoring framework
- **Task 2.10:** Add system metrics collection
- **Task 2.11:** Create latency monitoring
- **Task 2.12:** Implement performance alert thresholds

#### Week 4: Stream Lifecycle Events
- **Task 2.13:** Complete stream lifecycle event triggers
- **Task 2.14:** Extend existing event data with rich metadata
- **Task 2.15:** Integration testing with all new events
- **Task 2.16:** Documentation and examples update

### Phase 3: Advanced Features (6 weeks)

#### Weeks 5-6: Filtering System
- **Task 3.1:** Design and implement filter configuration structure
- **Task 3.2:** Create filter engine with condition evaluation
- **Task 3.3:** Implement rate limiting functionality
- **Task 3.4:** Add CLI configuration for filtering
- **Task 3.5:** Hot reload support for filters

#### Weeks 7-8: Authentication & Security
- **Task 3.6:** Implement API key authentication
- **Task 3.7:** Add HMAC signature support
- **Task 3.8:** Implement JWT token authentication  
- **Task 3.9:** Add OAuth2 support
- **Task 3.10:** Security testing and validation

#### Weeks 9-10: Retry Logic & Delivery
- **Task 3.11:** Implement retry configuration framework
- **Task 3.12:** Create delivery manager with retry logic
- **Task 3.13:** Add dead letter queue support
- **Task 3.14:** Implement delivery guarantees
- **Task 3.15:** Persistence layer for delivery tracking

#### Week 11: Configuration & Observability  
- **Task 3.16:** File-based configuration system
- **Task 3.17:** Configuration hot reload
- **Task 3.18:** Prometheus metrics integration
- **Task 3.19:** Health check endpoints
- **Task 3.20:** Comprehensive testing and validation

## Task Breakdown Structure

### Phase 2 Detailed Tasks

#### Task 2.1: Extend EventType Constants
**Files:** `internal/rtmp/server/hooks/events.go`
**Estimated Time:** 2 hours
**Dependencies:** None

```go
const (
    // ... existing events ...
    
    // Media events (Phase 2)
    EventCodecDetected    EventType = "codec_detected"
    EventStreamMetadata   EventType = "stream_metadata" 
    EventBitrateChange    EventType = "bitrate_change"
    EventResolutionChange EventType = "resolution_change"
    EventQualityAlert     EventType = "quality_alert"
    
    // Error events (Phase 2)
    EventConnectionError  EventType = "connection_error"
    EventProtocolError    EventType = "protocol_error"
    EventStreamError      EventType = "stream_error"
    
    // Performance events (Phase 2)
    EventPerformanceAlert EventType = "performance_alert"
    EventLatencyAlert     EventType = "latency_alert"
    EventResourceAlert    EventType = "resource_alert"
)
```

#### Task 2.2: Implement Codec Detection Triggers
**Files:** 
- `internal/rtmp/server/media_logger.go`
- `internal/rtmp/media/codec_detector.go`
**Estimated Time:** 8 hours
**Dependencies:** Task 2.1

**Implementation:**
```go
// In media_logger.go
func (ml *MediaLogger) ProcessMessage(msg *chunk.Message) {
    // ... existing logic ...
    
    // NEW: Trigger codec detection event
    if ml.audioCodec != "" && ml.videoCodec != "" && !ml.codecEventSent {
        ml.triggerCodecEvent(msg)
        ml.codecEventSent = true
    }
}

func (ml *MediaLogger) triggerCodecEvent(msg *chunk.Message) {
    if ml.hookManager == nil {
        return
    }
    
    eventData := map[string]interface{}{
        "media_type": getMediaType(msg.TypeID),
        "codec_name": getCodecName(msg.TypeID, ml),
        // ... additional codec metadata
    }
    
    event := hooks.NewEvent(hooks.EventCodecDetected).
        WithConnID(ml.connID).
        WithStreamKey(ml.streamKey)
    
    for key, value := range eventData {
        event.WithData(key, value)
    }
    
    ml.hookManager.TriggerEvent(context.Background(), *event)
}
```

#### Task 2.3: Stream Metadata Event Parsing
**Files:**
- `internal/rtmp/server/command_integration.go`
- `internal/rtmp/rpc/metadata.go` (new)
**Estimated Time:** 6 hours
**Dependencies:** Task 2.1

#### Task 2.5: Connection Error Event Triggers
**Files:**
- `internal/rtmp/conn/conn.go`
- `internal/rtmp/handshake/server.go`
**Estimated Time:** 4 hours
**Dependencies:** Task 2.1

**Implementation:**
```go
// In conn.go readLoop
func (c *Connection) startReadLoop() {
    // ... existing code ...
    
    msg, err := r.ReadMessage()
    if err != nil {
        // NEW: Trigger connection error event before logging
        c.triggerConnectionError(err)
        
        // ... existing error handling ...
    }
}

func (c *Connection) triggerConnectionError(err error) {
    if c.hookManager == nil {
        return
    }
    
    errorType := classifyError(err)
    eventData := map[string]interface{}{
        "error_type":    errorType,
        "error_message": err.Error(),
        "phase":         "message_reading",
        "peer_addr":     c.RemoteAddr().String(),
        // ... additional error context
    }
    
    event := hooks.NewEvent(hooks.EventConnectionError).WithConnID(c.ID())
    for key, value := range eventData {
        event.WithData(key, value)
    }
    
    c.hookManager.TriggerEvent(context.Background(), *event)
}
```

### Phase 3 Detailed Tasks

#### Task 3.1: Filter Configuration Structure
**Files:** `internal/rtmp/server/hooks/filter.go` (new)
**Estimated Time:** 6 hours
**Dependencies:** Phase 2 complete

#### Task 3.6: API Key Authentication
**Files:** `internal/rtmp/server/hooks/auth.go` (new)
**Estimated Time:** 4 hours
**Dependencies:** Task 3.1

**Implementation:**
```go
type APIKeyAuthenticator struct {
    key    string
    header string
}

func (a *APIKeyAuthenticator) Authenticate(req *http.Request) error {
    providedKey := req.Header.Get(a.header)
    if providedKey != a.key {
        return fmt.Errorf("invalid API key")
    }
    return nil
}

func (a *APIKeyAuthenticator) AddHeaders(req *http.Request) {
    req.Header.Set(a.header, a.key)
}
```

#### Task 3.11: Retry Configuration Framework
**Files:** `internal/rtmp/server/hooks/retry.go` (new)
**Estimated Time:** 8 hours
**Dependencies:** Tasks 3.1-3.10

## Backward Compatibility Requirements

### Configuration Compatibility

**Requirement:** All existing CLI flags must continue to work unchanged.

**Current Flags (must remain functional):**
```bash
--hook-script "event_type=script_path"
--hook-webhook "event_type=webhook_url"
--hook-stdio-format "json|env"
--hook-timeout "30s"  
--hook-concurrency 10
```

**Implementation Strategy:**
- Extend existing flag parsing without modifying current behavior
- New flags use different prefixes (e.g., `--hook-filter-*`, `--hook-auth-*`)
- Default values preserve current behavior
- Graceful degradation when advanced features not configured

### Event Compatibility

**Requirement:** Existing event types and data structures must remain unchanged.

**Current Event Schema (must preserve):**
```json
{
  "type": "connection_accept",
  "timestamp": 1634567890,
  "conn_id": "c000001",
  "stream_key": "", 
  "data": {
    "client_ip": "192.168.1.100",
    "client_port": 54321,
    "server_ip": "0.0.0.0",
    "server_port": 1935
  }
}
```

**Implementation Strategy:**
- Additive-only changes to event data
- New events use new event types
- Existing event triggers remain unchanged
- Optional fields in new event data

### API Compatibility

**Requirement:** Hook interface and manager API must remain stable.

**Current Interface (must preserve):**
```go
type Hook interface {
    Execute(ctx context.Context, event Event) error
    Type() string
    ID() string
}
```

**Implementation Strategy:**
- Extend interfaces with new optional methods
- Use composition for enhanced capabilities
- Maintain existing method signatures
- Default implementations for new features

## Testing Strategy

### Unit Testing

**Coverage Target:** 90% for new code, maintain existing coverage

**Test Categories:**
1. **Event Generation Tests**
   - Verify new event types trigger correctly
   - Validate event data structure and content
   - Test error conditions and edge cases

2. **Filter Engine Tests** 
   - Test all filter condition types
   - Validate rate limiting functionality
   - Test pattern matching and evaluation

3. **Authentication Tests**
   - Test each authentication method
   - Validate signature generation/verification
   - Test authentication failure scenarios

4. **Retry Logic Tests**
   - Test backoff algorithms
   - Validate retry conditions
   - Test dead letter queue functionality

### Integration Testing

**Test Scenarios:**
1. **End-to-End Event Flow**
   - Generate events → Filter → Authenticate → Deliver
   - Test with real webhook endpoints
   - Validate delivery guarantees

2. **Configuration Testing**
   - Test CLI flag parsing and validation
   - Test file-based configuration loading
   - Test hot reload functionality

3. **Performance Testing**
   - Load testing with high event volume
   - Concurrent hook execution testing
   - Memory and resource usage validation

4. **Failure Testing**
   - Network failure simulation
   - Authentication failure testing
   - Retry exhaustion scenarios

### Compatibility Testing

**Test Matrix:**
- Existing configurations with new server version
- Mixed old/new event types in same configuration
- Gradual migration scenarios

**Automated Tests:**
```bash
# Test existing functionality unchanged
./rtmp-server --hook-script "connection_accept=/old/script.sh" &
# ... verify old behavior works exactly as before

# Test new features don't break old ones  
./rtmp-server \
  --hook-script "connection_accept=/old/script.sh" \
  --hook-filter-events "publish_start" \
  --hook-auth-api-key "test123" &
# ... verify old hook still works, new features active
```

## Success Criteria

### Functional Requirements

1. **Event Coverage**
   - ✅ All 12 new event types implemented and tested
   - ✅ Rich event data with contextual metadata
   - ✅ Real-time event generation with <100ms latency

2. **Filtering Capabilities** 
   - ✅ Event type, stream pattern, conditional filtering
   - ✅ Rate limiting with configurable windows
   - ✅ Hot reload without service restart

3. **Authentication Support**
   - ✅ API key, HMAC, JWT, OAuth2 authentication
   - ✅ Secure credential handling
   - ✅ Authentication failure logging

4. **Reliability Features**
   - ✅ Configurable retry with exponential backoff
   - ✅ Dead letter queue for failed deliveries
   - ✅ At-least-once delivery guarantees

### Performance Requirements

1. **Throughput**
   - Handle 1000+ events/second without performance degradation
   - Support 100+ concurrent hook executions
   - Filter evaluation <1ms per event

2. **Resource Usage**
   - Memory usage increase <50MB for advanced features
   - CPU overhead <5% for event processing
   - Graceful handling of slow webhook endpoints

3. **Reliability**
   - 99.9% hook delivery success rate
   - <10s recovery time from transient failures
   - No memory leaks during extended operation

### Operational Requirements

1. **Observability**
   - Comprehensive Prometheus metrics
   - Structured logging for all operations
   - Health check endpoints for monitoring

2. **Configuration**
   - File-based configuration with validation
   - Hot reload capability
   - Configuration migration tools

3. **Documentation**
   - Complete API documentation
   - Configuration examples and best practices
   - Migration guide from Phase 1

### Compatibility Requirements

1. **Backward Compatibility**
   - 100% compatibility with existing configurations
   - No breaking changes to existing APIs
   - Graceful degradation when features unavailable

2. **Forward Compatibility**
   - Extensible configuration format
   - Plugin architecture for custom hooks
   - Version negotiation for webhook endpoints

This specification provides a comprehensive roadmap for implementing advanced event hook capabilities while maintaining the reliability, performance, and compatibility standards established in Phase 1. The implementation will transform the RTMP server into an enterprise-ready platform capable of sophisticated monitoring, automation, and analytics integration.
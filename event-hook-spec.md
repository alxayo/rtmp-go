# Event Hooks Specification for RTMP Server

## Overview

This specification defines a comprehensive event hook system for the RTMP server that allows external scripts and programs to be triggered on specific RTMP protocol events. The system provides maximum flexibility through both shell script execution and structured JSON output via stdio streams.

## Current Implementation Analysis

### Existing Architecture

The RTMP server has a well-structured architecture with clear separation of concerns:

- **Server Layer** (`internal/rtmp/server/server.go`): Main server with connection management
- **Connection Layer** (`internal/rtmp/conn/conn.go`): Individual connection lifecycle
- **Command Integration** (`internal/rtmp/server/command_integration.go`): RTMP command handling
- **Registry** (`internal/rtmp/server/registry.go`): Stream management and tracking
- **Handlers** (`internal/rtmp/server/*_handler.go`): Publish/play command processing

### Event Points Identified

Through code analysis, the following event trigger points have been identified:

1. **Connection Events**:
   - `conn.Accept()` in `server.go:acceptLoop()` - Connection accepted
   - `conn.Close()` in `conn.go` - Connection closed
   - `handshake.ServerHandshake()` completion - Handshake completed

2. **Stream Events**:
   - `HandlePublish()` in `publish_handler.go` - Stream publish start
   - `HandlePlay()` in `play_handler.go` - Stream play start  
   - `Registry.DeleteStream()` in `registry.go` - Stream stopped
   - `Registry.CreateStream()` in `registry.go` - Stream created

3. **Media Events**:
   - Media packet processing in `command_integration.go` - Video/audio data flow
   - Codec detection via `codecDetector` - Codec information available

### Current Configuration System

The server uses a flags-based configuration system in `cmd/rtmp-server/flags.go`:

```go
type cliConfig struct {
    listenAddr        string
    logLevel          string
    recordAll         bool
    recordDir         string
    chunkSize         uint
    showVersion       bool
    relayDestinations []string
}
```

The server configuration is passed via `srv.Config` to `srv.New()`.

## Gap Analysis

### What Exists
✅ **Well-defined event points** - Clear locations where events occur  
✅ **Structured logging** - Using `log/slog` for consistent logging  
✅ **Configuration system** - Flag-based configuration with validation  
✅ **Connection tracking** - Each connection has unique ID and metadata  
✅ **Stream registry** - Centralized stream state management  
✅ **Error handling** - Consistent error wrapping and context  

### What's Missing
❌ **Event hook system** - No mechanism to trigger external scripts  
❌ **Hook configuration** - No CLI flags for hook setup  
❌ **Event data extraction** - No standardized event data structure  
❌ **Hook manager** - No coordination of multiple hooks per event  
❌ **JSON stdio output** - No structured event output for external consumption  
❌ **Hook execution** - No shell script or webhook execution system  

## Requirements

### Functional Requirements

1. **Event Types**: Support for connection, stream, and media events
2. **Hook Types**: Shell scripts, webhooks, and custom handlers
3. **Data Passing**: Environment variables and JSON via stdin
4. **Configuration**: Command-line flags for hook setup
5. **Concurrency**: Non-blocking hook execution
6. **Error Handling**: Graceful hook failure handling
7. **Logging**: Comprehensive hook execution logging

### Non-Functional Requirements

1. **Performance**: Minimal impact on RTMP server performance
2. **Reliability**: Server continues if hooks fail
3. **Flexibility**: Support multiple hooks per event
4. **Maintainability**: Clean integration with existing codebase
5. **Observability**: Full visibility into hook execution

## Design

### Architecture Overview

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   RTMP Event    │───▶│   Hook Manager  │───▶│  Hook Executors │
│   (Trigger)     │    │   (Dispatch)    │    │   (Scripts)     │
└─────────────────┘    └─────────────────┘    └─────────────────┘
         │                       │                       │
         ▼                       ▼                       ▼
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│  Event Context  │    │ Hook Registry   │    │ Execution Pool  │
│  (Data/Meta)    │    │ (Configuration) │    │ (Goroutines)    │
└─────────────────┘    └─────────────────┘    └─────────────────┘
```

### Core Components

#### 1. Event System (`internal/rtmp/server/hooks/events.go`)

```go
type EventType string

const (
    EventConnectionAccept    EventType = "connection_accept"
    EventConnectionClose     EventType = "connection_close"
    EventHandshakeComplete   EventType = "handshake_complete"
    EventStreamCreate        EventType = "stream_create"
    EventStreamDelete        EventType = "stream_delete"
    EventPublishStart        EventType = "publish_start"
    EventPublishStop         EventType = "publish_stop"
    EventPlayStart           EventType = "play_start"
    EventPlayStop            EventType = "play_stop"
    EventCodecDetected       EventType = "codec_detected"
)

type Event struct {
    Type      EventType              `json:"type"`
    Timestamp int64                  `json:"timestamp"`
    ConnID    string                 `json:"conn_id,omitempty"`
    StreamKey string                 `json:"stream_key,omitempty"`
    Data      map[string]interface{} `json:"data,omitempty"`
}
```

#### 2. Hook Interface (`internal/rtmp/server/hooks/hook.go`)

```go
type Hook interface {
    Execute(ctx context.Context, event Event) error
    Type() string
    ID() string
}

type ShellHook struct {
    ID      string
    Command string
    Args    []string
    Env     []string
    PassJSON bool
}

type WebhookHook struct {
    ID      string
    URL     string
    Headers map[string]string
    Timeout time.Duration
}

type StdioHook struct {
    ID     string
    Format string // "json" or "env"
}
```

#### 3. Hook Manager (`internal/rtmp/server/hooks/manager.go`)

```go
type HookManager struct {
    hooks map[EventType][]Hook
    stdio *StdioHook
    mu    sync.RWMutex
    pool  *ExecutionPool
}

func (hm *HookManager) RegisterHook(eventType EventType, hook Hook)
func (hm *HookManager) TriggerEvent(ctx context.Context, event Event)
func (hm *HookManager) EnableStdioOutput(format string)
```

#### 4. Configuration Extension

```go
// Extend srv.Config
type Config struct {
    // ... existing fields ...
    HookScripts     map[string]string  // event_type -> script_path
    HookWebhooks    map[string]string  // event_type -> webhook_url
    HookStdioFormat string             // "json", "env", or ""
    HookTimeout     time.Duration
    HookConcurrency int
}
```

### Event Data Specification

#### Common Fields (All Events)
- `type`: Event type identifier
- `timestamp`: Unix timestamp
- `conn_id`: Connection identifier (when applicable)
- `stream_key`: Full stream key (when applicable)

#### Event-Specific Data

**ConnectionAccept**:
```json
{
  "type": "connection_accept",
  "timestamp": 1697635200,
  "conn_id": "c000001",
  "data": {
    "client_ip": "192.168.1.100",
    "client_port": 52341,
    "server_ip": "127.0.0.1",
    "server_port": 1935
  }
}
```

**PublishStart**:
```json
{
  "type": "publish_start", 
  "timestamp": 1697635200,
  "conn_id": "c000001",
  "stream_key": "live/test",
  "data": {
    "app": "live",
    "stream_name": "test",
    "publish_type": "live",
    "client_ip": "192.168.1.100"
  }
}
```

**StreamStop**:
```json
{
  "type": "publish_stop",
  "timestamp": 1697635500,
  "conn_id": "c000001", 
  "stream_key": "live/test",
  "data": {
    "duration_seconds": 300,
    "bytes_received": 15728640,
    "video_packets": 9000,
    "audio_packets": 15000,
    "reason": "client_disconnect"
  }
}
```

### Environment Variables

For shell hooks, the following environment variables are set:

```bash
# Core event data
RTMP_EVENT_TYPE="publish_start"
RTMP_TIMESTAMP="1697635200"
RTMP_CONN_ID="c000001"
RTMP_STREAM_KEY="live/test"

# Event-specific data (varies by event)
RTMP_APP="live"
RTMP_STREAM_NAME="test"
RTMP_CLIENT_IP="192.168.1.100"
RTMP_PUBLISH_TYPE="live"
# ... additional fields based on event type
```

## Implementation Plan

### Phase 1: Core Hook System

#### Step 1.1: Create Hook Infrastructure
- [ ] Create `internal/rtmp/server/hooks/` package
- [ ] Implement event types and data structures
- [ ] Implement hook interface and basic implementations
- [ ] Add comprehensive unit tests

**Files to create:**
- `internal/rtmp/server/hooks/events.go`
- `internal/rtmp/server/hooks/hook.go`
- `internal/rtmp/server/hooks/shell_hook.go`
- `internal/rtmp/server/hooks/webhook_hook.go`
- `internal/rtmp/server/hooks/stdio_hook.go`

#### Step 1.2: Implement Hook Manager
- [ ] Create hook manager with registration and execution
- [ ] Implement execution pool for concurrent hook execution
- [ ] Add error handling and logging
- [ ] Add comprehensive unit tests

**Files to create:**
- `internal/rtmp/server/hooks/manager.go`
- `internal/rtmp/server/hooks/execution_pool.go`

#### Step 1.3: Configuration Extension
- [ ] Extend CLI flags to support hook configuration
- [ ] Extend server Config struct
- [ ] Add configuration validation
- [ ] Update help text and documentation

**Files to modify:**
- `cmd/rtmp-server/flags.go`
- `internal/rtmp/server/server.go`

### Phase 2: Event Integration

#### Step 2.1: Connection Events
- [ ] Add hook triggers to connection accept/close
- [ ] Add handshake completion event
- [ ] Extract connection metadata for events
- [ ] Add integration tests

**Files to modify:**
- `internal/rtmp/server/server.go` (acceptLoop)
- `internal/rtmp/conn/conn.go` (Accept, Close)

#### Step 2.2: Stream Events  
- [ ] Add hook triggers to publish/play handlers
- [ ] Add stream create/delete events
- [ ] Extract stream metadata for events
- [ ] Add integration tests

**Files to modify:**
- `internal/rtmp/server/publish_handler.go`
- `internal/rtmp/server/play_handler.go`
- `internal/rtmp/server/registry.go`
- `internal/rtmp/server/command_integration.go`

#### Step 2.3: Media Events
- [ ] Add codec detection events
- [ ] Add media flow start/stop events
- [ ] Extract media metadata for events
- [ ] Add integration tests

**Files to modify:**
- `internal/rtmp/server/command_integration.go`

### Phase 3: Advanced Features

#### Step 3.1: Stdio Output System
- [ ] Implement structured JSON output to stdout/stderr
- [ ] Add environment variable output format
- [ ] Add filtering and formatting options
- [ ] Add integration tests

#### Step 3.2: Webhook Support
- [ ] Implement HTTP webhook hook
- [ ] Add retry logic and timeout handling
- [ ] Add authentication support
- [ ] Add integration tests

#### Step 3.3: Performance Optimization
- [ ] Add hook execution metrics
- [ ] Implement hook execution throttling
- [ ] Add configuration for hook limits
- [ ] Performance testing and optimization

### Phase 4: Testing and Documentation

#### Step 4.1: Comprehensive Testing
- [ ] Unit tests for all hook components
- [ ] Integration tests with real RTMP clients
- [ ] End-to-end tests with external scripts
- [ ] Performance and load testing

#### Step 4.2: Documentation and Examples
- [ ] API documentation for hook system
- [ ] Example hook scripts for common use cases
- [ ] Configuration guide and best practices
- [ ] Troubleshooting guide

## Integration Points

### 1. Connection Accept (server.go:acceptLoop)

**Location**: `internal/rtmp/server/server.go:acceptLoop()`
**Trigger Point**: After successful `iconn.Accept()`

```go
// After: s.conns[c.ID()] = c
if s.hookManager != nil {
    event := hooks.Event{
        Type:      hooks.EventConnectionAccept,
        Timestamp: time.Now().Unix(),
        ConnID:    c.ID(),
        Data: map[string]interface{}{
            "client_ip":   raw.RemoteAddr().(*net.TCPAddr).IP.String(),
            "client_port": raw.RemoteAddr().(*net.TCPAddr).Port,
            "server_ip":   s.l.Addr().(*net.TCPAddr).IP.String(),
            "server_port": s.l.Addr().(*net.TCPAddr).Port,
        },
    }
    s.hookManager.TriggerEvent(context.Background(), event)
}
```

### 2. Handshake Complete (conn.go:Accept)

**Location**: `internal/rtmp/conn/conn.go:Accept()`
**Trigger Point**: After successful `handshake.ServerHandshake()`

```go
// After: lgr.Info("Connection accepted", ...)
// Note: Will need to add hook manager to Accept function signature
```

### 3. Publish Start (publish_handler.go:HandlePublish)

**Location**: `internal/rtmp/server/publish_handler.go:HandlePublish()`
**Trigger Point**: After successful stream registration

```go
// After: stream.SetPublisher(conn)
// Will integrate via command_integration.go OnPublish handler
```

### 4. Connection Close (conn.go:Close)

**Location**: `internal/rtmp/conn/conn.go:Close()`
**Trigger Point**: Before connection cleanup

```go
// At start of Close() method
// Will need hook manager reference passed to connection
```

## CLI Interface

### New Command Line Flags

```bash
# Shell hook configuration
--hook-script <event_type>=<script_path>
  # Examples:
  --hook-script connection_accept=/hooks/notify-connect.sh
  --hook-script publish_start=/hooks/start-recording.sh
  --hook-script publish_stop=/hooks/stop-recording.sh

# Webhook configuration  
--hook-webhook <event_type>=<webhook_url>
  # Examples:
  --hook-webhook publish_start=https://api.example.com/webhooks/stream-start
  --hook-webhook connection_close=https://api.example.com/webhooks/disconnect

# Stdio output configuration
--hook-stdio-format <format>
  # Values: json, env, or empty (disabled)
  # Examples:
  --hook-stdio-format json
  --hook-stdio-format env

# Hook execution settings
--hook-timeout <duration>
  # Default: 30s
  # Example: --hook-timeout 60s

--hook-concurrency <count>
  # Default: 10
  # Example: --hook-concurrency 20
```

### Usage Examples

```bash
# Basic shell hooks
./rtmp-server \
  --listen :1935 \
  --hook-script publish_start=/hooks/notify-start.sh \
  --hook-script publish_stop=/hooks/cleanup.sh

# JSON output for external processing
./rtmp-server --listen :1935 --hook-stdio-format json | \
  while read event; do
    echo "$event" | jq '.' | /my/event/processor.sh
  done

# Combined hooks and webhooks
./rtmp-server \
  --listen :1935 \
  --hook-script publish_start=/hooks/start-recording.sh \
  --hook-webhook publish_start=https://api.discord.com/webhooks/... \
  --hook-stdio-format json
```

## Example Hook Scripts

### 1. Stream Start Notification

**File**: `/hooks/notify-start.sh`
```bash
#!/bin/bash
# Stream start notification hook

echo "🔴 LIVE: $RTMP_STREAM_KEY started at $(date)"
echo "Client: $RTMP_CLIENT_IP"
echo "App: $RTMP_APP"

# Send Discord notification
curl -X POST "$DISCORD_WEBHOOK_URL" \
  -H "Content-Type: application/json" \
  -d "{
    \"embeds\": [{
      \"title\": \"🔴 Stream Started\",
      \"description\": \"**$RTMP_STREAM_KEY** is now live!\",
      \"color\": 16711680,
      \"fields\": [
        {\"name\": \"Client\", \"value\": \"$RTMP_CLIENT_IP\", \"inline\": true},
        {\"name\": \"App\", \"value\": \"$RTMP_APP\", \"inline\": true}
      ],
      \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%S.000Z)\"
    }]
  }"

# Start FFmpeg recording
RECORDING_DIR="/recordings"
STREAM_FILE="${RTMP_STREAM_KEY//\//_}_$(date +%Y%m%d_%H%M%S).mp4"

ffmpeg -i "rtmp://localhost:1935/$RTMP_STREAM_KEY" \
       -c copy \
       -f mp4 \
       "$RECORDING_DIR/$STREAM_FILE" \
       > "/tmp/recording_${RTMP_CONN_ID}.log" 2>&1 &

echo $! > "/tmp/recording_${RTMP_CONN_ID}.pid"
echo "Recording started: $STREAM_FILE"
```

### 2. Stream Stop Cleanup

**File**: `/hooks/cleanup.sh`
```bash
#!/bin/bash
# Stream stop cleanup hook

echo "⚫ Stream stopped: $RTMP_STREAM_KEY at $(date)"
echo "Duration: ${RTMP_DURATION_SECONDS}s"
echo "Data received: $RTMP_BYTES_RECEIVED bytes"

# Stop recording process
if [ -f "/tmp/recording_${RTMP_CONN_ID}.pid" ]; then
    PID=$(cat "/tmp/recording_${RTMP_CONN_ID}.pid")
    kill $PID 2>/dev/null
    rm "/tmp/recording_${RTMP_CONN_ID}.pid"
    echo "Recording stopped (PID: $PID)"
fi

# Send notification
curl -X POST "$DISCORD_WEBHOOK_URL" \
  -H "Content-Type: application/json" \
  -d "{
    \"embeds\": [{
      \"title\": \"⚫ Stream Ended\",
      \"description\": \"**$RTMP_STREAM_KEY** went offline\",
      \"color\": 8421504,
      \"fields\": [
        {\"name\": \"Duration\", \"value\": \"${RTMP_DURATION_SECONDS}s\", \"inline\": true},
        {\"name\": \"Data\", \"value\": \"$(($RTMP_BYTES_RECEIVED / 1024))KB\", \"inline\": true}
      ]
    }]
  }"
```

### 3. JSON Event Processor

**File**: `/processors/event-handler.sh`
```bash
#!/bin/bash
# JSON event processor

while IFS= read -r line; do
    if [[ $line == RTMP_EVENT:* ]]; then
        # Extract JSON from RTMP_EVENT: prefix
        json_data="${line#RTMP_EVENT: }"
        
        # Parse with jq
        event_type=$(echo "$json_data" | jq -r '.type')
        stream_key=$(echo "$json_data" | jq -r '.stream_key // "unknown"')
        conn_id=$(echo "$json_data" | jq -r '.conn_id // "unknown"')
        
        case $event_type in
            "publish_start")
                echo "Stream started: $stream_key ($conn_id)"
                # Trigger recording, notifications, etc.
                ;;
            "publish_stop") 
                echo "Stream stopped: $stream_key ($conn_id)"
                # Cleanup, final notifications, etc.
                ;;
            "connection_accept")
                echo "New connection: $conn_id"
                ;;
            *)
                echo "Unknown event: $event_type"
                ;;
        esac
    else
        # Pass through normal log messages
        echo "$line"
    fi
done
```

## Testing Strategy

### Unit Tests

1. **Hook System Tests**:
   - Event creation and serialization
   - Hook registration and execution
   - Manager concurrent execution
   - Error handling scenarios

2. **Integration Tests**:
   - Hook triggers from actual RTMP events
   - Environment variable passing
   - JSON stdio output
   - Multiple hooks per event

3. **End-to-End Tests**:
   - Real RTMP client connections with hooks
   - Shell script execution verification
   - Webhook delivery testing
   - Performance under load

### Test Files Structure

```
internal/rtmp/server/hooks/
├── events_test.go
├── hook_test.go  
├── shell_hook_test.go
├── webhook_hook_test.go
├── stdio_hook_test.go
├── manager_test.go
└── integration_test.go

tests/integration/
├── hooks_integration_test.go
└── hooks_e2e_test.go

tests/fixtures/hooks/
├── test-shell-hook.sh
├── test-json-processor.sh
└── test-webhook-server.go
```

## Performance Considerations

### Hook Execution

1. **Asynchronous Execution**: All hooks run in separate goroutines
2. **Execution Pool**: Bounded pool to prevent resource exhaustion  
3. **Timeouts**: Configurable timeouts to prevent hanging
4. **Buffering**: Event queue to handle burst scenarios

### Resource Management

1. **Memory**: Event data optimized for minimal allocation
2. **CPU**: Hook execution throttling and prioritization
3. **I/O**: Efficient stdio output with minimal locking
4. **Goroutines**: Bounded execution pool with cleanup

### Monitoring

1. **Metrics**: Hook execution times, success/failure rates
2. **Logging**: Comprehensive hook execution logging
3. **Health**: Hook system health indicators
4. **Debugging**: Debug mode for detailed hook tracing

## Security Considerations

### Shell Hook Security

1. **Path Validation**: Validate hook script paths
2. **Permissions**: Verify script execution permissions
3. **Environment**: Sanitize environment variables
4. **Timeouts**: Prevent infinite execution

### Webhook Security

1. **URL Validation**: Validate webhook URLs
2. **Authentication**: Support for API keys/tokens
3. **Rate Limiting**: Prevent webhook abuse
4. **SSL/TLS**: Enforce HTTPS for webhooks

### Data Security

1. **Sensitive Data**: Avoid exposing sensitive information
2. **Input Validation**: Validate all hook inputs
3. **Error Messages**: Sanitize error output
4. **Logging**: Avoid logging sensitive data

## Migration Path

### Backward Compatibility

The hook system is designed to be completely optional and backward compatible:

1. **Default Behavior**: No hooks configured = no change in behavior
2. **Configuration**: All hook flags are optional
3. **Performance**: Zero overhead when hooks are disabled
4. **API**: No breaking changes to existing APIs

### Migration Steps

1. **Phase 1**: Deploy hook system in disabled state
2. **Phase 2**: Enable stdio output for monitoring
3. **Phase 3**: Add shell hooks for specific events
4. **Phase 4**: Enable full hook functionality

## Conclusion

This specification provides a comprehensive plan for implementing a flexible, performant, and secure event hook system for the RTMP server. The system integrates cleanly with the existing codebase while providing powerful extensibility for automation, monitoring, and integration scenarios.

The phased implementation approach ensures that the system can be developed incrementally with proper testing at each stage. The design prioritizes performance, reliability, and ease of use while maintaining the high-quality standards of the existing codebase.
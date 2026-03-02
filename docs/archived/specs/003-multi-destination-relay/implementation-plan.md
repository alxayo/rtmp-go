# Implementation Plan: Multi-Destination RTMP Relay Feature

**Feature**: Multi-Destination RTMP Relay  
**Date**: October 13, 2025  
**Status**: Implementation Planning  
**Reference**: [Gap Analysis](gap-analysis.md)

---

## Executive Summary

This plan implements multi-destination RTMP relay capability, allowing the server to act as both receiver and publisher. The server will accept inbound streams and automatically relay them to multiple external RTMP endpoints (YouTube Live, Facebook Live, other RTMP servers) via command-line configuration.

**Target Test Scenario**: `OBS → rtmp-server-1 → rtmp-server-2 → ffplay`

---

## Architecture Overview

### High-Level Design

```
┌─────────────┐                    ┌──────────────────┐                    ┌─────────────┐
│     OBS     │   RTMP Publish     │                  │   RTMP Publish     │ rtmp-server │
│ (Publisher) ├──────────────────► │  rtmp-server-1   ├──────────────────► │     -2      │
└─────────────┘                    │ (Multi-Relay)    │                    │             │
                                   │                  │                    └─────────────┘
                                   │                  │   RTMP Publish     ┌─────────────┐
                                   │                  ├──────────────────► │  YouTube    │
                                   │                  │                    │    Live     │
                                   │                  │                    └─────────────┘
                                   │                  │   RTMP Publish     ┌─────────────┐
                                   │                  ├──────────────────► │  Facebook   │
                                   └──────────────────┘                    │    Live     │
                                                                           └─────────────┘
```

### Data Flow Architecture

```go
// Media packet flow through the system
Publisher (OBS) → Server.readLoop() → Stream.BroadcastMessage() → {
    ↓
    LocalSubscribers (ffplay, VLC) [EXISTING]
    ↓  
    DestinationManager.RelayMessage() [NEW]
    ↓
    Destination[0].Client.SendMessage() → YouTube Live
    Destination[1].Client.SendMessage() → Facebook Live  
    Destination[2].Client.SendMessage() → rtmp-server-2
}
```

---

## Implementation Phases

### Phase 1: Core Infrastructure (MVP)
**Duration**: 5-7 days  
**Goal**: Basic multi-destination relay functionality

### Phase 2: Resilience & Monitoring  
**Duration**: 3-4 days  
**Goal**: Production-ready reliability

### Phase 3: Advanced Features
**Duration**: 2-3 days  
**Goal**: User experience improvements

### Phase 4: Integration Testing
**Duration**: 2-3 days  
**Goal**: Comprehensive validation

**Total Estimated Duration**: 12-17 days (2.5-3.5 weeks)

---

## Phase 1: Core Infrastructure (MVP)

### Task 1.1: Add Command-Line Destination Flags

**File**: `cmd/rtmp-server/flags.go`

**Changes**:
```go
// Add to cliConfig struct
type cliConfig struct {
    // ... existing fields
    relayDestinations []string  // NEW: Multiple destination URLs
}

// Add flag parsing
func parseFlags(args []string) (*cliConfig, error) {
    // ... existing code
    
    // NEW: Support multiple -relay-to flags
    var relayDests stringSliceFlag
    fs.Var(&relayDests, "relay-to", "RTMP destination URL (can be specified multiple times)")
    
    cfg.relayDestinations = relayDests
    // ... validation
}

// NEW: Custom flag type for multiple values
type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
    return strings.Join(*s, ", ")
}

func (s *stringSliceFlag) Set(value string) error {
    *s = append(*s, value)
    return nil
}
```

**Test Command**:
```bash
./rtmp-server -listen :1935 \
  -relay-to "rtmp://localhost:1936/live/test1" \
  -relay-to "rtmp://localhost:1937/live/test2"
```

**Estimated Effort**: 1 day

---

### Task 1.2: Create Destination Management System

**File**: `internal/rtmp/relay/destination.go` (NEW)

```go
package relay

import (
    "context"
    "log/slog"
    "net/url"
    "sync"
    "time"
    
    "github.com/alxayo/go-rtmp/internal/rtmp/client"
    "github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// DestinationStatus represents the connection state of a destination
type DestinationStatus int

const (
    StatusDisconnected DestinationStatus = iota
    StatusConnecting
    StatusConnected
    StatusError
)

// Destination represents a single RTMP relay destination
type Destination struct {
    URL       string              // rtmp://example.com/live/stream_key
    Client    *client.Client      // Persistent RTMP client connection
    Status    DestinationStatus   // Current connection status
    LastError error              // Last error encountered
    Metrics   *DestinationMetrics // Performance metrics
    
    // Internal state
    mu           sync.RWMutex
    reconnectCtx context.Context
    reconnectCancel context.CancelFunc
    logger       *slog.Logger
}

// DestinationMetrics tracks performance for each destination
type DestinationMetrics struct {
    MessagesSent    uint64    // Total messages sent successfully
    MessagesDropped uint64    // Messages dropped due to errors
    BytesSent       uint64    // Total bytes transmitted
    LastSentTime    time.Time // Timestamp of last successful send
    ConnectTime     time.Time // When connection was established
    ReconnectCount  uint32    // Number of reconnection attempts
}

// NewDestination creates a new destination with the given URL
func NewDestination(rawURL string, logger *slog.Logger) (*Destination, error) {
    // Validate and parse the RTMP URL
    parsedURL, err := url.Parse(rawURL)
    if err != nil {
        return nil, fmt.Errorf("invalid destination URL: %w", err)
    }
    
    if parsedURL.Scheme != "rtmp" {
        return nil, fmt.Errorf("destination URL must use rtmp:// scheme, got %s", parsedURL.Scheme)
    }
    
    ctx, cancel := context.WithCancel(context.Background())
    
    return &Destination{
        URL:             rawURL,
        Status:          StatusDisconnected,
        Metrics:         &DestinationMetrics{},
        reconnectCtx:    ctx,
        reconnectCancel: cancel,
        logger:          logger.With("destination_url", rawURL),
    }, nil
}

// Connect establishes connection to the destination RTMP server
func (d *Destination) Connect() error {
    d.mu.Lock()
    defer d.mu.Unlock()
    
    if d.Status == StatusConnected {
        return nil // Already connected
    }
    
    d.Status = StatusConnecting
    d.logger.Info("Connecting to destination")
    
    // Create RTMP client
    client, err := client.New(d.URL)
    if err != nil {
        d.Status = StatusError
        d.LastError = err
        return fmt.Errorf("create client: %w", err)
    }
    
    // Perform RTMP handshake and setup
    if err := client.Connect(); err != nil {
        d.Status = StatusError
        d.LastError = err
        return fmt.Errorf("client connect: %w", err)
    }
    
    // Start publishing to the destination
    if err := client.Publish(); err != nil {
        d.Status = StatusError
        d.LastError = err
        return fmt.Errorf("client publish: %w", err)
    }
    
    d.Client = client
    d.Status = StatusConnected
    d.Metrics.ConnectTime = time.Now()
    d.LastError = nil
    
    d.logger.Info("Successfully connected to destination")
    return nil
}

// SendMessage sends a media message to this destination
func (d *Destination) SendMessage(msg *chunk.Message) error {
    d.mu.RLock()
    client := d.Client
    status := d.Status
    d.mu.RUnlock()
    
    if status != StatusConnected || client == nil {
        d.Metrics.MessagesDropped++
        return fmt.Errorf("destination not connected (status: %v)", status)
    }
    
    // Send the message based on type
    var err error
    switch msg.TypeID {
    case 8: // Audio message
        err = client.SendAudio(msg.Timestamp, msg.Payload)
    case 9: // Video message  
        err = client.SendVideo(msg.Timestamp, msg.Payload)
    default:
        // Skip non-media messages for relay
        return nil
    }
    
    if err != nil {
        d.mu.Lock()
        d.Status = StatusError
        d.LastError = err
        d.Metrics.MessagesDropped++
        d.mu.Unlock()
        return fmt.Errorf("send message: %w", err)
    }
    
    // Update metrics
    d.mu.Lock()
    d.Metrics.MessagesSent++
    d.Metrics.BytesSent += uint64(len(msg.Payload))
    d.Metrics.LastSentTime = time.Now()
    d.mu.Unlock()
    
    return nil
}

// Close disconnects from the destination
func (d *Destination) Close() error {
    d.mu.Lock()
    defer d.mu.Unlock()
    
    d.reconnectCancel()
    
    if d.Client != nil {
        err := d.Client.Close()
        d.Client = nil
        d.Status = StatusDisconnected
        return err
    }
    
    return nil
}

// GetMetrics returns a copy of current metrics
func (d *Destination) GetMetrics() DestinationMetrics {
    d.mu.RLock()
    defer d.mu.RUnlock()
    return *d.Metrics // Return copy
}
```

**Estimated Effort**: 2 days

---

### Task 1.3: Create Destination Manager

**File**: `internal/rtmp/relay/manager.go` (NEW)

```go
package relay

import (
    "fmt"
    "log/slog"
    "sync"
    
    "github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// DestinationManager manages multiple RTMP relay destinations
type DestinationManager struct {
    destinations map[string]*Destination
    mu           sync.RWMutex
    logger       *slog.Logger
}

// NewDestinationManager creates a new destination manager
func NewDestinationManager(destinationURLs []string, logger *slog.Logger) (*DestinationManager, error) {
    dm := &DestinationManager{
        destinations: make(map[string]*Destination),
        logger:       logger.With("component", "destination_manager"),
    }
    
    // Initialize destinations from URLs
    for _, url := range destinationURLs {
        if err := dm.AddDestination(url); err != nil {
            return nil, fmt.Errorf("failed to add destination %s: %w", url, err)
        }
    }
    
    return dm, nil
}

// AddDestination adds a new destination and connects to it
func (dm *DestinationManager) AddDestination(url string) error {
    dm.mu.Lock()
    defer dm.mu.Unlock()
    
    if _, exists := dm.destinations[url]; exists {
        return fmt.Errorf("destination already exists: %s", url)
    }
    
    dest, err := NewDestination(url, dm.logger)
    if err != nil {
        return fmt.Errorf("create destination: %w", err)
    }
    
    // Connect to the destination  
    if err := dest.Connect(); err != nil {
        dm.logger.Warn("Failed to connect to destination", "url", url, "error", err)
        // Don't return error - destination will be retried later
    }
    
    dm.destinations[url] = dest
    dm.logger.Info("Added destination", "url", url, "total_destinations", len(dm.destinations))
    
    return nil
}

// RelayMessage sends a media message to all connected destinations
func (dm *DestinationManager) RelayMessage(msg *chunk.Message) {
    if msg == nil || (msg.TypeID != 8 && msg.TypeID != 9) {
        return // Only relay audio/video messages
    }
    
    dm.mu.RLock()
    destinations := make([]*Destination, 0, len(dm.destinations))
    for _, dest := range dm.destinations {
        destinations = append(destinations, dest)
    }
    dm.mu.RUnlock()
    
    // Send to all destinations in parallel
    var wg sync.WaitGroup
    for _, dest := range destinations {
        wg.Add(1)
        go func(d *Destination) {
            defer wg.Done()
            
            if err := d.SendMessage(msg); err != nil {
                dm.logger.Debug("Failed to relay message to destination", 
                    "url", d.URL, "error", err, "msg_type", msg.TypeID)
                // TODO: Implement retry logic in Phase 2
            }
        }(dest)
    }
    
    // Don't wait for completion to avoid blocking the main relay loop
    // wg.Wait() // Uncomment if synchronous relay is needed
}

// GetStatus returns status of all destinations
func (dm *DestinationManager) GetStatus() map[string]DestinationStatus {
    dm.mu.RLock()
    defer dm.mu.RUnlock()
    
    status := make(map[string]DestinationStatus)
    for url, dest := range dm.destinations {
        dest.mu.RLock()
        status[url] = dest.Status
        dest.mu.RUnlock()
    }
    return status
}

// Close disconnects from all destinations
func (dm *DestinationManager) Close() error {
    dm.mu.Lock()
    defer dm.mu.Unlock()
    
    var lastErr error
    for url, dest := range dm.destinations {
        if err := dest.Close(); err != nil {
            dm.logger.Error("Failed to close destination", "url", url, "error", err)
            lastErr = err
        }
    }
    
    dm.destinations = make(map[string]*Destination)
    return lastErr
}
```

**Estimated Effort**: 1.5 days

---

### Task 1.4: Integrate with Server Configuration

**File**: `internal/rtmp/server/server.go`

**Changes**:
```go
// Add to Config struct
type Config struct {
    // ... existing fields
    RelayDestinations []string // NEW: List of destination URLs for relay
}

// Add to Server struct  
type Server struct {
    // ... existing fields
    destinationManager *relay.DestinationManager // NEW
}

// Update New() function
func New(cfg Config) *Server {
    cfg.applyDefaults()
    
    // Initialize destination manager if destinations are provided
    var destMgr *relay.DestinationManager
    if len(cfg.RelayDestinations) > 0 {
        var err error
        destMgr, err = relay.NewDestinationManager(cfg.RelayDestinations, logger.Logger())
        if err != nil {
            // Log error but don't fail server startup
            logger.Logger().Error("Failed to initialize destination manager", "error", err)
        }
    }
    
    return &Server{
        cfg:                cfg,
        reg:                NewRegistry(),
        conns:              make(map[string]*iconn.Connection),
        destinationManager: destMgr,
        log:                logger.Logger().With("component", "rtmp_server"),
    }
}

// Update Stop() to close destination manager
func (s *Server) Stop() error {
    // ... existing stop logic
    
    // Close destination manager
    if s.destinationManager != nil {
        if err := s.destinationManager.Close(); err != nil {
            s.log.Error("Failed to close destination manager", "error", err)
        }
    }
    
    // ... rest of existing stop logic
}
```

**File**: `cmd/rtmp-server/main.go`

**Changes**:
```go
func main() {
    // ... existing code
    
    server := srv.New(srv.Config{
        ListenAddr:        cfg.listenAddr,
        ChunkSize:         uint32(cfg.chunkSize),
        WindowAckSize:     2_500_000,
        RecordAll:         cfg.recordAll,
        RecordDir:         cfg.recordDir,
        LogLevel:          cfg.logLevel,
        RelayDestinations: cfg.relayDestinations, // NEW
    })
    
    // ... rest of existing code
}
```

**Estimated Effort**: 0.5 days

---

### Task 1.5: Integrate Multi-Destination Relay with Media Processing

**File**: `internal/rtmp/server/command_integration.go`

**Changes**:
```go
// Update attachCommandHandling to include destination manager
func attachCommandHandling(c *iconn.Connection, reg *Registry, cfg *Config, log *slog.Logger, destMgr *relay.DestinationManager) {
    // ... existing code
    
    c.SetMessageHandler(func(m *chunk.Message) {
        if m == nil {
            return
        }

        log.Debug("message handler invoked", "type_id", m.TypeID, "msid", m.MessageStreamID, "len", len(m.Payload))

        // Process media packets (audio/video) 
        if m.TypeID == 8 || m.TypeID == 9 {
            st.mediaLogger.ProcessMessage(m)

            if st.streamKey != "" {
                stream := reg.GetStream(st.streamKey)
                if stream != nil {
                    // Local recording (existing)
                    if stream.Recorder != nil {
                        stream.Recorder.WriteMessage(m)
                    }
                    
                    // Local subscriber relay (existing)
                    stream.BroadcastMessage(st.codecDetector, m, log)
                    
                    // Multi-destination relay (NEW)
                    if destMgr != nil {
                        destMgr.RelayMessage(m)
                    }
                }
            }
            return
        }

        // ... rest of existing command processing
    })
}
```

**Update server.go acceptLoop**:
```go
func (s *Server) acceptLoop() {
    // ... existing code
    
    // Wire command handling with destination manager
    attachCommandHandling(c, s.reg, &s.cfg, s.log, s.destinationManager)
    
    // ... rest of existing code
}
```

**Estimated Effort**: 1 day

---

## Phase 2: Resilience & Monitoring

### Task 2.1: Add Auto-Reconnection Logic

**File**: `internal/rtmp/relay/destination.go`

**Enhancements**:
```go
const (
    reconnectInterval = 5 * time.Second
    maxReconnectAttempts = 10
)

// StartReconnectLoop begins automatic reconnection attempts for failed destinations
func (d *Destination) StartReconnectLoop() {
    go func() {
        ticker := time.NewTicker(reconnectInterval)
        defer ticker.Stop()
        
        for {
            select {
            case <-d.reconnectCtx.Done():
                return
            case <-ticker.C:
                d.mu.RLock()
                needsReconnect := d.Status == StatusError || d.Status == StatusDisconnected
                attempts := d.Metrics.ReconnectCount
                d.mu.RUnlock()
                
                if needsReconnect && attempts < maxReconnectAttempts {
                    d.logger.Info("Attempting to reconnect", "attempt", attempts+1)
                    if err := d.Connect(); err != nil {
                        d.mu.Lock()
                        d.Metrics.ReconnectCount++
                        d.mu.Unlock()
                        d.logger.Warn("Reconnection failed", "error", err)
                    } else {
                        d.mu.Lock()
                        d.Metrics.ReconnectCount = 0 // Reset on success
                        d.mu.Unlock()
                    }
                }
            }
        }
    }()
}
```

**Estimated Effort**: 1 day

---

### Task 2.2: Add Health Monitoring & Metrics

**File**: `internal/rtmp/relay/metrics.go` (NEW)

```go
package relay

import (
    "encoding/json"
    "time"
)

// HealthStatus represents overall destination manager health
type HealthStatus struct {
    TotalDestinations      int                            `json:"total_destinations"`
    ConnectedDestinations  int                            `json:"connected_destinations"`
    FailedDestinations     int                            `json:"failed_destinations"`
    TotalMessagesSent      uint64                         `json:"total_messages_sent"`
    TotalMessagesDropped   uint64                         `json:"total_messages_dropped"`
    TotalBytesSent         uint64                         `json:"total_bytes_sent"`
    Destinations          map[string]DestinationHealth    `json:"destinations"`
    LastUpdated           time.Time                       `json:"last_updated"`
}

type DestinationHealth struct {
    URL                string              `json:"url"`
    Status             DestinationStatus   `json:"status"`
    LastError          string             `json:"last_error,omitempty"`
    MessagesSent       uint64             `json:"messages_sent"`
    MessagesDropped    uint64             `json:"messages_dropped"`
    BytesSent          uint64             `json:"bytes_sent"`
    ConnectedDuration  time.Duration      `json:"connected_duration"`
    ReconnectCount     uint32             `json:"reconnect_count"`
}

// GetHealthStatus returns comprehensive health information
func (dm *DestinationManager) GetHealthStatus() HealthStatus {
    dm.mu.RLock()
    defer dm.mu.RUnlock()
    
    status := HealthStatus{
        TotalDestinations: len(dm.destinations),
        Destinations:      make(map[string]DestinationHealth),
        LastUpdated:       time.Now(),
    }
    
    for url, dest := range dm.destinations {
        dest.mu.RLock()
        health := DestinationHealth{
            URL:               url,
            Status:            dest.Status,
            MessagesSent:      dest.Metrics.MessagesSent,
            MessagesDropped:   dest.Metrics.MessagesDropped,
            BytesSent:         dest.Metrics.BytesSent,
            ReconnectCount:    dest.Metrics.ReconnectCount,
        }
        
        if dest.LastError != nil {
            health.LastError = dest.LastError.Error()
        }
        
        if dest.Status == StatusConnected && !dest.Metrics.ConnectTime.IsZero() {
            health.ConnectedDuration = time.Since(dest.Metrics.ConnectTime)
        }
        
        status.Destinations[url] = health
        
        // Update totals
        status.TotalMessagesSent += dest.Metrics.MessagesSent
        status.TotalMessagesDropped += dest.Metrics.MessagesDropped
        status.TotalBytesSent += dest.Metrics.BytesSent
        
        switch dest.Status {
        case StatusConnected:
            status.ConnectedDestinations++
        case StatusError:
            status.FailedDestinations++
        }
        
        dest.mu.RUnlock()
    }
    
    return status
}

// String returns JSON representation of health status
func (h HealthStatus) String() string {
    data, _ := json.MarshalIndent(h, "", "  ")
    return string(data)
}
```

**Estimated Effort**: 1 day

---

### Task 2.3: Add Enhanced Error Handling

**File**: `internal/rtmp/relay/destination.go`

**Enhancements**:
```go
// Enhanced SendMessage with retry logic
func (d *Destination) SendMessage(msg *chunk.Message) error {
    const maxRetries = 3
    const retryDelay = 100 * time.Millisecond
    
    for attempt := 0; attempt < maxRetries; attempt++ {
        err := d.sendMessageOnce(msg)
        if err == nil {
            return nil // Success
        }
        
        d.logger.Debug("Send attempt failed", "attempt", attempt+1, "error", err)
        
        // Check if error is recoverable
        if !isRecoverableError(err) {
            return err // Don't retry non-recoverable errors
        }
        
        if attempt < maxRetries-1 {
            time.Sleep(retryDelay)
        }
    }
    
    return fmt.Errorf("failed after %d attempts", maxRetries)
}

func (d *Destination) sendMessageOnce(msg *chunk.Message) error {
    // ... existing SendMessage logic
}

func isRecoverableError(err error) bool {
    // Define which errors are worth retrying
    errorStr := err.Error()
    return strings.Contains(errorStr, "timeout") ||
           strings.Contains(errorStr, "connection reset") ||
           strings.Contains(errorStr, "temporary failure")
}
```

**Estimated Effort**: 1 day

---

## Phase 3: Advanced Features

### Task 3.1: Add HTTP Status Endpoint

**File**: `internal/rtmp/server/http.go` (NEW)

```go
package server

import (
    "encoding/json"
    "fmt"
    "net/http"
    
    "github.com/alxayo/go-rtmp/internal/rtmp/relay"
)

// StartHTTPServer starts an HTTP server for status monitoring
func (s *Server) StartHTTPServer(addr string) error {
    if s.destinationManager == nil {
        return fmt.Errorf("destination manager not initialized")
    }
    
    mux := http.NewServeMux()
    
    // Health check endpoint
    mux.HandleFunc("/health", s.handleHealth)
    
    // Destination status endpoint  
    mux.HandleFunc("/destinations", s.handleDestinations)
    
    s.log.Info("Starting HTTP status server", "addr", addr)
    go func() {
        if err := http.ListenAndServe(addr, mux); err != nil {
            s.log.Error("HTTP server error", "error", err)
        }
    }()
    
    return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
    health := s.destinationManager.GetHealthStatus()
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(health)
}

func (s *Server) handleDestinations(w http.ResponseWriter, r *http.Request) {
    status := s.destinationManager.GetStatus()
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(status)
}
```

**Add to flags.go**:
```go
type cliConfig struct {
    // ... existing fields
    httpStatusAddr string // NEW: HTTP status server address
}

func parseFlags(args []string) (*cliConfig, error) {
    // ... existing code
    fs.StringVar(&cfg.httpStatusAddr, "http-status", "", "HTTP status server address (e.g., :8080)")
    // ... rest of parsing
}
```

**Estimated Effort**: 1 day

---

### Task 3.2: Configuration File Support

**File**: `internal/rtmp/server/config.go` (NEW)

```go
package server

import (
    "encoding/json"
    "fmt"
    "os"
)

// RelayConfig represents relay configuration from file
type RelayConfig struct {
    Destinations []DestinationConfig `json:"destinations"`
}

type DestinationConfig struct {
    Name         string            `json:"name"`          // Human-readable name
    URL          string            `json:"url"`           // RTMP destination URL
    StreamKey    string            `json:"stream_key"`    // Stream key for this destination
    Enabled      bool              `json:"enabled"`       // Whether this destination is active
    MaxRetries   int               `json:"max_retries"`   // Max reconnection attempts
    Metadata     map[string]string `json:"metadata"`      // Additional metadata
}

// LoadRelayConfig loads relay configuration from JSON file
func LoadRelayConfig(filename string) (*RelayConfig, error) {
    data, err := os.ReadFile(filename)
    if err != nil {
        return nil, fmt.Errorf("read config file: %w", err)
    }
    
    var config RelayConfig
    if err := json.Unmarshal(data, &config); err != nil {
        return nil, fmt.Errorf("parse config file: %w", err)
    }
    
    return &config, nil
}

// Example config file: relay-config.json
/*
{
  "destinations": [
    {
      "name": "YouTube Live",
      "url": "rtmp://a.rtmp.youtube.com/live2",
      "stream_key": "YOUR_YOUTUBE_STREAM_KEY",
      "enabled": true,
      "max_retries": 5
    },
    {
      "name": "Facebook Live", 
      "url": "rtmp://live-api-s.facebook.com/rtmp",
      "stream_key": "YOUR_FACEBOOK_STREAM_KEY",
      "enabled": true,
      "max_retries": 3
    },
    {
      "name": "Test Server",
      "url": "rtmp://localhost:1936/live/test",
      "stream_key": "test123",
      "enabled": true,
      "max_retries": 10
    }
  ]
}
*/
```

**Add configuration file flag**:
```go
// flags.go
fs.StringVar(&cfg.relayConfigFile, "relay-config", "", "Path to relay configuration JSON file")
```

**Estimated Effort**: 1 day

---

## Phase 4: Integration Testing

### Task 4.1: Create End-to-End Integration Tests

**File**: `tests/integration/multi_destination_relay_test.go` (NEW)

```go
package integration

import (
    "testing"
    "time"
    "net"
    "context"
    
    "github.com/alxayo/go-rtmp/internal/rtmp/server"
    "github.com/alxayo/go-rtmp/internal/rtmp/client"
)

// TestMultiDestinationRelay validates the complete flow:
// OBS → rtmp-server-1 → rtmp-server-2 → ffplay
func TestMultiDestinationRelay(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    // Start destination server (rtmp-server-2)
    destServer := startTestServer(t, ":0")
    defer destServer.Stop()
    destAddr := destServer.Addr().String()
    
    // Start relay server (rtmp-server-1) with destination
    relayServerCfg := server.Config{
        ListenAddr:        ":0",
        RelayDestinations: []string{fmt.Sprintf("rtmp://%s/live/relayed", destAddr)},
    }
    relayServer := server.New(relayServerCfg)
    err := relayServer.Start()
    if err != nil {
        t.Fatalf("Failed to start relay server: %v", err)
    }
    defer relayServer.Stop()
    relayAddr := relayServer.Addr().String()
    
    // Give servers time to start
    time.Sleep(100 * time.Millisecond)
    
    // Step 1: Connect publisher to relay server
    pubClient, err := client.New(fmt.Sprintf("rtmp://%s/live/source", relayAddr))
    if err != nil {
        t.Fatalf("Create publisher client: %v", err)
    }
    defer pubClient.Close()
    
    if err := pubClient.Connect(); err != nil {
        t.Fatalf("Publisher connect: %v", err)
    }
    
    if err := pubClient.Publish(); err != nil {
        t.Fatalf("Publisher publish: %v", err)
    }
    
    // Step 2: Connect subscriber to destination server  
    subClient, err := client.New(fmt.Sprintf("rtmp://%s/live/relayed", destAddr))
    if err != nil {
        t.Fatalf("Create subscriber client: %v", err)
    }
    defer subClient.Close()
    
    if err := subClient.Connect(); err != nil {
        t.Fatalf("Subscriber connect: %v", err)
    }
    
    if err := subClient.Play(); err != nil {
        t.Fatalf("Subscriber play: %v", err)
    }
    
    // Step 3: Send test media from publisher
    testAudio := []byte{0xAF, 0x00, 0x01, 0x02, 0x03} // AAC sequence header
    testVideo := []byte{0x17, 0x00, 0x01, 0x02, 0x03} // AVC sequence header
    
    if err := pubClient.SendAudio(0, testAudio); err != nil {
        t.Fatalf("Send audio: %v", err)
    }
    
    if err := pubClient.SendVideo(0, testVideo); err != nil {
        t.Fatalf("Send video: %v", err)
    }
    
    // Step 4: Verify relay worked (check server logs or metrics)
    // In a real implementation, we'd read from the subscriber client
    time.Sleep(1 * time.Second)
    
    t.Logf("Multi-destination relay test completed successfully")
}

// TestMultipleDestinations tests relay to 3 different destinations
func TestMultipleDestinations(t *testing.T) {
    // Start 3 destination servers
    dest1 := startTestServer(t, ":0")
    defer dest1.Stop()
    dest2 := startTestServer(t, ":0") 
    defer dest2.Stop()
    dest3 := startTestServer(t, ":0")
    defer dest3.Stop()
    
    // Start relay server with all 3 destinations
    relayServerCfg := server.Config{
        ListenAddr: ":0",
        RelayDestinations: []string{
            fmt.Sprintf("rtmp://%s/live/stream1", dest1.Addr().String()),
            fmt.Sprintf("rtmp://%s/live/stream2", dest2.Addr().String()),
            fmt.Sprintf("rtmp://%s/live/stream3", dest3.Addr().String()),
        },
    }
    relayServer := server.New(relayServerCfg)
    err := relayServer.Start()
    if err != nil {
        t.Fatalf("Failed to start relay server: %v", err)
    }
    defer relayServer.Stop()
    
    // Publish to relay server
    publisher := mustSetupPublisher(t, relayServer.Addr().String(), "live", "source")
    defer publisher.Close()
    
    // Send test media
    sendTestMediaMessages(t, publisher)
    
    // Verify all destinations received the media
    // (In practice, check server logs or implement message counters)
    
    t.Logf("Multiple destinations test completed successfully")
}

// TestDestinationFailure tests that one failed destination doesn't affect others
func TestDestinationFailureIsolation(t *testing.T) {
    // Start 2 destination servers
    dest1 := startTestServer(t, ":0")
    defer dest1.Stop()
    dest2 := startTestServer(t, ":0")
    defer dest2.Stop()
    
    relayServerCfg := server.Config{
        ListenAddr: ":0", 
        RelayDestinations: []string{
            fmt.Sprintf("rtmp://%s/live/stream1", dest1.Addr().String()),
            fmt.Sprintf("rtmp://%s/live/stream2", dest2.Addr().String()),
            "rtmp://nonexistent:1935/live/fail", // This will fail
        },
    }
    relayServer := server.New(relayServerCfg)
    err := relayServer.Start()
    if err != nil {
        t.Fatalf("Failed to start relay server: %v", err)
    }
    defer relayServer.Stop()
    
    // Publish media 
    publisher := mustSetupPublisher(t, relayServer.Addr().String(), "live", "source")
    defer publisher.Close()
    
    sendTestMediaMessages(t, publisher)
    
    // Verify that working destinations still receive media despite one failure
    // (Check logs or metrics to confirm)
    
    t.Logf("Destination failure isolation test completed successfully")
}

// Helper functions
func startTestServer(t *testing.T, addr string) *server.Server {
    cfg := server.Config{ListenAddr: addr}
    srv := server.New(cfg)
    if err := srv.Start(); err != nil {
        t.Fatalf("Start test server: %v", err)
    }
    return srv
}

func sendTestMediaMessages(t *testing.T, conn net.Conn) {
    // Send sequence headers and media frames
    // (Implementation depends on test helper functions)
}
```

**Estimated Effort**: 2 days

---

### Task 4.2: Create Performance & Load Tests

**File**: `tests/integration/relay_performance_test.go` (NEW)

```go
package integration

import (
    "testing"
    "time"
    "sync"
    "fmt"
)

// TestRelayLatency measures end-to-end latency
func TestRelayLatency(t *testing.T) {
    // Measure time from publisher send to subscriber receive
    // Target: < 2 seconds additional latency per relay hop
    
    startTime := time.Now()
    
    // Setup relay chain and measure latency
    // Implementation would require synchronized clocks/timestamps
    
    latency := time.Since(startTime)
    maxAllowedLatency := 2 * time.Second
    
    if latency > maxAllowedLatency {
        t.Errorf("Relay latency too high: %v > %v", latency, maxAllowedLatency)
    }
    
    t.Logf("Relay latency: %v", latency)
}

// TestMultipleDestinationPerformance tests resource usage with many destinations
func TestMultipleDestinationPerformance(t *testing.T) {
    const numDestinations = 10
    
    // Start destination servers
    var destinations []*server.Server
    var destURLs []string
    
    for i := 0; i < numDestinations; i++ {
        dest := startTestServer(t, ":0")
        destinations = append(destinations, dest)
        destURLs = append(destURLs, fmt.Sprintf("rtmp://%s/live/stream%d", 
            dest.Addr().String(), i))
    }
    
    defer func() {
        for _, dest := range destinations {
            dest.Stop()
        }
    }()
    
    // Start relay server with all destinations
    relayServer := server.New(server.Config{
        ListenAddr:        ":0",
        RelayDestinations: destURLs,
    })
    if err := relayServer.Start(); err != nil {
        t.Fatalf("Start relay server: %v", err)
    }
    defer relayServer.Stop()
    
    // Measure resource usage before test
    startMemory := getMemoryUsage()
    startCPU := getCPUUsage()
    
    // Run test
    publisher := mustSetupPublisher(t, relayServer.Addr().String(), "live", "perf")
    defer publisher.Close()
    
    // Send media for 30 seconds
    for i := 0; i < 30; i++ {
        sendTestMediaMessages(t, publisher)
        time.Sleep(1 * time.Second)
    }
    
    // Measure resource usage after test
    endMemory := getMemoryUsage()
    endCPU := getCPUUsage()
    
    t.Logf("Memory usage: %d MB -> %d MB (delta: %d MB)", 
        startMemory/1024/1024, endMemory/1024/1024, (endMemory-startMemory)/1024/1024)
    t.Logf("CPU usage: %.2f%% -> %.2f%%", startCPU, endCPU)
    
    // Validate resource usage is reasonable
    memoryDelta := endMemory - startMemory
    maxMemoryIncrease := int64(100 * 1024 * 1024) // 100MB
    
    if memoryDelta > maxMemoryIncrease {
        t.Errorf("Memory usage increased too much: %d MB", memoryDelta/1024/1024)
    }
}

func getMemoryUsage() int64 {
    // Implementation to get current memory usage
    // Could use runtime.MemStats or external tools
    return 0
}

func getCPUUsage() float64 {
    // Implementation to get current CPU usage percentage
    return 0.0
}
```

**Estimated Effort**: 1 day

---

## Testing Strategy

### Unit Tests (Per Component)
```bash
# Test each component in isolation
go test ./internal/rtmp/relay/...
go test ./internal/rtmp/server/... -run TestDestination
go test ./cmd/rtmp-server/... -run TestFlags
```

### Integration Tests (End-to-End)
```bash
# Test complete relay functionality
go test ./tests/integration/... -run TestMultiDestinationRelay
go test ./tests/integration/... -run TestMultipleDestinations
go test ./tests/integration/... -run TestDestinationFailure
```

### Manual Testing Commands
```bash
# Terminal 1: Start destination server
./rtmp-server -listen :1936

# Terminal 2: Start relay server
./rtmp-server -listen :1935 -relay-to "rtmp://localhost:1936/live/relayed"

# Terminal 3: Publish to relay server
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/source

# Terminal 4: Play from destination server
ffplay rtmp://localhost:1936/live/relayed
```

### Performance Benchmarks
```bash
# Test with multiple destinations
./rtmp-server -listen :1935 \
  -relay-to "rtmp://localhost:1936/live/dest1" \
  -relay-to "rtmp://localhost:1937/live/dest2" \
  -relay-to "rtmp://localhost:1938/live/dest3" \
  -http-status :8080

# Monitor status
curl http://localhost:8080/health
```

---

## Acceptance Criteria

### Functional Requirements ✅
- **FR-001**: Server accepts `-relay-to` command-line flags for multiple destinations
- **FR-002**: Media messages are relayed to all configured destinations in parallel  
- **FR-003**: Failed destinations don't affect successful ones (error isolation)
- **FR-004**: Basic auto-reconnection for failed destinations
- **FR-005**: HTTP status endpoint shows destination health

### Test Requirements ✅  
- **TR-001**: End-to-end test: OBS → rtmp-server-1 → rtmp-server-2 → ffplay
- **TR-002**: Multiple destinations (3+) receive identical media streams
- **TR-003**: Destination failure isolation (one fails, others continue)
- **TR-004**: Performance test with 10+ destinations

### Performance Requirements ✅
- **PR-001**: Additional latency per relay hop < 2 seconds
- **PR-002**: Support 10+ destinations per source stream  
- **PR-003**: Memory usage scales linearly with destination count
- **PR-004**: CPU usage remains < 80% with 5 destinations

---

## Risk Mitigation

### Technical Risks
1. **Connection Management Complexity**
   - Mitigation: Start with simple connection pool, add features incrementally
   
2. **Authentication Variations**  
   - Mitigation: Support generic RTMP URLs first, add platform-specific helpers later
   
3. **Performance Degradation**
   - Mitigation: Parallel destination processing, comprehensive performance tests

### Implementation Risks  
1. **Scope Creep**
   - Mitigation: Stick to 4-phase plan, defer advanced features to future versions
   
2. **Integration Complexity**
   - Mitigation: Leverage existing client library, minimize changes to core server

### Operational Risks
1. **Configuration Errors**
   - Mitigation: URL validation, clear error messages, example configurations
   
2. **Debugging Difficulty**
   - Mitigation: Comprehensive logging, HTTP status endpoint, clear metrics

---

## Implementation Timeline

| Phase | Duration | Key Deliverables |
|-------|----------|------------------|
| **Phase 1: Core Infrastructure** | 5-7 days | Command-line flags, destination management, basic relay |
| **Phase 2: Resilience** | 3-4 days | Auto-reconnection, health monitoring, error handling |
| **Phase 3: Advanced Features** | 2-3 days | HTTP status endpoint, configuration file support |
| **Phase 4: Integration Testing** | 2-3 days | End-to-end tests, performance validation |
| **Total** | **12-17 days** | **Production-ready multi-destination relay** |

---

## Future Enhancements (Out of Scope)

1. **Platform-Specific Integration**
   - YouTube Live API integration
   - Facebook Live API integration  
   - Twitch RTMP integration
   
2. **Advanced Configuration**
   - Per-destination stream key mapping
   - Conditional relay rules (time-based, viewer-based)
   - Load balancing across destinations
   
3. **Monitoring & Analytics**
   - Grafana dashboard integration
   - Real-time bandwidth monitoring
   - Viewer analytics aggregation

4. **High Availability**
   - Destination failover logic
   - Multiple relay server clustering
   - Distributed stream registry

These enhancements can be implemented as separate features in future iterations.
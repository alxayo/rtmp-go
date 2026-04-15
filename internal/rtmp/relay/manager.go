// File: manager.go
// Purpose: Manages multiple RTMP/RTMPS relay destinations. When a stream is published,
// the relay manager optionally forwards media to external RTMP or RTMPS servers for
// cross-organization delivery or backup archival.
//
// Key Types:
//   - DestinationManager: Tracks relay destinations, creates/manages relay clients
//   - Destination: Single relay target (URL, connection state, metrics)
//
// Key Functions:
//   - NewDestinationManager(urls, logger): Create manager with initial destinations
//   - (dm *DestinationManager) AddDestination(url): Add new relay target
//   - (dm *DestinationManager) RemoveDestination(url): Remove relay target
//   - (dm *DestinationManager) RelayMessage(msg): Fan-out message to all destinations
//   - (dm *DestinationManager) Close(): Gracefully close all relay connections
//
// Dependencies:
//   - internal/rtmp/client: RTMP client for connecting to relay destinations
//   - internal/rtmp/chunk: Message type for media frames
//   - sync: RWMutex for concurrent destination access
//   - log/slog: Structured logging
//
// Design: Each destination runs independently. If one relay fails, others continue.
// Messages are buffered (bounded channel) to provide backpressure on slow relays.
// Relay is optional — if no destinations are configured, RelayMessage is a no-op.
package relay

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// DestinationManager manages multiple RTMP relay destinations
type DestinationManager struct {
	destinations  map[string]*Destination
	mu            sync.RWMutex
	logger        *slog.Logger
	clientFactory RTMPClientFactory
}

// NewDestinationManager creates a new destination manager
func NewDestinationManager(destinationURLs []string, logger *slog.Logger, clientFactory RTMPClientFactory) (*DestinationManager, error) {
	dm := &DestinationManager{
		destinations:  make(map[string]*Destination),
		logger:        logger.With("component", "destination_manager"),
		clientFactory: clientFactory,
	}

	// Initialize destinations from URLs
	for _, url := range destinationURLs {
		if err := dm.AddDestination(url); err != nil {
			dm.logger.Warn("Failed to add destination", "url", url, "error", err)
			// Continue adding other destinations even if one fails
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

	dest, err := NewDestination(url, dm.logger, dm.clientFactory)
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
				dm.logger.Error("Failed to relay message",
					"url", d.URL, "type_id", msg.TypeID, "error", err)
			}
		}(dest)
	}
	wg.Wait()
}

// GetStatus returns status of all destinations
func (dm *DestinationManager) GetStatus() map[string]DestinationStatus {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	status := make(map[string]DestinationStatus)
	for url, dest := range dm.destinations {
		status[url] = dest.GetStatus()
	}
	return status
}

// GetMetrics returns metrics for all destinations
func (dm *DestinationManager) GetMetrics() map[string]DestinationMetrics {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	metrics := make(map[string]DestinationMetrics)
	for url, dest := range dm.destinations {
		metrics[url] = dest.GetMetrics()
	}
	return metrics
}

// DestinationInfo represents a point-in-time snapshot of a relay destination
// for the metrics endpoint.
type DestinationInfo struct {
	URL             string `json:"url"`
	Status          string `json:"status"`
	MessagesSent    uint64 `json:"messages_sent"`
	MessagesDropped uint64 `json:"messages_dropped"`
	BytesSent       uint64 `json:"bytes_sent"`
	ReconnectCount  uint32 `json:"reconnect_count"`
	LastError       string `json:"last_error,omitempty"`
}

// Snapshot returns a point-in-time view of all relay destinations for the
// metrics endpoint. Safe for concurrent use.
func (dm *DestinationManager) Snapshot() []DestinationInfo {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	infos := make([]DestinationInfo, 0, len(dm.destinations))
	for _, d := range dm.destinations {
		d.mu.RLock()
		info := DestinationInfo{
			URL:             d.URL,
			Status:          d.Status.String(),
			MessagesSent:    d.Metrics.MessagesSent,
			MessagesDropped: d.Metrics.MessagesDropped,
			BytesSent:       d.Metrics.BytesSent,
			ReconnectCount:  d.Metrics.ReconnectCount,
		}
		if d.LastError != nil {
			info.LastError = d.LastError.Error()
		}
		d.mu.RUnlock()
		infos = append(infos, info)
	}
	return infos
}

// Close disconnects from all destinations
func (dm *DestinationManager) Close() error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	var lastErr error
	for url, dest := range dm.destinations {
		if err := dest.Close(); err != nil {
			dm.logger.Error("Error closing destination", "url", url, "error", err)
			lastErr = err
		}
	}

	dm.destinations = make(map[string]*Destination)
	return lastErr
}

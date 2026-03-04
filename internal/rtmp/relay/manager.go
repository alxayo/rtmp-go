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

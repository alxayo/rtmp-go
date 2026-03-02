package relay

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"sync"
	"time"

	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// RTMPClient defines the interface for connecting to a remote RTMP server
// and sending media data. This interface exists to decouple the relay system
// from the concrete client implementation, making it testable with mock clients.
type RTMPClient interface {
	Connect() error                                  // Perform TCP dial + RTMP handshake + connect command
	Publish() error                                  // Send publish command to start streaming
	SendAudio(timestamp uint32, payload []byte) error // Send a raw audio message
	SendVideo(timestamp uint32, payload []byte) error // Send a raw video message
	Close() error                                    // Disconnect and clean up
}

// RTMPClientFactory is a constructor function that creates RTMPClient instances.
// Using a factory allows the relay system to create fresh clients for each
// destination without knowing the concrete client type.
type RTMPClientFactory func(url string) (RTMPClient, error)

// DestinationStatus tracks the connection state of a relay destination.
type DestinationStatus int

const (
	StatusDisconnected DestinationStatus = iota // Not connected (initial state)
	StatusConnecting                           // Connection attempt in progress
	StatusConnected                            // Successfully connected and publishing
	StatusError                                // Connection failed or was lost
)

// String returns a string representation of the destination status
func (s DestinationStatus) String() string {
	switch s {
	case StatusDisconnected:
		return "disconnected"
	case StatusConnecting:
		return "connecting"
	case StatusConnected:
		return "connected"
	case StatusError:
		return "error"
	default:
		return "unknown"
	}
}

// Destination represents a single RTMP relay destination
// Destination represents a single relay target — a remote RTMP server that
// receives a copy of the publisher's audio/video stream.
type Destination struct {
	URL           string              // Full RTMP URL (e.g. rtmp://cdn.example.com/live/key)
	Client        RTMPClient          // Active RTMP client connection to the destination
	Status        DestinationStatus   // Current connection state
	LastError     error               // Most recent error (nil if healthy)
	Metrics       *DestinationMetrics // Counters for sent/dropped messages and bytes
	clientFactory RTMPClientFactory   // Creates new client instances for (re)connection

	// Internal state
	mu              sync.RWMutex
	reconnectCtx    context.Context
	reconnectCancel context.CancelFunc
	logger          *slog.Logger
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
func NewDestination(rawURL string, logger *slog.Logger, clientFactory RTMPClientFactory) (*Destination, error) {
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
		clientFactory:   clientFactory,
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
		return nil
	}

	d.Status = StatusConnecting
	d.logger.Info("Connecting to destination")

	client, err := d.clientFactory(d.URL)
	if err != nil {
		d.Status = StatusError
		d.LastError = err
		d.logger.Error("Failed to create RTMP client", "error", err)
		return fmt.Errorf("create client: %w", err)
	}

	if err := client.Connect(); err != nil {
		d.Status = StatusError
		d.LastError = err
		d.logger.Error("Failed to connect RTMP client", "error", err)
		return fmt.Errorf("client connect: %w", err)
	}

	if err := client.Publish(); err != nil {
		d.Status = StatusError
		d.LastError = err
		d.logger.Error("Failed to publish to destination", "error", err)
		return fmt.Errorf("client publish: %w", err)
	}

	d.Client = client
	d.Status = StatusConnected
	d.Metrics.ConnectTime = time.Now()
	d.LastError = nil
	d.logger.Info("Connected to destination")
	return nil
}

// SendMessage sends a media message to this destination
func (d *Destination) SendMessage(msg *chunk.Message) error {
	d.mu.RLock()
	client := d.Client
	status := d.Status
	d.mu.RUnlock()

	if status != StatusConnected || client == nil {
		d.mu.Lock()
		d.Metrics.MessagesDropped++
		d.mu.Unlock()
		return fmt.Errorf("destination not connected (status: %v)", status)
	}

	var err error
	switch msg.TypeID {
	case 8: // Audio message
		err = client.SendAudio(msg.Timestamp, msg.Payload)
	case 9: // Video message
		err = client.SendVideo(msg.Timestamp, msg.Payload)
	default:
		return nil // Skip non-media messages
	}

	if err != nil {
		d.mu.Lock()
		d.Status = StatusError
		d.LastError = err
		d.Metrics.MessagesDropped++
		d.mu.Unlock()
		d.logger.Error("relay send failed", "type_id", msg.TypeID, "error", err)
		return fmt.Errorf("send message: %w", err)
	}

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

// GetStatus returns the current connection status
func (d *Destination) GetStatus() DestinationStatus {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.Status
}

// GetLastError returns the last error encountered
func (d *Destination) GetLastError() error {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.LastError
}

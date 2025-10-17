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

// RTMPClient interface defines the methods we need from an RTMP client
// to avoid circular dependencies with the client package
type RTMPClient interface {
	Connect() error
	Publish() error
	SendAudio(timestamp uint32, payload []byte) error
	SendVideo(timestamp uint32, payload []byte) error
	Close() error
}

// RTMPClientFactory creates new RTMP clients
type RTMPClientFactory func(url string) (RTMPClient, error)

// DestinationStatus represents the connection state of a destination
type DestinationStatus int

const (
	StatusDisconnected DestinationStatus = iota
	StatusConnecting
	StatusConnected
	StatusError
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
type Destination struct {
	URL           string              // rtmp://example.com/live/stream_key
	Client        RTMPClient          // Persistent RTMP client connection
	Status        DestinationStatus   // Current connection status
	LastError     error               // Last error encountered
	Metrics       *DestinationMetrics // Performance metrics
	clientFactory RTMPClientFactory   // Factory to create new clients

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
		d.logger.Debug("Already connected to destination")
		return nil // Already connected
	}

	d.Status = StatusConnecting
	d.logger.Info("Connecting to destination", "url", d.URL)

	// Create RTMP client
	d.logger.Debug("Creating RTMP client", "url", d.URL)
	client, err := d.clientFactory(d.URL)
	if err != nil {
		d.Status = StatusError
		d.LastError = err
		d.logger.Error("Failed to create RTMP client", "url", d.URL, "error", err)
		return fmt.Errorf("create client: %w", err)
	}

	// Perform RTMP handshake and setup
	d.logger.Debug("Performing RTMP handshake and connect", "url", d.URL)
	if err := client.Connect(); err != nil {
		d.Status = StatusError
		d.LastError = err
		d.logger.Error("Failed to connect RTMP client", "url", d.URL, "error", err)
		return fmt.Errorf("client connect: %w", err)
	}

	// Start publishing to the destination
	d.logger.Debug("Starting publish to destination", "url", d.URL)
	if err := client.Publish(); err != nil {
		d.Status = StatusError
		d.LastError = err
		d.logger.Error("Failed to publish to destination", "url", d.URL, "error", err)
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
	d.logger.Debug("SendMessage called", "type_id", msg.TypeID, "payload_len", len(msg.Payload), "timestamp", msg.Timestamp)

	d.mu.RLock()
	client := d.Client
	status := d.Status
	d.mu.RUnlock()

	d.logger.Debug("Destination status check", "status", status.String(), "client_nil", client == nil)

	if status != StatusConnected || client == nil {
		d.mu.Lock()
		d.Metrics.MessagesDropped++
		d.mu.Unlock()
		d.logger.Warn("Destination not connected, dropping message", "status", status.String(), "type_id", msg.TypeID)
		return fmt.Errorf("destination not connected (status: %v)", status)
	}

	// Send the message based on type
	var err error
	d.logger.Debug("Calling client send method", "type_id", msg.TypeID, "method", func() string {
		if msg.TypeID == 8 {
			return "SendAudio"
		}
		return "SendVideo"
	}())

	switch msg.TypeID {
	case 8: // Audio message
		err = client.SendAudio(msg.Timestamp, msg.Payload)
	case 9: // Video message
		err = client.SendVideo(msg.Timestamp, msg.Payload)
	default:
		// Skip non-media messages for relay
		d.logger.Debug("Skipping non-media message", "type_id", msg.TypeID)
		return nil
	}

	if err != nil {
		d.mu.Lock()
		d.Status = StatusError
		d.LastError = err
		d.Metrics.MessagesDropped++
		d.mu.Unlock()
		d.logger.Error("Client send method failed", "type_id", msg.TypeID, "error", err)
		return fmt.Errorf("send message: %w", err)
	}

	// Update metrics
	d.mu.Lock()
	d.Metrics.MessagesSent++
	d.Metrics.BytesSent += uint64(len(msg.Payload))
	d.Metrics.LastSentTime = time.Now()
	d.mu.Unlock()

	d.logger.Debug("SendMessage completed successfully", "type_id", msg.TypeID, "bytes_sent", len(msg.Payload))
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

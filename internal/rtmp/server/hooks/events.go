// Event Hook System
// =================
// The hook system lets the RTMP server notify external systems when important
// events occur (e.g. a client connects, a stream starts publishing, etc.).
//
// Three types of hooks are supported:
//   - Webhook: sends an HTTP POST with JSON event data to a URL
//   - Shell: runs a shell script with event data as environment variables
//   - Stdio: prints structured event data to stderr (for log pipelines)
//
// This file defines the event types and the Event data structure.
package hooks

import (
	"time"
)

// EventType represents the type of RTMP event that occurred
type EventType string

const (
	// Connection events
	EventConnectionAccept  EventType = "connection_accept"
	EventConnectionClose   EventType = "connection_close"
	EventHandshakeComplete EventType = "handshake_complete"

	// Stream events
	EventStreamCreate EventType = "stream_create"
	EventStreamDelete EventType = "stream_delete"
	EventPublishStart EventType = "publish_start"
	EventPublishStop  EventType = "publish_stop"
	EventPlayStart    EventType = "play_start"
	EventPlayStop     EventType = "play_stop"

	// Media events
	EventCodecDetected EventType = "codec_detected"
)

// Event represents a single RTMP event that can trigger hooks.
// It carries enough context for any hook to act on: what happened (Type),
// which connection (ConnID), which stream (StreamKey), and event-specific
// details (Data). Events are serialized to JSON for webhooks and stdio output.
type Event struct {
	Type      EventType              `json:"type"`                // What happened (e.g. "publish_start")
	Timestamp int64                  `json:"timestamp"`           // Unix timestamp when the event occurred
	ConnID    string                 `json:"conn_id,omitempty"`   // Connection that triggered the event
	StreamKey string                 `json:"stream_key,omitempty"` // Stream key (e.g. "live/mystream")
	Data      map[string]interface{} `json:"data,omitempty"`      // Event-specific key-value data
}

// NewEvent creates a new event with the current timestamp
func NewEvent(eventType EventType) *Event {
	return &Event{
		Type:      eventType,
		Timestamp: time.Now().Unix(),
		Data:      make(map[string]interface{}),
	}
}

// WithConnID sets the connection ID for the event
func (e *Event) WithConnID(connID string) *Event {
	e.ConnID = connID
	return e
}

// WithStreamKey sets the stream key for the event
func (e *Event) WithStreamKey(streamKey string) *Event {
	e.StreamKey = streamKey
	return e
}

// WithData adds data fields to the event
func (e *Event) WithData(key string, value interface{}) *Event {
	if e.Data == nil {
		e.Data = make(map[string]interface{})
	}
	e.Data[key] = value
	return e
}

// String returns a human-readable string representation of the event
func (e *Event) String() string {
	if e.StreamKey != "" {
		return string(e.Type) + ":" + e.StreamKey
	}
	if e.ConnID != "" {
		return string(e.Type) + ":" + e.ConnID
	}
	return string(e.Type)
}

// Event system for RTMP server hooks
// This file defines the core event types and data structures used by the hook system.
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

// Event represents a single RTMP event that can trigger hooks
type Event struct {
	Type      EventType              `json:"type"`
	Timestamp int64                  `json:"timestamp"`
	ConnID    string                 `json:"conn_id,omitempty"`
	StreamKey string                 `json:"stream_key,omitempty"`
	Data      map[string]interface{} `json:"data,omitempty"`
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

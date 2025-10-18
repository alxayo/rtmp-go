// Webhook hook implementation
// This file implements a hook that sends HTTP POST requests to webhook URLs
package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// WebhookHook sends HTTP POST requests to webhook URLs when events occur
type WebhookHook struct {
	id      string
	url     string
	headers map[string]string
	timeout time.Duration
	client  *http.Client
}

// NewWebhookHook creates a new webhook hook
func NewWebhookHook(id, url string, timeout time.Duration) *WebhookHook {
	return &WebhookHook{
		id:      id,
		url:     url,
		headers: make(map[string]string),
		timeout: timeout,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// SetHeaders sets custom HTTP headers for the webhook request
func (h *WebhookHook) SetHeaders(headers map[string]string) *WebhookHook {
	h.headers = headers
	return h
}

// AddHeader adds a single HTTP header
func (h *WebhookHook) AddHeader(key, value string) *WebhookHook {
	if h.headers == nil {
		h.headers = make(map[string]string)
	}
	h.headers[key] = value
	return h
}

// Execute sends the event data as JSON to the webhook URL
func (h *WebhookHook) Execute(ctx context.Context, event Event) error {
	// Marshal event to JSON
	jsonData, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("webhook hook %s: failed to marshal JSON: %w", h.id, err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", h.url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("webhook hook %s: failed to create request: %w", h.id, err)
	}

	// Set default content type
	req.Header.Set("Content-Type", "application/json")

	// Set custom headers
	for key, value := range h.headers {
		req.Header.Set(key, value)
	}

	// Execute request
	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook hook %s: request failed: %w", h.id, err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook hook %s: server returned status %d", h.id, resp.StatusCode)
	}

	return nil
}

// Type returns the hook type
func (h *WebhookHook) Type() string {
	return "webhook"
}

// ID returns the hook ID
func (h *WebhookHook) ID() string {
	return h.id
}

package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// CallbackValidator delegates authentication to an external HTTP service.
// For each publish/play request, it sends a JSON POST to the configured URL.
// The external service responds with an HTTP status code:
//
//   - 200 OK → allow the request
//   - Any other status → deny the request
//
// # Request Body (JSON)
//
// New per-event format:
//	{
//	  "streamKeyHash": "event-slug-abc123def456",
//	  "token":         "rtmp_abc123def456xyz789",
//	  "action":        "publish" or "play",
//	  "publisherIp":   "192.168.1.100:54321"
//	}
//
// Legacy format (for backward compatibility):
//	{
//	  "action":      "publish" or "play",
//	  "app":         "live",
//	  "stream_name": "mystream",
//	  "stream_key":  "live/mystream",
//	  "token":       "abc123",
//	  "remote_addr": "192.168.1.100:54321"
//	}
type CallbackValidator struct {
	URL                   string       // webhook URL (e.g. "https://auth.example.com/validate")
	Client                *http.Client // HTTP client with configured timeout
	InternalAPIKey        string       // X-Internal-Api-Key header for streamgate auth
	EnablePerEventTokens  bool         // true = use new per-event format; false = legacy format
}

// NewCallbackValidator creates a CallbackValidator with the given webhook URL
// and HTTP timeout. The timeout controls how long the server waits for the
// webhook response before treating it as a failure.
func NewCallbackValidator(callbackURL string, timeout time.Duration) *CallbackValidator {
	return &CallbackValidator{
		URL:    callbackURL,
		Client: &http.Client{Timeout: timeout},
	}
}

// NewCallbackValidatorWithAPIKey creates a CallbackValidator for per-event tokens.
// This version uses X-Internal-Api-Key header auth and streamKeyHash-based validation.
func NewCallbackValidatorWithAPIKey(callbackURL string, timeout time.Duration, apiKey string) *CallbackValidator {
	return &CallbackValidator{
		URL:                  callbackURL,
		Client:               &http.Client{Timeout: timeout},
		InternalAPIKey:       apiKey,
		EnablePerEventTokens: true,
	}
}

// callbackRequest is the JSON body sent to the webhook (new per-event format).
type callbackRequest struct {
	StreamKeyHash string `json:"streamKeyHash"`
	Token         string `json:"token"`
	Action        string `json:"action"`
	PublisherIp   string `json:"publisherIp"`
}

// legacyCallbackRequest is the old JSON format for backward compatibility.
type legacyCallbackRequest struct {
	Action     string `json:"action"`
	App        string `json:"app"`
	StreamName string `json:"stream_name"`
	StreamKey  string `json:"stream_key"`
	Token      string `json:"token"`
	RemoteAddr string `json:"remote_addr"`
}

// ValidatePublish sends a "publish" callback to the webhook.
func (v *CallbackValidator) ValidatePublish(ctx context.Context, req *Request) error {
	return v.call(ctx, "publish", req)
}

// ValidatePlay sends a "play" callback to the webhook.
func (v *CallbackValidator) ValidatePlay(ctx context.Context, req *Request) error {
	return v.call(ctx, "play", req)
}

// call performs the HTTP POST to the webhook and interprets the response.
func (v *CallbackValidator) call(ctx context.Context, action string, req *Request) error {
	var (
		data       []byte
		err        error
		httpReq    *http.Request
	)

	if v.EnablePerEventTokens {
		// New per-event token format
		// Stream name is the streamKeyHash (e.g., "event-slug-abc123def456")
		// Token comes from query params (e.g., ?token=rtmp_abc123def456xyz789)
		token := req.QueryParams["token"]
		if token == "" {
			return ErrTokenMissing
		}

		body := callbackRequest{
			StreamKeyHash: req.StreamName, // StreamName is the hash after parsing URL
			Token:         token,
			Action:        action,
			PublisherIp:   req.RemoteAddr,
		}

		data, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("auth callback marshal: %w", err)
		}

		httpReq, err = http.NewRequestWithContext(ctx, http.MethodPost, v.URL, bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("auth callback request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("X-Internal-Api-Key", v.InternalAPIKey)
	} else {
		// Legacy format (backward compatible)
		body := legacyCallbackRequest{
			Action:     action,
			App:        req.App,
			StreamName: req.StreamName,
			StreamKey:  req.StreamKey,
			Token:      req.QueryParams["token"],
			RemoteAddr: req.RemoteAddr,
		}

		data, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("auth callback marshal: %w", err)
		}

		httpReq, err = http.NewRequestWithContext(ctx, http.MethodPost, v.URL, bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("auth callback request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
	}

	resp, err := v.Client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("auth callback failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}
	return ErrUnauthorized
}

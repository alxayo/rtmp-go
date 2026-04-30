package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestCallbackValidator exercises the webhook validator against a local
// test server returning different HTTP status codes.
func TestCallbackValidator(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    error
	}{
		{"200_allow", http.StatusOK, nil},
		{"401_deny", http.StatusUnauthorized, ErrUnauthorized},
		{"403_deny", http.StatusForbidden, ErrUnauthorized},
		{"500_deny", http.StatusInternalServerError, ErrUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Start a local HTTP server that responds with the test status code
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify the request is well-formed
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}
				if r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("expected application/json, got %s", r.Header.Get("Content-Type"))
				}
				w.WriteHeader(tt.statusCode)
			}))
			defer srv.Close()

			v := NewCallbackValidator(srv.URL, 5*time.Second)
			req := &Request{
				App:         "live",
				StreamName:  "test",
				StreamKey:   "live/test",
				QueryParams: map[string]string{"token": "abc"},
				RemoteAddr:  "1.2.3.4:5678",
			}

			// Test both ValidatePublish and ValidatePlay
			err := v.ValidatePublish(context.Background(), req)
			if tt.wantErr == nil && err != nil {
				t.Errorf("ValidatePublish: expected nil, got %v", err)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("ValidatePublish: expected %v, got %v", tt.wantErr, err)
			}

			err = v.ValidatePlay(context.Background(), req)
			if tt.wantErr == nil && err != nil {
				t.Errorf("ValidatePlay: expected nil, got %v", err)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("ValidatePlay: expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestCallbackValidator_PerEventPayloadIncludesClientAddressFields(t *testing.T) {
	var got callbackRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Internal-Api-Key") != "test-key" {
			t.Fatalf("expected X-Internal-Api-Key header")
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Per-event mode is the StreamGate integration path that sends the newer JSON body.
	v := NewCallbackValidatorWithAPIKey(srv.URL, 5*time.Second, "test-key")
	err := v.ValidatePlay(context.Background(), &Request{
		StreamName:  "event-slug-abc123def456",
		QueryParams: map[string]string{"token": "abc123def456ghi789jkl012"},
		RemoteAddr:  "203.0.113.42:54321",
	})
	if err != nil {
		t.Fatalf("ValidatePlay: %v", err)
	}

	if got.StreamKeyHash != "event-slug-abc123def456" || got.Action != "play" {
		t.Fatalf("unexpected request body: %+v", got)
	}
	// All three fields should carry the same peer address during the compatibility window.
	if got.PublisherIp != "203.0.113.42:54321" || got.ClientIp != got.PublisherIp || got.RemoteAddr != got.PublisherIp {
		t.Fatalf("expected all address fields to match remote addr, got %+v", got)
	}
}

// TestCallbackValidator_Timeout verifies that a slow webhook triggers a
// timeout error (not ErrUnauthorized).
func TestCallbackValidator_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	v := NewCallbackValidator(srv.URL, 50*time.Millisecond) // very short timeout
	err := v.ValidatePublish(context.Background(), &Request{
		StreamKey:   "live/test",
		QueryParams: map[string]string{"token": "x"},
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	// Should be a transport error, not ErrUnauthorized
	if errors.Is(err, ErrUnauthorized) {
		t.Fatal("timeout should not be ErrUnauthorized")
	}
}

// TestCallbackValidator_ContextCanceled verifies that a canceled context
// propagates correctly.
func TestCallbackValidator_ContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	v := NewCallbackValidator(srv.URL, 10*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := v.ValidatePublish(ctx, &Request{
		StreamKey:   "live/test",
		QueryParams: map[string]string{"token": "x"},
	})
	if err == nil {
		t.Fatal("expected error on canceled context")
	}
}

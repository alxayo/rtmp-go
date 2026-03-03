package auth

import (
	"context"
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

			err := v.ValidatePublish(context.Background(), req)
			if tt.wantErr == nil && err != nil {
				t.Errorf("expected nil, got %v", err)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("expected %v, got %v", tt.wantErr, err)
			}
		})
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

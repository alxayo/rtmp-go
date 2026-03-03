package auth

import (
	"context"
	"errors"
	"testing"
)

// TestTokenValidator covers the main scenarios for static token validation.
func TestTokenValidator(t *testing.T) {
	v := &TokenValidator{
		Tokens: map[string]string{
			"live/s1": "secret",
			"live/s2": "other",
		},
	}
	ctx := context.Background()

	tests := []struct {
		name      string
		streamKey string
		token     string
		wantErr   error
	}{
		{"valid_token", "live/s1", "secret", nil},
		{"valid_token_other_stream", "live/s2", "other", nil},
		{"wrong_token", "live/s1", "wrong", ErrUnauthorized},
		{"missing_token", "live/s1", "", ErrTokenMissing},
		{"unknown_stream_key", "live/unknown", "any", ErrUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &Request{
				StreamKey:   tt.streamKey,
				QueryParams: map[string]string{},
			}
			if tt.token != "" {
				req.QueryParams["token"] = tt.token
			}

			// Test both publish and play — same behavior for TokenValidator
			t.Run("publish", func(t *testing.T) {
				err := v.ValidatePublish(ctx, req)
				if tt.wantErr == nil && err != nil {
					t.Errorf("expected nil, got %v", err)
				}
				if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
					t.Errorf("expected %v, got %v", tt.wantErr, err)
				}
			})
			t.Run("play", func(t *testing.T) {
				err := v.ValidatePlay(ctx, req)
				if tt.wantErr == nil && err != nil {
					t.Errorf("expected nil, got %v", err)
				}
				if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
					t.Errorf("expected %v, got %v", tt.wantErr, err)
				}
			})
		})
	}
}

// TestTokenValidator_EmptyMap verifies that an empty Tokens map rejects
// all requests.
func TestTokenValidator_EmptyMap(t *testing.T) {
	v := &TokenValidator{Tokens: map[string]string{}}
	err := v.ValidatePublish(context.Background(), &Request{
		StreamKey:   "live/s1",
		QueryParams: map[string]string{"token": "anything"},
	})
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized, got %v", err)
	}
}

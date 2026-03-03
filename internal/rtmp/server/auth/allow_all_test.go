package auth

import (
	"context"
	"testing"
)

// TestAllowAllValidator_AlwaysAllows verifies that AllowAllValidator
// returns nil for every request, regardless of content.
func TestAllowAllValidator_AlwaysAllows(t *testing.T) {
	v := &AllowAllValidator{}
	ctx := context.Background()

	tests := []struct {
		name string
		req  *Request
	}{
		{"nil_request", nil},
		{"empty_request", &Request{}},
		{"full_request", &Request{
			App:         "live",
			StreamName:  "test",
			StreamKey:   "live/test",
			QueryParams: map[string]string{"token": "abc"},
			RemoteAddr:  "1.2.3.4:5678",
		}},
	}

	for _, tt := range tests {
		if err := v.ValidatePublish(ctx, tt.req); err != nil {
			t.Errorf("%s: ValidatePublish returned error: %v", tt.name, err)
		}
		if err := v.ValidatePlay(ctx, tt.req); err != nil {
			t.Errorf("%s: ValidatePlay returned error: %v", tt.name, err)
		}
	}
}

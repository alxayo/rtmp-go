package auth

import "context"

// TokenValidator validates requests against a static in-memory map of
// stream keys to expected tokens. The map is populated once at server
// startup from CLI flags (e.g. -auth-token "live/stream1=secret123")
// and is never modified at runtime, making it inherently thread-safe.
//
// Token lookup flow:
//  1. Extract "token" from req.QueryParams
//  2. Look up req.StreamKey in the Tokens map
//  3. Compare: if missing or mismatched → reject
type TokenValidator struct {
	Tokens map[string]string // streamKey → expected token (e.g. "live/stream1" → "secret123")
}

// ValidatePublish checks the token for a publish request.
func (v *TokenValidator) ValidatePublish(_ context.Context, req *Request) error {
	return v.validate(req)
}

// ValidatePlay checks the token for a play (subscribe) request.
func (v *TokenValidator) ValidatePlay(_ context.Context, req *Request) error {
	return v.validate(req)
}

// validate performs the actual token comparison for both publish and play.
func (v *TokenValidator) validate(req *Request) error {
	token := req.QueryParams["token"]
	if token == "" {
		return ErrTokenMissing
	}
	expected, exists := v.Tokens[req.StreamKey]
	if !exists || token != expected {
		return ErrUnauthorized
	}
	return nil
}

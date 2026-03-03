package auth

import "context"

// AllowAllValidator permits every publish and play request without checking
// credentials. This is the default validator when no authentication mode is
// configured, preserving backward compatibility with existing deployments.
type AllowAllValidator struct{}

// ValidatePublish always returns nil (allow).
func (v *AllowAllValidator) ValidatePublish(_ context.Context, _ *Request) error { return nil }

// ValidatePlay always returns nil (allow).
func (v *AllowAllValidator) ValidatePlay(_ context.Context, _ *Request) error { return nil }

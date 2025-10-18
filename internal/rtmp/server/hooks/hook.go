// Hook interface and basic hook implementations
// This file defines the core hook interface and provides basic implementations
package hooks

import (
	"context"
)

// Hook represents a handler that can be executed when an event occurs
type Hook interface {
	// Execute runs the hook with the given event
	Execute(ctx context.Context, event Event) error

	// Type returns the hook type identifier
	Type() string

	// ID returns a unique identifier for this hook instance
	ID() string
}

// HookConfig represents the configuration for hooks
type HookConfig struct {
	// Timeout for hook execution (default: 30s)
	Timeout string `json:"timeout"`

	// Maximum number of concurrent hook executions (default: 10)
	Concurrency int `json:"concurrency"`

	// Whether to enable structured stdio output
	StdioFormat string `json:"stdio_format"` // "json", "env", or ""
}

// DefaultHookConfig returns a configuration with sensible defaults
func DefaultHookConfig() HookConfig {
	return HookConfig{
		Timeout:     "30s",
		Concurrency: 10,
		StdioFormat: "",
	}
}

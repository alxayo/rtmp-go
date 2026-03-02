// Hook System Core Types
// ======================
// This file defines the Hook interface that all hook implementations must
// satisfy, and the HookConfig that controls execution behavior.
package hooks

import (
	"context"
)

// Hook is the interface that all event handlers must implement.
// The server calls Execute() when a matching event occurs. Each hook has a
// Type ("webhook", "shell", "stdio") and a unique ID for management.
type Hook interface {
	Execute(ctx context.Context, event Event) error // Run the hook for the given event
	Type() string                                   // Hook type identifier (e.g. "webhook")
	ID() string                                     // Unique identifier for this hook instance
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

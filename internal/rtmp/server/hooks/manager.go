// Hook manager implementation
// This file implements the central manager for registering and executing hooks
package hooks

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// HookManager manages hook registration and execution
type HookManager struct {
	hooks     map[EventType][]Hook
	stdioHook *StdioHook
	mu        sync.RWMutex
	pool      *executionPool
	logger    *slog.Logger
	config    HookConfig
}

// NewHookManager creates a new hook manager
func NewHookManager(config HookConfig, logger *slog.Logger) *HookManager {
	if logger == nil {
		logger = slog.Default()
	}

	// Parse timeout
	_, err := time.ParseDuration(config.Timeout)
	if err != nil {
		logger.Warn("Invalid hook timeout, using default", "timeout", config.Timeout, "error", err)
	}

	manager := &HookManager{
		hooks:  make(map[EventType][]Hook),
		logger: logger,
		config: config,
		pool:   newExecutionPool(config.Concurrency, logger),
	}

	// Enable stdio output if configured
	if config.StdioFormat != "" {
		manager.EnableStdioOutput(config.StdioFormat)
	}

	return manager
}

// RegisterHook registers a hook for the specified event type
func (hm *HookManager) RegisterHook(eventType EventType, hook Hook) error {
	if hook == nil {
		return fmt.Errorf("cannot register nil hook")
	}

	hm.mu.Lock()
	defer hm.mu.Unlock()

	hm.hooks[eventType] = append(hm.hooks[eventType], hook)
	hm.logger.Info("Hook registered",
		"event_type", eventType,
		"hook_type", hook.Type(),
		"hook_id", hook.ID())

	return nil
}

// UnregisterHook removes a hook by ID from the specified event type
func (hm *HookManager) UnregisterHook(eventType EventType, hookID string) bool {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	hooks := hm.hooks[eventType]
	for i, hook := range hooks {
		if hook.ID() == hookID {
			// Remove hook from slice
			hm.hooks[eventType] = append(hooks[:i], hooks[i+1:]...)
			hm.logger.Info("Hook unregistered",
				"event_type", eventType,
				"hook_id", hookID)
			return true
		}
	}

	return false
}

// TriggerEvent executes all registered hooks for the given event
func (hm *HookManager) TriggerEvent(ctx context.Context, event Event) {
	if hm == nil {
		return
	}

	// Get hooks for this event type
	hm.mu.RLock()
	hooks := make([]Hook, len(hm.hooks[event.Type]))
	copy(hooks, hm.hooks[event.Type])
	hm.mu.RUnlock()

	// Add stdio hook if enabled
	if hm.stdioHook != nil {
		hooks = append(hooks, hm.stdioHook)
	}

	if len(hooks) == 0 {
		return // No hooks registered for this event
	}

	hm.logger.Debug("Triggering event",
		"event_type", event.Type,
		"hook_count", len(hooks),
		"event", event.String())

	// Execute hooks asynchronously
	for _, hook := range hooks {
		hm.pool.execute(ctx, hook, event)
	}
}

// EnableStdioOutput enables structured output to stdout/stderr
func (hm *HookManager) EnableStdioOutput(format string) error {
	if format != "json" && format != "env" {
		return fmt.Errorf("unsupported stdio format: %s", format)
	}

	hm.mu.Lock()
	defer hm.mu.Unlock()

	hm.stdioHook = NewStdioHook("stdio", format)
	hm.logger.Info("Stdio output enabled", "format", format)

	return nil
}

// DisableStdioOutput disables structured output
func (hm *HookManager) DisableStdioOutput() {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	hm.stdioHook = nil
	hm.logger.Info("Stdio output disabled")
}

// GetStats returns statistics about registered hooks
func (hm *HookManager) GetStats() map[string]interface{} {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	stats := map[string]interface{}{
		"event_types":   len(hm.hooks),
		"total_hooks":   0,
		"stdio_enabled": hm.stdioHook != nil,
		"pool_size":     hm.pool.size,
		"pool_active":   hm.pool.active,
	}

	hooksByType := make(map[string]int)
	totalHooks := 0

	for eventType, hooks := range hm.hooks {
		hooksByType[string(eventType)] = len(hooks)
		totalHooks += len(hooks)
	}

	stats["total_hooks"] = totalHooks
	stats["hooks_by_type"] = hooksByType

	return stats
}

// Close shuts down the hook manager and waits for pending executions
func (hm *HookManager) Close() error {
	if hm.pool != nil {
		hm.pool.close()
	}
	hm.logger.Info("Hook manager closed")
	return nil
}

// executionPool manages concurrent hook execution
type executionPool struct {
	workers chan struct{}
	size    int
	active  int
	mu      sync.Mutex
	logger  *slog.Logger
}

// newExecutionPool creates a new execution pool
func newExecutionPool(size int, logger *slog.Logger) *executionPool {
	if size <= 0 {
		size = 10 // default
	}

	return &executionPool{
		workers: make(chan struct{}, size),
		size:    size,
		logger:  logger,
	}
}

// execute runs a hook in the execution pool
func (ep *executionPool) execute(ctx context.Context, hook Hook, event Event) {
	go func() {
		// Acquire worker slot (blocks if pool is full)
		ep.workers <- struct{}{}
		defer func() { <-ep.workers }()

		ep.mu.Lock()
		ep.active++
		ep.mu.Unlock()

		defer func() {
			ep.mu.Lock()
			ep.active--
			ep.mu.Unlock()
		}()

		// Execute hook with timeout
		start := time.Now()
		err := hook.Execute(ctx, event)
		duration := time.Since(start)

		if err != nil {
			ep.logger.Error("Hook execution failed",
				"hook_type", hook.Type(),
				"hook_id", hook.ID(),
				"event_type", event.Type,
				"duration_ms", duration.Milliseconds(),
				"error", err)
		} else {
			ep.logger.Debug("Hook executed successfully",
				"hook_type", hook.Type(),
				"hook_id", hook.ID(),
				"event_type", event.Type,
				"duration_ms", duration.Milliseconds())
		}
	}()
}

// close shuts down the execution pool
func (ep *executionPool) close() {
	// Wait for all workers to finish by acquiring all slots
	for i := 0; i < cap(ep.workers); i++ {
		ep.workers <- struct{}{}
	}
}

// Package hooks – tests for the event hook system.
//
// The hook system lets the RTMP server execute external actions when
// lifecycle events occur (connection accepted, stream published, etc.).
// Three hook types exist:
//   - ShellHook:   runs a shell command with event data as env vars.
//   - StdioHook:   writes structured event data to a stdio stream.
//   - WebhookHook: POSTs JSON-encoded event data to an HTTP endpoint.
//
// A HookManager coordinates registration, un-registration, and triggering
// of hooks, fanning out events to all registered hooks of a given type.
//
// These tests verify the builder pattern for Event objects, the identity
// and type metadata of each hook implementation, and the basic lifecycle
// of the HookManager (register → trigger → unregister → close).
package hooks

import (
	"context"
	"testing"
	"time"
)

// TestEvent verifies the Event builder pattern used throughout the hook system.
//
// Events carry:
//   - Type      – an enum-like string (e.g. EventConnectionAccept)
//   - ConnID    – the connection that triggered the event
//   - StreamKey – the stream key involved (e.g. "test/stream")
//   - Data      – arbitrary key/value metadata (WithData adds entries)
//   - String()  – human-readable form "<type>:<stream_key>"
//
// The test exercises every builder method and confirms the resulting fields.
func TestEvent(t *testing.T) {
	event := NewEvent(EventConnectionAccept).
		WithConnID("test-conn").
		WithStreamKey("test/stream").
		WithData("client_ip", "192.168.1.100").
		WithData("client_port", 12345)

	if event.Type != EventConnectionAccept {
		t.Errorf("Expected event type %s, got %s", EventConnectionAccept, event.Type)
	}

	if event.ConnID != "test-conn" {
		t.Errorf("Expected conn ID 'test-conn', got %s", event.ConnID)
	}

	if event.StreamKey != "test/stream" {
		t.Errorf("Expected stream key 'test/stream', got %s", event.StreamKey)
	}

	if event.Data["client_ip"] != "192.168.1.100" {
		t.Errorf("Expected client_ip '192.168.1.100', got %v", event.Data["client_ip"])
	}

	if event.Data["client_port"] != 12345 {
		t.Errorf("Expected client_port 12345, got %v", event.Data["client_port"])
	}

	// Test string representation
	str := event.String()
	if str != "connection_accept:test/stream" {
		t.Errorf("Expected string 'connection_accept:test/stream', got %s", str)
	}
}

// TestShellHook verifies ShellHook identity and metadata.
//
// A ShellHook wraps a command path (e.g. "/bin/echo") that the manager
// will execute when the subscribed event fires.  This test creates two
// variants – the simple constructor (NewShellHook) and the explicit one
// (NewShellHookWithCommand) – and checks their Type() and ID() accessors.
func TestShellHook(t *testing.T) {
	hook := NewShellHook("test-hook", "/bin/echo", 10*time.Second)

	if hook.Type() != "shell" {
		t.Errorf("Expected hook type 'shell', got %s", hook.Type())
	}

	if hook.ID() != "test-hook" {
		t.Errorf("Expected hook ID 'test-hook', got %s", hook.ID())
	}

	// Test with custom command
	customHook := NewShellHookWithCommand("custom", "/bin/true", []string{}, 5*time.Second)
	if customHook.command != "/bin/true" {
		t.Errorf("Expected command '/bin/true', got %s", customHook.command)
	}
}

// TestHookManager exercises the full lifecycle of the manager:
//
//  1. Create a manager with DefaultHookConfig and a nil logger.
//  2. Register a ShellHook for EventConnectionAccept.
//  3. Verify GetStats() reports 1 total hook.
//  4. UnregisterHook by event+ID → confirm success.
//  5. TriggerEvent with no remaining hooks → must not panic.
//  6. Close the manager to release resources.
//
// This is a smoke test; shell execution itself requires a real binary
// and is therefore not exercised here.
func TestHookManager(t *testing.T) {
	config := DefaultHookConfig()
	manager := NewHookManager(config, nil)

	// Test hook registration
	hook := NewShellHook("test", "/bin/true", 10*time.Second)
	err := manager.RegisterHook(EventConnectionAccept, hook)
	if err != nil {
		t.Errorf("Failed to register hook: %v", err)
	}

	// Test stats
	stats := manager.GetStats()
	if stats["total_hooks"] != 1 {
		t.Errorf("Expected 1 total hook, got %v", stats["total_hooks"])
	}

	// Test unregistration
	success := manager.UnregisterHook(EventConnectionAccept, "test")
	if !success {
		t.Error("Failed to unregister hook")
	}

	// Test event triggering (should not crash with no hooks)
	event := NewEvent(EventConnectionAccept)
	manager.TriggerEvent(context.Background(), *event)

	// Clean up
	manager.Close()
}

// TestStdioHook verifies StdioHook constructor stores type, ID, and format.
//
// A StdioHook writes event data to a stdio stream (stdout/stderr) in
// the given format ("json", "text", etc.).  This test only checks the
// metadata; actual write behavior would be tested with a captured writer.
func TestStdioHook(t *testing.T) {
	hook := NewStdioHook("stdio-test", "json")

	if hook.Type() != "stdio" {
		t.Errorf("Expected hook type 'stdio', got %s", hook.Type())
	}

	if hook.ID() != "stdio-test" {
		t.Errorf("Expected hook ID 'stdio-test', got %s", hook.ID())
	}

	if hook.format != "json" {
		t.Errorf("Expected format 'json', got %s", hook.format)
	}
}

// TestWebhookHook verifies WebhookHook constructor and header management.
//
// A WebhookHook POSTs event data as JSON to the configured URL.
// This test checks:
//   - Type() returns "webhook".
//   - ID()   returns the given identifier.
//   - The URL is stored correctly.
//   - AddHeader stores custom HTTP headers (e.g. Authorization).
//
// No real HTTP calls are made; this is a pure in-memory unit check.
func TestWebhookHook(t *testing.T) {
	hook := NewWebhookHook("webhook-test", "https://example.com/webhook", 30*time.Second)

	if hook.Type() != "webhook" {
		t.Errorf("Expected hook type 'webhook', got %s", hook.Type())
	}

	if hook.ID() != "webhook-test" {
		t.Errorf("Expected hook ID 'webhook-test', got %s", hook.ID())
	}

	if hook.url != "https://example.com/webhook" {
		t.Errorf("Expected URL 'https://example.com/webhook', got %s", hook.url)
	}

	// Test adding headers
	hook.AddHeader("Authorization", "Bearer token")
	if hook.headers["Authorization"] != "Bearer token" {
		t.Errorf("Expected Authorization header 'Bearer token', got %s", hook.headers["Authorization"])
	}
}

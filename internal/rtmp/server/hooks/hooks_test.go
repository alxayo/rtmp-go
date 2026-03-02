// Hook system tests
package hooks

import (
	"context"
	"testing"
	"time"
)

// TestEvent tests basic event creation and functionality
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

// TestShellHook tests shell hook creation and basic functionality
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

// TestHookManager tests hook manager registration and basic functionality
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

// TestStdioHook tests stdio hook creation and basic functionality
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

// TestWebhookHook tests webhook hook creation and basic functionality
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

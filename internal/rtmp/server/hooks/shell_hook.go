// Shell hook implementation
// This file implements a hook that executes shell scripts with environment variables
package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ShellHook executes shell scripts when events occur
type ShellHook struct {
	id       string
	command  string
	args     []string
	env      []string
	passJSON bool
	timeout  time.Duration
}

// NewShellHook creates a new shell hook
func NewShellHook(id, scriptPath string, timeout time.Duration) *ShellHook {
	return &ShellHook{
		id:      id,
		command: "/bin/bash",
		args:    []string{scriptPath},
		env:     []string{},
		timeout: timeout,
	}
}

// NewShellHookWithCommand creates a shell hook with a custom command
func NewShellHookWithCommand(id, command string, args []string, timeout time.Duration) *ShellHook {
	return &ShellHook{
		id:      id,
		command: command,
		args:    args,
		env:     []string{},
		timeout: timeout,
	}
}

// SetPassJSON enables passing event data as JSON via stdin
func (h *ShellHook) SetPassJSON(passJSON bool) *ShellHook {
	h.passJSON = passJSON
	return h
}

// SetEnv sets additional environment variables for the script
func (h *ShellHook) SetEnv(env []string) *ShellHook {
	h.env = env
	return h
}

// Execute runs the shell script with event data passed as environment variables
func (h *ShellHook) Execute(ctx context.Context, event Event) error {
	// Create context with timeout
	execCtx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	// Create command
	cmd := exec.CommandContext(execCtx, h.command, h.args...)

	// Build environment variables from event data
	env := h.buildEnvironment(event)
	cmd.Env = append(cmd.Env, env...)

	// Pass JSON via stdin if enabled
	if h.passJSON {
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return fmt.Errorf("shell hook %s: failed to create stdin pipe: %w", h.id, err)
		}

		go func() {
			defer stdin.Close()
			if err := json.NewEncoder(stdin).Encode(event); err != nil {
				// Log error but don't fail the hook execution
				// The script might not need JSON input
			}
		}()
	}

	// Execute the command
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("shell hook %s: execution failed: %w", h.id, err)
	}

	return nil
}

// Type returns the hook type
func (h *ShellHook) Type() string {
	return "shell"
}

// ID returns the hook ID
func (h *ShellHook) ID() string {
	return h.id
}

// buildEnvironment creates environment variables from event data
func (h *ShellHook) buildEnvironment(event Event) []string {
	env := make([]string, 0)

	// Add custom environment variables
	env = append(env, h.env...)

	// Add core event data
	env = append(env, "RTMP_EVENT_TYPE="+string(event.Type))
	env = append(env, fmt.Sprintf("RTMP_TIMESTAMP=%d", event.Timestamp))

	if event.ConnID != "" {
		env = append(env, "RTMP_CONN_ID="+event.ConnID)
	}

	if event.StreamKey != "" {
		env = append(env, "RTMP_STREAM_KEY="+event.StreamKey)
	}

	// Add event-specific data as environment variables
	for key, value := range event.Data {
		envKey := "RTMP_" + strings.ToUpper(key)
		envValue := fmt.Sprintf("%v", value)
		env = append(env, envKey+"="+envValue)
	}

	return env
}

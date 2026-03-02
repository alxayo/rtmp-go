// Stdio Hook
// ==========
// Prints structured event data to stderr for consumption by log pipelines,
// monitoring tools, or parent processes. Supports two output formats:
//   - "json": one JSON object per line, prefixed with "RTMP_EVENT: "
//   - "env": shell-style KEY=VALUE lines (easy to source in scripts)
package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// StdioHook prints event data to stderr in a structured format.
type StdioHook struct {
	id     string   // unique identifier
	format string   // output format: "json" or "env"
	output *os.File // destination (default: stderr to avoid mixing with server stdout)
}

// NewStdioHook creates a new stdio hook
func NewStdioHook(id, format string) *StdioHook {
	return &StdioHook{
		id:     id,
		format: format,
		output: os.Stderr, // Use stderr to avoid mixing with normal server output
	}
}

// SetOutput sets the output destination (default: stderr)
func (h *StdioHook) SetOutput(output *os.File) *StdioHook {
	h.output = output
	return h
}

// Execute outputs the event data in the configured format
func (h *StdioHook) Execute(ctx context.Context, event Event) error {
	switch h.format {
	case "json":
		return h.outputJSON(event)
	case "env":
		return h.outputEnv(event)
	default:
		return fmt.Errorf("stdio hook %s: unsupported format: %s", h.id, h.format)
	}
}

// Type returns the hook type
func (h *StdioHook) Type() string {
	return "stdio"
}

// ID returns the hook ID
func (h *StdioHook) ID() string {
	return h.id
}

// outputJSON outputs the event as a JSON line prefixed with RTMP_EVENT:
func (h *StdioHook) outputJSON(event Event) error {
	jsonData, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("stdio hook %s: failed to marshal JSON: %w", h.id, err)
	}

	_, err = fmt.Fprintf(h.output, "RTMP_EVENT: %s\n", string(jsonData))
	if err != nil {
		return fmt.Errorf("stdio hook %s: failed to write JSON: %w", h.id, err)
	}

	return nil
}

// outputEnv outputs the event as environment variable assignments
func (h *StdioHook) outputEnv(event Event) error {
	lines := []string{
		"# RTMP Event: " + string(event.Type),
		fmt.Sprintf("RTMP_EVENT_TYPE=%s", string(event.Type)),
		fmt.Sprintf("RTMP_TIMESTAMP=%d", event.Timestamp),
	}

	if event.ConnID != "" {
		lines = append(lines, "RTMP_CONN_ID="+event.ConnID)
	}

	if event.StreamKey != "" {
		lines = append(lines, "RTMP_STREAM_KEY="+event.StreamKey)
	}

	// Add event-specific data
	for key, value := range event.Data {
		envKey := "RTMP_" + strings.ToUpper(key)
		envValue := fmt.Sprintf("%v", value)
		lines = append(lines, envKey+"="+envValue)
	}

	lines = append(lines, "") // Add blank line for readability

	for _, line := range lines {
		if _, err := fmt.Fprintln(h.output, line); err != nil {
			return fmt.Errorf("stdio hook %s: failed to write env line: %w", h.id, err)
		}
	}

	return nil
}

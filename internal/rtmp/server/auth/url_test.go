package auth

import "testing"

// TestParseStreamURL exercises the URL parser with a table-driven approach
// covering normal, edge, and empty inputs.
func TestParseStreamURL(t *testing.T) {
	tests := []struct {
		raw        string
		wantName   string
		wantParams map[string]string
	}{
		// Normal cases
		{"mystream", "mystream", map[string]string{}},
		{"mystream?token=abc", "mystream", map[string]string{"token": "abc"}},
		{"mystream?token=a&expires=123", "mystream", map[string]string{"token": "a", "expires": "123"}},

		// Edge cases
		{"", "", map[string]string{}},
		{"stream?", "stream", map[string]string{}},
		{"stream?key=a%20b", "stream", map[string]string{"key": "a b"}},

		// Multiple values for same key — only first is kept
		{"s?k=1&k=2", "s", map[string]string{"k": "1"}},

		// Stream name with path separators (unusual but valid)
		{"path/to/stream?token=x", "path/to/stream", map[string]string{"token": "x"}},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got := ParseStreamURL(tt.raw)

			if got.StreamName != tt.wantName {
				t.Errorf("StreamName = %q, want %q", got.StreamName, tt.wantName)
			}
			if len(got.QueryParams) != len(tt.wantParams) {
				t.Errorf("QueryParams len = %d, want %d", len(got.QueryParams), len(tt.wantParams))
			}
			for k, want := range tt.wantParams {
				if got.QueryParams[k] != want {
					t.Errorf("QueryParams[%q] = %q, want %q", k, got.QueryParams[k], want)
				}
			}
		})
	}
}

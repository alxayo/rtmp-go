package relay

import (
	"log/slog"
	"testing"
)

func TestNewDestination_URLSchemes(t *testing.T) {
	logger := slog.Default()
	factory := func(url string) (RTMPClient, error) { return nil, nil }

	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"rtmp valid", "rtmp://cdn.example.com/live/key", false},
		{"rtmps valid", "rtmps://cdn.example.com/live/key", false},
		{"http rejected", "http://cdn.example.com/live/key", true},
		{"https rejected", "https://cdn.example.com/live/key", true},
		{"invalid url", "://bad", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewDestination(tt.url, logger, factory)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewDestination(%q) err=%v, wantErr=%v", tt.url, err, tt.wantErr)
			}
		})
	}
}

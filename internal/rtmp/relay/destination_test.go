package relay

import (
	"log/slog"
	"testing"
)

// noopClientFactory is a test stub that returns a nil client and no error.
// Used when we only need to test URL validation, not actual RTMP connections.
func noopClientFactory(url string) (RTMPClient, error) {
	return nil, nil
}

func TestNewDestination_AcceptsRTMP(t *testing.T) {
	dest, err := NewDestination("rtmp://cdn.example.com/live/key", slog.Default(), noopClientFactory)
	if err != nil {
		t.Fatalf("expected rtmp:// to be accepted, got error: %v", err)
	}
	if dest.URL != "rtmp://cdn.example.com/live/key" {
		t.Errorf("URL mismatch: got %q", dest.URL)
	}
}

func TestNewDestination_AcceptsRTMPS(t *testing.T) {
	dest, err := NewDestination("rtmps://secure-cdn.example.com/live/key", slog.Default(), noopClientFactory)
	if err != nil {
		t.Fatalf("expected rtmps:// to be accepted, got error: %v", err)
	}
	if dest.URL != "rtmps://secure-cdn.example.com/live/key" {
		t.Errorf("URL mismatch: got %q", dest.URL)
	}
}

func TestNewDestination_RejectsHTTP(t *testing.T) {
	_, err := NewDestination("http://example.com/live/key", slog.Default(), noopClientFactory)
	if err == nil {
		t.Fatal("expected http:// to be rejected, got nil error")
	}
}

func TestNewDestination_RejectsHTTPS(t *testing.T) {
	_, err := NewDestination("https://example.com/live/key", slog.Default(), noopClientFactory)
	if err == nil {
		t.Fatal("expected https:// to be rejected, got nil error")
	}
}

func TestNewDestination_RejectsEmptyScheme(t *testing.T) {
	_, err := NewDestination("example.com/live/key", slog.Default(), noopClientFactory)
	if err == nil {
		t.Fatal("expected missing scheme to be rejected, got nil error")
	}
}

func TestNewDestination_InitialStatus(t *testing.T) {
	dest, err := NewDestination("rtmps://cdn.example.com/live/key", slog.Default(), noopClientFactory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dest.Status != StatusDisconnected {
		t.Errorf("expected initial status %v, got %v", StatusDisconnected, dest.Status)
	}
	if dest.LastError != nil {
		t.Errorf("expected nil initial error, got %v", dest.LastError)
	}
}

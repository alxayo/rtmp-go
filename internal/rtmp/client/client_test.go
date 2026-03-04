// client_test.go – placeholder tests for the RTMP client.
//
// These tests are currently SKIPPED because the client package imports the
// server package for in-process testing, causing a Go import cycle. In Go,
// packages cannot import each other (A imports B and B imports A is illegal).
//
// The solution is to move these tests to the tests/integration/ package,
// which can import both client and server without creating a cycle.
// Each test is preserved here as a design reference for the future.
package client

import (
	"testing"
	// Temporary comment to resolve import cycle - will fix in integration tests
	// "fmt"
	// "time"
	// "github.com/alxayo/go-rtmp/internal/rtmp/server"
)

// TestConnectFlow would dial a real in-process server and exercise the
// handshake → connect → createStream sequence.
//
// Why it's skipped: The client package cannot import the server package
// because server already imports client (import cycle). This test will be
// moved to tests/integration/ where it can use both packages.
func TestConnectFlow(t *testing.T) {
	t.Skip("Temporarily disabled due to import cycle - will move to integration tests")
	/* TODO: Move to integration tests
	s := server.New(server.Config{ListenAddr: ":0"})
	if err := s.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer s.Stop()
	addr := s.Addr().String()
	c, err := New(fmt.Sprintf("rtmp://%s/app/stream", addr))
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if err := c.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	_ = c.Close()
	*/
}

// TestPublishFlow would connect then send a publish command followed by
// audio and video frames. Skipped for the same import-cycle reason.
func TestPublishFlow(t *testing.T) {
	t.Skip("Temporarily disabled due to import cycle - will move to integration tests")
	/* TODO: Move to integration tests
	s := server.New(server.Config{ListenAddr: ":0"})
	if err := s.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer s.Stop()
	addr := s.Addr().String()
	c, err := New(fmt.Sprintf("rtmp://%s/app/stream", addr))
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if err := c.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := c.Publish(); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := c.SendAudio(0, []byte{0xFF, 0xE0}); err != nil {
		t.Errorf("send audio: %v", err)
	}
	if err := c.SendVideo(0, []byte{0x17, 0x00}); err != nil {
		t.Errorf("send video: %v", err)
	}
	_ = c.Close()
	*/
}

// TestPlayFlow would connect, send a play command, and exercise the read
// loop for receiving media. Skipped for the same import-cycle reason.
func TestPlayFlow(t *testing.T) {
	t.Skip("Temporarily disabled due to import cycle - will move to integration tests")
	/* TODO: Move to integration tests
	s := server.New(server.Config{ListenAddr: ":0"})
	if err := s.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer s.Stop()
	addr := s.Addr().String()
	c, err := New(fmt.Sprintf("rtmp://%s/app/stream", addr))
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if err := c.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := c.Play(); err != nil {
		t.Fatalf("play: %v", err)
	}
	_ = c.Close()
	*/
}

// TestNew_URLSchemes verifies URL scheme validation for rtmp:// and rtmps://.
func TestNew_URLSchemes(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"rtmp valid", "rtmp://host/app/stream", false},
		{"rtmps valid", "rtmps://host/app/stream", false},
		{"https invalid", "https://host/app/stream", true},
		{"http invalid", "http://host/app/stream", true},
		{"no scheme", "host/app/stream", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.url)
			if (err != nil) != tt.wantErr {
				t.Fatalf("New(%q) err=%v, wantErr=%v", tt.url, err, tt.wantErr)
			}
		})
	}
}

// TestNewWithTLSConfig_Stores verifies that NewWithTLSConfig stores the config.
func TestNewWithTLSConfig_Stores(t *testing.T) {
	t.Run("rtmps with nil config", func(t *testing.T) {
		c, err := NewWithTLSConfig("rtmps://host/app/stream", nil)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if c.tlsConfig != nil {
			t.Fatal("expected nil tlsConfig when passing nil")
		}
	})

	t.Run("rtmp with nil config", func(t *testing.T) {
		c, err := NewWithTLSConfig("rtmp://host/app/stream", nil)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		_ = c
	})

	t.Run("http scheme rejected", func(t *testing.T) {
		_, err := NewWithTLSConfig("http://host/app/stream", nil)
		if err == nil {
			t.Fatal("expected error for http:// scheme")
		}
	})
}

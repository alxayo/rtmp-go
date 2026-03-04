package server

import (
	"crypto/tls"
	"net"
	"testing"
	"time"

	"github.com/alxayo/go-rtmp/internal/rtmp/handshake"
	"github.com/alxayo/go-rtmp/internal/testutil/tlstest"
)

func TestServerTLS_StartStop(t *testing.T) {
	certPath, keyPath := tlstest.GenerateCert(t)

	s := New(Config{
		ListenAddr:    "127.0.0.1:0",
		TLSCertFile:   certPath,
		TLSKeyFile:    keyPath,
		TLSListenAddr: "127.0.0.1:0",
	})
	if err := s.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if s.Addr() == nil {
		t.Fatal("expected non-nil RTMP addr")
	}
	if s.TLSAddr() == nil {
		t.Fatal("expected non-nil TLS addr")
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
	if s.TLSAddr() != nil {
		t.Fatal("expected nil TLS addr after stop")
	}
}

func TestServerTLS_AcceptConnection(t *testing.T) {
	certPath, keyPath := tlstest.GenerateCert(t)

	s := New(Config{
		ListenAddr:    "127.0.0.1:0",
		TLSCertFile:   certPath,
		TLSKeyFile:    keyPath,
		TLSListenAddr: "127.0.0.1:0",
	})
	if err := s.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer s.Stop()

	tlsAddr := s.TLSAddr().String()
	time.Sleep(50 * time.Millisecond)

	// Dial with TLS (skip verification since self-signed)
	conn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: 2 * time.Second},
		"tcp", tlsAddr,
		&tls.Config{InsecureSkipVerify: true},
	)
	if err != nil {
		t.Fatalf("tls dial failed: %v", err)
	}
	defer conn.Close()

	// Complete RTMP handshake over TLS
	if err := handshake.ClientHandshake(conn); err != nil {
		t.Fatalf("handshake over TLS failed: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.ConnectionCount() >= 1 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if s.ConnectionCount() < 1 {
		t.Fatalf("expected at least 1 connection via TLS, got %d", s.ConnectionCount())
	}
}

func TestServerTLS_InvalidCert(t *testing.T) {
	s := New(Config{
		ListenAddr:    "127.0.0.1:0",
		TLSCertFile:   "/nonexistent/cert.pem",
		TLSKeyFile:    "/nonexistent/key.pem",
		TLSListenAddr: "127.0.0.1:0",
	})
	err := s.Start()
	if err == nil {
		s.Stop()
		t.Fatal("expected error for invalid cert, got nil")
	}
}

func TestServerTLS_Disabled(t *testing.T) {
	s := New(Config{ListenAddr: "127.0.0.1:0"})
	if err := s.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer s.Stop()

	if s.TLSAddr() != nil {
		t.Fatal("expected nil TLS addr when TLS disabled")
	}
}

func TestConfig_TLSEnabled(t *testing.T) {
	tests := []struct {
		name     string
		cert     string
		key      string
		expected bool
	}{
		{"both empty", "", "", false},
		{"both set", "a.crt", "a.key", true},
		{"only cert", "a.crt", "", false},
		{"only key", "", "a.key", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{TLSCertFile: tt.cert, TLSKeyFile: tt.key}
			if got := cfg.TLSEnabled(); got != tt.expected {
				t.Fatalf("TLSEnabled() = %v, want %v", got, tt.expected)
			}
		})
	}
}

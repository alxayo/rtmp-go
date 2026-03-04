// Package integration - RTMPS (TLS) end-to-end integration tests.
//
// These tests verify that the full RTMP protocol works identically over
// TLS-encrypted connections.
package integration

import (
	"crypto/tls"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/alxayo/go-rtmp/internal/rtmp/client"
	"github.com/alxayo/go-rtmp/internal/rtmp/handshake"
	srv "github.com/alxayo/go-rtmp/internal/rtmp/server"
	"github.com/alxayo/go-rtmp/internal/testutil/tlstest"
)

func TestRTMPS_HandshakeOverTLS(t *testing.T) {
	certPath, keyPath := tlstest.GenerateCert(t)

	s := srv.New(srv.Config{
		ListenAddr:    "127.0.0.1:0",
		TLSCertFile:   certPath,
		TLSKeyFile:    keyPath,
		TLSListenAddr: "127.0.0.1:0",
	})
	if err := s.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	defer s.Stop()

	tlsAddr := s.TLSAddr().String()

	conn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: 2 * time.Second},
		"tcp", tlsAddr,
		&tls.Config{InsecureSkipVerify: true},
	)
	if err != nil {
		t.Fatalf("tls dial: %v", err)
	}
	defer conn.Close()

	if err := handshake.ClientHandshake(conn); err != nil {
		t.Fatalf("handshake over TLS: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.ConnectionCount() >= 1 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("server did not register TLS connection")
}

func TestRTMPS_PublishOverTLS(t *testing.T) {
	certPath, keyPath := tlstest.GenerateCert(t)

	s := srv.New(srv.Config{
		ListenAddr:    "127.0.0.1:0",
		TLSCertFile:   certPath,
		TLSKeyFile:    keyPath,
		TLSListenAddr: "127.0.0.1:0",
	})
	if err := s.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	defer s.Stop()

	_, tlsPort, _ := net.SplitHostPort(s.TLSAddr().String())

	c, err := client.NewWithTLSConfig(
		fmt.Sprintf("rtmps://localhost:%s/live/tls_test", tlsPort),
		&tls.Config{InsecureSkipVerify: true},
	)
	if err != nil {
		t.Fatalf("client new: %v", err)
	}
	defer c.Close()

	if err := c.Connect(); err != nil {
		t.Fatalf("connect over TLS: %v", err)
	}

	if err := c.Publish(); err != nil {
		t.Fatalf("publish over TLS: %v", err)
	}

	if err := c.SendAudio(0, []byte{0xAF, 0x00, 0x12, 0x10}); err != nil {
		t.Fatalf("send audio over TLS: %v", err)
	}
	if err := c.SendVideo(0, []byte{0x17, 0x00, 0x00, 0x00, 0x00}); err != nil {
		t.Fatalf("send video over TLS: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	if s.ConnectionCount() < 1 {
		t.Fatalf("expected at least 1 connection, got %d", s.ConnectionCount())
	}
}

func TestRTMPS_PlainAndTLSCoexist(t *testing.T) {
	certPath, keyPath := tlstest.GenerateCert(t)

	s := srv.New(srv.Config{
		ListenAddr:    "127.0.0.1:0",
		TLSCertFile:   certPath,
		TLSKeyFile:    keyPath,
		TLSListenAddr: "127.0.0.1:0",
	})
	if err := s.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	defer s.Stop()

	_, plainPort, _ := net.SplitHostPort(s.Addr().String())
	_, tlsPort, _ := net.SplitHostPort(s.TLSAddr().String())

	plainClient, err := client.New(fmt.Sprintf("rtmp://localhost:%s/live/plain_stream", plainPort))
	if err != nil {
		t.Fatalf("plain client new: %v", err)
	}
	defer plainClient.Close()

	if err := plainClient.Connect(); err != nil {
		t.Fatalf("plain connect: %v", err)
	}
	if err := plainClient.Publish(); err != nil {
		t.Fatalf("plain publish: %v", err)
	}

	tlsClient, err := client.NewWithTLSConfig(
		fmt.Sprintf("rtmps://localhost:%s/live/tls_stream", tlsPort),
		&tls.Config{InsecureSkipVerify: true},
	)
	if err != nil {
		t.Fatalf("tls client new: %v", err)
	}
	defer tlsClient.Close()

	if err := tlsClient.Connect(); err != nil {
		t.Fatalf("tls connect: %v", err)
	}
	if err := tlsClient.Publish(); err != nil {
		t.Fatalf("tls publish: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	if s.ConnectionCount() < 2 {
		t.Fatalf("expected at least 2 connections, got %d", s.ConnectionCount())
	}
}

func TestRTMPS_InvalidCert_ServerFailsToStart(t *testing.T) {
	s := srv.New(srv.Config{
		ListenAddr:    "127.0.0.1:0",
		TLSCertFile:   "/nonexistent/cert.pem",
		TLSKeyFile:    "/nonexistent/key.pem",
		TLSListenAddr: "127.0.0.1:0",
	})
	err := s.Start()
	if err == nil {
		s.Stop()
		t.Fatal("expected error for invalid cert paths")
	}
}

func TestRTMPS_NoCert_PlainOnly(t *testing.T) {
	s := srv.New(srv.Config{ListenAddr: "127.0.0.1:0"})
	if err := s.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Stop()

	if s.TLSAddr() != nil {
		t.Fatal("expected nil TLS addr when TLS is not configured")
	}

	_, port, _ := net.SplitHostPort(s.Addr().String())
	c, err := client.New(fmt.Sprintf("rtmp://localhost:%s/live/test", port))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer c.Close()

	if err := c.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
}

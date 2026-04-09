// Package integration – TLS (RTMPS) integration tests.
//
// These tests verify that the RTMP server correctly accepts encrypted TLS
// connections alongside plaintext ones. Self-signed certificates are generated
// at test time using crypto/x509 + crypto/ecdsa so no external cert files are
// needed.
package integration

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alxayo/go-rtmp/internal/rtmp/client"
	srv "github.com/alxayo/go-rtmp/internal/rtmp/server"
)

// genSelfSignedCert generates a self-signed ECDSA certificate and writes PEM
// files to the given directory. Returns the cert and key file paths.
func genSelfSignedCert(t *testing.T, dir string) (certFile, keyFile string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")

	certOut, err := os.Create(certFile)
	if err != nil {
		t.Fatalf("create cert file: %v", err)
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		t.Fatalf("encode cert: %v", err)
	}
	certOut.Close()

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyOut, err := os.Create(keyFile)
	if err != nil {
		t.Fatalf("create key file: %v", err)
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}); err != nil {
		t.Fatalf("encode key: %v", err)
	}
	keyOut.Close()

	return certFile, keyFile
}

// TestRTMPS_PublishPlay verifies that a client can connect via rtmps://,
// complete the RTMP handshake, and publish a stream over TLS.
func TestRTMPS_PublishPlay(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := genSelfSignedCert(t, dir)

	s := srv.New(srv.Config{
		ListenAddr:    "127.0.0.1:0",
		TLSListenAddr: "127.0.0.1:0",
		TLSCertFile:   certFile,
		TLSKeyFile:    keyFile,
		ChunkSize:     4096,
	})
	if err := s.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	defer s.Stop()

	tlsAddr := s.TLSAddr()
	if tlsAddr == nil {
		t.Fatal("expected TLS listener to be active")
	}

	// Connect via RTMPS with self-signed cert (skip verification)
	c, err := client.New(fmt.Sprintf("rtmps://%s/live/tls_test", tlsAddr.String()))
	if err != nil {
		t.Fatalf("client new: %v", err)
	}
	c.TLSConfig = &tls.Config{InsecureSkipVerify: true}
	defer c.Close()

	if err := c.Connect(); err != nil {
		t.Fatalf("connect over TLS: %v", err)
	}

	if err := c.Publish(); err != nil {
		t.Fatalf("publish over TLS: %v", err)
	}

	// Send a small audio + video message to ensure media flows over TLS
	if err := c.SendAudio(0, []byte{0xAF, 0x00, 0x12, 0x10}); err != nil {
		t.Fatalf("send audio over TLS: %v", err)
	}
	if err := c.SendVideo(0, []byte{0x17, 0x00, 0x00, 0x00, 0x00}); err != nil {
		t.Fatalf("send video over TLS: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	if s.ConnectionCount() < 1 {
		t.Fatalf("expected at least 1 TLS connection, got %d", s.ConnectionCount())
	}
	t.Log("✓ RTMPS publish test passed")
}

// TestRTMPS_DualListener verifies that both plain RTMP and RTMPS listeners
// work simultaneously on the same server instance.
func TestRTMPS_DualListener(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := genSelfSignedCert(t, dir)

	s := srv.New(srv.Config{
		ListenAddr:    "127.0.0.1:0",
		TLSListenAddr: "127.0.0.1:0",
		TLSCertFile:   certFile,
		TLSKeyFile:    keyFile,
		ChunkSize:     4096,
	})
	if err := s.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	defer s.Stop()

	plainAddr := s.Addr().String()
	tlsAddr := s.TLSAddr().String()

	// Connect via plain RTMP
	plainClient, err := client.New(fmt.Sprintf("rtmp://%s/live/plain_stream", plainAddr))
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

	// Connect via RTMPS
	tlsClient, err := client.New(fmt.Sprintf("rtmps://%s/live/tls_stream", tlsAddr))
	if err != nil {
		t.Fatalf("tls client new: %v", err)
	}
	tlsClient.TLSConfig = &tls.Config{InsecureSkipVerify: true}
	defer tlsClient.Close()

	if err := tlsClient.Connect(); err != nil {
		t.Fatalf("tls connect: %v", err)
	}
	if err := tlsClient.Publish(); err != nil {
		t.Fatalf("tls publish: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	if s.ConnectionCount() < 2 {
		t.Fatalf("expected at least 2 connections (plain + TLS), got %d", s.ConnectionCount())
	}
	t.Log("✓ Dual listener test passed: both plain RTMP and RTMPS work simultaneously")
}

// TestRTMPS_InvalidCertPaths verifies the server fails to start when given
// invalid TLS certificate file paths.
func TestRTMPS_InvalidCertPaths(t *testing.T) {
	s := srv.New(srv.Config{
		ListenAddr:    "127.0.0.1:0",
		TLSListenAddr: "127.0.0.1:0",
		TLSCertFile:   "/nonexistent/cert.pem",
		TLSKeyFile:    "/nonexistent/key.pem",
		ChunkSize:     4096,
	})
	err := s.Start()
	if err == nil {
		s.Stop()
		t.Fatal("expected server to fail with invalid cert paths, but it started successfully")
	}
	t.Logf("✓ Server correctly rejected invalid cert paths: %v", err)
}

// TestRTMPS_PlainOnlyNoTLS verifies that a server started without TLS config
// has no TLS listener.
func TestRTMPS_PlainOnlyNoTLS(t *testing.T) {
	s := srv.New(srv.Config{
		ListenAddr: "127.0.0.1:0",
		ChunkSize:  4096,
	})
	if err := s.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	defer s.Stop()

	if s.TLSAddr() != nil {
		t.Fatal("expected no TLS listener when TLS is not configured")
	}
	t.Log("✓ No TLS listener when TLS is not configured")
}

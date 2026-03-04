package tlstest

import (
	"crypto/tls"
	"os"
	"testing"
)

func TestGenerateCert(t *testing.T) {
	certPath, keyPath := GenerateCert(t)

	// Verify files exist
	if _, err := os.Stat(certPath); err != nil {
		t.Fatalf("cert file not found: %v", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("key file not found: %v", err)
	}

	// Verify the cert+key can be loaded as a valid TLS keypair
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatalf("failed to load keypair: %v", err)
	}
	if len(cert.Certificate) == 0 {
		t.Fatal("no certificates in keypair")
	}
}

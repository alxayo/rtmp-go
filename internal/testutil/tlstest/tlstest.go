// Package tlstest provides TLS certificate generation helpers for testing.
package tlstest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// GenerateCert creates a temporary self-signed cert+key pair on disk
// and returns the file paths. Files are cleaned up when the test finishes.
func GenerateCert(t *testing.T) (certPath, keyPath string) {
	t.Helper()
	dir := t.TempDir()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	certPath = filepath.Join(dir, "server.crt")
	keyPath = filepath.Join(dir, "server.key")

	certFile, err := os.Create(certPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		t.Fatal(err)
	}
	certFile.Close()

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyFile, err := os.Create(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}); err != nil {
		t.Fatal(err)
	}
	keyFile.Close()

	return certPath, keyPath
}

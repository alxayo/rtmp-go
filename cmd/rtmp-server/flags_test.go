package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseFlags_TLSValidation(t *testing.T) {
	t.Run("both flags omitted", func(t *testing.T) {
		cfg, err := parseFlags([]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.tlsCert != "" || cfg.tlsKey != "" {
			t.Fatal("expected empty TLS fields when flags omitted")
		}
	})

	t.Run("cert without key", func(t *testing.T) {
		_, err := parseFlags([]string{"-tls-cert", "cert.pem"})
		if err == nil {
			t.Fatal("expected error when -tls-cert set without -tls-key")
		}
	})

	t.Run("key without cert", func(t *testing.T) {
		_, err := parseFlags([]string{"-tls-key", "key.pem"})
		if err == nil {
			t.Fatal("expected error when -tls-key set without -tls-cert")
		}
	})

	t.Run("both set with nonexistent files", func(t *testing.T) {
		_, err := parseFlags([]string{"-tls-cert", "/nonexistent/c.pem", "-tls-key", "/nonexistent/k.pem"})
		if err == nil {
			t.Fatal("expected error for nonexistent cert file")
		}
	})

	t.Run("both set with valid files", func(t *testing.T) {
		dir := t.TempDir()
		cert := filepath.Join(dir, "cert.pem")
		key := filepath.Join(dir, "key.pem")
		os.WriteFile(cert, []byte("fake"), 0600)
		os.WriteFile(key, []byte("fake"), 0600)

		cfg, err := parseFlags([]string{"-tls-cert", cert, "-tls-key", key})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.tlsCert != cert || cfg.tlsKey != key {
			t.Fatalf("expected cert=%s key=%s, got cert=%s key=%s", cert, key, cfg.tlsCert, cfg.tlsKey)
		}
	})

	t.Run("custom tls-listen address", func(t *testing.T) {
		dir := t.TempDir()
		cert := filepath.Join(dir, "cert.pem")
		key := filepath.Join(dir, "key.pem")
		os.WriteFile(cert, []byte("fake"), 0600)
		os.WriteFile(key, []byte("fake"), 0600)

		cfg, err := parseFlags([]string{"-tls-cert", cert, "-tls-key", key, "-tls-listen", ":8443"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.tlsListen != ":8443" {
			t.Fatalf("expected tls-listen=:8443, got %s", cfg.tlsListen)
		}
	})
}

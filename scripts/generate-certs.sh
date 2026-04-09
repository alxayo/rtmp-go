#!/usr/bin/env bash
# generate-certs.sh — Generate self-signed TLS certificates for RTMPS testing
# Usage: ./generate-certs.sh [--force]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CERT_DIR="$SCRIPT_DIR/.certs"
CERT_FILE="$CERT_DIR/cert.pem"
KEY_FILE="$CERT_DIR/key.pem"
FORCE=false

for arg in "$@"; do
    case "$arg" in
        --force|-f) FORCE=true ;;
    esac
done

mkdir -p "$CERT_DIR"

# Check if certs already exist and are valid
if [[ -f "$CERT_FILE" && -f "$KEY_FILE" && "$FORCE" == "false" ]]; then
    # Check expiry with Go (cross-platform)
    if go run -modfile /dev/null - <<'GOCHECK' "$CERT_FILE" 2>/dev/null; then
package main
import (
    "crypto/x509"
    "encoding/pem"
    "fmt"
    "os"
    "time"
)
func main() {
    data, err := os.ReadFile(os.Args[1])
    if err != nil { os.Exit(1) }
    block, _ := pem.Decode(data)
    if block == nil { os.Exit(1) }
    cert, err := x509.ParseCertificate(block.Bytes)
    if err != nil { os.Exit(1) }
    if time.Now().After(cert.NotAfter.Add(-24*time.Hour)) {
        fmt.Println("expired")
        os.Exit(1)
    }
    fmt.Printf("valid until %s\n", cert.NotAfter.Format("2006-01-02"))
}
GOCHECK
        echo "Certificates already exist and are valid. Use --force to regenerate."
        echo "  cert: $CERT_FILE"
        echo "  key:  $KEY_FILE"
        exit 0
    fi
    echo "Existing certificates expired or invalid. Regenerating..."
fi

echo "Generating self-signed TLS certificates..."

# Use Go to generate certs (cross-platform, no openssl dependency)
cd "$SCRIPT_DIR"
go run - "$CERT_FILE" "$KEY_FILE" <<'GOGEN'
package main

import (
    "crypto/ecdsa"
    "crypto/elliptic"
    "crypto/rand"
    "crypto/x509"
    "crypto/x509/pkix"
    "encoding/pem"
    "fmt"
    "math/big"
    "net"
    "os"
    "time"
)

func main() {
    if len(os.Args) < 3 {
        fmt.Fprintf(os.Stderr, "usage: %s <cert.pem> <key.pem>\n", os.Args[0])
        os.Exit(1)
    }
    certPath, keyPath := os.Args[1], os.Args[2]

    key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
    if err != nil {
        fmt.Fprintf(os.Stderr, "generate key: %v\n", err)
        os.Exit(1)
    }

    template := &x509.Certificate{
        SerialNumber: big.NewInt(1),
        Subject:      pkix.Name{CommonName: "localhost", Organization: []string{"go-rtmp test"}},
        NotBefore:    time.Now().Add(-time.Hour),
        NotAfter:     time.Now().Add(365 * 24 * time.Hour),
        KeyUsage:     x509.KeyUsageDigitalSignature,
        ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
        DNSNames:     []string{"localhost"},
        IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
    }

    certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
    if err != nil {
        fmt.Fprintf(os.Stderr, "create cert: %v\n", err)
        os.Exit(1)
    }

    certOut, err := os.Create(certPath)
    if err != nil {
        fmt.Fprintf(os.Stderr, "create cert file: %v\n", err)
        os.Exit(1)
    }
    pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
    certOut.Close()

    keyDER, err := x509.MarshalECPrivateKey(key)
    if err != nil {
        fmt.Fprintf(os.Stderr, "marshal key: %v\n", err)
        os.Exit(1)
    }
    keyOut, err := os.Create(keyPath)
    if err != nil {
        fmt.Fprintf(os.Stderr, "create key file: %v\n", err)
        os.Exit(1)
    }
    pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
    keyOut.Close()

    fmt.Printf("Generated: %s, %s (valid 365 days)\n", certPath, keyPath)
}
GOGEN

echo "Done."
echo "  cert: $CERT_FILE"
echo "  key:  $KEY_FILE"

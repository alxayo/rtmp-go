# generate-certs.ps1 — Generate self-signed TLS certificates for RTMPS testing
# Usage: .\generate-certs.ps1 [-Force]
param(
    [switch]$Force
)

$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$CertDir = Join-Path $ScriptDir ".certs"
$CertFile = Join-Path $CertDir "cert.pem"
$KeyFile = Join-Path $CertDir "key.pem"

if (-not (Test-Path $CertDir)) {
    New-Item -ItemType Directory -Path $CertDir -Force | Out-Null
}

# Check if certs already exist and are valid
if ((Test-Path $CertFile) -and (Test-Path $KeyFile) -and (-not $Force)) {
    Write-Host "Certificates already exist. Use -Force to regenerate." -ForegroundColor Yellow
    Write-Host "  cert: $CertFile"
    Write-Host "  key:  $KeyFile"
    exit 0
}

Write-Host "Generating self-signed TLS certificates..." -ForegroundColor Cyan

# Write the Go helper to a temp file
$goSource = @'
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
'@

$tempFile = Join-Path ([System.IO.Path]::GetTempPath()) "gen_certs.go"
$goSource | Out-File -Encoding utf8 -FilePath $tempFile

try {
    & go run $tempFile $CertFile $KeyFile
    if ($LASTEXITCODE -ne 0) {
        throw "go run failed with exit code $LASTEXITCODE"
    }
} finally {
    Remove-Item $tempFile -ErrorAction SilentlyContinue
}

Write-Host "Done." -ForegroundColor Green
Write-Host "  cert: $CertFile"
Write-Host "  key:  $KeyFile"

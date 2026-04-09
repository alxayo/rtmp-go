---
title: "RTMPS (TLS)"
weight: 5
---

# RTMPS (TLS Encryption)

RTMPS adds TLS encryption to RTMP connections, protecting stream data in transit. go-rtmp implements RTMPS via **TLS termination at the transport layer** — the TLS handshake wraps the TCP connection before the RTMP protocol begins, so all protocol layers (handshake, chunks, AMF, commands, media) work identically over both plain and encrypted connections.

## How It Works

```
Publisher (OBS/FFmpeg)
    │
    │  TLS handshake (rtmps://server:1936)
    ▼
go-rtmp server
    │  tls.NewListener() wraps net.Listener
    │  Returns tls.Conn (implements net.Conn)
    ▼
RTMP protocol (identical to plain RTMP)
    │
    ▼
Subscribers, Recording, Relay, Hooks
```

The server uses Go's `crypto/tls` package with `tls.NewListener()` to wrap the standard TCP listener. The resulting `tls.Conn` implements `net.Conn`, so the entire RTMP stack — handshake, chunk parsing, AMF encoding, command dispatch, and media relay — requires zero changes. TLS is purely a transport concern.

## Quick Setup

### 1. Generate Self-Signed Certificates

For development and testing, use the included helper script:

**Linux/macOS:**
```bash
./scripts/generate-certs.sh
```

**Windows (PowerShell):**
```powershell
.\scripts\generate-certs.ps1
```

This generates `scripts/.certs/cert.pem` and `scripts/.certs/key.pem` using Go's `crypto/x509` package — valid for localhost and 127.0.0.1, expires in 365 days.

### 2. Start with TLS

```bash
./rtmp-server \
  -tls-listen :1936 \
  -tls-cert scripts/.certs/cert.pem \
  -tls-key scripts/.certs/key.pem \
  -log-level info
```

You should see:

```json
{"level":"INFO","msg":"server started","addr":":1935","tls_addr":":1936","version":"dev"}
```

### 3. Publish via RTMPS

Using the Go client or any RTMPS-capable tool:

```bash
# Using Go client (built-in RTMPS support)
go run ./cmd/rtmp-client -url rtmps://localhost:1936/live/test -publish test.flv

# Using FFmpeg (plain RTMP to the same server)
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test
```

> **Note**: FFmpeg does not natively support `rtmps://` output. Use plain RTMP with FFmpeg — the TLS encryption is validated via the Go client. The hook system, recording, and relay all work identically regardless of transport.

## Dual Listener (RTMP + RTMPS)

Run both plain and encrypted listeners simultaneously:

```bash
./rtmp-server \
  -listen :1935 \
  -tls-listen :1936 \
  -tls-cert cert.pem \
  -tls-key key.pem
```

This is the recommended production setup during migration — existing clients continue using `rtmp://server:1935` while new clients use `rtmps://server:1936`. Both listeners share the same stream registry, so a publisher on RTMPS and a subscriber on plain RTMP (or vice versa) see the same streams.

## Production Certificates

### Let's Encrypt (Recommended)

Use [certbot](https://certbot.eff.org/) to obtain free, trusted TLS certificates:

```bash
# Obtain certificate
sudo certbot certonly --standalone -d stream.example.com

# Start server with Let's Encrypt certs
./rtmp-server \
  -tls-listen :443 \
  -tls-cert /etc/letsencrypt/live/stream.example.com/fullchain.pem \
  -tls-key /etc/letsencrypt/live/stream.example.com/privkey.pem
```

> **Port 443**: RTMPS conventionally uses port 443 in production, matching the HTTPS standard port. This also helps with firewall traversal.

### Certificate Renewal

Certificates from Let's Encrypt expire every 90 days. Set up auto-renewal:

```bash
# Test renewal
sudo certbot renew --dry-run

# The server must be restarted to pick up new certificates
# Use a cron job or systemd timer:
0 3 * * * certbot renew --quiet && systemctl restart rtmp-server
```

## Combined with Other Features

### RTMPS + Authentication

```bash
./rtmp-server \
  -tls-listen :443 \
  -tls-cert cert.pem \
  -tls-key key.pem \
  -auth-mode token \
  -auth-token "live/stream1=secret123"
```

Clients connect with:
```
rtmps://server:443/live/stream1?token=secret123
```

### RTMPS + Recording + Relay

```bash
./rtmp-server \
  -listen :1935 \
  -tls-listen :443 \
  -tls-cert cert.pem \
  -tls-key key.pem \
  -record-all true \
  -relay-to rtmp://a.rtmp.youtube.com/live2/YOUTUBE_KEY \
  -relay-to rtmps://ingest.example.com:443/live
```

Relay destinations can also use `rtmps://` URLs — the server's RTMP client handles TLS for outbound relay connections.

### RTMPS + Hooks

Hooks execute identically for both plain and RTMPS connections. The hook system operates after TLS termination, so hook scripts and webhooks don't need to be aware of the transport layer:

```bash
./rtmp-server \
  -tls-listen :443 \
  -tls-cert cert.pem \
  -tls-key key.pem \
  -hook-script "publish_start=./scripts/on-publish-hls.sh"
```

## Client Configuration

### OBS Studio

| Field | Value |
|-------|-------|
| **Server** | `rtmps://stream.example.com:443/live` |
| **Stream Key** | `mystream` (or `mystream?token=secret` with auth) |

OBS supports RTMPS natively. It will establish a TLS connection before starting the RTMP handshake.

### Go Client (Programmatic)

```go
import "go-rtmp/internal/rtmp/client"
import "crypto/tls"

c := client.New("rtmps://localhost:1936/live/test", nil)
c.TLSConfig = &tls.Config{
    // For self-signed certs in development:
    InsecureSkipVerify: true,
}
err := c.Connect(ctx)
```

## Security Notes

- **Minimum TLS version**: TLS 1.2 (enforced by the server)
- **TLS startup is fatal**: If the TLS listener fails to start (bad certificate, port conflict), the entire server shuts down — it won't silently fall back to unencrypted mode
- **Self-signed certs**: Fine for development and testing, but browsers and most clients will reject them. Use trusted certificates in production
- **Certificate permissions**: Ensure key files are readable only by the server process (`chmod 600 key.pem`)

## Troubleshooting

| Problem | Solution |
|---------|----------|
| `tls: failed to find any PEM data` | Certificate file is empty or not in PEM format |
| `tls: private key does not match public key` | Cert and key are from different keypairs |
| Server exits on TLS error | TLS startup failure is intentionally fatal — fix the cert/key issue |
| OBS says "Failed to connect" | Check that the port is correct and the certificate is trusted |
| Self-signed cert rejected | Use `-InsecureSkipVerify` in Go client, or install the CA cert on the client machine |

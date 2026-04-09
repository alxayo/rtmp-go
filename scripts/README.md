# go-rtmp Scripts

Cross-platform helper scripts for running, testing, and developing with the go-rtmp server.
Each script is available as both **Bash** (`.sh` for Linux/macOS) and **PowerShell** (`.ps1` for Windows).

## Prerequisites

- **Go** 1.21+ (for building the server and generating TLS certs)
- **ffmpeg**, **ffplay**, **ffprobe** (for streaming and verification)
- Run `check-deps` to verify all tools are available

## Scripts

### `check-deps` — Dependency Checker
Verifies that all required tools are installed and reports their versions.

```bash
# Linux/macOS
./scripts/check-deps.sh

# Windows
.\scripts\check-deps.ps1
```

### `generate-certs` — TLS Certificate Generator
Generates self-signed ECDSA certificates for RTMPS testing. Uses Go's crypto stdlib
(no OpenSSL required). Certificates are saved to `scripts/.certs/`.

```bash
# Linux/macOS
./scripts/generate-certs.sh           # Generate (idempotent)
./scripts/generate-certs.sh --force   # Force regeneration

# Windows
.\scripts\generate-certs.ps1          # Generate (idempotent)
.\scripts\generate-certs.ps1 -Force   # Force regeneration
```

### `on-publish-hls` — HLS Conversion Hook
Shell hook script triggered by the server when a stream starts publishing.
Launches ffmpeg in the background to convert the RTMP stream to HLS segments.

**Not called directly** — registered via the `-hook-script` server flag:
```bash
./rtmp-server -hook-script "publish_start=scripts/on-publish-hls.sh"
```

HLS output appears in `hls-output/<stream_name>/playlist.m3u8`.

### `start-server` — Server Launcher
Starts the go-rtmp server with configurable options. Handles building the binary,
generating TLS certs, and waiting for the server to be ready.

```bash
# Linux/macOS
./scripts/start-server.sh                                # Plain RTMP
./scripts/start-server.sh --mode tls                     # RTMPS only
./scripts/start-server.sh --mode both --enable-hls       # RTMP + RTMPS + HLS hooks
./scripts/start-server.sh --enable-auth --foreground     # With auth, foreground mode

# Windows
.\scripts\start-server.ps1                                # Plain RTMP
.\scripts\start-server.ps1 -Mode tls                      # RTMPS only
.\scripts\start-server.ps1 -Mode both -EnableHLS          # RTMP + RTMPS + HLS hooks
.\scripts\start-server.ps1 -EnableAuth -Foreground        # With auth, foreground mode
```

**Options:**

| Bash | PowerShell | Description |
|------|-----------|-------------|
| `--mode plain\|tls\|both` | `-Mode plain\|tls\|both` | Transport mode (default: plain) |
| `--enable-hls` | `-EnableHLS` | Register HLS conversion hook |
| `--enable-auth` | `-EnableAuth` | Enable token auth with test tokens |
| `--port PORT` | `-Port PORT` | RTMP port (default: 1935) |
| `--tls-port PORT` | `-TLSPort PORT` | RTMPS port (default: 1936) |
| `--log-level LEVEL` | `-LogLevel LEVEL` | debug\|info\|warn\|error |
| `--foreground` | `-Foreground` | Run in foreground |

**Test tokens** (when `--enable-auth` is used):
- `live/test` → `secret123`
- `live/secure` → `mytoken456`

### `test-e2e` — End-to-End Test Suite
Comprehensive automated test suite with 7 test cases covering RTMP, RTMPS, HLS, and auth.

```bash
# Linux/macOS — run all tests
./scripts/test-e2e.sh

# Run a specific test
./scripts/test-e2e.sh --test rtmp-basic

# Windows — run all tests
.\scripts\test-e2e.ps1

# Run a specific test
.\scripts\test-e2e.ps1 -Test rtmp-basic
```

**Test Cases:**

| Name | Description |
|------|-------------|
| `rtmp-basic` | Publish test pattern → capture → verify with ffprobe |
| `rtmps-basic` | Start dual listener (RTMP+RTMPS) → verify TLS active |
| `rtmp-hls` | Publish → HLS hook fires → verify .m3u8 + .ts segments |
| `rtmps-hls` | Same as rtmp-hls but with TLS listener active |
| `auth-allow` | Publish with valid token → verify success |
| `auth-reject` | Publish with invalid token → verify rejection |
| `rtmps-auth` | TLS + auth combined → verify both work together |

Each test uses unique ports (19351–19367) to avoid conflicts and cleans up all
processes on exit.

### `run-all-tests` — Full Test Runner
Convenience wrapper that runs the complete E2E test suite and reports results.

```bash
# Linux/macOS
./scripts/run-all-tests.sh

# Windows
.\scripts\run-all-tests.ps1
```

## Directory Structure

```
scripts/
├── .certs/              # Generated TLS certificates (gitignored)
├── logs/                # Test and server logs (gitignored)
├── .test-tmp/           # Temporary test artifacts (cleaned up)
├── README.md            # This file
├── check-deps.sh/.ps1
├── generate-certs.sh/.ps1
├── on-publish-hls.sh/.ps1
├── start-server.sh/.ps1
├── test-e2e.sh/.ps1
└── run-all-tests.sh/.ps1
```

## Quick Start

```bash
# 1. Check dependencies
./scripts/check-deps.sh

# 2. Start server with HLS conversion
./scripts/start-server.sh --mode both --enable-hls

# 3. Publish a test stream (in another terminal)
ffmpeg -re -f lavfi -i testsrc=size=640x480:rate=30 \
  -c:v libx264 -preset veryfast -f flv rtmp://localhost:1935/live/test

# 4. View HLS output
ls hls-output/live_test/

# 5. Run automated tests
./scripts/run-all-tests.sh
```

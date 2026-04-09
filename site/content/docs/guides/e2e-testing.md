---
title: "E2E Testing Scripts"
weight: 4
---

# End-to-End Testing Scripts

go-rtmp includes a comprehensive suite of cross-platform E2E testing scripts in the `scripts/` directory. These scripts validate the full streaming pipeline — from publishing through relay to playback — covering plain RTMP, RTMPS (TLS), HLS via hooks, and authentication.

## Prerequisites

Check that all required tools are available:

**Linux/macOS:**
```bash
./scripts/check-deps.sh
```

**Windows (PowerShell):**
```powershell
.\scripts\check-deps.ps1
```

This verifies: `ffmpeg`, `ffplay`, `ffprobe` in PATH, and that the `rtmp-server` binary exists. Each tool's version is reported.

## Script Overview

| Script | Purpose |
|--------|---------|
| `check-deps` | Verify tool availability (ffmpeg, ffplay, ffprobe, go-rtmp) |
| `generate-certs` | Generate self-signed TLS certificates for testing |
| `on-publish-hls` | Hook script: convert RTMP stream to HLS via FFmpeg |
| `start-server` | Start the RTMP server with configurable options |
| `test-e2e` | Run the full E2E test suite (7 test cases) |
| `run-all-tests` | Execute all tests with summary reporting |

Each script ships as both `.sh` (Bash for Linux/macOS) and `.ps1` (PowerShell for Windows).

## Running Tests

### Full Test Suite

**Linux/macOS:**
```bash
./scripts/run-all-tests.sh
```

**Windows:**
```powershell
.\scripts\run-all-tests.ps1
```

This runs all 7 E2E test cases and prints a summary:

```
=== Test Results ===
  PASS  RTMP Publish + Capture
  PASS  RTMPS Publish + Capture
  PASS  RTMP + HLS via Hook
  PASS  RTMPS + HLS via Hook
  PASS  RTMP + Auth (allowed)
  PASS  RTMP + Auth (rejected)
  PASS  RTMPS + Auth

7 passed, 0 failed, 0 skipped
```

### Individual Test Cases

Run specific tests by name:

```bash
./scripts/test-e2e.sh --test "RTMP Publish + Capture"
```

## Test Cases

### 1. RTMP Publish + Capture

Publishes a test pattern via FFmpeg to the RTMP server, captures the output with FFmpeg, and verifies the captured file with `ffprobe` (duration, video stream present).

### 2. RTMPS Publish + Capture

Same as test 1, but uses a dual-listener server (plain + TLS). Verifies that TLS connections work by connecting the Go client via `rtmps://`.

### 3. RTMP + HLS via Hook

Starts the server with the `on-publish-hls` hook script. When a stream is published, the hook automatically launches FFmpeg to convert the RTMP stream into HLS segments. Verifies that `.m3u8` playlist and `.ts` segment files are created.

### 4. RTMPS + HLS via Hook

Same as test 3, but with TLS transport. Demonstrates that hooks work identically regardless of whether the publisher connected via plain or encrypted RTMP.

### 5. RTMP + Auth (Allowed)

Starts the server with token authentication. Publishes with the correct token and verifies the stream is accepted.

### 6. RTMP + Auth (Rejected)

Starts the server with token authentication. Attempts to publish without a token (or with the wrong token) and verifies the connection is rejected.

### 7. RTMPS + Auth

Combines TLS encryption with token authentication. Verifies both features work together.

## Using start-server for Development

The `start-server` script is useful for manual development and testing:

```bash
# Plain RTMP server
./scripts/start-server.sh --mode plain

# RTMPS server (generates certs if needed)
./scripts/start-server.sh --mode tls

# Dual listener with HLS hook
./scripts/start-server.sh --mode both --enable-hls

# With authentication
./scripts/start-server.sh --mode plain --enable-auth
```

**Parameters:**

| Parameter | Default | Description |
|-----------|---------|-------------|
| `--mode` | `plain` | Listener mode: `plain`, `tls`, or `both` |
| `--enable-hls` | off | Enable HLS conversion via publish hook |
| `--enable-auth` | off | Enable token-based authentication |
| `--port` | `1935` | RTMP listener port |
| `--tls-port` | `1936` | RTMPS listener port (when mode=tls or both) |
| `--log-level` | `info` | Server log level |

## Generating TLS Certificates

For TLS tests, the scripts use self-signed certificates:

```bash
./scripts/generate-certs.sh          # Generate (skips if valid certs exist)
./scripts/generate-certs.sh --force  # Force regeneration
```

Certificates are stored in `scripts/.certs/` (git-ignored) and are valid for localhost + 127.0.0.1 with a 365-day expiry.

## HLS Hook Script

The `on-publish-hls` script is designed to be used as a shell hook:

```bash
./rtmp-server \
  -hook-script "publish_start=./scripts/on-publish-hls.sh" \
  -hook-timeout 30s
```

When a publisher connects, the hook:
1. Reads `RTMP_STREAM_KEY` and `RTMP_CONN_ID` from environment variables
2. Starts FFmpeg in the background to subscribe to the RTMP stream
3. Converts the stream to HLS segments in `hls-output/{stream_name}/`
4. Logs to `scripts/logs/hls-{key}.log`

## Directory Structure

```
scripts/
├── .certs/              # Generated TLS certificates (git-ignored)
│   ├── cert.pem
│   └── key.pem
├── logs/                # Test and hook logs (git-ignored)
├── check-deps.sh/.ps1
├── generate-certs.sh/.ps1
├── on-publish-hls.sh/.ps1
├── start-server.sh/.ps1
├── test-e2e.sh/.ps1
├── run-all-tests.sh/.ps1
└── README.md
```

## Port Allocation

Each test case uses unique ports to avoid conflicts when running in parallel:

| Test | RTMP Port | RTMPS Port |
|------|-----------|------------|
| RTMP Publish + Capture | 19351 | — |
| RTMPS Publish + Capture | 19353 | 19354 |
| RTMP + HLS via Hook | 19355 | — |
| RTMPS + HLS via Hook | 19357 | 19358 |
| RTMP + Auth (allowed) | 19359 | — |
| RTMP + Auth (rejected) | 19361 | — |
| RTMPS + Auth | 19363 | 19364 |

## Troubleshooting

| Problem | Solution |
|---------|----------|
| `ffmpeg not found` | Install FFmpeg and ensure it's in your PATH |
| `rtmp-server not found` | Run `go build -o rtmp-server ./cmd/rtmp-server` first |
| Port already in use | Check for leftover server processes; each test uses unique ports |
| TLS cert errors | Delete `scripts/.certs/` and re-run `generate-certs` |
| Tests timeout | Increase timeout or check firewall settings |

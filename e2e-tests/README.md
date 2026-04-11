# E2E Test Suite for go-rtmp

End-to-end tests for the go-rtmp server using FFmpeg, ffprobe, and the server binary.

## Prerequisites

- **Go 1.21+** — to build the server binary
- **FFmpeg** — with libx264, aac encoders (libx265 for H.265 tests)
- **ffprobe** — for media file verification (bundled with FFmpeg)
- **openssl** — for TLS certificate generation (RTMPS tests)
- **curl** — for metrics endpoint tests
- **python3** — for webhook listener tests (optional)
- **SRT support in FFmpeg** — for SRT ingest tests (optional, tests skip if missing)

## Quick Start

```bash
# Run all tests (Bash/Linux/macOS)
./e2e-tests/run-all.sh

# Run all tests WITHOUT camera tests (CI/headless environments)
./e2e-tests/run-all-no-camera.sh

# Run all tests (PowerShell/Windows)
.\e2e-tests\run-all.ps1

# Run all tests without camera (PowerShell)
.\e2e-tests\run-all-no-camera.ps1

# Run a single test
./e2e-tests/rtmp-publish-play-h264.sh

# Run tests matching a pattern
./e2e-tests/run-all.sh --filter "rtmp-"

# List all available tests
./e2e-tests/run-all.sh --list
```

## Manual Testing

For manual testing without the automation framework, see **[MANUAL_TESTING.md](MANUAL_TESTING.md)** which provides:

- **Exact command reference** for each tool (FFmpeg, ffprobe, ffplay, rtmp-server)
- **Per-test walkthroughs** showing how to run tests manually in separate terminals
- **Quick reference** for common commands (start server, publish, capture, verify)
- **Verification examples** using ffprobe to check codecs and file properties

Example:
```bash
# From MANUAL_TESTING.md — publish H.264 stream manually
./rtmp-server -listen localhost:1935 -log-level debug

# In another terminal:
ffmpeg -hide_banner -loglevel error -re \
  -f lavfi -i "testsrc=duration=5:size=320x240:rate=25" \
  -f lavfi -i "sine=frequency=440:duration=5" \
  -c:v libx264 -preset ultrafast -tune zerolatency \
  -c:a aac -b:a 64k \
  -f flv "rtmp://localhost:1935/live/test"
```

## Exit Codes

| Code | Meaning |
|------|---------|
| 0    | PASS    |
| 1    | FAIL    |
| 2    | SKIP (missing prerequisites) |

## Disabling Camera Tests

Camera tests (`srt-camera-ingest`) auto-skip if no camera is detected. For CI/headless environments, you can explicitly disable them:

```bash
# Bash: Set SKIP_CAMERA_TESTS environment variable
export SKIP_CAMERA_TESTS=1
./e2e-tests/run-all.sh

# Or use the convenience script (recommended for CI)
./e2e-tests/run-all-no-camera.sh

# PowerShell: Set environment variable
$env:SKIP_CAMERA_TESTS = "1"
.\e2e-tests\run-all.ps1

# Or use the convenience script
.\e2e-tests\run-all-no-camera.ps1
```

## Exit Codes

| Code | Meaning |
|------|---------|
| 0    | PASS    |
| 1    | FAIL    |
| 2    | SKIP (missing prerequisites) |

## Test Groups

### RTMP Basic (5 tests)
| Test | Description |
|------|-------------|
| `rtmp-publish-h264` | Basic H.264+AAC publish, connection registers |
| `rtmp-publish-play-h264` | Full publish→subscribe cycle with capture verification |
| `rtmp-publish-audio-only` | Audio-only stream (no video track) |
| `rtmp-concurrent-publishers` | 2 publishers on different stream keys |
| `rtmp-concurrent-subscribers` | 1 publisher → 3 subscribers fan-out |

### RTMPS / TLS (2 tests)
| Test | Description |
|------|-------------|
| `rtmps-publish-play` | Publish and subscribe over RTMPS (TLS) |
| `rtmps-dual-listener` | RTMP + RTMPS both active simultaneously |

### Enhanced RTMP (1 test)
| Test | Description |
|------|-------------|
| `enhanced-rtmp-h265` | H.265/HEVC via Enhanced RTMP (E-RTMP v2 FourCC) |

### SRT Ingest (4 tests)
| Test | Description |
|------|-------------|
| `srt-publish-h264` | SRT ingest with H.264+AAC (MPEG-TS over SRT) |
| `srt-publish-h265` | SRT ingest with H.265/HEVC |
| `srt-publish-play-via-rtmp` | SRT publish → RTMP subscribe (cross-protocol) |
| `srt-camera-ingest` | SRT from system camera (optional, skips if no camera) |

### FLV Recording (3 tests)
| Test | Description |
|------|-------------|
| `recording-flv-h264` | Server-side FLV recording with H.264+AAC |
| `recording-flv-h265` | Recording preserves H.265 codec (Enhanced RTMP) |
| `recording-flv-audio-video` | Recording has both audio and video tracks |

### Authentication (4 tests)
| Test | Description |
|------|-------------|
| `auth-token-publish-allowed` | Publish with correct token succeeds |
| `auth-token-publish-rejected` | Publish with wrong token is rejected |
| `auth-token-play-allowed` | Subscribe with correct token succeeds |
| `auth-token-play-rejected` | Subscribe with wrong token is rejected |

### Event Hooks (3 tests)
| Test | Description |
|------|-------------|
| `hooks-shell-publish-start` | Shell hook fires on publish_start event |
| `hooks-webhook-publish-start` | Webhook POST fires on publish_start |
| `hooks-hls-conversion` | HLS output generates .m3u8 + .ts segments |

### Relay (2 tests)
| Test | Description |
|------|-------------|
| `relay-single-destination` | Media relay to 1 destination server |
| `relay-multi-destination` | Media relay to 2 destination servers |

### Metrics (1 test)
| Test | Description |
|------|-------------|
| `metrics-expvar-counters` | /debug/vars returns RTMP counters |

### Connection Lifecycle (3 tests)
| Test | Description |
|------|-------------|
| `reconnect-publisher-disconnect` | Subscriber handles publisher disconnect |
| `reconnect-subscriber-late-join` | Late-joining subscriber gets cached headers |
| `server-graceful-shutdown` | Server shutdown doesn't corrupt recordings |

## Architecture

```
e2e-tests/
├── _lib.sh / _lib.ps1       # Shared helper libraries
├── run-all.sh / run-all.ps1  # Test runners
├── README.md                 # This file
└── {group}-{test}.sh/.ps1    # Individual test scripts (28 tests)
```

### Shared Library (`_lib.sh` / `_lib.ps1`)

Every test sources the shared library which provides:
- **Server management**: `build_server()`, `start_server()`, `stop_server()`
- **FFmpeg helpers**: `publish_test_pattern()`, `publish_h265_test_pattern()`, `start_capture()`, etc.
- **Assertions**: `assert_file_exists()`, `assert_video_codec()`, `assert_duration()`, `assert_decodable()`, etc.
- **Port isolation**: `unique_port()` hashes test name to a unique port (19400-19599)
- **Lifecycle**: `setup()` / `teardown()` manage temp dirs and process cleanup

### Naming Convention

- **Group prefix**: `rtmp-`, `rtmps-`, `enhanced-rtmp-`, `srt-`, `recording-`, `auth-`, `hooks-`, `relay-`, `metrics-`, `reconnect-`, `server-`
- **Utility files**: Prefixed with `_` (e.g., `_lib.sh`)
- All test content is synthetic (FFmpeg `lavfi` generators) — no input files needed.

## Adding New Tests

1. Create `e2e-tests/{group}-{descriptive-name}.sh` and `.ps1`
2. Source `_lib.sh` / `_lib.ps1`
3. Use `setup "test-name"` / `teardown` / `report_result`
4. Add comprehensive header comments (see existing tests for format)
5. Use `unique_port "$TEST_NAME"` for port isolation

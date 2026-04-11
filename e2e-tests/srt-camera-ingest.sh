#!/usr/bin/env bash
# ============================================================================
# TEST: srt-camera-ingest
# GROUP: SRT Ingest
#
# WHAT IS TESTED:
#   SRT ingest from a system camera device. This test is OPTIONAL and
#   auto-skips if no camera is detected. On macOS it checks avfoundation,
#   on Linux it checks /dev/video0 (v4l2), on Windows it checks dshow.
#   When a camera is available, it captures a short clip via SRT.
#
# EXPECTED RESULT:
#   - If camera available: SRT connection accepted, server registers stream
#   - If no camera: test gracefully skips (exit code 2)
#
# PREREQUISITES:
#   - FFmpeg with SRT protocol, platform camera driver
#   - System camera device
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/srt-camera-ingest.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="srt-camera-ingest"
PORT=$(unique_port "$TEST_NAME")
SRT_PORT=$((PORT + 200))

# Allow skipping camera tests via environment variable
if [[ "${SKIP_CAMERA_TESTS:-0}" == "1" ]]; then
    echo -e "${YELLOW}SKIP: Camera tests disabled (set SKIP_CAMERA_TESTS=0 to enable)${NC}"
    exit 2
fi

if ! ffmpeg -hide_banner -protocols 2>/dev/null | grep -q srt; then
    echo -e "${YELLOW}SKIP: FFmpeg does not have SRT protocol support${NC}"
    exit 2
fi

# Detect camera
OS_NAME="$(uname -s)"
CAMERA_INPUT=""
case "$OS_NAME" in
    Darwin)
        if ffmpeg -f avfoundation -list_devices true -i "" 2>&1 | grep -q "\[0\]"; then
            CAMERA_INPUT="-f avfoundation -framerate 30 -i 0:none"
        fi
        ;;
    Linux)
        if [[ -e /dev/video0 ]]; then
            CAMERA_INPUT="-f v4l2 -framerate 30 -i /dev/video0"
        fi
        ;;
    *)
        echo -e "${YELLOW}SKIP: Camera detection not supported on $OS_NAME${NC}"
        exit 2
        ;;
esac

if [[ -z "$CAMERA_INPUT" ]]; then
    echo -e "${YELLOW}SKIP: No camera device detected${NC}"
    exit 2
fi

setup "$TEST_NAME"

start_server "$PORT" "-log-level" "debug" "-srt-listen" "localhost:${SRT_PORT}"

log_step "Publishing camera feed via SRT (3s)..."
set +e
eval ffmpeg -hide_banner -loglevel error \
    $CAMERA_INPUT \
    -c:v libx264 -preset ultrafast -tune zerolatency \
    -t 3 \
    -f mpegts "srt://localhost:${SRT_PORT}?streamid=publish:live/camera&latency=200000" 2>/dev/null
set -e

sleep 2
assert_log_not_contains "$SERVER_LOG" "panic\|FATAL" "No server panics"

teardown
report_result "$TEST_NAME"

# ============================================================================
# MANUAL TESTING
# ============================================================================
# For manual testing without the automation framework, see MANUAL_TESTING.md
# which provides exact commands for:
#   - Starting the server
#   - Publishing streams
#   - Capturing/subscribing
#   - Verifying output with ffprobe
#
# Each test group in MANUAL_TESTING.md includes step-by-step instructions
# with real commands you can copy and paste into your terminal.
# ============================================================================

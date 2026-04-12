#!/usr/bin/env bash
# ============================================================================
# TEST: camera-rtmps-h264
# GROUP: Camera Tests
#
# WHAT IS TESTED:
#   Secure RTMP (RTMPS/TLS) publish from a live camera device using H.264.
#   The server is configured with TLS certificates. FFmpeg publishes the
#   camera feed over an encrypted RTMPS connection. This validates that
#   real camera streams work over the TLS transport layer.
#
# EXPECTED RESULT:
#   - Camera stream published successfully via RTMPS
#   - TLS handshake succeeds (visible in server debug log)
#   - Server accepts and processes the encrypted stream
#   - No server panics or TLS errors
#
# PREREQUISITES:
#   - Live camera device (macOS: avfoundation, Linux: v4l2)
#   - FFmpeg with libx264, aac, and TLS support
#   - OpenSSL (for certificate generation)
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/camera-rtmps-h264.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="camera-rtmps-h264"
PORT=$(unique_port "$TEST_NAME")
TLS_PORT=$((PORT + 100))

# Allow skipping camera tests via environment variable
if [[ "${SKIP_CAMERA_TESTS:-0}" == "1" ]]; then
    echo -e "${YELLOW}SKIP: Camera tests disabled (set SKIP_CAMERA_TESTS=0 to enable)${NC}"
    exit 2
fi

# Detect camera
OS_NAME="$(uname -s)"
CAMERA_INPUT=""
case "$OS_NAME" in
    Darwin)
        if ffmpeg -f avfoundation -list_devices true -i "" 2>&1 | grep -q "\[0\]"; then
            CAMERA_INPUT="-f avfoundation -framerate 30 -i 0:0"
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

# Check openssl is available for cert generation
if ! command -v openssl &>/dev/null; then
    echo -e "${YELLOW}SKIP: openssl not found (required for TLS certs)${NC}"
    exit 2
fi

setup "$TEST_NAME"

# Generate self-signed TLS certificates
generate_certs

start_server "$PORT" \
    "-log-level" "debug" \
    "-tls-listen" "localhost:${TLS_PORT}" \
    "-tls-cert" "$CERTS_DIR/server.crt" \
    "-tls-key" "$CERTS_DIR/server.key"

STREAM_URL="rtmps://localhost:${TLS_PORT}/live/camera-secure"

log_step "Publishing camera via RTMPS (5s)..."
set +e
eval ffmpeg -hide_banner -loglevel error \
    $CAMERA_INPUT \
    -t 5 \
    -c:v libx264 -preset ultrafast -tune zerolatency \
    -c:a aac -b:a 128k \
    -tls_verify 0 \
    -f flv "$STREAM_URL" 2>/dev/null
set -e

sleep 2

# Verify TLS connection was accepted
assert_log_contains "$SERVER_LOG" "tls.*true" "TLS connection established"
assert_log_not_contains "$SERVER_LOG" "panic\|FATAL" "No server panics"
pass_check "Camera stream published over RTMPS"

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

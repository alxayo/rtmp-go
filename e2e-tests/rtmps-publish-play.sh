#!/usr/bin/env bash
# ============================================================================
# TEST: rtmps-publish-play
# GROUP: RTMPS (TLS)
#
# WHAT IS TESTED:
#   Publish and subscribe over RTMPS (RTMP over TLS). Generates a self-signed
#   certificate, starts the server with TLS enabled, publishes H.264+AAC via
#   the rtmps:// URL, and captures the stream through a TLS subscriber.
#
# EXPECTED RESULT:
#   - Server starts with TLS listener enabled
#   - Publisher connects over TLS (rtmps://)
#   - Subscriber captures valid video over TLS
#   - Captured file has H.264 video codec
#
# PREREQUISITES:
#   - FFmpeg with TLS support (typically OpenSSL/GnuTLS linked)
#   - openssl CLI (for cert generation)
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/rtmps-publish-play.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="rtmps-publish-play"
PORT=$(unique_port "$TEST_NAME")
TLS_PORT=$((PORT + 100))

# Check openssl is available
if ! command -v openssl &>/dev/null; then
    echo -e "${YELLOW}SKIP: openssl not found (required for cert generation)${NC}"
    exit 2
fi

setup "$TEST_NAME"

# Generate self-signed certs
generate_certs

start_server "$PORT" \
    "-log-level" "debug" \
    "-tls-listen" "localhost:${TLS_PORT}" \
    "-tls-cert" "$CERTS_DIR/server.crt" \
    "-tls-key" "$CERTS_DIR/server.key"

# Publisher sends over RTMPS first (8s, background)
log_step "Publishing over RTMPS (8s, background)..."
ffmpeg -hide_banner -loglevel error \
    -re \
    -f lavfi -i "testsrc=duration=8:size=320x240:rate=25" \
    -f lavfi -i "sine=frequency=440:duration=8" \
    -c:v libx264 -preset ultrafast -tune zerolatency \
    -c:a aac -b:a 64k \
    -tls_verify 0 \
    -f flv "rtmps://localhost:${TLS_PORT}/live/tls-test" &
PUB_PID=$!
_PIDS+=($PUB_PID)

sleep 2
# Subscriber connects over RTMPS (ignore cert verification for self-signed)
CAPTURE="$TMPDIR/capture.flv"
log_step "Starting RTMPS subscriber capture..."
ffmpeg -hide_banner -loglevel error \
    -tls_verify 0 \
    -i "rtmps://localhost:${TLS_PORT}/live/tls-test" \
    -c copy -t 10 "$CAPTURE" &
CAPTURE_PID=$!
_PIDS+=($CAPTURE_PID)

wait $PUB_PID 2>/dev/null || true
sleep 2
kill $CAPTURE_PID 2>/dev/null || true
wait $CAPTURE_PID 2>/dev/null || true

assert_file_exists "$CAPTURE" "RTMPS capture file exists"
assert_video_codec "$CAPTURE" "h264"

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

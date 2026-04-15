#!/usr/bin/env bash
# ============================================================================
# TEST: srt-encrypt-publish-play-via-rtmp
# GROUP: SRT Encryption
#
# WHAT IS TESTED:
#   Cross-protocol streaming with encryption: encrypted SRT publish → RTMP
#   subscriber. An FFmpeg publisher sends H.264+AAC content via encrypted
#   SRT (AES-128), while an RTMP subscriber captures the relayed stream.
#   This validates that encryption is transparent to the RTMP bridge —
#   SRT decrypts the media before relaying it to RTMP subscribers.
#
# EXPECTED RESULT:
#   - Encrypted SRT publisher connects successfully
#   - RTMP subscriber captures valid H.264 video
#   - Capture duration is at least 2 seconds
#   - Server log shows "SRT encryption enabled" with "AES-128"
#
# PREREQUISITES:
#   - FFmpeg with SRT protocol support (libsrt) and libx264
#   - ffprobe
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/srt-encrypt-publish-play-via-rtmp.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="srt-encrypt-publish-play-via-rtmp"
PORT=$(unique_port "$TEST_NAME")
SRT_PORT=$((PORT + 200))

if ! ffmpeg -hide_banner -protocols 2>/dev/null | grep -q srt; then
    echo -e "${YELLOW}SKIP: FFmpeg does not have SRT protocol support${NC}"
    exit 2
fi

setup "$TEST_NAME"

# Start server with SRT encryption enabled (AES-128)
start_server "$PORT" "-log-level" "debug" \
    "-srt-listen" "localhost:${SRT_PORT}" \
    "-srt-passphrase" "test-secret-passphrase" \
    "-srt-pbkeylen" "16"

# Start encrypted SRT publisher first (8s, background)
log_step "Publishing H.264 via encrypted SRT (10s, background)..."
publish_srt_h264 "srt://localhost:${SRT_PORT}?streamid=publish:live/encrypt-cross&latency=200000&passphrase=test-secret-passphrase&pbkeylen=16" 10 &
PUB_PID=$!

# Wait for the encrypted SRT handshake to complete and the publish session to register.
# We must wait for the stream to be available before starting the RTMP subscriber,
# otherwise the play command will fail with "stream not found".
if ! wait_for_log "$SERVER_LOG" "publish session started" 10; then
    log_error "SRT publisher did not register within 10s"
    teardown
    exit 1
fi
sleep 1
# Start RTMP subscriber for the SRT-bridged stream
CAPTURE="$TMPDIR/capture.flv"
log_step "Starting RTMP subscriber for encrypted SRT-bridged stream..."
start_capture "rtmp://localhost:${PORT}/live/encrypt-cross" "$CAPTURE" 5

wait_and_stop_capture
wait $PUB_PID 2>/dev/null || true

assert_file_exists "$CAPTURE" "Cross-protocol encrypted capture exists"
assert_video_codec "$CAPTURE" "h264"
assert_duration "$CAPTURE" 2.0 12.0
assert_log_contains "$SERVER_LOG" "SRT encryption enabled" "Encryption is enabled"

teardown
report_result "$TEST_NAME"

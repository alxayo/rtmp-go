#!/usr/bin/env bash
# ============================================================================
# TEST: srt-publish-play-via-rtmp
# GROUP: SRT Ingest
#
# WHAT IS TESTED:
#   Cross-protocol streaming: SRT publish → RTMP subscriber. An FFmpeg
#   publisher sends H.264+AAC content via SRT ingest, while an RTMP
#   subscriber captures the relayed stream. This validates the SRT-to-RTMP
#   bridge pipeline.
#
# EXPECTED RESULT:
#   - SRT publisher connects and is bridged to the RTMP stream registry
#   - RTMP subscriber captures valid H.264 video
#   - Capture duration is at least 2 seconds
#
# PREREQUISITES:
#   - FFmpeg with SRT protocol and libx264
#   - ffprobe
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/srt-publish-play-via-rtmp.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="srt-publish-play-via-rtmp"
PORT=$(unique_port "$TEST_NAME")
SRT_PORT=$((PORT + 200))

if ! ffmpeg -hide_banner -protocols 2>/dev/null | grep -q srt; then
    echo -e "${YELLOW}SKIP: FFmpeg does not have SRT protocol support${NC}"
    exit 2
fi

setup "$TEST_NAME"

start_server "$PORT" "-log-level" "debug" "-srt-listen" "localhost:${SRT_PORT}"

# Start SRT publisher first (8s, background)
log_step "Publishing H.264 via SRT (8s, background)..."
publish_srt_h264 "srt://localhost:${SRT_PORT}?streamid=publish:live/srt-cross&latency=200000" 8 &
PUB_PID=$!

sleep 2
# Start RTMP subscriber for the SRT-bridged stream
CAPTURE="$TMPDIR/capture.flv"
log_step "Starting RTMP subscriber for SRT-bridged stream..."
start_capture "rtmp://localhost:${PORT}/live/srt-cross" "$CAPTURE" 5

wait_and_stop_capture
wait $PUB_PID 2>/dev/null || true

assert_file_exists "$CAPTURE" "Cross-protocol capture exists"
assert_video_codec "$CAPTURE" "h264"
assert_duration "$CAPTURE" 2.0 12.0

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

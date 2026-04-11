#!/usr/bin/env bash
# ============================================================================
# TEST: srt-publish-h265
# GROUP: SRT Ingest
#
# WHAT IS TESTED:
#   SRT ingest with H.265/HEVC content via MPEG-TS. Note: based on the
#   current server implementation, H.265 over SRT may be silently dropped
#   in the bridge layer (bridge.onFrame default case). This test documents
#   the current behavior and will be updated when full SRT H.265 support
#   is added.
#
# EXPECTED RESULT:
#   - SRT connection is accepted by the server
#   - Server does not panic or crash
#   - (Current limitation: H.265 frames may be dropped in bridge layer)
#
# PREREQUISITES:
#   - FFmpeg with SRT protocol and libx265
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/srt-publish-h265.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="srt-publish-h265"
PORT=$(unique_port "$TEST_NAME")
SRT_PORT=$((PORT + 200))

if ! ffmpeg -hide_banner -protocols 2>/dev/null | grep -q srt; then
    echo -e "${YELLOW}SKIP: FFmpeg does not have SRT protocol support${NC}"
    exit 2
fi
if ! ffmpeg -hide_banner -encoders 2>/dev/null | grep -q libx265; then
    echo -e "${YELLOW}SKIP: FFmpeg does not have libx265 encoder${NC}"
    exit 2
fi

setup "$TEST_NAME"

start_server "$PORT" "-log-level" "debug" "-srt-listen" "localhost:${SRT_PORT}"

log_step "Publishing H.265 via SRT (5s)..."
publish_srt_h265 "srt://localhost:${SRT_PORT}?streamid=publish:live/srt-h265&latency=200000" 5
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

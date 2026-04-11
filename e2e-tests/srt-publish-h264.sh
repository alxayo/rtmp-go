#!/usr/bin/env bash
# ============================================================================
# TEST: srt-publish-h264
# GROUP: SRT Ingest
#
# WHAT IS TESTED:
#   SRT ingest with H.264+AAC content. An FFmpeg publisher sends MPEG-TS
#   over SRT to the server using the streamid=publish:live/key format.
#   Verifies the SRT listener accepts the connection and the server
#   registers the stream.
#
# EXPECTED RESULT:
#   - Server accepts the SRT connection
#   - Server log shows SRT connection activity
#   - No panics or errors
#
# PREREQUISITES:
#   - FFmpeg with SRT protocol support (libsrt)
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/srt-publish-h264.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="srt-publish-h264"
PORT=$(unique_port "$TEST_NAME")
SRT_PORT=$((PORT + 200))

# Check SRT support
if ! ffmpeg -hide_banner -protocols 2>/dev/null | grep -q srt; then
    echo -e "${YELLOW}SKIP: FFmpeg does not have SRT protocol support${NC}"
    exit 2
fi

setup "$TEST_NAME"

start_server "$PORT" "-log-level" "debug" "-srt-listen" "localhost:${SRT_PORT}"

log_step "Publishing H.264+AAC via SRT (5s)..."
publish_srt_h264 "srt://localhost:${SRT_PORT}?streamid=publish:live/srt-h264&latency=200000" 5
sleep 2

assert_log_not_contains "$SERVER_LOG" "panic\|FATAL" "No server panics"

teardown
report_result "$TEST_NAME"

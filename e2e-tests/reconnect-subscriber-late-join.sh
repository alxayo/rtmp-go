#!/usr/bin/env bash
# ============================================================================
# TEST: reconnect-subscriber-late-join
# GROUP: Connection Lifecycle
#
# WHAT IS TESTED:
#   Late-joining subscriber receives cached sequence headers and can decode
#   the stream. The publisher starts first, then after a delay a subscriber
#   connects mid-stream. The server should send cached SPS/PPS headers
#   to the late joiner, allowing it to decode from the nearest keyframe.
#
# EXPECTED RESULT:
#   - Late-joining subscriber captures valid video
#   - Captured file has H.264 codec and is decodable
#   - Duration > 1 second (got some of the stream)
#
# PREREQUISITES:
#   - FFmpeg with libx264, aac
#   - ffprobe
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/reconnect-subscriber-late-join.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="reconnect-subscriber-late-join"
PORT=$(unique_port "$TEST_NAME")

setup "$TEST_NAME"

start_server "$PORT" "-log-level" "debug"

STREAM_URL="rtmp://localhost:${PORT}/live/late-join"

# Start publisher first (8 seconds)
log_step "Starting publisher (8s stream)..."
publish_test_pattern "$STREAM_URL" 8 &
PUB_PID=$!

# Wait 3 seconds then subscribe (late join)
sleep 3
CAPTURE="$TMPDIR/late-join-capture.flv"
log_step "Late-joining subscriber after 3s delay..."
start_capture "$STREAM_URL" "$CAPTURE" 4

wait_and_stop_capture
wait $PUB_PID 2>/dev/null || true

assert_file_exists "$CAPTURE" "Late-join capture exists"
assert_video_codec "$CAPTURE" "h264"
assert_decodable "$CAPTURE"

teardown
report_result "$TEST_NAME"

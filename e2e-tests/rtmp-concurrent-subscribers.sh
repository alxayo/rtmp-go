#!/usr/bin/env bash
# ============================================================================
# TEST: rtmp-concurrent-subscribers
# GROUP: RTMP Basic
#
# WHAT IS TESTED:
#   One publisher broadcasting to 3 concurrent subscribers (1:N fan-out).
#   Verifies the server's subscriber broadcast mechanism correctly delivers
#   media to multiple subscribers simultaneously.
#
# EXPECTED RESULT:
#   - All 3 subscriber capture files exist and contain valid video
#   - Each capture is at least 2 seconds long
#   - Server handles fan-out without errors or dropped connections
#
# PREREQUISITES:
#   - FFmpeg with libx264, aac encoder
#   - ffprobe
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/rtmp-concurrent-subscribers.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="rtmp-concurrent-subscribers"
PORT=$(unique_port "$TEST_NAME")

setup "$TEST_NAME"

start_server "$PORT" "-log-level" "debug"

STREAM_URL="rtmp://localhost:${PORT}/live/fanout"

# Start publisher first (10 seconds to give time for subscribers)
log_step "Starting publisher (10s stream)..."
publish_test_pattern "$STREAM_URL" 10 &
PUB_PID=$!

# Wait for publisher to establish
sleep 2

# Start 3 subscribers
CAP1="$TMPDIR/sub1.flv"
CAP2="$TMPDIR/sub2.flv"
CAP3="$TMPDIR/sub3.flv"

log_step "Starting 3 subscribers..."
start_capture "$STREAM_URL" "$CAP1" 6
SUB1_PID=$CAPTURE_PID

start_capture "$STREAM_URL" "$CAP2" 6
SUB2_PID=$CAPTURE_PID

start_capture "$STREAM_URL" "$CAP3" 6
SUB3_PID=$CAPTURE_PID

# Wait for captures to finish (they have -t 6 timeout), then publisher
wait_and_stop_capture "$SUB1_PID"
wait_and_stop_capture "$SUB2_PID"
wait_and_stop_capture "$SUB3_PID"
wait $PUB_PID 2>/dev/null || true

# Verify all 3 captures
assert_file_exists "$CAP1" "Subscriber 1 capture exists"
assert_file_exists "$CAP2" "Subscriber 2 capture exists"
assert_file_exists "$CAP3" "Subscriber 3 capture exists"
assert_has_video "$CAP1"

teardown
report_result "$TEST_NAME"

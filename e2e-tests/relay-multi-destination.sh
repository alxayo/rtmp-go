#!/usr/bin/env bash
# ============================================================================
# TEST: relay-multi-destination
# GROUP: Relay
#
# WHAT IS TESTED:
#   Media relay to TWO destination servers simultaneously. Three server
#   instances are started: primary + 2 destinations. The primary relays
#   to both. Subscribers on each destination should capture the stream.
#
# EXPECTED RESULT:
#   - Both destination subscribers capture valid H.264 video
#   - Media is correctly fanned out to multiple relay targets
#
# PREREQUISITES:
#   - FFmpeg with libx264, aac
#   - ffprobe
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/relay-multi-destination.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="relay-multi-destination"
PORT=$(unique_port "$TEST_NAME")
DEST1_PORT=$((PORT + 1))
DEST2_PORT=$((PORT + 2))

setup "$TEST_NAME"
build_server

# Start 2 destination servers
DEST1_LOG="$LOG_DIR/relay-dest1-${DEST1_PORT}.log"
DEST2_LOG="$LOG_DIR/relay-dest2-${DEST2_PORT}.log"

log_step "Starting destination server 1 on port $DEST1_PORT..."
"$BINARY" -listen "localhost:${DEST1_PORT}" -log-level debug > "$DEST1_LOG" 2>&1 &
_PIDS+=($!)

log_step "Starting destination server 2 on port $DEST2_PORT..."
"$BINARY" -listen "localhost:${DEST2_PORT}" -log-level debug > "$DEST2_LOG" 2>&1 &
_PIDS+=($!)

sleep 1

# Start primary with relay to both destinations
start_server "$PORT" "-log-level" "debug" \
    "-relay-to" "rtmp://localhost:${DEST1_PORT}/live,rtmp://localhost:${DEST2_PORT}/live"

# Publish first (10s, background)
log_step "Publishing to primary (10s, background)..."
publish_test_pattern "rtmp://localhost:${PORT}/live/multi-relay" 10 &
PUB_PID=$!

sleep 2
# Subscribers on both destinations
CAP1="$TMPDIR/relay-cap1.flv"
CAP2="$TMPDIR/relay-cap2.flv"

start_capture "rtmp://localhost:${DEST1_PORT}/live/multi-relay" "$CAP1" 6
SUB1_PID=$CAPTURE_PID

start_capture "rtmp://localhost:${DEST2_PORT}/live/multi-relay" "$CAP2" 6
SUB2_PID=$CAPTURE_PID

wait_and_stop_capture "$SUB1_PID"
wait_and_stop_capture "$SUB2_PID"
wait $PUB_PID 2>/dev/null || true
wait_and_stop_capture "$SUB2_PID"

assert_file_exists "$CAP1" "Destination 1 relay capture exists"
assert_file_exists "$CAP2" "Destination 2 relay capture exists"
assert_video_codec "$CAP1" "h264"
assert_video_codec "$CAP2" "h264"

teardown
report_result "$TEST_NAME"

#!/usr/bin/env bash
# ============================================================================
# TEST: relay-single-destination
# GROUP: Relay
#
# WHAT IS TESTED:
#   Media relay to a single destination server. Two server instances are
#   started: a primary server and a relay destination. The primary is
#   configured with -relay-to pointing to the destination. A publisher
#   sends to the primary, and a subscriber on the destination captures
#   the relayed stream.
#
# EXPECTED RESULT:
#   - Primary server relays media to the destination server
#   - Subscriber on destination captures valid H.264 video
#   - Capture duration is at least 2 seconds
#
# PREREQUISITES:
#   - FFmpeg with libx264, aac
#   - ffprobe
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/relay-single-destination.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="relay-single-destination"
PORT=$(unique_port "$TEST_NAME")
DEST_PORT=$((PORT + 1))

setup "$TEST_NAME"

# Start destination server first
DEST_LOG="$LOG_DIR/relay-dest-${DEST_PORT}.log"
build_server
log_step "Starting destination server on port $DEST_PORT..."
"$BINARY" -listen "localhost:${DEST_PORT}" -log-level debug > "$DEST_LOG" 2>&1 &
DEST_PID=$!
_PIDS+=($DEST_PID)
sleep 1

# Start primary server with relay
start_server "$PORT" "-log-level" "debug" \
    "-relay-to" "rtmp://localhost:${DEST_PORT}/live"

# Publish first (8s, background)
log_step "Publishing to primary server (8s, background)..."
publish_test_pattern "rtmp://localhost:${PORT}/live/relay-test" 8 &
PUB_PID=$!

sleep 2
# Start subscriber on destination
CAPTURE="$TMPDIR/relay-capture.flv"
log_step "Starting subscriber on destination server..."
start_capture "rtmp://localhost:${DEST_PORT}/live/relay-test" "$CAPTURE" 5

wait_and_stop_capture
wait $PUB_PID 2>/dev/null || true

assert_file_exists "$CAPTURE" "Relay capture exists on destination"
assert_video_codec "$CAPTURE" "h264"
assert_duration "$CAPTURE" 2.0 12.0

teardown
report_result "$TEST_NAME"

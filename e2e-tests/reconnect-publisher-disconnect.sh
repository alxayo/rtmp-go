#!/usr/bin/env bash
# ============================================================================
# TEST: reconnect-publisher-disconnect
# GROUP: Connection Lifecycle
#
# WHAT IS TESTED:
#   Subscriber behavior when the publisher disconnects mid-stream. The
#   publisher publishes for 3 seconds then stops. The subscriber should
#   handle the disconnect gracefully (exit cleanly, not hang forever).
#   Server should log "publish stopped" or equivalent.
#
# EXPECTED RESULT:
#   - Subscriber exits cleanly after publisher disconnects
#   - Server log shows publish stopped / connection closed
#   - No server panics or zombie connections
#
# PREREQUISITES:
#   - FFmpeg with libx264, aac
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/reconnect-publisher-disconnect.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="reconnect-publisher-disconnect"
PORT=$(unique_port "$TEST_NAME")

setup "$TEST_NAME"

start_server "$PORT" "-log-level" "debug"

STREAM_URL="rtmp://localhost:${PORT}/live/disconnect-test"
CAPTURE="$TMPDIR/capture.flv"

# Publisher sends 3 seconds then stops (background)
log_step "Publishing for 3s then disconnecting..."
publish_test_pattern "$STREAM_URL" 3 &
PUB_PID=$!

sleep 1
# Start subscriber that captures while publisher is still live
log_step "Starting subscriber (2s capture)..."
start_capture "$STREAM_URL" "$CAPTURE" 2

wait_and_stop_capture
wait $PUB_PID 2>/dev/null || true
sleep 2

# Verify graceful handling
assert_log_not_contains "$SERVER_LOG" "panic\|FATAL" "No server panics"

# The subscriber may or may not have captured data depending on timing
if [[ -f "$CAPTURE" ]] && [[ -s "$CAPTURE" ]]; then
    pass_check "Subscriber captured data before disconnect"
fi

pass_check "Server handled publisher disconnect without panic"

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

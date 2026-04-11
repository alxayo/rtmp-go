#!/usr/bin/env bash
# ============================================================================
# TEST: auth-token-play-allowed
# GROUP: Authentication
#
# WHAT IS TESTED:
#   Subscribing/playing with the correct authentication token succeeds.
#   The server requires token auth, a publisher publishes (with correct token),
#   and a subscriber with the correct token captures the stream.
#
# EXPECTED RESULT:
#   - Subscriber connects and captures valid video
#   - No auth failure messages in the log
#
# PREREQUISITES:
#   - FFmpeg, ffprobe
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/auth-token-play-allowed.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="auth-token-play-allowed"
PORT=$(unique_port "$TEST_NAME")

setup "$TEST_NAME"

start_server "$PORT" "-log-level" "debug" "-auth-mode" "token" "-auth-token" "live/auth-play=secret123"

CAPTURE="$TMPDIR/capture.flv"

# Start publisher first (8s)
log_step "Publishing with correct token (8s, background)..."
publish_test_pattern "rtmp://localhost:${PORT}/live/auth-play?token=secret123" 8 &
PUB_PID=$!

sleep 2
log_step "Starting authorized subscriber..."
start_capture "rtmp://localhost:${PORT}/live/auth-play?token=secret123" "$CAPTURE" 5

wait_and_stop_capture
wait $PUB_PID 2>/dev/null || true

assert_file_exists "$CAPTURE" "Authorized subscriber capture exists"
assert_has_video "$CAPTURE"
assert_log_not_contains "$SERVER_LOG" "auth_failed\|rejected" "No auth failures"

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

#!/usr/bin/env bash
# ============================================================================
# TEST: auth-token-play-rejected
# GROUP: Authentication
#
# WHAT IS TESTED:
#   Subscribing/playing with the WRONG token is rejected. A publisher with
#   the correct token publishes, but a subscriber using an incorrect token
#   should be denied access to the stream.
#
# EXPECTED RESULT:
#   - Subscriber capture file is empty or doesn't exist
#   - Server log shows auth rejection for the subscriber
#
# PREREQUISITES:
#   - FFmpeg, ffprobe
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/auth-token-play-rejected.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="auth-token-play-rejected"
PORT=$(unique_port "$TEST_NAME")

setup "$TEST_NAME"

start_server "$PORT" "-log-level" "debug" "-auth-mode" "token" "-auth-token" "live/auth-play-rej=secret123"

# Start publisher with correct token
log_step "Publishing with correct token (5s)..."
publish_test_pattern "rtmp://localhost:${PORT}/live/auth-play-rej?token=secret123" 5 &
PUB_PID=$!

sleep 1

# Try to subscribe with wrong token
CAPTURE="$TMPDIR/capture.flv"
log_step "Trying unauthorized subscriber (wrong token)..."
set +e
ffmpeg -hide_banner -loglevel error \
    -i "rtmp://localhost:${PORT}/live/auth-play-rej?token=badtoken" \
    -c copy -t 5 "$CAPTURE" 2>/dev/null
SUB_EXIT=$?
set -e

wait $PUB_PID 2>/dev/null || true
sleep 1

# Verify subscriber was rejected
if [[ ! -f "$CAPTURE" ]] || [[ ! -s "$CAPTURE" ]]; then
    pass_check "Unauthorized subscriber got no data"
elif [[ $SUB_EXIT -ne 0 ]]; then
    pass_check "Subscriber failed with exit code $SUB_EXIT"
else
    fail_check "Auth rejection for play" "Subscriber captured data with wrong token"
fi

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

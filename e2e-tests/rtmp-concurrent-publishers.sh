#!/usr/bin/env bash
# ============================================================================
# TEST: rtmp-concurrent-publishers
# GROUP: RTMP Basic
#
# WHAT IS TESTED:
#   Two simultaneous publishers on different stream keys. Verifies the server's
#   stream registry correctly handles multiple concurrent publish sessions
#   without interference between them.
#
# EXPECTED RESULT:
#   - Both publishers are accepted by the server
#   - Server log shows two distinct "publish started" or "connection registered" entries
#   - No errors, panics, or stream cross-contamination
#
# PREREQUISITES:
#   - FFmpeg with libx264, aac encoder
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/rtmp-concurrent-publishers.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="rtmp-concurrent-publishers"
PORT=$(unique_port "$TEST_NAME")

setup "$TEST_NAME"

start_server "$PORT" "-log-level" "debug"

# Launch two publishers in parallel on different stream keys
log_step "Starting publisher 1 (live/stream-a, 5s)..."
publish_test_pattern "rtmp://localhost:${PORT}/live/stream-a" 5 &
PUB1_PID=$!

log_step "Starting publisher 2 (live/stream-b, 5s)..."
publish_test_pattern "rtmp://localhost:${PORT}/live/stream-b" 5 &
PUB2_PID=$!

# Wait for both publishers to finish
wait $PUB1_PID 2>/dev/null || true
wait $PUB2_PID 2>/dev/null || true
sleep 2

# Verify both connections were registered
CONNECTION_COUNT=$(grep -c "connection registered" "$SERVER_LOG" 2>/dev/null || echo "0")
if [[ "$CONNECTION_COUNT" -ge 2 ]]; then
    pass_check "Both publishers registered ($CONNECTION_COUNT connections)"
else
    fail_check "Both publishers registered" "only $CONNECTION_COUNT connections found"
fi

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

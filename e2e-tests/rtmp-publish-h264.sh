#!/usr/bin/env bash
# ============================================================================
# TEST: rtmp-publish-h264
# GROUP: RTMP Basic
#
# WHAT IS TESTED:
#   Basic RTMP publish with H.264 video + AAC audio. Verifies the server
#   accepts an inbound RTMP connection from FFmpeg and registers the
#   publisher without errors. This is the most fundamental server test.
#
# EXPECTED RESULT:
#   - Server starts and accepts the connection
#   - Server log shows "connection registered" and publish activity
#   - No errors or crashes during the publish session
#
# PREREQUISITES:
#   - FFmpeg with libx264 and aac encoder
#   - Go 1.21+ (to build the server)
#
# USAGE:
#   ./e2e-tests/rtmp-publish-h264.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="rtmp-publish-h264"
PORT=$(unique_port "$TEST_NAME")

setup "$TEST_NAME"

# Step 1: Start server
start_server "$PORT" "-log-level" "debug"

# Step 2: Publish a 5-second H.264+AAC test pattern
log_step "Publishing H.264+AAC test pattern (5s)..."
publish_test_pattern "rtmp://localhost:${PORT}/live/test" 5
sleep 2

# Step 3: Verify server log
assert_log_contains "$SERVER_LOG" "connection registered" "Server registered the connection"
assert_log_not_contains "$SERVER_LOG" "panic\|FATAL\|fatal error" "No server panics"

teardown
report_result "$TEST_NAME"

# ============================================================================
# MANUAL TESTING
# ============================================================================
# To run this test manually without the automation:
#
# Terminal 1 - Start Server:
#   ./rtmp-server -listen localhost:1935 -log-level debug
#
# Terminal 2 - Publish:
#   ffmpeg -hide_banner -loglevel error -re \
#     -f lavfi -i "testsrc=duration=5:size=320x240:rate=25" \
#     -f lavfi -i "sine=frequency=440:duration=5" \
#     -c:v libx264 -preset ultrafast -tune zerolatency \
#     -c:a aac -b:a 64k \
#     -f flv "rtmp://localhost:1935/live/test"
#
# Verify: Check server log (Terminal 1) for "connection registered" message.
# See MANUAL_TESTING.md for complete manual testing guide.
# ============================================================================

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

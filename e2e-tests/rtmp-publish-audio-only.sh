#!/usr/bin/env bash
# ============================================================================
# TEST: rtmp-publish-audio-only
# GROUP: RTMP Basic
#
# WHAT IS TESTED:
#   Audio-only RTMP stream (AAC, no video track). Verifies the server
#   correctly handles streams without a video component — important for
#   audio-only broadcasting and radio-style use cases.
#
# EXPECTED RESULT:
#   - Server accepts the audio-only publish
#   - Server log shows connection registered, no errors
#   - No panics or crashes from missing video track
#
# PREREQUISITES:
#   - FFmpeg with aac encoder
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/rtmp-publish-audio-only.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="rtmp-publish-audio-only"
PORT=$(unique_port "$TEST_NAME")

setup "$TEST_NAME"

start_server "$PORT" "-log-level" "debug"

log_step "Publishing audio-only stream (AAC, 5s)..."
publish_audio_only "rtmp://localhost:${PORT}/live/audio" 5
sleep 2

assert_log_contains "$SERVER_LOG" "connection registered" "Server registered the connection"
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

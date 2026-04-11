#!/usr/bin/env bash
# ============================================================================
# TEST: rtmp-publish-play-h264
# GROUP: RTMP Basic
#
# WHAT IS TESTED:
#   Full RTMP publish-to-subscribe cycle using H.264 video + AAC audio.
#   An FFmpeg publisher sends a synthetic test pattern to the server,
#   while a separate FFmpeg subscriber captures the relayed stream to a file.
#   This validates the complete pub/sub pipeline.
#
# EXPECTED RESULT:
#   - Server accepts both publisher and subscriber connections
#   - Subscriber captures the stream to an FLV file
#   - Captured file contains H.264 video and AAC audio
#   - Captured duration is at least 2 seconds (allowing for startup latency)
#
# PREREQUISITES:
#   - FFmpeg with libx264, aac encoder
#   - ffprobe (bundled with FFmpeg)
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/rtmp-publish-play-h264.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="rtmp-publish-play-h264"
PORT=$(unique_port "$TEST_NAME")

setup "$TEST_NAME"

# Step 1: Start server
start_server "$PORT" "-log-level" "debug"

# Step 2: Start publisher in background (real-time, 8 seconds)
log_step "Starting publisher in background (8s)..."
publish_test_pattern "rtmp://localhost:${PORT}/live/test" 8 &
PUB_PID=$!

# Step 3: Wait for publisher to establish, then subscribe
sleep 2
CAPTURE="$TMPDIR/capture.flv"
log_step "Starting subscriber capture (5s max)..."
# Use a shorter capture timeout than publisher duration so we capture
# while publisher is still live, then FFmpeg exits cleanly via -t
start_capture "rtmp://localhost:${PORT}/live/test" "$CAPTURE" 5

# Step 4: Wait for capture to finish (it has a -t timeout), then publisher
wait_and_stop_capture
wait $PUB_PID 2>/dev/null || true

# Step 5: Verify captured file
assert_file_exists "$CAPTURE" "Capture file exists"
assert_video_codec "$CAPTURE" "h264"
assert_audio_codec "$CAPTURE" "aac"
assert_duration "$CAPTURE" 2.0 10.0

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

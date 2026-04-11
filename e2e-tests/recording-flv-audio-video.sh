#!/usr/bin/env bash
# ============================================================================
# TEST: recording-flv-audio-video
# GROUP: FLV Recording
#
# WHAT IS TESTED:
#   FLV recording contains both audio and video tracks. Verifies the
#   recording pipeline correctly interleaves and writes both media types.
#   Uses ffprobe to count the number of streams in the output file.
#
# EXPECTED RESULT:
#   - Recording has exactly 2 streams (1 video, 1 audio)
#   - Video codec is H.264, audio codec is AAC
#   - Duration is at least 2 seconds
#
# PREREQUISITES:
#   - FFmpeg with libx264, aac
#   - ffprobe
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/recording-flv-audio-video.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="recording-flv-audio-video"
PORT=$(unique_port "$TEST_NAME")

setup "$TEST_NAME"

RECORD_DIR="$TMPDIR/recordings"
mkdir -p "$RECORD_DIR"

start_server "$PORT" "-log-level" "debug" "-record-all" "true" "-record-dir" "$RECORD_DIR"

log_step "Publishing H.264+AAC test pattern (5s)..."
publish_test_pattern "rtmp://localhost:${PORT}/live/av-test" 5
sleep 3

RECORDING=$(find "$RECORD_DIR" -name "*.flv" -type f | head -n 1)
if [[ -z "$RECORDING" ]]; then
    fail_check "Recording file created" "No .flv file found"
else
    pass_check "Recording file created"
    # Verify both audio and video streams
    STREAM_COUNT=$(ffprobe -v error -show_entries format=nb_streams -of csv=p=0 "$RECORDING" 2>/dev/null || echo "0")
    if [[ "$STREAM_COUNT" -ge 2 ]]; then
        pass_check "Recording has $STREAM_COUNT streams (audio + video)"
    else
        fail_check "Recording has audio + video" "only $STREAM_COUNT stream(s) found"
    fi
    assert_video_codec "$RECORDING" "h264"
    assert_audio_codec "$RECORDING" "aac"
    assert_duration "$RECORDING" 2.0 10.0
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

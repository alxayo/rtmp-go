#!/usr/bin/env bash
# ============================================================================
# TEST: recording-flv-h264
# GROUP: FLV Recording
#
# WHAT IS TESTED:
#   Server-side FLV recording with -record-all flag. A publisher sends
#   H.264+AAC content and the server writes it to a .flv file. Verifies
#   the recording file is valid, contains the correct codecs, and can be
#   fully decoded without errors.
#
# EXPECTED RESULT:
#   - A .flv file appears in the recording directory
#   - ffprobe shows H.264 video and AAC audio
#   - File is fully decodable (no corruption)
#   - Duration is at least 2 seconds
#
# PREREQUISITES:
#   - FFmpeg with libx264, aac
#   - ffprobe
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/recording-flv-h264.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="recording-flv-h264"
PORT=$(unique_port "$TEST_NAME")

setup "$TEST_NAME"

RECORD_DIR="$TMPDIR/recordings"
mkdir -p "$RECORD_DIR"

start_server "$PORT" "-log-level" "debug" "-record-all" "true" "-record-dir" "$RECORD_DIR"

log_step "Publishing H.264+AAC test pattern (5s)..."
publish_test_pattern "rtmp://localhost:${PORT}/live/rec-test" 5
sleep 3

# Find the recording file
RECORDING=$(find "$RECORD_DIR" -name "*.flv" -type f | head -n 1)
if [[ -z "$RECORDING" ]]; then
    fail_check "Recording file created" "No .flv file found in $RECORD_DIR"
else
    pass_check "Recording file created: $(basename "$RECORDING")"
    assert_video_codec "$RECORDING" "h264"
    assert_audio_codec "$RECORDING" "aac"
    assert_duration "$RECORDING" 2.0 10.0
    assert_decodable "$RECORDING"
fi

teardown
report_result "$TEST_NAME"

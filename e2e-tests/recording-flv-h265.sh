#!/usr/bin/env bash
# ============================================================================
# TEST: recording-flv-h265
# GROUP: FLV Recording
#
# WHAT IS TESTED:
#   Server-side FLV recording preserves H.265/HEVC codec when published via
#   Enhanced RTMP. Verifies that the recording pipeline correctly handles
#   HEVC content including Enhanced RTMP FourCC signaling.
#
# EXPECTED RESULT:
#   - A .flv recording file is created
#   - ffprobe shows HEVC (H.265) video codec
#   - File is decodable
#
# PREREQUISITES:
#   - FFmpeg 6.1+ with libx265 encoder
#   - ffprobe
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/recording-flv-h265.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="recording-flv-h265"
PORT=$(unique_port "$TEST_NAME")

# Check for libx265 support
if ! ffmpeg -hide_banner -encoders 2>/dev/null | grep -q libx265; then
    echo -e "${YELLOW}SKIP: FFmpeg does not have libx265 encoder${NC}"
    exit 2
fi

setup "$TEST_NAME"

RECORD_DIR="$TMPDIR/recordings"
mkdir -p "$RECORD_DIR"

start_server "$PORT" "-log-level" "debug" "-record-all" "true" "-record-dir" "$RECORD_DIR"

log_step "Publishing H.265 test pattern via Enhanced RTMP (5s)..."
publish_h265_test_pattern "rtmp://localhost:${PORT}/live/h265-rec" 5
sleep 3

RECORDING=$(find "$RECORD_DIR" -name "*.flv" -type f | head -n 1)
if [[ -z "$RECORDING" ]]; then
    fail_check "H.265 recording file created" "No .flv file found in $RECORD_DIR"
else
    pass_check "H.265 recording file created: $(basename "$RECORDING")"
    assert_video_codec "$RECORDING" "hevc"
    assert_decodable "$RECORDING"
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

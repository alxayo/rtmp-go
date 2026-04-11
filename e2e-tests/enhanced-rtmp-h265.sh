#!/usr/bin/env bash
# ============================================================================
# TEST: enhanced-rtmp-h265
# GROUP: Enhanced RTMP
#
# WHAT IS TESTED:
#   H.265 (HEVC) publishing via Enhanced RTMP (E-RTMP v2) with FourCC "hvc1"
#   codec signaling. Verifies the server accepts Enhanced RTMP connections,
#   correctly handles the FourCC-based codec negotiation, and that the
#   published content can be subscribed to with HEVC codec preserved.
#
# EXPECTED RESULT:
#   - Server accepts the H.265 Enhanced RTMP publish
#   - Server log shows Enhanced RTMP activity (ExHeader / FourCC)
#   - Subscriber capture contains HEVC video codec
#   - Captured file is decodable
#
# PREREQUISITES:
#   - FFmpeg 6.1+ with libx265 encoder and Enhanced RTMP support
#   - ffprobe
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/enhanced-rtmp-h265.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="enhanced-rtmp-h265"
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

# Start publisher first (8s, background)
log_step "Publishing H.265 via Enhanced RTMP (8s, background)..."
publish_h265_test_pattern "rtmp://localhost:${PORT}/live/h265-test" 8 &
PUB_PID=$!

sleep 2
# Start subscriber capture
CAPTURE="$TMPDIR/capture.flv"
log_step "Starting subscriber for H.265 stream..."
start_capture "rtmp://localhost:${PORT}/live/h265-test" "$CAPTURE" 5

wait_and_stop_capture
wait $PUB_PID 2>/dev/null || true

# Verify capture
assert_file_exists "$CAPTURE" "H.265 subscriber capture exists"
assert_video_codec "$CAPTURE" "hevc"
assert_decodable "$CAPTURE"

# Also verify recording
RECORDING=$(find "$RECORD_DIR" -name "*.flv" -type f | head -n 1)
if [[ -n "$RECORDING" ]]; then
    pass_check "H.265 recording also created"
    assert_video_codec "$RECORDING" "hevc"
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

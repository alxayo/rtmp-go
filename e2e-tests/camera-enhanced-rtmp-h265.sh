#!/usr/bin/env bash
# ============================================================================
# TEST: camera-enhanced-rtmp-h265
# GROUP: Camera Tests
#
# WHAT IS TESTED:
#   Enhanced RTMP publish from a live camera using H.265 (HEVC) codec.
#   The server records the stream to an MP4 file (H.265 streams use MP4
#   container format). After publishing, the recording is validated to
#   contain HEVC video and be fully decodable.
#
# EXPECTED RESULT:
#   - Camera H.265 stream published via Enhanced RTMP
#   - Server records to MP4 file
#   - Recording contains HEVC video codec
#   - Recording is fully decodable
#   - No server panics
#
# PREREQUISITES:
#   - Live camera device (macOS: avfoundation, Linux: v4l2)
#   - FFmpeg with libx265 and aac support
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/camera-enhanced-rtmp-h265.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="camera-enhanced-rtmp-h265"
PORT=$(unique_port "$TEST_NAME")

# Skip if camera tests disabled
if [[ "${SKIP_CAMERA_TESTS:-0}" == "1" ]]; then
    echo -e "${YELLOW}SKIP: Camera tests disabled (SKIP_CAMERA_TESTS=1)${NC}"
    exit 2
fi

# Check H.265 encoder availability
if ! ffmpeg -codecs 2>/dev/null | grep -q "libx265"; then
    echo -e "${YELLOW}SKIP: libx265 encoder not available${NC}"
    exit 2
fi

# Detect platform camera
OS_NAME="$(uname -s)"
CAMERA_INPUT=""
case "$OS_NAME" in
    Darwin)
        if ffmpeg -f avfoundation -list_devices true -i "" 2>&1 | grep -q "video"; then
            CAMERA_INPUT="-f avfoundation -framerate 30 -i 0:0"
        fi
        ;;
    Linux)
        if [[ -e /dev/video0 ]]; then
            CAMERA_INPUT="-f v4l2 -framerate 30 -i /dev/video0"
        fi
        ;;
    *)
        echo -e "${YELLOW}SKIP: Camera detection not supported on $OS_NAME${NC}"
        exit 2
        ;;
esac

if [[ -z "$CAMERA_INPUT" ]]; then
    echo -e "${YELLOW}SKIP: No camera device detected${NC}"
    exit 2
fi

setup "$TEST_NAME"
RECORD_DIR="$TMPDIR/recordings"
mkdir -p "$RECORD_DIR"

start_server "$PORT" "-log-level" "debug" "-record-all" "true" "-record-dir" "$RECORD_DIR"

STREAM_URL="rtmp://localhost:${PORT}/live/camera-h265"

log_step "Publishing camera via Enhanced RTMP H.265 (5s)..."
set +e
eval ffmpeg -hide_banner -loglevel error \
    $CAMERA_INPUT \
    -t 5 \
    -c:v libx265 -preset ultrafast \
    -c:a aac -b:a 128k \
    -f flv "$STREAM_URL" \
    > "$LOG_DIR/${TEST_NAME}-publish.log" 2>&1
set -e

sleep 2

# Find recording (H.265 → MP4, fallback to FLV)
RECORD_FILE=""
for f in "$RECORD_DIR"/live_camera-h265_*.mp4 "$RECORD_DIR"/live_camera-h265_*.flv; do
    [[ -f "$f" ]] && RECORD_FILE="$f" && break
done

assert_file_exists "${RECORD_FILE:-missing}" "Recording created"
if [[ -n "$RECORD_FILE" ]] && [[ -f "$RECORD_FILE" ]]; then
    assert_video_codec "$RECORD_FILE" "hevc" "Video codec is HEVC"
    assert_decodable "$RECORD_FILE" "Recording is decodable"
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

#!/usr/bin/env bash
# ============================================================================
# TEST: camera-rtmp-h264
# GROUP: Camera Tests
#
# WHAT IS TESTED:
#   RTMP publish from a live camera device using H.264 codec. The server
#   records the stream to an FLV file. After publishing, the recording
#   is validated with ffprobe to ensure the video codec is H.264, audio
#   is AAC, and the file is fully decodable.
#
# EXPECTED RESULT:
#   - Camera stream published successfully via RTMP
#   - Server records to FLV file in the recording directory
#   - Recording contains H.264 video and AAC audio
#   - No server panics or errors
#
# PREREQUISITES:
#   - Live camera device (macOS: avfoundation, Linux: v4l2)
#   - FFmpeg with libx264 and aac support
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/camera-rtmp-h264.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="camera-rtmp-h264"
PORT=$(unique_port "$TEST_NAME")

# Skip if camera tests disabled
if [[ "${SKIP_CAMERA_TESTS:-0}" == "1" ]]; then
    echo -e "${YELLOW}SKIP: Camera tests disabled (set SKIP_CAMERA_TESTS=0 to enable)${NC}"
    exit 2
fi

# Detect platform camera
OS_NAME="$(uname -s)"
CAMERA_INPUT=""
case "$OS_NAME" in
    Darwin)
        if ffmpeg -f avfoundation -list_devices true -i "" 2>&1 | grep -q "\[0\]"; then
            # First video device + first audio device; no video_size for max compatibility
            CAMERA_INPUT="-f avfoundation -framerate 30 -i 0:0"
        fi
        ;;
    Linux)
        if [[ -e /dev/video0 ]]; then
            CAMERA_INPUT="-f v4l2 -framerate 30 -i /dev/video0"
            if command -v arecord &>/dev/null || [[ -d /dev/snd ]]; then
                CAMERA_INPUT="-f v4l2 -framerate 30 -i /dev/video0 -f alsa -i default"
            fi
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

STREAM_URL="rtmp://localhost:${PORT}/live/camera-h264"

log_step "Publishing camera stream via RTMP H.264 (5s)..."
set +e
eval ffmpeg -hide_banner -loglevel error \
    $CAMERA_INPUT \
    -t 5 \
    -c:v libx264 -preset ultrafast -tune zerolatency \
    -c:a aac -b:a 128k \
    -f flv "$STREAM_URL" \
    > "$LOG_DIR/${TEST_NAME}-publish.log" 2>&1
set -e

sleep 2  # Wait for recording flush

# Find the recording file
RECORDING=$(find "$RECORD_DIR" -name "*.flv" -type f | head -n 1)
if [[ -z "$RECORDING" ]]; then
    fail_check "FLV recording created" "No .flv file found in $RECORD_DIR"
else
    pass_check "FLV recording created: $(basename "$RECORDING")"
    assert_video_codec "$RECORDING" "h264"
    assert_audio_codec "$RECORDING" "aac"
    assert_decodable "$RECORDING"
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

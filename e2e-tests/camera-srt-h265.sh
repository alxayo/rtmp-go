#!/usr/bin/env bash
# ============================================================================
# TEST: camera-srt-h265
# GROUP: Camera Tests
#
# WHAT IS TESTED:
#   SRT ingest from a live system camera with H.265/HEVC encoding and AAC
#   audio. The camera feed is captured via platform-native APIs (avfoundation
#   on macOS, v4l2 on Linux), encoded with libx265, and streamed over SRT
#   to the server. Recording is enabled, and the test verifies that the
#   resulting file (MP4 for H.265, with FLV fallback) contains valid HEVC
#   video that can be decoded.
#
# EXPECTED RESULT:
#   - Server accepts the SRT camera stream
#   - Recording file (.mp4 or .flv) is created with HEVC video
#   - Recording has a video stream and is fully decodable
#   - No server panics or fatal errors
#
# PREREQUISITES:
#   - Live camera device (macOS: avfoundation, Linux: v4l2)
#   - FFmpeg with SRT, libx265, and AAC support
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/camera-srt-h265.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="camera-srt-h265"
PORT=$(unique_port "$TEST_NAME")
SRT_PORT=$((PORT + 200))

# Allow skipping camera tests via environment variable
if [[ "${SKIP_CAMERA_TESTS:-0}" == "1" ]]; then
    echo -e "${YELLOW}SKIP: Camera tests disabled (SKIP_CAMERA_TESTS=1)${NC}"
    exit 2
fi

# Check SRT protocol support
if ! ffmpeg -hide_banner -protocols 2>/dev/null | grep -q srt; then
    echo -e "${YELLOW}SKIP: FFmpeg does not have SRT protocol support${NC}"
    exit 2
fi

# Check H.265 encoder support
if ! ffmpeg -hide_banner -encoders 2>/dev/null | grep -q libx265; then
    echo -e "${YELLOW}SKIP: FFmpeg does not have libx265 encoder${NC}"
    exit 2
fi

# Platform camera detection
CAMERA_INPUT=""
case "$(uname -s)" in
    Darwin)
        if ffmpeg -f avfoundation -list_devices true -i "" 2>&1 | grep -q "video"; then
            CAMERA_INPUT="-f avfoundation -framerate 30 -i 0:none"
        fi
        ;;
    Linux)
        if [[ -e /dev/video0 ]]; then
            CAMERA_INPUT="-f v4l2 -framerate 30 -i /dev/video0"
        fi
        ;;
    *)
        echo -e "${YELLOW}SKIP: Camera detection not supported on $(uname -s)${NC}"
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

start_server "$PORT" "-log-level" "debug" \
    "-srt-listen" "localhost:${SRT_PORT}" \
    "-record-all" "true" "-record-dir" "$RECORD_DIR"

log_step "Publishing camera feed via SRT H.265 (5s)..."
PUBLISH_LOG="$LOG_DIR/${TEST_NAME}-publish.log"
set +e
eval ffmpeg -hide_banner -loglevel error \
    $CAMERA_INPUT \
    -c:v libx265 -preset ultrafast \
    -c:a aac -b:a 64k \
    -t 5 \
    -f mpegts "\"srt://localhost:${SRT_PORT}?streamid=publish:live/camera-h265&latency=200000\"" \
    > "$PUBLISH_LOG" 2>&1
set -e

log_step "Waiting for recording flush..."
sleep 3

# Assertions
assert_log_not_contains "$SERVER_LOG" "panic\|FATAL" "No server panics"

# H.265 recordings should be MP4; fall back to checking FLV for backwards compat
RECORDING=$(find "$RECORD_DIR" -name "*.mp4" -type f | head -n 1)
if [[ -z "$RECORDING" ]]; then
    RECORDING=$(find "$RECORD_DIR" -name "*.flv" -type f | head -n 1)
fi
if [[ -z "$RECORDING" ]]; then
    fail_check "H.265 recording file created" "No recording file found in $RECORD_DIR"
else
    pass_check "H.265 recording file created: $(basename "$RECORDING")"
    assert_has_video "$RECORDING"
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
# Quick manual test:
#   ./rtmp-server -listen :1935 -srt-listen localhost:10080 \
#       -record-all true -record-dir ./recordings -log-level debug
#
#   # macOS camera (H.265):
#   ffmpeg -f avfoundation -framerate 30 -i 0:none \
#       -c:v libx265 -preset ultrafast \
#       -c:a aac -b:a 64k -t 10 \
#       -f mpegts "srt://localhost:10080?streamid=publish:live/camera-h265"
#
#   # Linux camera (H.265):
#   ffmpeg -f v4l2 -framerate 30 -i /dev/video0 \
#       -c:v libx265 -preset ultrafast \
#       -c:a aac -b:a 64k -t 10 \
#       -f mpegts "srt://localhost:10080?streamid=publish:live/camera-h265"
# ============================================================================

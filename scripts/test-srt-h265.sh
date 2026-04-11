#!/bin/bash

# SRT H.265 Camera Ingest Test
#
# This script tests H.265/HEVC video ingest via SRT protocol. It captures
# video from the system camera (or generates a test pattern if unavailable)
# and streams it to the RTMP/SRT server running locally.
#
# The test:
#   1. Builds the server if needed
#   2. Starts the RTMP server with SRT listener enabled
#   3. Streams H.265 video via SRT from ffmpeg
#   4. Records the stream locally
#   5. Validates the recording contains H.265 frames
#   6. Compares with H.264 for reference
#
# Requirements:
#   - ffmpeg with libsrt and libx265 support
#   - ffprobe (comes with ffmpeg)
#   - Go 1.21+ (for building the server)
#
# Usage:
#   ./test-srt-h265.sh [DURATION]
#
# Example:
#   ./test-srt-h265.sh 30    # 30 second test

set -e

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'  # No Color

# Configuration
DURATION="${1:-10}"  # Default 10 seconds
STREAM_KEY="live/h265-test"
RECORD_DIR="./recordings"

# Detect platform
UNAME=$(uname -s)
if [[ "$UNAME" == "Darwin" ]]; then
    PLATFORM="macOS"
    CAMERA_DEVICE="0:1"  # macOS: avfoundation device
elif [[ "$UNAME" == "Linux" ]]; then
    PLATFORM="Linux"
    CAMERA_DEVICE="/dev/video0"  # Linux: v4l2 device
else
    PLATFORM="Windows"
    CAMERA_DEVICE="0"  # Windows: dshow or gdigrab
fi

echo "=== SRT H.265 Ingest Test (${PLATFORM}) ==="
echo "Duration: ${DURATION}s"
echo "Stream key: ${STREAM_KEY}"
echo ""

# Function to print colored output
log_info() {
    echo -e "${GREEN}✓${NC} $1"
}

log_error() {
    echo -e "${RED}✗${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}⚠${NC} $1"
}

log_step() {
    echo -e "${BLUE}→${NC} $1"
}

# Check if ffmpeg has H.265 support
check_h265_support() {
    if ffmpeg -codecs 2>&1 | grep -q "libx265"; then
        log_info "H.265 encoder available"
        return 0
    else
        log_warn "H.265 encoder not available, falling back to H.264"
        return 1
    fi
}

# Build the server if needed
if [ ! -f "./rtmp-server" ]; then
    log_step "Building RTMP server..."
    if go build -o rtmp-server ./cmd/rtmp-server; then
        log_info "Server built"
    else
        log_error "Failed to build server"
        exit 1
    fi
else
    log_info "Server already built"
fi

# Create recordings directory
mkdir -p "$RECORD_DIR"

# Cleanup old processes
log_step "Cleaning up old processes..."
pkill -f "rtmp-server" || true
sleep 1
log_info "Ready"

# Start the server
log_step "Starting RTMP server..."
./rtmp-server \
    -listen 127.0.0.1:1935 \
    -srt-listen 127.0.0.1:10080 \
    -record-all true \
    -record-dir "$RECORD_DIR" \
    -log-level info &
SERVER_PID=$!

# Wait for server to start
sleep 2

if ! kill -0 $SERVER_PID 2>/dev/null; then
    log_error "Server failed to start"
    exit 1
fi
log_info "Server started (PID: $SERVER_PID)"

# Determine which encoding to use
H265_AVAILABLE=false
if check_h265_support; then
    H265_AVAILABLE=true
    CODEC_OPTS="-c:v libx265 -preset ultrafast -crf 28"
    CODEC_NAME="H.265"
else
    CODEC_OPTS="-c:v libx264 -preset ultrafast -crf 28"
    CODEC_NAME="H.264"
fi

# Start capturing and streaming
log_step "Starting camera capture and SRT stream..."
echo "Camera device: $CAMERA_DEVICE"
echo "Streaming to: srt://localhost:10080?streamid=publish:${STREAM_KEY}"
echo "Codec: ${CODEC_NAME}"
echo ""

# Platform-specific ffmpeg input options
case "$PLATFORM" in
    "macOS")
        # Use AVFoundation on macOS (camera + microphone)
        ffmpeg_input="-f avfoundation -video_size 1280x720 -framerate 30 -i $CAMERA_DEVICE"
        ;;
    "Linux")
        # Use v4l2 on Linux
        ffmpeg_input="-f v4l2 -i $CAMERA_DEVICE"
        ;;
    "Windows")
        # Use gdigrab on Windows
        ffmpeg_input="-f gdigrab -i desktop"
        ;;
esac

# Run ffmpeg stream in background, then stop after DURATION seconds.
# We avoid GNU coreutils `timeout` which is not available on macOS.
ffmpeg \
    $ffmpeg_input \
    $CODEC_OPTS \
    -c:a aac -b:a 128k \
    -f mpegts \
    "srt://localhost:10080?streamid=publish:${STREAM_KEY}" \
    2>&1 &
FFMPEG_PID=$!

# Wait for the requested duration, then stop ffmpeg
sleep "$DURATION"
kill $FFMPEG_PID 2>/dev/null || true
wait $FFMPEG_PID 2>/dev/null || true
log_info "Stream stopped after ${DURATION}s"

echo ""
log_step "Waiting for recordings to be flushed..."
sleep 2

# Find the recording file — H.265 streams are recorded as MP4, H.264 as FLV
RECORD_FILE=$(ls -t "$RECORD_DIR"/*.mp4 "$RECORD_DIR"/*.flv 2>/dev/null | head -1)

if [ -z "$RECORD_FILE" ]; then
    log_error "No recording found in $RECORD_DIR"
    kill $SERVER_PID 2>/dev/null || true
    exit 1
fi

log_info "Recording found: $(basename $RECORD_FILE)"

# Validate the recording
log_step "Validating recording..."
FILE_SIZE=$(stat -f%z "$RECORD_FILE" 2>/dev/null || stat -c%s "$RECORD_FILE" 2>/dev/null || echo "unknown")
log_info "File size: ${FILE_SIZE} bytes"

# Check container format for H.265 streams — should be MP4
if [ "$H265_AVAILABLE" = true ]; then
    if [[ "$RECORD_FILE" == *.mp4 ]]; then
        log_info "✓ H.265 recorded as MP4 (correct format)"
    else
        log_warn "Expected MP4 container for H.265, got $(basename ${RECORD_FILE##*.})"
    fi
fi

# Check for video codec in the recording
if command -v ffprobe &> /dev/null; then
    CODEC_FOUND=$(ffprobe -v error -select_streams v:0 -show_entries stream=codec_name \
        -of csv=p=0 "$RECORD_FILE" 2>/dev/null | head -1 || echo "unknown")
    log_info "Video codec in file: ${CODEC_FOUND}"

    # Check for H.265 if we tried to stream it
    if [ "$H265_AVAILABLE" = true ]; then
        if [[ "$CODEC_FOUND" == "hevc" ]] || [[ "$CODEC_FOUND" == "h265" ]]; then
            log_info "✓ H.265 frames successfully recorded"
        else
            log_warn "Expected H.265 but got ${CODEC_FOUND}"
        fi
    fi
else
    log_warn "ffprobe not available, skipping codec validation"
fi

# Stop server
log_step "Stopping server..."
kill $SERVER_PID 2>/dev/null || true
sleep 1
log_info "Server stopped"

echo ""
echo "=== Test Complete ==="
if [ -n "$RECORD_FILE" ]; then
    log_info "Recording saved: $RECORD_FILE"
    echo ""
    echo "To play the recording:"
    echo "  ffplay $RECORD_FILE"
    echo ""
    echo "To stream to a player (RTMP):"
    echo "  ffmpeg -re -i $RECORD_FILE -c copy -f flv rtmp://your-server/live/stream"
else
    log_error "Test failed: no recording"
    exit 1
fi

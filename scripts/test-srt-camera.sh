#!/bin/bash
# SRT Camera Ingest Test Script
# 
# This script demonstrates SRT ingest from an integrated camera:
# 1. Builds the RTMP server (if not already built)
# 2. Starts the server with SRT enabled and recording
# 3. Captures video from the integrated camera using FFmpeg
# 4. Streams the camera feed via SRT to the server
# 5. Records the stream locally to FLV format
# 
# Usage:
#   ./scripts/test-srt-camera.sh [--duration SECONDS]
#   ./scripts/test-srt-camera.sh --duration 30
#
# Requirements:
#   - FFmpeg with camera support (avfoundation on macOS, v4l2 on Linux)
#   - go 1.21+

set -e

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
SERVER_BINARY="$PROJECT_ROOT/rtmp-server"
RECORDINGS_DIR="$PROJECT_ROOT/recordings"
SRT_PORT=10080
RTMP_PORT=1935
STREAM_KEY="live/camera-test"
DURATION="${1:-30}"  # Default 30 seconds, can override

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --duration)
            DURATION="$2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== SRT Camera Ingest Test ===${NC}"
echo "Duration: ${DURATION}s"
echo "Stream key: ${STREAM_KEY}"
echo ""

# Step 1: Build server if needed
if [ ! -f "$SERVER_BINARY" ]; then
    echo -e "${YELLOW}Building server...${NC}"
    cd "$PROJECT_ROOT"
    go build -o rtmp-server ./cmd/rtmp-server
    echo -e "${GREEN}✓ Server built${NC}"
else
    echo -e "${GREEN}✓ Server already built${NC}"
fi

# Step 2: Clean up old processes
echo -e "${YELLOW}Cleaning up old processes...${NC}"
pkill -f "rtmp-server" || true
pkill -f "ffmpeg.*srt://" || true
sleep 1

# Step 3: Create recordings directory
mkdir -p "$RECORDINGS_DIR"

# Step 4: Start server
echo -e "${YELLOW}Starting RTMP server...${NC}"
"$SERVER_BINARY" \
    -listen "localhost:$RTMP_PORT" \
    -srt-listen "localhost:$SRT_PORT" \
    -record-all true \
    -record-dir "$RECORDINGS_DIR" \
    -log-level debug &

SERVER_PID=$!
echo -e "${GREEN}✓ Server started (PID: $SERVER_PID)${NC}"

# Wait for server to be ready
sleep 2

# Step 5: Detect OS and set camera input device
OS=$(uname -s)
case "$OS" in
    Darwin)
        # macOS: avfoundation
        # List available devices: ffmpeg -f avfoundation -list_devices true -i ""
        CAMERA_INPUT="0"  # Default built-in camera
        CAMERA_FLAG="-f avfoundation"
        AUDIO_INPUT=":1"  # Built-in microphone
        ;;
    Linux)
        # Linux: v4l2
        CAMERA_INPUT="/dev/video0"
        CAMERA_FLAG="-f v4l2"
        AUDIO_INPUT="-f pulse -i default"
        ;;
    *)
        echo -e "${RED}Unsupported OS: $OS${NC}"
        kill $SERVER_PID || true
        exit 1
        ;;
esac

# Step 6: Capture camera and stream via SRT
echo -e "${YELLOW}Starting camera capture and SRT stream...${NC}"
echo "Camera device: $CAMERA_INPUT"
echo "Streaming to: srt://localhost:$SRT_PORT?streamid=publish:$STREAM_KEY"
echo ""

# FFmpeg command with camera input
# Start FFmpeg in background and kill after duration
if [ "$OS" = "Darwin" ]; then
    # macOS: avfoundation with camera + microphone
    ffmpeg \
        -f avfoundation \
        -video_size 1280x720 \
        -framerate 30 \
        -i "$CAMERA_INPUT$AUDIO_INPUT" \
        -c:v libx264 \
        -preset ultrafast \
        -tune zerolatency \
        -b:v 2500k \
        -c:a aac \
        -b:a 128k \
        -f mpegts \
        "srt://localhost:$SRT_PORT?streamid=publish:$STREAM_KEY" \
        2>&1 &
else
    # Linux: v4l2 with camera
    ffmpeg \
        -f v4l2 \
        -video_size 1280x720 \
        -framerate 30 \
        -i "$CAMERA_INPUT" \
        -c:v libx264 \
        -preset ultrafast \
        -tune zerolatency \
        -b:v 2500k \
        -f mpegts \
        "srt://localhost:$SRT_PORT?streamid=publish:$STREAM_KEY" \
        2>&1 &
fi

FFMPEG_PID=$!
# Wait for duration, then kill FFmpeg
sleep "$DURATION"
kill $FFMPEG_PID 2>/dev/null || true
wait $FFMPEG_PID 2>/dev/null || true

echo -e "${YELLOW}FFmpeg finished, waiting for recordings to finalize...${NC}"
sleep 2

# Step 7: Stop server
echo -e "${YELLOW}Stopping server...${NC}"
kill $SERVER_PID || true
wait $SERVER_PID 2>/dev/null || true
echo -e "${GREEN}✓ Server stopped${NC}"

# Step 8: Check recordings
echo ""
echo -e "${GREEN}=== Test Complete ===${NC}"
if [ -f "$RECORDINGS_DIR"/*.flv ]; then
    echo -e "${GREEN}✓ Recording saved:${NC}"
    ls -lh "$RECORDINGS_DIR"/*.flv | tail -1
    
    # Show file info
    LATEST_RECORDING=$(ls -t "$RECORDINGS_DIR"/*.flv 2>/dev/null | head -1)
    if [ -n "$LATEST_RECORDING" ]; then
        echo ""
        echo -e "${YELLOW}Recording info:${NC}"
        ffprobe -show_format -show_streams "$LATEST_RECORDING" 2>&1 | grep -E "duration|codec_name|width|height" | head -10
        echo ""
        echo -e "${YELLOW}To play the recording:${NC}"
        echo "ffplay \"$LATEST_RECORDING\""
    fi
else
    echo -e "${RED}✗ No recordings found${NC}"
    exit 1
fi

echo -e "${GREEN}✓ SRT camera test completed successfully!${NC}"

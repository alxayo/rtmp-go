#!/bin/bash

# =======================================================
# RTMP Streaming Script (macOS)
# Author: Gemini
# Description: Takes a local MP4 file and streams it continuously to a specified RTMP endpoint.
# Usage: ./stream.sh "rtmp://[server_address]/[app]/[stream_key]"
# =======================================================

# --- Configuration Variables ---
# 1. Input File Path:
# IMPORTANT: Change 'input.mp4' to the actual path of your video file.
INPUT_FILE="/Users/alex/Downloads/bbb_sunflower_1080p_60fps_normal.mp4"

# 2. RTMP Destination:
# This variable is assigned the first argument ($1) passed to the script.
RTMP_URL="$1"

# --- Pre-Flight Checks ---
# Check if the RTMP URL argument was provided
if [ -z "$RTMP_URL" ]; then
echo "ERROR: Missing RTMP destination URL."
echo "Usage: $0 "rtmp://[server_address]/[app]/[stream_key]""
exit 1
fi

# Check if the input file exists
if [ ! -f "$INPUT_FILE" ]; then
echo "ERROR: Input file not found at path: $INPUT_FILE"
echo "Please update the 'INPUT_FILE' variable within the script."
exit 1
fi

# Check if ffmpeg is installed
if ! command -v ffmpeg &> /dev/null
then
echo "ERROR: ffmpeg could not be found."
echo "Please ensure ffmpeg is installed and available in your PATH (e.g., via Homebrew)."
exit 1
fi

# --- Streaming Command ---
echo "========================================================="
echo "Starting stream of $INPUT_FILE to:"
echo "$RTMP_URL"
echo "========================================================="

# The core ffmpeg command:
# -re: Read input at native frame rate (crucial for simulating a live stream)
ffmpeg \
    -re \
    -i "$INPUT_FILE" \
    -c:v libx264 \
    -preset veryfast \
    -b:v 3000k \
    -maxrate 3000k \
    -bufsize 6000k \
    -pix_fmt yuv420p \
    -g 50 \
    -c:a aac \
    -b:a 128k \
    -ar 44100 \
    -f flv \
    "$RTMP_URL"

# Check the exit status of ffmpeg
if [ $? -ne 0 ]; then
echo "FFmpeg stream failed. Check the error output above for details."
else
echo "FFmpeg stream finished successfully."
fi
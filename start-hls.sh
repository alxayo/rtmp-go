#!/usr/bin/env bash
# start-hls.sh — RTMP → HLS pipeline launcher
# Usage: ./start-hls.sh [stream-key]
set -euo pipefail

STREAM_KEY="${1:-test}"
RTMP_URL="rtmp://127.0.0.1:1935/live/${STREAM_KEY}"
HLS_DIR="$(dirname "$0")/hls"
LOG="$HLS_DIR/ffmpeg.log"

mkdir -p "$HLS_DIR"

echo "[hls] Watching RTMP stream key: ${STREAM_KEY}"
echo "[hls] Output: ${HLS_DIR}/stream.m3u8"
echo "[hls] Log:    ${LOG}"

while true; do
  echo "[$(date +%T)] Connecting to ${RTMP_URL} ..." | tee -a "$LOG"
  ffmpeg -hide_banner -loglevel warning \
    -i "$RTMP_URL" \
    -c:v libx264 -preset veryfast -g 30 -sc_threshold 0 \
    -c:a aac -b:a 128k \
    -f hls \
    -hls_time 2 \
    -hls_list_size 10 \
    -hls_flags delete_segments+append_list+temp_file \
    -hls_segment_filename "${HLS_DIR}/seg_%05d.ts" \
    "${HLS_DIR}/stream.m3u8" 2>>"$LOG" || true
  echo "[$(date +%T)] ffmpeg exited – retrying in 3s..." | tee -a "$LOG"
  sleep 3
done

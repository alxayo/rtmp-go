#!/usr/bin/env bash
# on-publish-hls.sh — Shell hook: converts incoming RTMP stream to HLS via ffmpeg
# Called by the go-rtmp server hook system on publish_start events.
# Environment variables set by the hook system:
#   RTMP_EVENT_TYPE   — "publish_start"
#   RTMP_STREAM_KEY   — e.g. "live/test"
#   RTMP_CONN_ID      — connection identifier
#   RTMP_TIMESTAMP    — unix timestamp

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOG_DIR="$SCRIPT_DIR/logs"
HLS_DIR="$SCRIPT_DIR/../hls-output"
RTMP_HOST="${RTMP_HOST:-localhost}"
RTMP_PORT="${RTMP_PORT:-1935}"

# Validate required environment
if [[ -z "${RTMP_STREAM_KEY:-}" ]]; then
    echo "ERROR: RTMP_STREAM_KEY not set" >&2
    exit 1
fi

# Derive output directory from stream key (e.g. "live/test" → "live_test")
SAFE_KEY="${RTMP_STREAM_KEY//\//_}"
OUTPUT_DIR="$HLS_DIR/$SAFE_KEY"

mkdir -p "$OUTPUT_DIR" "$LOG_DIR"

RTMP_URL="rtmp://${RTMP_HOST}:${RTMP_PORT}/${RTMP_STREAM_KEY}"
LOG_FILE="$LOG_DIR/hls-${SAFE_KEY}.log"

echo "$(date '+%Y-%m-%d %H:%M:%S') Starting HLS conversion for ${RTMP_STREAM_KEY}" >> "$LOG_FILE"
echo "  Source: $RTMP_URL" >> "$LOG_FILE"
echo "  Output: $OUTPUT_DIR/playlist.m3u8" >> "$LOG_FILE"

# Start ffmpeg in the background to convert RTMP → HLS
# -c copy: no transcoding (fast, preserves quality)
# -hls_time 4: 4-second segments
# -hls_list_size 5: keep 5 segments in playlist
# -hls_flags delete_segments: remove old segments
nohup ffmpeg -hide_banner -loglevel warning \
    -i "$RTMP_URL" \
    -c copy \
    -f hls \
    -hls_time 4 \
    -hls_list_size 5 \
    -hls_flags delete_segments \
    -hls_segment_filename "$OUTPUT_DIR/segment_%03d.ts" \
    "$OUTPUT_DIR/playlist.m3u8" \
    >> "$LOG_FILE" 2>&1 &

FFMPEG_PID=$!
echo "  ffmpeg PID: $FFMPEG_PID" >> "$LOG_FILE"
echo "$FFMPEG_PID" > "$OUTPUT_DIR/.ffmpeg.pid"

echo "HLS conversion started for ${RTMP_STREAM_KEY} (PID: $FFMPEG_PID)"

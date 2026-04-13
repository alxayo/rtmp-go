#!/usr/bin/env bash
# on-publish-abr.sh — Shell hook: starts parallel FFmpeg instances for ABR HLS
# Called by the go-rtmp server hook system on publish_start events.
#
# Spawns 3 independent FFmpeg transcoders (1080p, 720p, 480p) with aligned
# keyframe parameters for seamless adaptive bitrate switching, plus writes a
# master.m3u8 playlist.
#
# Environment variables set by the hook system:
#   RTMP_EVENT_TYPE   — "publish_start"
#   RTMP_STREAM_KEY   — e.g. "live/test"
#   RTMP_CONN_ID      — connection identifier
#   RTMP_TIMESTAMP    — unix timestamp
#
# Usage:
#   ./rtmp-server -hook-script "publish_start=./scripts/on-publish-abr.sh"

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
RTMP_URL="rtmp://${RTMP_HOST}:${RTMP_PORT}/${RTMP_STREAM_KEY}"
LOG_FILE="$LOG_DIR/abr-${SAFE_KEY}.log"

# Alignment parameters — must be identical across all instances.
# Uses time-based keyframe forcing so alignment works at any input fps.
SEG_TIME=2          # HLS segment duration in seconds
LIST_SIZE=10        # Number of segments in playlist
FPS=30              # Output frame rate (normalized across all renditions)

# Rendition presets: name:resolution:video_bitrate:maxrate:bufsize:audio_bitrate
PRESETS=(
    "1080p:1920x1080:5000k:5500k:10000k:192k"
    "720p:1280x720:2500k:2750k:5000k:128k"
    "480p:854x480:1000k:1100k:2000k:96k"
)

mkdir -p "$LOG_DIR"

echo "$(date '+%Y-%m-%d %H:%M:%S') Starting ABR HLS for ${RTMP_STREAM_KEY}" >> "$LOG_FILE"
echo "  Source: $RTMP_URL" >> "$LOG_FILE"
echo "  Output: $OUTPUT_DIR/" >> "$LOG_FILE"

# Clean stale segments from previous session to prevent playlist corruption
if [[ -d "$OUTPUT_DIR" ]]; then
    find "$OUTPUT_DIR" -name '*.ts' -delete 2>/dev/null || true
    find "$OUTPUT_DIR" -name '*.m3u8' -not -name 'master.m3u8' -delete 2>/dev/null || true
    echo "  Cleaned stale segments from previous session" >> "$LOG_FILE"
fi

PIDS=()
for preset in "${PRESETS[@]}"; do
    IFS=: read -r name res vbitrate maxrate bufsize abitrate <<< "$preset"

    mkdir -p "$OUTPUT_DIR/$name"
    RENDITION_LOG="$LOG_DIR/abr-${SAFE_KEY}-${name}.log"

    echo "  Starting $name ($res @ ${vbitrate})..." >> "$LOG_FILE"

    nohup ffmpeg -hide_banner -loglevel warning \
        -i "$RTMP_URL" \
        -c:v libx264 -s "$res" -b:v "$vbitrate" \
        -maxrate "$maxrate" -bufsize "$bufsize" \
        -preset veryfast -r $FPS \
        -force_key_frames "expr:gte(t,n_forced*${SEG_TIME})" \
        -sc_threshold 0 \
        -c:a aac -b:a "$abitrate" -ar 48000 \
        -f hls \
        -hls_time $SEG_TIME \
        -hls_list_size $LIST_SIZE \
        -hls_flags delete_segments+temp_file \
        -hls_segment_filename "$OUTPUT_DIR/$name/seg_%05d.ts" \
        "$OUTPUT_DIR/$name/index.m3u8" \
        >> "$RENDITION_LOG" 2>&1 &

    PID=$!
    PIDS+=("$PID")
    echo "$PID" > "$OUTPUT_DIR/$name/.ffmpeg.pid"
    echo "  $name PID: $PID" >> "$LOG_FILE"
done

# Write master playlist
cat > "$OUTPUT_DIR/master.m3u8" << 'EOF'
#EXTM3U
#EXT-X-VERSION:3
#EXT-X-INDEPENDENT-SEGMENTS

#EXT-X-STREAM-INF:BANDWIDTH=5700000,RESOLUTION=1920x1080
1080p/index.m3u8

#EXT-X-STREAM-INF:BANDWIDTH=2900000,RESOLUTION=1280x720
720p/index.m3u8

#EXT-X-STREAM-INF:BANDWIDTH=1200000,RESOLUTION=854x480
480p/index.m3u8
EOF

echo "  Master playlist: $OUTPUT_DIR/master.m3u8" >> "$LOG_FILE"

# Save all PIDs for cleanup
printf '%s\n' "${PIDS[@]}" > "$OUTPUT_DIR/.abr-pids"

echo "ABR HLS started for ${RTMP_STREAM_KEY} — 3 renditions (PIDs: ${PIDS[*]})"

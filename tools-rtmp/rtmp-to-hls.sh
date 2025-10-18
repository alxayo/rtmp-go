#!/bin/bash

# RTMP to HLS Conversion Script
# Usage: ./rtmp-to-hls.sh [basic|quality|adaptive] [stream_name]

RTMP_URL="rtmp://192.168.0.7:1935/live"
STREAM_NAME="${2:-test}"
OUTPUT_DIR="./hls-output"
MODE="${1:-basic}"

# Create output directory
mkdir -p "$OUTPUT_DIR"

echo "🎬 Converting RTMP stream to HLS..."
echo "📡 Source: $RTMP_URL/$STREAM_NAME"
echo "📁 Output: $OUTPUT_DIR"
echo "🎯 Mode: $MODE"
echo ""

case "$MODE" in
  "basic")
    echo "🚀 Starting basic HLS conversion (copy codecs)..."
    ffmpeg -i "$RTMP_URL/$STREAM_NAME" \
      -c copy \
      -f hls \
      -hls_time 10 \
      -hls_list_size 6 \
      -hls_flags delete_segments \
      -hls_segment_filename "$OUTPUT_DIR/segment_%03d.ts" \
      "$OUTPUT_DIR/playlist.m3u8"
    ;;
    
  "quality")
    echo "🎨 Starting quality HLS conversion (re-encode)..."
    ffmpeg -i "$RTMP_URL/$STREAM_NAME" \
      -c:v libx264 -preset fast -crf 23 \
      -c:a aac -b:a 128k \
      -f hls \
      -hls_time 10 \
      -hls_list_size 6 \
      -hls_flags delete_segments \
      -hls_segment_filename "$OUTPUT_DIR/segment_%03d.ts" \
      "$OUTPUT_DIR/playlist.m3u8"
    ;;
    
  "adaptive")
    echo "📊 Starting adaptive HLS conversion (multi-bitrate)..."
    ffmpeg -i "$RTMP_URL/$STREAM_NAME" \
      -filter_complex \
      "[0:v]split=3[v1][v2][v3]; \
       [v1]scale=w=1920:h=1080[v1out]; \
       [v2]scale=w=1280:h=720[v2out]; \
       [v3]scale=w=854:h=480[v3out]" \
      -map "[v1out]" -c:v:0 libx264 -b:v:0 5000k -preset fast \
      -map "[v2out]" -c:v:1 libx264 -b:v:1 2500k -preset fast \
      -map "[v3out]" -c:v:2 libx264 -b:v:2 1000k -preset fast \
      -map a:0 -c:a:0 aac -b:a:0 128k \
      -map a:0 -c:a:1 aac -b:a:1 128k \
      -map a:0 -c:a:2 aac -b:a:2 96k \
      -f hls \
      -hls_time 10 \
      -hls_list_size 6 \
      -hls_flags delete_segments \
      -master_pl_name master.m3u8 \
      -var_stream_map "v:0,a:0 v:1,a:1 v:2,a:2" \
      -hls_segment_filename "$OUTPUT_DIR/stream_%v/segment_%03d.ts" \
      "$OUTPUT_DIR/stream_%v.m3u8"
    ;;
    
  *)
    echo "❌ Invalid mode. Use: basic, quality, or adaptive"
    echo ""
    echo "Examples:"
    echo "  ./rtmp-to-hls.sh basic test"
    echo "  ./rtmp-to-hls.sh quality mystream"
    echo "  ./rtmp-to-hls.sh adaptive livestream"
    exit 1
    ;;
esac

echo ""
echo "✅ HLS conversion completed!"
echo "🎥 Playlist available at: $OUTPUT_DIR/playlist.m3u8"
if [ "$MODE" = "adaptive" ]; then
  echo "🎯 Master playlist: $OUTPUT_DIR/master.m3u8"
fi
echo ""
echo "🌐 To serve HLS files, use:"
echo "   python3 -m http.server 8080"
echo "   Then open: http://localhost:8080/$OUTPUT_DIR/"
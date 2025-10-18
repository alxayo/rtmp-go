#!/bin/bash

# H.264 Frame Analysis Tool
# Analyzes frame types, sizes, and GOP structure in video streams

RTMP_URL="rtmp://192.168.0.7:1935/live"
STREAM_NAME="${1:-test}"
OUTPUT_DIR="./frame-analysis"

echo "🎬 H.264 Frame Analysis Tool"
echo "📡 Analyzing: $RTMP_URL/$STREAM_NAME"
echo ""

mkdir -p "$OUTPUT_DIR"

# Function to analyze RTMP stream frames
analyze_rtmp_stream() {
    echo "🔍 Analyzing RTMP stream frame types and sizes..."
    
    ffprobe -v quiet -select_streams v:0 -show_frames -show_entries frame=pict_type,pkt_size,pts_time \
        -of csv=p=0 "$RTMP_URL/$STREAM_NAME" 2>/dev/null | head -50 > "$OUTPUT_DIR/frame_data.csv"
    
    if [ -s "$OUTPUT_DIR/frame_data.csv" ]; then
        echo "✅ Frame data captured!"
        echo ""
        analyze_frame_data
    else
        echo "❌ No stream data found. Make sure:"
        echo "   1. RTMP server is running"
        echo "   2. Stream '$STREAM_NAME' is actively publishing"
        echo "   3. Stream contains H.264 video"
    fi
}

# Function to analyze local video file
analyze_video_file() {
    local file="$1"
    echo "🔍 Analyzing video file: $file"
    
    ffprobe -v quiet -select_streams v:0 -show_frames -show_entries frame=pict_type,pkt_size,pts_time \
        -of csv=p=0 "$file" 2>/dev/null | head -100 > "$OUTPUT_DIR/frame_data.csv"
    
    if [ -s "$OUTPUT_DIR/frame_data.csv" ]; then
        echo "✅ Frame data extracted!"
        echo ""
        analyze_frame_data
    else
        echo "❌ Could not analyze file: $file"
    fi
}

# Function to analyze the captured frame data
analyze_frame_data() {
    local data_file="$OUTPUT_DIR/frame_data.csv"
    
    echo "📊 FRAME TYPE ANALYSIS"
    echo "====================="
    
    # Count frame types
    local i_frames=$(grep ",I," "$data_file" | wc -l | tr -d ' ')
    local p_frames=$(grep ",P," "$data_file" | wc -l | tr -d ' ')
    local b_frames=$(grep ",B," "$data_file" | wc -l | tr -d ' ')
    local total_frames=$((i_frames + p_frames + b_frames))
    
    echo "Frame Type Distribution:"
    echo "  I-Frames (Key): $i_frames ($(( i_frames * 100 / total_frames ))%)"
    echo "  P-Frames (Predicted): $p_frames ($(( p_frames * 100 / total_frames ))%)"
    echo "  B-Frames (Bi-predicted): $b_frames ($(( b_frames * 100 / total_frames ))%)"
    echo "  Total Frames: $total_frames"
    echo ""
    
    # Average frame sizes
    echo "📏 FRAME SIZE ANALYSIS"
    echo "======================"
    
    if [ $i_frames -gt 0 ]; then
        local avg_i_size=$(grep ",I," "$data_file" | cut -d',' -f2 | awk '{sum+=$1} END {print int(sum/NR)}')
        echo "  Average I-Frame size: $avg_i_size bytes ($(( avg_i_size / 1024 )) KB)"
    fi
    
    if [ $p_frames -gt 0 ]; then
        local avg_p_size=$(grep ",P," "$data_file" | cut -d',' -f2 | awk '{sum+=$1} END {print int(sum/NR)}')
        echo "  Average P-Frame size: $avg_p_size bytes ($(( avg_p_size / 1024 )) KB)"
    fi
    
    if [ $b_frames -gt 0 ]; then
        local avg_b_size=$(grep ",B," "$data_file" | cut -d',' -f2 | awk '{sum+=$1} END {print int(sum/NR)}')
        echo "  Average B-Frame size: $avg_b_size bytes ($(( avg_b_size / 1024 )) KB)"
    fi
    
    echo ""
    
    # GOP analysis
    echo "🎯 GOP (Group of Pictures) ANALYSIS"
    echo "==================================="
    
    # Find GOP pattern
    local gop_pattern=$(grep ",I," "$data_file" | head -2 | cut -d',' -f3)
    if [ -n "$gop_pattern" ]; then
        local first_i=$(echo "$gop_pattern" | head -1)
        local second_i=$(echo "$gop_pattern" | tail -1)
        if [ "$first_i" != "$second_i" ]; then
            local gop_size=$(echo "$second_i - $first_i" | bc 2>/dev/null || echo "~30")
            echo "  Estimated GOP size: ${gop_size} frames"
        fi
    fi
    
    echo ""
    
    # Show frame sequence (first 20 frames)
    echo "🎞️  FRAME SEQUENCE (First 20 frames)"
    echo "=================================="
    head -20 "$data_file" | while IFS=',' read -r frame_type size timestamp; do
        local size_kb=$((size / 1024))
        printf "  %s-Frame: %3d KB at %.2fs\n" "$frame_type" "$size_kb" "$timestamp"
    done
    
    echo ""
    echo "💡 UNDERSTANDING THE OUTPUT:"
    echo "============================"
    echo "  🔑 I-Frames: Complete pictures (largest, ~10-50KB typical)"
    echo "  📐 P-Frames: Predicted from previous frames (medium, ~2-10KB)"
    echo "  🔄 B-Frames: Bi-predicted from past/future (smallest, ~1-5KB)"
    echo ""
    echo "  📊 Typical GOP patterns:"
    echo "     • IPPPPPPPPP... (low latency, streaming)"
    echo "     • IBBPBBPBBP... (high compression, file storage)"
    echo "     • GOP size 30 = I-frame every 1 second at 30fps"
    echo ""
    
    # Create visualization
    create_frame_visualization
}

# Function to create a simple ASCII visualization
create_frame_visualization() {
    echo "🎨 FRAME PATTERN VISUALIZATION"
    echo "=============================="
    
    # Create visual pattern from first 30 frames
    local pattern=""
    head -30 "$OUTPUT_DIR/frame_data.csv" | while IFS=',' read -r frame_type size timestamp; do
        case "$frame_type" in
            "I") echo -n "🔑" ;;
            "P") echo -n "📐" ;;
            "B") echo -n "🔄" ;;
        esac
    done
    echo ""
    echo ""
    
    # Legend
    echo "Legend: 🔑=I-Frame  📐=P-Frame  🔄=B-Frame"
    echo ""
}

# Main script logic
case "$1" in
    "--help"|"-h")
        echo "Usage: $0 [stream_name] | [video_file] | --help"
        echo ""
        echo "Examples:"
        echo "  $0 test                    # Analyze RTMP stream 'test'"
        echo "  $0 mystream               # Analyze RTMP stream 'mystream'"  
        echo "  $0 /path/to/video.mp4     # Analyze local video file"
        echo ""
        echo "Requirements:"
        echo "  - ffprobe (part of FFmpeg)"
        echo "  - Active RTMP stream (for stream analysis)"
        ;;
    *.mp4|*.mov|*.avi|*.mkv|*.flv)
        if [ -f "$1" ]; then
            analyze_video_file "$1"
        else
            echo "❌ File not found: $1"
        fi
        ;;
    *)
        analyze_rtmp_stream
        ;;
esac
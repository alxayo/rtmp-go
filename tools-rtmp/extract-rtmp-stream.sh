#!/bin/bash

# RTMP Stream Extractor Script
# Extract specific media components from RTMP streams
# Usage: ./extract-rtmp-stream.sh [video|audio|iframes] [stream_name] [options]

RTMP_URL="rtmp://192.168.0.7:1935/live"
STREAM_NAME="${2:-test}"
OUTPUT_DIR="./extracted-media"
MODE="${1}"

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Function to display help
show_help() {
    echo "🎬 RTMP Stream Extractor"
    echo "========================"
    echo ""
    echo "Extract specific media components from RTMP streams using FFmpeg"
    echo ""
    echo "Usage: $0 [MODE] [STREAM_NAME] [OPTIONS]"
    echo ""
    echo "MODES:"
    echo "  video     Extract only video data (H.264)"
    echo "  audio     Extract only audio data (AAC)"
    echo "  iframes   Extract only I-frames (keyframes)"
    echo ""
    echo "STREAM_NAME:"
    echo "  Name of the RTMP stream (default: test)"
    echo ""
    echo "OPTIONS:"
    echo "  --duration SEC    Record for specific duration in seconds"
    echo "  --output FILE     Custom output filename"
    echo "  --format FORMAT   Output format (mp4, mkv, flv, etc.)"
    echo "  --quality PRESET  Quality preset (ultrafast, fast, medium, slow)"
    echo "  --help, -h        Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0 video test"
    echo "  $0 audio mystream --duration 60 --format aac"
    echo "  $0 iframes livestream --output keyframes.mp4"
    echo ""
    echo "Output Directory: $OUTPUT_DIR"
    echo "RTMP Source: $RTMP_URL/[STREAM_NAME]"
}

# Function to parse command line options
parse_options() {
    DURATION=""
    OUTPUT_FILE=""
    FORMAT=""
    QUALITY="medium"
    
    while [[ $# -gt 0 ]]; do
        case $1 in
            --duration)
                DURATION="$2"
                shift 2
                ;;
            --output)
                OUTPUT_FILE="$2"
                shift 2
                ;;
            --format)
                FORMAT="$2"
                shift 2
                ;;
            --quality)
                QUALITY="$2"
                shift 2
                ;;
            --help|-h)
                show_help
                exit 0
                ;;
            *)
                shift
                ;;
        esac
    done
}

# Function to check if RTMP stream is available
check_stream() {
    echo -e "${BLUE}🔍 Checking RTMP stream availability...${NC}"
    
    if ! ffprobe -v quiet -select_streams v:0 -show_entries stream=codec_name \
         -of csv=p=0 "$RTMP_URL/$STREAM_NAME" 2>/dev/null; then
        echo -e "${RED}❌ Error: RTMP stream '$STREAM_NAME' not available${NC}"
        echo -e "${YELLOW}💡 Make sure:${NC}"
        echo "   1. RTMP server is running on port 1935"
        echo "   2. Stream '$STREAM_NAME' is actively publishing"
        echo "   3. Stream contains video data"
        exit 1
    fi
    
    echo -e "${GREEN}✅ RTMP stream found and accessible${NC}"
}

# Function to get stream information
get_stream_info() {
    echo -e "${BLUE}📊 Analyzing stream information...${NC}"
    
    # Get video codec and resolution
    VIDEO_CODEC=$(ffprobe -v quiet -select_streams v:0 -show_entries stream=codec_name \
                  -of csv=p=0 "$RTMP_URL/$STREAM_NAME" 2>/dev/null)
    VIDEO_RES=$(ffprobe -v quiet -select_streams v:0 -show_entries stream=width,height \
                -of csv=p=0 "$RTMP_URL/$STREAM_NAME" 2>/dev/null | tr ',' 'x')
    
    # Get audio codec and sample rate
    AUDIO_CODEC=$(ffprobe -v quiet -select_streams a:0 -show_entries stream=codec_name \
                  -of csv=p=0 "$RTMP_URL/$STREAM_NAME" 2>/dev/null)
    AUDIO_RATE=$(ffprobe -v quiet -select_streams a:0 -show_entries stream=sample_rate \
                 -of csv=p=0 "$RTMP_URL/$STREAM_NAME" 2>/dev/null)
    
    echo "   Video: $VIDEO_CODEC ($VIDEO_RES)"
    echo "   Audio: $AUDIO_CODEC (${AUDIO_RATE}Hz)"
    echo ""
}

# Function to extract video only
extract_video() {
    local timestamp=$(date +%Y%m%d_%H%M%S)
    local default_output="$OUTPUT_DIR/video_${STREAM_NAME}_${timestamp}.mp4"
    local output_file="${OUTPUT_FILE:-$default_output}"
    
    # Set format based on file extension or --format option
    if [[ -n "$FORMAT" ]]; then
        output_file="${output_file%.*}.${FORMAT}"
    fi
    
    echo -e "${GREEN}🎥 Extracting video stream...${NC}"
    echo "   Input: $RTMP_URL/$STREAM_NAME"
    echo "   Output: $output_file"
    echo "   Mode: Video only (no audio)"
    
    # Build FFmpeg command
    local ffmpeg_cmd="ffmpeg -i \"$RTMP_URL/$STREAM_NAME\""
    
    # Add duration if specified
    if [[ -n "$DURATION" ]]; then
        ffmpeg_cmd="$ffmpeg_cmd -t $DURATION"
        echo "   Duration: ${DURATION} seconds"
    fi
    
    # Video options
    ffmpeg_cmd="$ffmpeg_cmd -map 0:v:0"  # Map only video stream
    ffmpeg_cmd="$ffmpeg_cmd -c:v copy"   # Copy video codec (no re-encoding)
    ffmpeg_cmd="$ffmpeg_cmd -an"         # No audio
    ffmpeg_cmd="$ffmpeg_cmd -y"          # Overwrite output file
    ffmpeg_cmd="$ffmpeg_cmd \"$output_file\""
    
    echo -e "${YELLOW}🚀 Starting video extraction...${NC}"
    echo ""
    
    eval $ffmpeg_cmd
    
    if [[ $? -eq 0 ]]; then
        echo ""
        echo -e "${GREEN}✅ Video extraction completed!${NC}"
        echo "   Output file: $output_file"
        
        # Show file info
        local file_size=$(du -h "$output_file" | cut -f1)
        echo "   File size: $file_size"
    else
        echo -e "${RED}❌ Video extraction failed${NC}"
        exit 1
    fi
}

# Function to extract audio only
extract_audio() {
    local timestamp=$(date +%Y%m%d_%H%M%S)
    local default_format="${FORMAT:-aac}"
    local default_output="$OUTPUT_DIR/audio_${STREAM_NAME}_${timestamp}.${default_format}"
    local output_file="${OUTPUT_FILE:-$default_output}"
    
    echo -e "${GREEN}🎵 Extracting audio stream...${NC}"
    echo "   Input: $RTMP_URL/$STREAM_NAME"
    echo "   Output: $output_file"
    echo "   Mode: Audio only (no video)"
    
    # Build FFmpeg command
    local ffmpeg_cmd="ffmpeg -i \"$RTMP_URL/$STREAM_NAME\""
    
    # Add duration if specified
    if [[ -n "$DURATION" ]]; then
        ffmpeg_cmd="$ffmpeg_cmd -t $DURATION"
        echo "   Duration: ${DURATION} seconds"
    fi
    
    # Audio options
    ffmpeg_cmd="$ffmpeg_cmd -map 0:a:0"  # Map only audio stream
    ffmpeg_cmd="$ffmpeg_cmd -c:a copy"   # Copy audio codec (no re-encoding)
    ffmpeg_cmd="$ffmpeg_cmd -vn"         # No video
    ffmpeg_cmd="$ffmpeg_cmd -y"          # Overwrite output file
    ffmpeg_cmd="$ffmpeg_cmd \"$output_file\""
    
    echo -e "${YELLOW}🚀 Starting audio extraction...${NC}"
    echo ""
    
    eval $ffmpeg_cmd
    
    if [[ $? -eq 0 ]]; then
        echo ""
        echo -e "${GREEN}✅ Audio extraction completed!${NC}"
        echo "   Output file: $output_file"
        
        # Show file info
        local file_size=$(du -h "$output_file" | cut -f1)
        echo "   File size: $file_size"
    else
        echo -e "${RED}❌ Audio extraction failed${NC}"
        exit 1
    fi
}

# Function to extract I-frames only
extract_iframes() {
    local timestamp=$(date +%Y%m%d_%H%M%S)
    local default_output="$OUTPUT_DIR/iframes_${STREAM_NAME}_${timestamp}.mp4"
    local output_file="${OUTPUT_FILE:-$default_output}"
    
    # Set format based on file extension or --format option
    if [[ -n "$FORMAT" ]]; then
        output_file="${output_file%.*}.${FORMAT}"
    fi
    
    echo -e "${GREEN}🔑 Extracting I-frames (keyframes)...${NC}"
    echo "   Input: $RTMP_URL/$STREAM_NAME"
    echo "   Output: $output_file"
    echo "   Mode: I-frames only (keyframes)"
    
    # Build FFmpeg command for I-frame extraction
    local ffmpeg_cmd="ffmpeg -i \"$RTMP_URL/$STREAM_NAME\""
    
    # Add duration if specified
    if [[ -n "$DURATION" ]]; then
        ffmpeg_cmd="$ffmpeg_cmd -t $DURATION"
        echo "   Duration: ${DURATION} seconds"
    fi
    
    # I-frame extraction options
    ffmpeg_cmd="$ffmpeg_cmd -map 0:v:0"           # Map only video stream
    ffmpeg_cmd="$ffmpeg_cmd -vf \"select=eq(pict_type\\,I)\""  # Select only I-frames
    ffmpeg_cmd="$ffmpeg_cmd -vsync vfr"           # Variable frame rate (since we're dropping frames)
    ffmpeg_cmd="$ffmpeg_cmd -c:v libx264"         # Re-encode with H.264
    ffmpeg_cmd="$ffmpeg_cmd -preset $QUALITY"     # Encoding quality
    ffmpeg_cmd="$ffmpeg_cmd -crf 18"              # High quality constant rate factor
    ffmpeg_cmd="$ffmpeg_cmd -an"                  # No audio
    ffmpeg_cmd="$ffmpeg_cmd -y"                   # Overwrite output file
    ffmpeg_cmd="$ffmpeg_cmd \"$output_file\""
    
    echo -e "${YELLOW}🚀 Starting I-frame extraction...${NC}"
    echo "   ℹ️  This will create a video with only keyframes (choppy playback is normal)"
    echo ""
    
    eval $ffmpeg_cmd
    
    if [[ $? -eq 0 ]]; then
        echo ""
        echo -e "${GREEN}✅ I-frame extraction completed!${NC}"
        echo "   Output file: $output_file"
        
        # Show file info
        local file_size=$(du -h "$output_file" | cut -f1)
        echo "   File size: $file_size"
        
        # Count extracted frames
        local frame_count=$(ffprobe -v quiet -select_streams v:0 -show_entries stream=nb_frames \
                           -of csv=p=0 "$output_file" 2>/dev/null)
        echo "   I-frames extracted: $frame_count"
        
        echo ""
        echo -e "${BLUE}💡 Note: The output video contains only I-frames, so playback will appear jerky.${NC}"
        echo "   This is normal - each frame is a complete keyframe from the original stream."
    else
        echo -e "${RED}❌ I-frame extraction failed${NC}"
        exit 1
    fi
}

# Function to create analysis report
create_analysis_report() {
    local report_file="$OUTPUT_DIR/extraction_report_$(date +%Y%m%d_%H%M%S).txt"
    
    echo "RTMP Stream Extraction Report" > "$report_file"
    echo "============================" >> "$report_file"
    echo "Generated: $(date)" >> "$report_file"
    echo "" >> "$report_file"
    echo "Source Stream: $RTMP_URL/$STREAM_NAME" >> "$report_file"
    echo "Extraction Mode: $MODE" >> "$report_file"
    echo "Video Codec: $VIDEO_CODEC" >> "$report_file"
    echo "Audio Codec: $AUDIO_CODEC" >> "$report_file"
    echo "" >> "$report_file"
    
    # List extracted files
    echo "Extracted Files:" >> "$report_file"
    ls -la "$OUTPUT_DIR"/*_${STREAM_NAME}_* 2>/dev/null | tail -5 >> "$report_file"
    
    echo -e "${BLUE}📄 Analysis report saved: $report_file${NC}"
}

# Main script execution
main() {
    echo "🎬 RTMP Stream Extractor"
    echo "========================"
    echo ""
    
    # Parse additional options
    parse_options "$@"
    
    # Check if mode is provided
    if [[ -z "$MODE" ]]; then
        echo -e "${RED}❌ Error: No extraction mode specified${NC}"
        echo ""
        show_help
        exit 1
    fi
    
    # Check if FFmpeg is available
    if ! command -v ffmpeg &> /dev/null; then
        echo -e "${RED}❌ Error: FFmpeg not found${NC}"
        echo "Please install FFmpeg to use this script"
        exit 1
    fi
    
    # Validate and execute based on mode
    case "$MODE" in
        "video")
            check_stream
            get_stream_info
            extract_video
            ;;
        "audio")
            check_stream
            get_stream_info
            extract_audio
            ;;
        "iframes")
            check_stream
            get_stream_info
            extract_iframes
            ;;
        "--help"|"-h"|"help")
            show_help
            exit 0
            ;;
        *)
            echo -e "${RED}❌ Error: Invalid mode '$MODE'${NC}"
            echo "Valid modes: video, audio, iframes"
            echo ""
            show_help
            exit 1
            ;;
    esac
    
    # Create analysis report
    create_analysis_report
    
    echo ""
    echo -e "${GREEN}🎯 Extraction completed successfully!${NC}"
    echo "Check the '$OUTPUT_DIR' directory for your extracted media files."
}

# Execute main function with all arguments
main "$@"
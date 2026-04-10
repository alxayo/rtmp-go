#!/usr/bin/env bash
# test-enhanced-rtmp.sh — Enhanced RTMP (H.265/HEVC) end-to-end test
#
# PURPOSE:
#   Validates that the go-rtmp server correctly receives, processes, and records
#   an H.265/HEVC stream sent via Enhanced RTMP (E-RTMP v2). The test publishes
#   a synthetic test pattern from FFmpeg using the libx265 encoder, which triggers
#   Enhanced RTMP signaling (IsExHeader + FourCC "hvc1"). The server records the
#   stream to an FLV file, and the test verifies the recording preserves the
#   original codecs and content.
#
# PREREQUISITES:
#   - Go 1.21+ (to build the server)
#   - FFmpeg 6.1+ with libx265 encoder (Enhanced RTMP support)
#   - ffprobe (usually bundled with FFmpeg)
#   - ffplay (optional, for --play mode)
#
# USAGE:
#   ./scripts/test-enhanced-rtmp.sh           # Run automated test
#   ./scripts/test-enhanced-rtmp.sh --play    # Run test + play recorded file
#
# WHAT IT TESTS:
#   1. Server accepts Enhanced RTMP connections with fourCcList negotiation
#   2. H.265 video is received and recorded without re-encoding (passthrough)
#   3. AAC audio is received and recorded correctly
#   4. Recorded FLV file is valid, decodable, and contains expected codecs
#
# VERIFICATION CHECKS (5 steps):
#   ✓ Recorded file exists and is non-empty
#   ✓ Video codec is HEVC (H.265)
#   ✓ Audio codec is AAC
#   ✓ Duration is within ±2 seconds of source (5s)
#   ✓ File is fully decodable (every frame decoded without errors)
#
# EXIT CODES:
#   0 — All checks passed
#   1 — One or more checks failed
#   2 — Missing prerequisites (FFmpeg too old, no libx265, etc.)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LOG_DIR="$SCRIPT_DIR/logs"
BINARY="$PROJECT_ROOT/rtmp-server"

# ===========================
# Configuration
# ===========================

PORT=19370                          # Unique port to avoid conflicts with test-e2e.sh (19351-19367)
STREAM_KEY="live/etest"             # Stream key for this test
SOURCE_DURATION=5                   # Seconds of test content to generate
DURATION_TOLERANCE=2                # Acceptable duration drift (seconds)
RECORD_DIR=""                       # Set during setup (temp directory)

# ===========================
# Colors for terminal output
# ===========================

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# ===========================
# Parse arguments
# ===========================

PLAY_MODE=false
while [[ $# -gt 0 ]]; do
    case "$1" in
        --play)  PLAY_MODE=true; shift ;;
        --help|-h)
            echo "Usage: $0 [--play]"
            echo "  --play   After verification, play the recorded file with ffplay"
            exit 0
            ;;
        *) echo "Unknown option: $1"; exit 1 ;;
    esac
done

# ===========================
# Track PIDs for cleanup
# ===========================

PIDS=()

cleanup() {
    # Terminate all tracked processes on exit (normal, error, or interrupt).
    for pid in "${PIDS[@]+"${PIDS[@]}"}"; do
        if kill -0 "$pid" 2>/dev/null; then
            kill "$pid" 2>/dev/null || true
            sleep 0.5
            kill -9 "$pid" 2>/dev/null || true
        fi
    done
    PIDS=()
    # Remove temp directories created by this test.
    rm -rf "$SCRIPT_DIR/.test-tmp/enhanced-rtmp" 2>/dev/null || true
}

trap cleanup EXIT INT TERM

# ===========================
# Helper functions
# ===========================

log() {
    echo -e "$(date '+%H:%M:%S') $1"
}

pass_check() {
    echo -e "  ${GREEN}✓ $1${NC}"
}

fail_check() {
    echo -e "  ${RED}✗ $1${NC}"
    if [[ -n "${2:-}" ]]; then
        echo -e "    ${RED}$2${NC}"
    fi
}

# wait_for_log polls a log file until a pattern appears or timeout is reached.
# Used to detect when the server is ready to accept connections.
wait_for_log() {
    local file="$1" pattern="$2" timeout="${3:-15}"
    for i in $(seq 1 "$timeout"); do
        if [[ -f "$file" ]] && grep -q "$pattern" "$file" 2>/dev/null; then
            return 0
        fi
        sleep 1
    done
    return 1
}

# ===========================
# Prerequisite checks
# ===========================

echo -e "${BLUE}=== Enhanced RTMP (H.265) End-to-End Test ===${NC}"
echo ""

# Check that FFmpeg is installed and supports libx265.
if ! command -v ffmpeg &>/dev/null; then
    echo -e "${RED}ERROR: ffmpeg not found. Install FFmpeg 6.1+ with libx265 support.${NC}"
    exit 2
fi

if ! command -v ffprobe &>/dev/null; then
    echo -e "${RED}ERROR: ffprobe not found. Install FFmpeg (includes ffprobe).${NC}"
    exit 2
fi

# Verify FFmpeg has libx265 encoder (required for H.265 test content generation).
if ! ffmpeg -hide_banner -encoders 2>/dev/null | grep -q libx265; then
    echo -e "${RED}ERROR: FFmpeg was built without libx265 encoder.${NC}"
    echo -e "${RED}       Install FFmpeg with H.265 support (e.g., brew install ffmpeg).${NC}"
    exit 2
fi

# Verify FFmpeg version is 6.1+ (Enhanced RTMP support).
# FFmpeg version string format: "ffmpeg version N.N..." or "ffmpeg version nN.N-..."
FFMPEG_VERSION=$(ffmpeg -version 2>/dev/null | head -1 | sed -n 's/.*version \([0-9]*\)\..*/\1/p')
if [[ -n "$FFMPEG_VERSION" ]] && [[ "$FFMPEG_VERSION" -lt 6 ]]; then
    echo -e "${YELLOW}WARNING: FFmpeg version $FFMPEG_VERSION detected. Enhanced RTMP requires 6.1+.${NC}"
    echo -e "${YELLOW}         Test may fail if Enhanced RTMP is not supported.${NC}"
fi

log "Prerequisites OK: ffmpeg with libx265, ffprobe available"

# ===========================
# Build server
# ===========================

# Use .exe extension on Windows (MSYS/MinGW).
if [[ "$(uname -s)" == *MINGW* ]] || [[ "$(uname -s)" == *MSYS* ]]; then
    BINARY="$PROJECT_ROOT/rtmp-server.exe"
fi

log "Building server..."
cd "$PROJECT_ROOT"
go build -o "$BINARY" ./cmd/rtmp-server
log "Built: $BINARY"

# ===========================
# Prepare directories
# ===========================

mkdir -p "$LOG_DIR"

# Create isolated temp and recording directories for this test.
TMPDIR="$SCRIPT_DIR/.test-tmp/enhanced-rtmp"
RECORD_DIR="$TMPDIR/recordings"
mkdir -p "$TMPDIR" "$RECORD_DIR"

# ===========================
# Step 1: Start server with recording enabled
# ===========================

echo ""
log "${BLUE}Step 1: Starting server with recording (port $PORT)${NC}"

SERVER_LOG="$LOG_DIR/test-enhanced-rtmp-server.log"

# Start the RTMP server with:
#   -record-all     : Record every published stream to FLV
#   -record-dir     : Write recordings to our temp directory
#   -log-level debug: Capture Enhanced RTMP negotiation details in logs
"$BINARY" \
    -listen ":${PORT}" \
    -record-all \
    -record-dir "$RECORD_DIR" \
    -log-level debug \
    > "$SERVER_LOG" 2>&1 &
SERVER_PID=$!
PIDS+=("$SERVER_PID")

# Wait for the server to be ready (look for startup log message).
if ! wait_for_log "$SERVER_LOG" "server started\|server listening" 10; then
    echo -e "${RED}ERROR: Server failed to start. Log:${NC}"
    [[ -f "$SERVER_LOG" ]] && tail -20 "$SERVER_LOG"
    exit 1
fi

log "Server started (PID $SERVER_PID)"

# ===========================
# Step 2: Publish H.265 stream via Enhanced RTMP
# ===========================

echo ""
log "${BLUE}Step 2: Publishing H.265+AAC test stream via Enhanced RTMP${NC}"

PUBLISH_LOG="$LOG_DIR/test-enhanced-rtmp-publish.log"

# Generate a synthetic test pattern and encode it as H.265 (HEVC) + AAC.
# When FFmpeg 6.1+ writes H.265 to the FLV muxer, it automatically uses
# Enhanced RTMP signaling (IsExHeader=1, FourCC="hvc1") instead of the
# legacy CodecID field.
#
# Source:  testsrc2 (video) + sine (audio) — no input file needed
# Video:  libx265, ultrafast preset (fast encoding for tests)
# Audio:  AAC, 64 kbps
# Output: RTMP to the local server
ffmpeg -hide_banner -loglevel warning \
    -f lavfi -i "testsrc2=duration=${SOURCE_DURATION}:size=640x480:rate=30" \
    -f lavfi -i "sine=frequency=440:duration=${SOURCE_DURATION}" \
    -c:v libx265 -preset ultrafast \
    -c:a aac -b:a 64k \
    -f flv "rtmp://localhost:${PORT}/${STREAM_KEY}" \
    > "$PUBLISH_LOG" 2>&1 || true

# Allow time for the server to flush the recording to disk.
sleep 2

log "Publish complete"

# ===========================
# Step 3: Stop the server
# ===========================

log "Stopping server..."
if kill -0 "$SERVER_PID" 2>/dev/null; then
    kill "$SERVER_PID" 2>/dev/null || true
    sleep 1
    kill -9 "$SERVER_PID" 2>/dev/null || true
fi
# Remove server PID from cleanup array (already stopped).
NEW_PIDS=()
for p in "${PIDS[@]}"; do
    [[ "$p" != "$SERVER_PID" ]] && NEW_PIDS+=("$p")
done
PIDS=("${NEW_PIDS[@]+"${NEW_PIDS[@]}"}")

# ===========================
# Step 4: Find the recorded FLV file
# ===========================

echo ""
log "${BLUE}Step 3: Verifying recorded file${NC}"

# The recorder saves files as: {streamkey_with_slashes_replaced}_{timestamp}.flv
# For stream key "live/etest", the file is "live_etest_YYYYMMDD_HHMMSS.flv"
RECORDED_FILE=""
for f in "$RECORD_DIR"/live_etest_*.flv; do
    if [[ -f "$f" ]]; then
        RECORDED_FILE="$f"
        break
    fi
done

# ===========================
# Step 5: Verification checks
# ===========================

CHECKS_PASSED=0
CHECKS_FAILED=0

# --- Check 1: File exists and is non-empty ---
if [[ -z "$RECORDED_FILE" ]] || [[ ! -s "$RECORDED_FILE" ]]; then
    fail_check "File exists and is non-empty" "No recording found in $RECORD_DIR"
    CHECKS_FAILED=$((CHECKS_FAILED + 1))

    # Show server log tail to diagnose why recording failed.
    echo ""
    echo -e "${YELLOW}Server log (last 30 lines):${NC}"
    tail -30 "$SERVER_LOG" 2>/dev/null || true
    echo ""
    echo -e "${YELLOW}Publish log:${NC}"
    cat "$PUBLISH_LOG" 2>/dev/null || true
    echo ""
    echo -e "${RED}RESULT: FAIL — No recorded file produced${NC}"
    exit 1
fi

FILE_SIZE=$(stat -f%z "$RECORDED_FILE" 2>/dev/null || stat --format=%s "$RECORDED_FILE" 2>/dev/null || echo "?")
pass_check "File exists and is non-empty ($(basename "$RECORDED_FILE"), ${FILE_SIZE} bytes)"
CHECKS_PASSED=$((CHECKS_PASSED + 1))

# --- Check 2: Video codec is HEVC ---
# Use ffprobe to extract the video codec name from the recorded FLV.
VIDEO_CODEC=$(ffprobe -v error -select_streams v:0 \
    -show_entries stream=codec_name -of csv=p=0 \
    "$RECORDED_FILE" 2>/dev/null || echo "")

if [[ "$VIDEO_CODEC" == "hevc" ]]; then
    pass_check "Video codec is HEVC (got: $VIDEO_CODEC)"
    CHECKS_PASSED=$((CHECKS_PASSED + 1))
else
    fail_check "Video codec is HEVC" "got: '${VIDEO_CODEC:-<none>}'"
    CHECKS_FAILED=$((CHECKS_FAILED + 1))
fi

# --- Check 3: Audio codec is AAC ---
AUDIO_CODEC=$(ffprobe -v error -select_streams a:0 \
    -show_entries stream=codec_name -of csv=p=0 \
    "$RECORDED_FILE" 2>/dev/null || echo "")

if [[ "$AUDIO_CODEC" == "aac" ]]; then
    pass_check "Audio codec is AAC (got: $AUDIO_CODEC)"
    CHECKS_PASSED=$((CHECKS_PASSED + 1))
else
    fail_check "Audio codec is AAC" "got: '${AUDIO_CODEC:-<none>}'"
    CHECKS_FAILED=$((CHECKS_FAILED + 1))
fi

# --- Check 4: Duration within tolerance ---
# The source duration is 5 seconds; allow ±2s for streaming/recording overhead.
RECORDED_DURATION=$(ffprobe -v error -show_entries format=duration \
    -of csv=p=0 "$RECORDED_FILE" 2>/dev/null || echo "0")

# bc may not be available everywhere; use awk for floating point comparison.
DURATION_OK=$(awk "BEGIN {
    d = $RECORDED_DURATION + 0;
    lo = $SOURCE_DURATION - $DURATION_TOLERANCE;
    hi = $SOURCE_DURATION + $DURATION_TOLERANCE;
    print (d >= lo && d <= hi) ? 1 : 0
}" 2>/dev/null || echo "0")

if [[ "$DURATION_OK" == "1" ]]; then
    pass_check "Duration within tolerance (${RECORDED_DURATION}s, expected ~${SOURCE_DURATION}s ±${DURATION_TOLERANCE}s)"
    CHECKS_PASSED=$((CHECKS_PASSED + 1))
else
    fail_check "Duration within tolerance" "got ${RECORDED_DURATION}s, expected ${SOURCE_DURATION}s ±${DURATION_TOLERANCE}s"
    CHECKS_FAILED=$((CHECKS_FAILED + 1))
fi

# --- Check 5: Full decode test ---
# Decode the entire recorded file to /dev/null. If any frame is corrupted or
# the container is malformed, ffmpeg will exit with a non-zero status.
DECODE_LOG="$LOG_DIR/test-enhanced-rtmp-decode.log"
if ffmpeg -hide_banner -v error -i "$RECORDED_FILE" -f null - > "$DECODE_LOG" 2>&1; then
    pass_check "Full decode test passed (all frames decodable)"
    CHECKS_PASSED=$((CHECKS_PASSED + 1))
else
    fail_check "Full decode test" "ffmpeg decode errors (see $DECODE_LOG)"
    CHECKS_FAILED=$((CHECKS_FAILED + 1))
fi

# ===========================
# Results summary
# ===========================

echo ""
echo -e "${BLUE}=== Results ===${NC}"
echo -e "  Checks passed: ${GREEN}${CHECKS_PASSED}${NC}"
echo -e "  Checks failed: ${RED}${CHECKS_FAILED}${NC}"

# Show Enhanced RTMP negotiation from server log (informational).
if grep -q "Enhanced RTMP client detected\|fourCcList\|Enhanced video packet\|enhanced.*hvc1" "$SERVER_LOG" 2>/dev/null; then
    echo ""
    echo -e "${BLUE}Enhanced RTMP activity in server log:${NC}"
    grep -i "enhanced\|fourcc\|hvc1\|hevc\|H265" "$SERVER_LOG" 2>/dev/null | head -10 | sed 's/^/  /' || true
fi

# ===========================
# Optional: play the recorded file
# ===========================

if [[ "$PLAY_MODE" == true ]] && [[ "$CHECKS_FAILED" -eq 0 ]]; then
    echo ""
    if command -v ffplay &>/dev/null; then
        log "${BLUE}Playing recorded file with ffplay (close window to exit)...${NC}"
        ffplay -autoexit -window_title "Enhanced RTMP H.265 Recording" "$RECORDED_FILE" 2>/dev/null || true
    else
        echo -e "${YELLOW}ffplay not found — skipping playback${NC}"
    fi
fi

# ===========================
# Final exit
# ===========================

echo ""
if [[ "$CHECKS_FAILED" -gt 0 ]]; then
    echo -e "${RED}RESULT: FAIL — $CHECKS_FAILED check(s) failed${NC}"
    echo -e "  Server log: $SERVER_LOG"
    echo -e "  Publish log: $PUBLISH_LOG"
    exit 1
else
    echo -e "${GREEN}RESULT: PASS — Enhanced RTMP H.265 end-to-end test succeeded${NC}"
    exit 0
fi

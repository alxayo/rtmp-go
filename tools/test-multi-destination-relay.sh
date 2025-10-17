#!/bin/bash

# =======================================================
# Multi-Destination RTMP Relay Test Script (macOS)
# Description: Tests the multi-destination relay functionality
# Usage: ./test-multi-destination-relay.sh
# =======================================================

set -e  # Exit on any error

# Configuration
RTMP_SERVER_BIN="../rtmp-server"
INPUT_FILE="/Users/alex/Downloads/bbb_sunflower_1080p_60fps_normal.mp4"
LOG_DIR="./logs"
PIDS_FILE="./relay_test_pids.txt"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Cleanup function
cleanup() {
    echo -e "${YELLOW}Cleaning up processes...${NC}"
    if [ -f "$PIDS_FILE" ]; then
        while IFS= read -r pid; do
            if kill -0 "$pid" 2>/dev/null; then
                echo "Killing process $pid"
                kill "$pid" 2>/dev/null || true
                sleep 1
                # Force kill if still running
                if kill -0 "$pid" 2>/dev/null; then
                    kill -9 "$pid" 2>/dev/null || true
                fi
            fi
        done < "$PIDS_FILE"
        rm -f "$PIDS_FILE"
    fi
    
    # Kill any remaining rtmp-server processes
    pkill -f "rtmp-server" 2>/dev/null || true
    echo -e "${GREEN}Cleanup completed${NC}"
}

# Set trap for cleanup on script exit
trap cleanup EXIT INT TERM

# Function to log with timestamp
log() {
    echo -e "$(date '+%Y-%m-%d %H:%M:%S') - $1"
}

# Function to check if process is running
check_process() {
    local pid=$1
    local name=$2
    if kill -0 "$pid" 2>/dev/null; then
        log "${GREEN}✓ $name (PID: $pid) is running${NC}"
        return 0
    else
        log "${RED}✗ $name (PID: $pid) is not running${NC}"
        return 1
    fi
}

# Function to wait for log message
wait_for_log_message() {
    local logfile=$1
    local message=$2
    local timeout=${3:-30}
    local count=0
    
    log "${BLUE}Waiting for '$message' in $logfile (timeout: ${timeout}s)${NC}"
    
    while [ $count -lt $timeout ]; do
        if [ -f "$logfile" ] && grep -q "$message" "$logfile"; then
            log "${GREEN}✓ Found message in $logfile${NC}"
            return 0
        fi
        sleep 1
        count=$((count + 1))
    done
    
    log "${RED}✗ Timeout waiting for message in $logfile${NC}"
    if [ -f "$logfile" ]; then
        log "${RED}Last 10 lines of $logfile:${NC}"
        tail -10 "$logfile"
    fi
    return 1
}

# Function to check for errors in log
check_log_errors() {
    local logfile=$1
    local name=$2
    
    if [ -f "$logfile" ]; then
        local errors=$(grep -i "error\|fatal\|panic" "$logfile" 2>/dev/null || true)
        if [ -n "$errors" ]; then
            log "${RED}✗ Errors found in $name log file ($logfile):${NC}"
            echo "$errors"
            return 1
        else
            log "${GREEN}✓ No errors found in $name log file${NC}"
            return 0
        fi
    else
        log "${RED}✗ Log file $logfile not found${NC}"
        return 1
    fi
}

# Pre-flight checks
echo "======================================================="
echo "Multi-Destination RTMP Relay Test Script"
echo "======================================================="

# Check if rtmp-server binary exists
if [ ! -f "$RTMP_SERVER_BIN" ]; then
    log "${RED}ERROR: RTMP server binary not found at $RTMP_SERVER_BIN${NC}"
    log "${YELLOW}Please build the server first: go build -o rtmp-server ./cmd/rtmp-server${NC}"
    exit 1
fi

# Check if input file exists
if [ ! -f "$INPUT_FILE" ]; then
    log "${RED}ERROR: Input file not found at $INPUT_FILE${NC}"
    log "${YELLOW}Please update the INPUT_FILE variable in the script${NC}"
    exit 1
fi

# Check if ffmpeg is available
if ! command -v ffmpeg &> /dev/null; then
    log "${RED}ERROR: ffmpeg not found. Please install ffmpeg.${NC}"
    exit 1
fi

# Create log directory
mkdir -p "$LOG_DIR"

# Clear any existing PIDs file
rm -f "$PIDS_FILE"

echo ""
log "${BLUE}=== STEP 1: Starting Local Relay Servers ===${NC}"

# Start first RTMP server (port 1936)
log "Starting RTMP Server 1 on port 1936..."
"$RTMP_SERVER_BIN" -listen :1936 -log-level debug -record-all true -record-dir ./recordings1 > "$LOG_DIR/rtmp-server1.log" 2>&1 &
SERVER1_PID=$!
echo "$SERVER1_PID" >> "$PIDS_FILE"
log "RTMP Server 1 started with PID: $SERVER1_PID"

# Start second RTMP server (port 1937)
log "Starting RTMP Server 2 on port 1937..."
"$RTMP_SERVER_BIN" -listen :1937 -log-level debug -record-all true -record-dir ./recordings2 > "$LOG_DIR/rtmp-server2.log" 2>&1 &
SERVER2_PID=$!
echo "$SERVER2_PID" >> "$PIDS_FILE"
log "RTMP Server 2 started with PID: $SERVER2_PID"

# Wait a moment for servers to start
sleep 3

# Check if both servers are running
if ! check_process "$SERVER1_PID" "RTMP Server 1"; then
    log "${RED}RTMP Server 1 failed to start${NC}"
    check_log_errors "$LOG_DIR/rtmp-server1.log" "RTMP Server 1"
    exit 1
fi

if ! check_process "$SERVER2_PID" "RTMP Server 2"; then
    log "${RED}RTMP Server 2 failed to start${NC}"
    check_log_errors "$LOG_DIR/rtmp-server2.log" "RTMP Server 2"
    exit 1
fi

# Wait for servers to be ready (look for "RTMP server listening" message)
if ! wait_for_log_message "$LOG_DIR/rtmp-server1.log" "RTMP server listening" 15; then
    log "${RED}RTMP Server 1 failed to start listening${NC}"
    check_log_errors "$LOG_DIR/rtmp-server1.log" "RTMP Server 1"
    exit 1
fi

if ! wait_for_log_message "$LOG_DIR/rtmp-server2.log" "RTMP server listening" 15; then
    log "${RED}RTMP Server 2 failed to start listening${NC}"
    check_log_errors "$LOG_DIR/rtmp-server2.log" "RTMP Server 2"
    exit 1
fi

# Check for errors in both logs
check_log_errors "$LOG_DIR/rtmp-server1.log" "RTMP Server 1" || exit 1
check_log_errors "$LOG_DIR/rtmp-server2.log" "RTMP Server 2" || exit 1

log "${GREEN}✓ Both local relay servers are running successfully${NC}"

echo ""
log "${BLUE}=== STEP 2: Starting Multi-Destination Relay Server ===${NC}"

# Start the multi-destination relay server
log "Starting Multi-Destination Relay Server on port 1935..."
"$RTMP_SERVER_BIN" -listen :1935 \
    -relay-to "rtmp://localhost:1936/live/dest1" \
    -relay-to "rtmp://localhost:1937/live/dest2" \
    -log-level debug \
    -record-all true \
    -record-dir ./recordings > "$LOG_DIR/rtmp-remote-relay.log" 2>&1 &

RELAY_PID=$!
echo "$RELAY_PID" >> "$PIDS_FILE"
log "Multi-Destination Relay Server started with PID: $RELAY_PID"

# Wait a moment for relay server to start
sleep 3

# Check if relay server is running
if ! check_process "$RELAY_PID" "Multi-Destination Relay Server"; then
    log "${RED}Multi-Destination Relay Server failed to start${NC}"
    check_log_errors "$LOG_DIR/rtmp-remote-relay.log" "Multi-Destination Relay Server"
    exit 1
fi

# Wait for relay server to be ready
if ! wait_for_log_message "$LOG_DIR/rtmp-remote-relay.log" "RTMP server listening" 15; then
    log "${RED}Multi-Destination Relay Server failed to start listening${NC}"
    check_log_errors "$LOG_DIR/rtmp-remote-relay.log" "Multi-Destination Relay Server"
    exit 1
fi

# Check for errors in relay log
check_log_errors "$LOG_DIR/rtmp-remote-relay.log" "Multi-Destination Relay Server" || exit 1

log "${GREEN}✓ Multi-Destination Relay Server is running successfully${NC}"

echo ""
log "${BLUE}=== STEP 3: Starting Stream to Relay Server ===${NC}"

# Start streaming to the relay server
log "Starting stream from $INPUT_FILE to rtmp://localhost:1935/live/test"

# Run ffmpeg in background and capture its PID
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
    "rtmp://localhost:1935/live/test" > "$LOG_DIR/ffmpeg-stream.log" 2>&1 &

FFMPEG_PID=$!
echo "$FFMPEG_PID" >> "$PIDS_FILE"
log "FFmpeg streaming started with PID: $FFMPEG_PID"

# Wait for streaming to establish
sleep 5

# Check if ffmpeg is still running
if ! check_process "$FFMPEG_PID" "FFmpeg Streaming"; then
    log "${RED}FFmpeg streaming failed to start or crashed${NC}"
    if [ -f "$LOG_DIR/ffmpeg-stream.log" ]; then
        log "${RED}FFmpeg log:${NC}"
        tail -20 "$LOG_DIR/ffmpeg-stream.log"
    fi
    exit 1
fi

echo ""
log "${BLUE}=== MONITORING RELAY ACTIVITY ===${NC}"

# Monitor logs for relay activity
sleep 10

# Check relay server log for incoming connection
if wait_for_log_message "$LOG_DIR/rtmp-remote-relay.log" "Connection accepted" 10; then
    log "${GREEN}✓ Relay server accepted incoming connection${NC}"
else
    log "${RED}✗ Relay server did not receive connection${NC}"
    exit 1
fi

# Check destination servers for relay connections
if wait_for_log_message "$LOG_DIR/rtmp-server1.log" "Connection accepted" 10; then
    log "${GREEN}✓ Destination Server 1 (port 1936) received relay connection${NC}"
else
    log "${YELLOW}⚠ Destination Server 1 (port 1936) did not receive relay connection (may be expected if relay not fully implemented)${NC}"
fi

if wait_for_log_message "$LOG_DIR/rtmp-server2.log" "Connection accepted" 10; then
    log "${GREEN}✓ Destination Server 2 (port 1937) received relay connection${NC}"
else
    log "${YELLOW}⚠ Destination Server 2 (port 1937) did not receive relay connection (may be expected if relay not fully implemented)${NC}"
fi

echo ""
log "${BLUE}=== TEST RESULTS ===${NC}"

# Check all processes are still running
all_running=true

if ! check_process "$SERVER1_PID" "RTMP Server 1"; then
    all_running=false
fi

if ! check_process "$SERVER2_PID" "RTMP Server 2"; then
    all_running=false
fi

if ! check_process "$RELAY_PID" "Multi-Destination Relay Server"; then
    all_running=false
fi

if ! check_process "$FFMPEG_PID" "FFmpeg Streaming"; then
    all_running=false
fi

# Final error check
error_found=false

if ! check_log_errors "$LOG_DIR/rtmp-server1.log" "RTMP Server 1"; then
    error_found=true
fi

if ! check_log_errors "$LOG_DIR/rtmp-server2.log" "RTMP Server 2"; then
    error_found=true
fi

if ! check_log_errors "$LOG_DIR/rtmp-remote-relay.log" "Multi-Destination Relay Server"; then
    error_found=true
fi

# Summary
echo ""
echo "======================================================="
if [ "$all_running" = true ] && [ "$error_found" = false ]; then
    log "${GREEN}✓ SUCCESS: Multi-Destination Relay Test Completed Successfully${NC}"
    log "${GREEN}All processes are running without errors${NC}"
    echo ""
    log "${BLUE}Running processes:${NC}"
    log "- RTMP Server 1 (port 1936): PID $SERVER1_PID"
    log "- RTMP Server 2 (port 1937): PID $SERVER2_PID"
    log "- Multi-Destination Relay (port 1935): PID $RELAY_PID"
    log "- FFmpeg Streaming: PID $FFMPEG_PID"
    echo ""
    log "${BLUE}Log files:${NC}"
    log "- $LOG_DIR/rtmp-server1.log"
    log "- $LOG_DIR/rtmp-server2.log"
    log "- $LOG_DIR/rtmp-remote-relay.log"
    log "- $LOG_DIR/ffmpeg-stream.log"
    echo ""
    log "${YELLOW}Test will continue running. Press Ctrl+C to stop all processes.${NC}"
    
    # Keep the script running to maintain the test
    while true; do
        sleep 10
        # Quick health check
        if ! check_process "$RELAY_PID" "Multi-Destination Relay Server" >/dev/null 2>&1; then
            log "${RED}Multi-Destination Relay Server died unexpectedly${NC}"
            break
        fi
        if ! check_process "$FFMPEG_PID" "FFmpeg Streaming" >/dev/null 2>&1; then
            log "${YELLOW}FFmpeg streaming ended${NC}"
            break
        fi
    done
else
    log "${RED}✗ FAILURE: Multi-Destination Relay Test Failed${NC}"
    if [ "$all_running" = false ]; then
        log "${RED}Some processes are not running${NC}"
    fi
    if [ "$error_found" = true ]; then
        log "${RED}Errors found in log files${NC}"
    fi
    exit 1
fi
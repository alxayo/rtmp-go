#!/bin/bash

# =======================================================
# Combined RTMP Test and Traffic Capture Script (macOS)
# Description: Runs RTMP multi-destination test with network traffic capture
# Usage: ./run-test-with-capture.sh [capture_duration_seconds]
# =======================================================

set -e  # Exit on any error

# Configuration
CAPTURE_DURATION=${1:-120}  # Default 2 minutes, or first argument
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CAPTURE_SCRIPT="$SCRIPT_DIR/capture-rtmp-traffic.sh"
TEST_SCRIPT="$SCRIPT_DIR/test-multi-destination-relay.sh"
PIDS_FILE="./combined_test_pids.txt"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to log with timestamp
log() {
    echo -e "$(date '+%Y-%m-%d %H:%M:%S') - $1"
}

# Cleanup function
cleanup() {
    echo -e "${YELLOW}Cleaning up all processes...${NC}"
    
    # Kill processes from our PID file
    if [ -f "$PIDS_FILE" ]; then
        while IFS= read -r pid; do
            if kill -0 "$pid" 2>/dev/null; then
                log "Killing process $pid"
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
    
    # Kill any remaining processes
    pkill -f "rtmp-server" 2>/dev/null || true
    pkill -f "ffmpeg.*rtmp://" 2>/dev/null || true
    pkill -f "tcpdump.*rtmp_traffic" 2>/dev/null || true
    
    echo -e "${GREEN}Cleanup completed${NC}"
}

# Set trap for cleanup on script exit
trap cleanup EXIT INT TERM

# Pre-flight checks
echo "======================================================="
echo "Combined RTMP Test and Traffic Capture Script"
echo "======================================================="

# Check if required scripts exist
if [ ! -f "$CAPTURE_SCRIPT" ]; then
    log "${RED}ERROR: Capture script not found at $CAPTURE_SCRIPT${NC}"
    exit 1
fi

if [ ! -f "$TEST_SCRIPT" ]; then
    log "${RED}ERROR: Test script not found at $TEST_SCRIPT${NC}"
    exit 1
fi

# Check if scripts are executable
if [ ! -x "$CAPTURE_SCRIPT" ]; then
    log "${YELLOW}Making capture script executable...${NC}"
    chmod +x "$CAPTURE_SCRIPT"
fi

if [ ! -x "$TEST_SCRIPT" ]; then
    log "${YELLOW}Making test script executable...${NC}"
    chmod +x "$TEST_SCRIPT"
fi

# Clear any existing PIDs file
rm -f "$PIDS_FILE"

echo ""
log "${BLUE}=== PHASE 1: STARTING TRAFFIC CAPTURE ===${NC}"
log "Capture Duration: ${CAPTURE_DURATION} seconds"

# Start the network capture in background
"$CAPTURE_SCRIPT" "$CAPTURE_DURATION" > ./logs/capture_output.log 2>&1 &
CAPTURE_PID=$!
echo "$CAPTURE_PID" >> "$PIDS_FILE"

log "Traffic capture started with PID: $CAPTURE_PID"

# Wait for capture to initialize
log "Waiting 5 seconds for traffic capture to initialize..."
sleep 5

# Check if capture is running
if ! kill -0 "$CAPTURE_PID" 2>/dev/null; then
    log "${RED}ERROR: Traffic capture failed to start${NC}"
    if [ -f "./logs/capture_output.log" ]; then
        log "${RED}Capture log:${NC}"
        cat "./logs/capture_output.log"
    fi
    exit 1
fi

log "${GREEN}✓ Traffic capture is running${NC}"

echo ""
log "${BLUE}=== PHASE 2: STARTING RTMP TEST ===${NC}"

# Start the RTMP test
log "Starting RTMP multi-destination test..."

# Run the test script but capture its output and PID for monitoring
"$TEST_SCRIPT" > ./logs/test_output.log 2>&1 &
TEST_PID=$!
echo "$TEST_PID" >> "$PIDS_FILE"

log "RTMP test started with PID: $TEST_PID"

echo ""
log "${BLUE}=== MONITORING BOTH PROCESSES ===${NC}"

# Monitor both processes
start_time=$(date +%s)
end_time=$((start_time + CAPTURE_DURATION))

while [ $(date +%s) -lt $end_time ]; do
    current_time=$(date +%s)
    elapsed=$((current_time - start_time))
    remaining=$((CAPTURE_DURATION - elapsed))
    
    # Check capture process
    if kill -0 "$CAPTURE_PID" 2>/dev/null; then
        capture_status="${GREEN}RUNNING${NC}"
    else
        capture_status="${RED}STOPPED${NC}"
        log "${YELLOW}Traffic capture process ended${NC}"
    fi
    
    # Check test process
    if kill -0 "$TEST_PID" 2>/dev/null; then
        test_status="${GREEN}RUNNING${NC}"
    else
        test_status="${RED}STOPPED${NC}"
        log "${YELLOW}RTMP test process ended${NC}"
    fi
    
    log "${BLUE}Status - Elapsed: ${elapsed}s | Remaining: ${remaining}s | Capture: ${capture_status} | Test: ${test_status}${NC}"
    
    # Show recent log entries
    if [ -f "./logs/test_output.log" ]; then
        recent_test_logs=$(tail -3 "./logs/test_output.log" 2>/dev/null | head -1)
        if [ -n "$recent_test_logs" ]; then
            log "${BLUE}Recent test: $recent_test_logs${NC}"
        fi
    fi
    
    # If test failed, we might want to continue capturing for a bit to see cleanup traffic
    if ! kill -0 "$TEST_PID" 2>/dev/null; then
        if [ $remaining -gt 30 ]; then
            log "${YELLOW}Test ended early, continuing capture for remaining ${remaining}s to catch cleanup traffic${NC}"
        fi
    fi
    
    sleep 10
done

echo ""
log "${BLUE}=== CAPTURE DURATION COMPLETED ===${NC}"

# Wait a bit more for processes to clean up
log "Waiting 10 seconds for processes to complete cleanup..."
sleep 10

# Check final status
if kill -0 "$CAPTURE_PID" 2>/dev/null; then
    log "Stopping traffic capture process..."
    kill -TERM "$CAPTURE_PID" 2>/dev/null || true
    sleep 3
    if kill -0 "$CAPTURE_PID" 2>/dev/null; then
        kill -9 "$CAPTURE_PID" 2>/dev/null || true
    fi
fi

if kill -0 "$TEST_PID" 2>/dev/null; then
    log "Stopping RTMP test process..."
    kill -TERM "$TEST_PID" 2>/dev/null || true
    sleep 3
    if kill -0 "$TEST_PID" 2>/dev/null; then
        kill -9 "$TEST_PID" 2>/dev/null || true
    fi
fi

echo ""
log "${BLUE}=== RESULTS SUMMARY ===${NC}"

# Show test results
if [ -f "./logs/test_output.log" ]; then
    test_size=$(ls -lh "./logs/test_output.log" | awk '{print $5}')
    log "Test output log: ./logs/test_output.log ($test_size)"
    
    # Check for success indicators in test log
    if grep -q "SUCCESS.*Multi-Destination Relay Test Completed Successfully" "./logs/test_output.log"; then
        log "${GREEN}✓ RTMP Test: SUCCESS${NC}"
    elif grep -q "FAILURE.*Multi-Destination Relay Test Failed" "./logs/test_output.log"; then
        log "${RED}✗ RTMP Test: FAILURE${NC}"
    else
        log "${YELLOW}? RTMP Test: UNKNOWN (check log file)${NC}"
    fi
    
    echo ""
    log "${BLUE}Last 10 lines of test output:${NC}"
    tail -10 "./logs/test_output.log"
fi

echo ""

# Show capture results
if [ -f "./logs/capture_output.log" ]; then
    capture_size=$(ls -lh "./logs/capture_output.log" | awk '{print $5}')
    log "Capture output log: ./logs/capture_output.log ($capture_size)"
    
    # Look for capture file mentions in the log
    capture_files=$(grep -o "captures/rtmp_traffic_[0-9_]*\.pcap" "./logs/capture_output.log" 2>/dev/null | head -1)
    if [ -n "$capture_files" ]; then
        log "${GREEN}✓ Traffic Capture: Files created${NC}"
        log "PCAP File: $capture_files"
        
        # Check if ASCII conversion was successful
        ascii_file="${capture_files%.pcap}.txt"
        if [ -f "$ascii_file" ]; then
            log "ASCII File: $ascii_file"
        fi
        
        summary_file="${capture_files%.pcap}_summary.txt"
        if [ -f "$summary_file" ]; then
            log "Summary File: $summary_file"
        fi
    else
        log "${YELLOW}? Traffic Capture: Check log for details${NC}"
    fi
    
    echo ""
    log "${BLUE}Last 10 lines of capture output:${NC}"
    tail -10 "./logs/capture_output.log"
fi

echo ""
echo "======================================================="
log "${GREEN}✓ COMBINED TEST COMPLETED${NC}"
echo "======================================================="

echo ""
log "${BLUE}Next Steps:${NC}"
log "1. Review test results in: ./logs/test_output.log"
log "2. Review capture results in: ./logs/capture_output.log"
log "3. Analyze network traffic using the generated PCAP files"
log "4. Check summary files for quick analysis"

echo ""
log "${BLUE}Analysis Commands:${NC}"
if [ -n "$capture_files" ] && [ -f "$capture_files" ]; then
    log "tcpdump -r $capture_files -nn -v"
    log "wireshark $capture_files &"
fi
log "less ./logs/test_output.log"
log "less ./logs/capture_output.log"
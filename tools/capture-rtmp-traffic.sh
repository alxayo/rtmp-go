#!/bin/bash

# =======================================================
# RTMP Network Traffic Capture Script (macOS)
# Description: Captures all network communication between RTMP servers and FFmpeg
# Usage: ./capture-rtmp-traffic.sh [capture_duration_seconds]
# =======================================================

set -e  # Exit on any error

# Configuration
CAPTURE_DURATION=${1:-60}  # Default 60 seconds, or first argument
CAPTURE_DIR="./captures"
LOG_DIR="./logs"
TIMESTAMP=$(date '+%Y%m%d_%H%M%S')
PCAP_FILE="$CAPTURE_DIR/rtmp_traffic_${TIMESTAMP}.pcap"
ASCII_FILE="$CAPTURE_DIR/rtmp_traffic_${TIMESTAMP}.txt"
PIDS_FILE="./capture_pids.txt"

# RTMP Ports to monitor
RTMP_PORTS=(1935 1936 1937)

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
    echo -e "${YELLOW}Stopping network capture...${NC}"
    if [ -f "$PIDS_FILE" ]; then
        while IFS= read -r pid; do
            if kill -0 "$pid" 2>/dev/null; then
                log "Stopping tcpdump process $pid"
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
    
    # Kill any remaining tcpdump processes started by this script
    pkill -f "tcpdump.*rtmp_traffic" 2>/dev/null || true
    echo -e "${GREEN}Cleanup completed${NC}"
}

# Set trap for cleanup on script exit
trap cleanup EXIT INT TERM

# Pre-flight checks
echo "======================================================="
echo "RTMP Network Traffic Capture Script"
echo "======================================================="

# Check if running as root or with necessary privileges
if ! tcpdump --version &>/dev/null; then
    log "${RED}ERROR: tcpdump not found or not accessible${NC}"
    log "${YELLOW}Please install tcpdump: brew install tcpdump${NC}"
    exit 1
fi

# Test tcpdump permissions
if ! timeout 1 tcpdump -i lo0 -c 1 &>/dev/null; then
    log "${RED}ERROR: tcpdump requires root privileges or special capabilities${NC}"
    log "${YELLOW}Please run with sudo or configure tcpdump permissions${NC}"
    log "${YELLOW}Alternative: sudo chown root $(which tcpdump) && sudo chmod +s $(which tcpdump)${NC}"
    exit 1
fi

# Check if tshark is available for ASCII conversion
if ! command -v tshark &> /dev/null; then
    log "${YELLOW}WARNING: tshark not found. Will skip ASCII conversion.${NC}"
    log "${YELLOW}Install with: brew install wireshark${NC}"
    TSHARK_AVAILABLE=false
else
    TSHARK_AVAILABLE=true
fi

# Create directories
mkdir -p "$CAPTURE_DIR"
mkdir -p "$LOG_DIR"

# Clear any existing PIDs file
rm -f "$PIDS_FILE"

echo ""
log "${BLUE}=== NETWORK CAPTURE CONFIGURATION ===${NC}"
log "Capture Duration: ${CAPTURE_DURATION} seconds"
log "PCAP File: $PCAP_FILE"
log "ASCII File: $ASCII_FILE"
log "Monitoring Ports: ${RTMP_PORTS[*]}"
log "Interface: lo0 (localhost)"

echo ""
log "${BLUE}=== STARTING NETWORK CAPTURE ===${NC}"

# Build tcpdump filter for all RTMP ports
# Capture traffic on localhost interface for all RTMP ports
FILTER=""
for port in "${RTMP_PORTS[@]}"; do
    if [ -n "$FILTER" ]; then
        FILTER="$FILTER or "
    fi
    FILTER="${FILTER}port $port"
done

log "Starting tcpdump with filter: ($FILTER)"

# Start tcpdump capture
tcpdump -i lo0 \
    -w "$PCAP_FILE" \
    -s 0 \
    -v \
    "($FILTER)" > "$LOG_DIR/tcpdump_${TIMESTAMP}.log" 2>&1 &

TCPDUMP_PID=$!
echo "$TCPDUMP_PID" >> "$PIDS_FILE"
log "tcpdump started with PID: $TCPDUMP_PID"

# Wait a moment for tcpdump to initialize
sleep 2

# Check if tcpdump is running
if ! kill -0 "$TCPDUMP_PID" 2>/dev/null; then
    log "${RED}ERROR: tcpdump failed to start${NC}"
    if [ -f "$LOG_DIR/tcpdump_${TIMESTAMP}.log" ]; then
        log "${RED}tcpdump log:${NC}"
        cat "$LOG_DIR/tcpdump_${TIMESTAMP}.log"
    fi
    exit 1
fi

log "${GREEN}✓ Network capture started successfully${NC}"

echo ""
log "${BLUE}=== CAPTURE STATUS ===${NC}"
log "Capturing network traffic for ${CAPTURE_DURATION} seconds..."
log "You can now start your RTMP test script in another terminal:"
log "${YELLOW}  ./test-multi-destination-relay.sh${NC}"

# Show real-time packet count every 5 seconds
start_time=$(date +%s)
end_time=$((start_time + CAPTURE_DURATION))

while [ $(date +%s) -lt $end_time ]; do
    current_time=$(date +%s)
    elapsed=$((current_time - start_time))
    remaining=$((CAPTURE_DURATION - elapsed))
    
    # Count packets in the capture file (if it exists and has content)
    if [ -f "$PCAP_FILE" ] && [ -s "$PCAP_FILE" ]; then
        # Use tcpdump to count packets (more reliable than file size)
        packet_count=$(tcpdump -r "$PCAP_FILE" 2>/dev/null | wc -l | tr -d ' ')
        log "${BLUE}Elapsed: ${elapsed}s | Remaining: ${remaining}s | Packets captured: ${packet_count}${NC}"
    else
        log "${BLUE}Elapsed: ${elapsed}s | Remaining: ${remaining}s | Packets captured: 0${NC}"
    fi
    
    # Check if tcpdump is still running
    if ! kill -0 "$TCPDUMP_PID" 2>/dev/null; then
        log "${RED}ERROR: tcpdump process died unexpectedly${NC}"
        exit 1
    fi
    
    sleep 5
done

log "${GREEN}✓ Capture duration completed${NC}"

echo ""
log "${BLUE}=== STOPPING CAPTURE ===${NC}"

# Stop tcpdump gracefully
if kill -0 "$TCPDUMP_PID" 2>/dev/null; then
    log "Sending SIGTERM to tcpdump (PID: $TCPDUMP_PID)"
    kill -TERM "$TCPDUMP_PID"
    
    # Wait for graceful shutdown
    for i in {1..10}; do
        if ! kill -0 "$TCPDUMP_PID" 2>/dev/null; then
            log "${GREEN}✓ tcpdump stopped gracefully${NC}"
            break
        fi
        sleep 1
    done
    
    # Force kill if still running
    if kill -0 "$TCPDUMP_PID" 2>/dev/null; then
        log "${YELLOW}Force killing tcpdump${NC}"
        kill -9 "$TCPDUMP_PID" 2>/dev/null || true
    fi
fi

echo ""
log "${BLUE}=== CAPTURE RESULTS ===${NC}"

# Check if capture file exists and has content
if [ ! -f "$PCAP_FILE" ]; then
    log "${RED}ERROR: Capture file not created${NC}"
    exit 1
fi

if [ ! -s "$PCAP_FILE" ]; then
    log "${YELLOW}WARNING: Capture file is empty (no packets captured)${NC}"
    log "${YELLOW}This might indicate:${NC}"
    log "${YELLOW}  - No RTMP traffic occurred during capture${NC}"
    log "${YELLOW}  - RTMP servers/clients not running${NC}"
    log "${YELLOW}  - Incorrect network interface${NC}"
else
    # Get capture file info
    file_size=$(ls -lh "$PCAP_FILE" | awk '{print $5}')
    packet_count=$(tcpdump -r "$PCAP_FILE" 2>/dev/null | wc -l | tr -d ' ')
    
    log "${GREEN}✓ Capture completed successfully${NC}"
    log "File: $PCAP_FILE"
    log "Size: $file_size"
    log "Packets: $packet_count"
    
    # Display basic packet summary
    echo ""
    log "${BLUE}=== PACKET SUMMARY ===${NC}"
    tcpdump -r "$PCAP_FILE" -nn | head -20
    if [ $packet_count -gt 20 ]; then
        log "${YELLOW}... (showing first 20 packets, ${packet_count} total)${NC}"
    fi
fi

echo ""
log "${BLUE}=== CONVERTING TO ASCII FORMAT ===${NC}"

if [ "$TSHARK_AVAILABLE" = true ] && [ -s "$PCAP_FILE" ]; then
    log "Converting PCAP to human-readable ASCII format..."
    
    # Create detailed ASCII dump with tshark
    tshark -r "$PCAP_FILE" \
        -V \
        -x \
        -T text > "$ASCII_FILE" 2>/dev/null
    
    if [ $? -eq 0 ] && [ -s "$ASCII_FILE" ]; then
        ascii_size=$(ls -lh "$ASCII_FILE" | awk '{print $5}')
        log "${GREEN}✓ ASCII conversion completed${NC}"
        log "File: $ASCII_FILE"
        log "Size: $ascii_size"
        
        # Create a summary file with just the important info
        SUMMARY_FILE="${ASCII_FILE%.txt}_summary.txt"
        log "Creating summary file: $SUMMARY_FILE"
        
        {
            echo "======================================================="
            echo "RTMP Traffic Capture Summary"
            echo "Timestamp: $(date)"
            echo "Duration: ${CAPTURE_DURATION} seconds"
            echo "Total Packets: $packet_count"
            echo "======================================================="
            echo ""
            echo "=== CONNECTION SUMMARY ==="
            tshark -r "$PCAP_FILE" -T fields -e ip.src -e ip.dst -e tcp.srcport -e tcp.dstport -e tcp.flags 2>/dev/null | sort | uniq -c | sort -nr
            echo ""
            echo "=== TCP STREAMS ==="
            tshark -r "$PCAP_FILE" -T fields -e tcp.stream -e ip.src -e tcp.srcport -e ip.dst -e tcp.dstport 2>/dev/null | sort -n | uniq
            echo ""
            echo "=== RTMP HANDSHAKE PACKETS (first 50 bytes of payload) ==="
            tshark -r "$PCAP_FILE" -Y "tcp.len > 0" -T fields -e frame.number -e tcp.srcport -e tcp.dstport -e data.data 2>/dev/null | head -20
            echo ""
            echo "=== DETAILED PACKET ANALYSIS ==="
            echo "(See full ASCII file for complete details: $ASCII_FILE)"
        } > "$SUMMARY_FILE"
        
        log "${GREEN}✓ Summary file created: $SUMMARY_FILE${NC}"
        
        # Show a preview of the summary
        echo ""
        log "${BLUE}=== CAPTURE SUMMARY PREVIEW ===${NC}"
        head -30 "$SUMMARY_FILE"
        
    else
        log "${RED}ERROR: ASCII conversion failed${NC}"
    fi
else
    if [ "$TSHARK_AVAILABLE" = false ]; then
        log "${YELLOW}Skipping ASCII conversion (tshark not available)${NC}"
    else
        log "${YELLOW}Skipping ASCII conversion (no packets captured)${NC}"
    fi
fi

echo ""
echo "======================================================="
log "${GREEN}✓ CAPTURE COMPLETED SUCCESSFULLY${NC}"
echo "======================================================="

echo ""
log "${BLUE}Generated Files:${NC}"
if [ -f "$PCAP_FILE" ]; then
    log "📦 PCAP File: $PCAP_FILE"
fi
if [ -f "$ASCII_FILE" ]; then
    log "📄 ASCII File: $ASCII_FILE"
fi
if [ -f "${ASCII_FILE%.txt}_summary.txt" ]; then
    log "📋 Summary File: ${ASCII_FILE%.txt}_summary.txt"
fi

echo ""
log "${BLUE}Analysis Commands:${NC}"
log "View with tcpdump:    tcpdump -r $PCAP_FILE -nn -v"
log "View with Wireshark:  wireshark $PCAP_FILE"
if [ -f "$ASCII_FILE" ]; then
    log "View ASCII dump:      less $ASCII_FILE"
fi
if [ -f "${ASCII_FILE%.txt}_summary.txt" ]; then
    log "View summary:         less ${ASCII_FILE%.txt}_summary.txt"
fi

echo ""
log "${YELLOW}Note: Run this script before or during your RTMP test to capture traffic${NC}"
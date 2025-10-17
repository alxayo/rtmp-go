#!/bin/bash

# =======================================================
# RTMP Server Process Killer Script
# Description: Checks for running rtmp-server instances and kills them
# Usage: ./kill-rtmp-servers.sh
# =======================================================

set -e  # Exit on any error

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Function to log with timestamp and color
log() {
    local color=$1
    local message=$2
    echo -e "${color}$(date '+%Y-%m-%d %H:%M:%S') - $message${NC}"
}

# Function to log info messages
log_info() {
    log "$BLUE" "INFO: $1"
}

# Function to log success messages
log_success() {
    log "$GREEN" "SUCCESS: $1"
}

# Function to log warning messages
log_warning() {
    log "$YELLOW" "WARNING: $1"
}

# Function to log error messages
log_error() {
    log "$RED" "ERROR: $1"
}

# Function to log action messages
log_action() {
    log "$CYAN" "ACTION: $1"
}

echo "======================================================="
echo "RTMP Server Process Management Script"
echo "======================================================="

log_info "Starting rtmp-server process check..."

# Step 1: Check if any rtmp-server processes are running
log_action "Searching for running rtmp-server processes..."

# Get list of PIDs
PIDS=$(pgrep -f rtmp-server 2>/dev/null || true)
PID_COUNT=$(pgrep -c rtmp-server 2>/dev/null || echo "0")

log_info "Found $PID_COUNT rtmp-server process(es)"

if [ "$PID_COUNT" -eq 0 ]; then
    log_success "No rtmp-server processes are currently running"
    log_info "System is clean - no action needed"
    echo ""
    echo "======================================================="
    echo "SUMMARY: No processes to kill"
    echo "======================================================="
    exit 0
fi

# Step 2: Display detailed information about running processes
log_warning "$PID_COUNT rtmp-server process(es) found running"
log_action "Gathering detailed process information..."

echo ""
echo "--- RUNNING RTMP-SERVER PROCESSES ---"
ps aux | head -1  # Header
ps aux | grep rtmp-server | grep -v grep || true
echo "--- END PROCESS LIST ---"
echo ""

# Step 3: Show PIDs and command lines
log_info "Process details:"
while IFS= read -r pid; do
    if [ -n "$pid" ]; then
        # Get command line for this PID
        CMDLINE=$(ps -p "$pid" -o pid,ppid,cmd --no-headers 2>/dev/null || echo "Process not found")
        log_info "  PID $pid: $CMDLINE"
    fi
done <<< "$PIDS"

# Step 4: Confirm kill action
echo ""
log_warning "About to kill $PID_COUNT rtmp-server process(es)"
log_action "Proceeding with process termination..."

# Step 5: Kill processes with SIGTERM first (graceful)
log_action "Sending SIGTERM (graceful shutdown) to processes..."
TERM_SUCCESS=0
TERM_FAILED=0

while IFS= read -r pid; do
    if [ -n "$pid" ]; then
        log_action "Sending SIGTERM to PID $pid..."
        if kill -TERM "$pid" 2>/dev/null; then
            log_success "SIGTERM sent to PID $pid"
            ((TERM_SUCCESS++))
        else
            log_error "Failed to send SIGTERM to PID $pid (may have already exited)"
            ((TERM_FAILED++))
        fi
    fi
done <<< "$PIDS"

log_info "SIGTERM results: $TERM_SUCCESS successful, $TERM_FAILED failed"

# Step 6: Wait for graceful shutdown
log_action "Waiting 3 seconds for graceful shutdown..."
sleep 3

# Step 7: Check which processes are still running
log_action "Checking for remaining processes..."
REMAINING_PIDS=$(pgrep -f rtmp-server 2>/dev/null || true)
REMAINING_COUNT=$(pgrep -c rtmp-server 2>/dev/null || echo "0")

if [ "$REMAINING_COUNT" -eq 0 ]; then
    log_success "All processes terminated gracefully"
else
    log_warning "$REMAINING_COUNT process(es) still running after SIGTERM"
    log_action "Proceeding with SIGKILL (force kill)..."
    
    # Step 8: Force kill remaining processes
    KILL_SUCCESS=0
    KILL_FAILED=0
    
    while IFS= read -r pid; do
        if [ -n "$pid" ]; then
            log_action "Sending SIGKILL to PID $pid..."
            if kill -KILL "$pid" 2>/dev/null; then
                log_success "SIGKILL sent to PID $pid"
                ((KILL_SUCCESS++))
            else
                log_error "Failed to send SIGKILL to PID $pid"
                ((KILL_FAILED++))
            fi
        fi
    done <<< "$REMAINING_PIDS"
    
    log_info "SIGKILL results: $KILL_SUCCESS successful, $KILL_FAILED failed"
    
    # Step 9: Final check
    sleep 1
    FINAL_COUNT=$(pgrep -c rtmp-server 2>/dev/null || echo "0")
    
    if [ "$FINAL_COUNT" -eq 0 ]; then
        log_success "All processes force-killed successfully"
    else
        log_error "$FINAL_COUNT process(es) still running after SIGKILL"
        log_error "Manual intervention may be required"
    fi
fi

# Step 10: Final verification and summary
echo ""
log_action "Performing final verification..."
FINAL_PIDS=$(pgrep -f rtmp-server 2>/dev/null || true)
FINAL_COUNT=$(pgrep -c rtmp-server 2>/dev/null || echo "0")

echo ""
echo "======================================================="
echo "FINAL SUMMARY"
echo "======================================================="

if [ "$FINAL_COUNT" -eq 0 ]; then
    log_success "SUCCESS: All rtmp-server processes have been terminated"
    log_info "Initial processes: $PID_COUNT"
    log_info "Processes killed: $PID_COUNT"
    log_info "Remaining processes: 0"
    echo ""
    echo "System is now clean ✓"
else
    log_error "FAILURE: $FINAL_COUNT rtmp-server process(es) still running"
    log_info "These processes may require manual intervention:"
    while IFS= read -r pid; do
        if [ -n "$pid" ]; then
            CMDLINE=$(ps -p "$pid" -o cmd --no-headers 2>/dev/null || echo "Unknown")
            log_error "  PID $pid: $CMDLINE"
        fi
    done <<< "$FINAL_PIDS"
    echo ""
    echo "Manual kill commands:"
    while IFS= read -r pid; do
        if [ -n "$pid" ]; then
            echo "  sudo kill -9 $pid"
        fi
    done <<< "$FINAL_PIDS"
    
    exit 1
fi

echo "======================================================="
log_info "Script completed successfully"
echo "======================================================="
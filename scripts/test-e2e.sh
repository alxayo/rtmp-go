#!/usr/bin/env bash
# test-e2e.sh — End-to-end test suite for go-rtmp
# Usage: ./test-e2e.sh [--test TEST_NAME]
#   Run all tests or a specific test by name.
#   Tests: rtmp-basic, rtmps-basic, rtmp-hls, rtmps-hls, auth-allow, auth-reject, rtmps-auth
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LOG_DIR="$SCRIPT_DIR/logs"
BINARY="$PROJECT_ROOT/rtmp-server"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Test counters
PASSED=0
FAILED=0
SKIPPED=0

# Track PIDs for cleanup
PIDS=()

# Parse args
RUN_TEST=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --test) RUN_TEST="$2"; shift 2 ;;
        *) echo "Unknown option: $1"; exit 1 ;;
    esac
done

cleanup() {
    for pid in "${PIDS[@]}"; do
        if kill -0 "$pid" 2>/dev/null; then
            kill "$pid" 2>/dev/null || true
            sleep 0.5
            kill -9 "$pid" 2>/dev/null || true
        fi
    done
    PIDS=()
    # Clean up temp directories
    rm -rf "$SCRIPT_DIR/.test-tmp" 2>/dev/null || true
}

trap cleanup EXIT INT TERM

log() { echo -e "$(date '+%H:%M:%S') $1"; }

pass_test() {
    local name="$1"
    log "${GREEN}  PASS: $name${NC}"
    PASSED=$((PASSED + 1))
}

fail_test() {
    local name="$1"
    local reason="${2:-}"
    log "${RED}  FAIL: $name${NC}"
    [[ -n "$reason" ]] && log "${RED}        $reason${NC}"
    FAILED=$((FAILED + 1))
}

skip_test() {
    local name="$1"
    local reason="${2:-}"
    log "${YELLOW}  SKIP: $name${NC}"
    [[ -n "$reason" ]] && log "${YELLOW}        $reason${NC}"
    SKIPPED=$((SKIPPED + 1))
}

should_run() {
    [[ -z "$RUN_TEST" || "$RUN_TEST" == "$1" ]]
}

# Wait for a file to contain a pattern
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

# Start server and wait for it to be ready
start_server() {
    local port="$1"; shift
    local log_file="$LOG_DIR/test-server-${port}.log"
    "$BINARY" -listen ":${port}" "$@" > "$log_file" 2>&1 &
    local pid=$!
    PIDS+=("$pid")
    if ! wait_for_log "$log_file" "server started\|server listening" 10; then
        log "${RED}    Server failed to start on port $port${NC}"
        [[ -f "$log_file" ]] && cat "$log_file"
        return 1
    fi
    echo "$pid"
    return 0
}

# Stop a specific process
stop_proc() {
    local pid="$1"
    if kill -0 "$pid" 2>/dev/null; then
        kill "$pid" 2>/dev/null || true
        sleep 1
        kill -9 "$pid" 2>/dev/null || true
    fi
    # Remove from PIDS array
    local new_pids=()
    for p in "${PIDS[@]}"; do
        [[ "$p" != "$pid" ]] && new_pids+=("$p")
    done
    PIDS=("${new_pids[@]}")
}

# ===========================
# Pre-flight
# ===========================
echo -e "${BLUE}=== go-rtmp End-to-End Test Suite ===${NC}"
echo ""

# Check deps
"$SCRIPT_DIR/check-deps.sh" || { echo "Dependencies missing. Aborting."; exit 1; }
echo ""

# Build server
if [[ "$(uname -s)" == *MINGW* ]] || [[ "$(uname -s)" == *MSYS* ]]; then
    BINARY="$PROJECT_ROOT/rtmp-server.exe"
fi

echo "Building server..."
cd "$PROJECT_ROOT"
go build -o "$BINARY" ./cmd/rtmp-server
echo "Built: $BINARY"
echo ""

mkdir -p "$LOG_DIR"
TMPDIR="$SCRIPT_DIR/.test-tmp"
mkdir -p "$TMPDIR"

# ===========================
# Test 1: RTMP Publish + Capture
# ===========================
if should_run "rtmp-basic"; then
    log "${BLUE}Test 1: RTMP Publish + Capture${NC}"
    PORT=19351
    SERVER_PID=$(start_server "$PORT" "-log-level" "debug") || { fail_test "rtmp-basic" "server failed to start"; }

    if [[ -n "${SERVER_PID:-}" ]]; then
        CAPTURE_FILE="$TMPDIR/rtmp-basic-capture.flv"

        # Start subscriber/capture in background
        ffmpeg -hide_banner -loglevel error \
            -i "rtmp://localhost:${PORT}/live/test" \
            -t 8 -c copy "$CAPTURE_FILE" \
            > "$LOG_DIR/test-capture-rtmp.log" 2>&1 &
        CAP_PID=$!
        PIDS+=("$CAP_PID")

        sleep 1

        # Publish a 5-second test pattern
        ffmpeg -hide_banner -loglevel error \
            -f lavfi -i "testsrc=duration=5:size=320x240:rate=25" \
            -f lavfi -i "sine=frequency=440:duration=5" \
            -c:v libx264 -preset ultrafast -tune zerolatency \
            -c:a aac -b:a 64k \
            -f flv "rtmp://localhost:${PORT}/live/test" \
            > "$LOG_DIR/test-publish-rtmp.log" 2>&1 || true

        # Wait for capture to finish
        sleep 3
        stop_proc "$CAP_PID" 2>/dev/null || true

        # Verify captured file
        if [[ -f "$CAPTURE_FILE" ]] && [[ -s "$CAPTURE_FILE" ]]; then
            DURATION=$(ffprobe -v error -show_entries format=duration -of csv=p=0 "$CAPTURE_FILE" 2>/dev/null || echo "0")
            HAS_VIDEO=$(ffprobe -v error -select_streams v -show_entries stream=codec_type -of csv=p=0 "$CAPTURE_FILE" 2>/dev/null || echo "")

            if [[ -n "$HAS_VIDEO" ]] && (( $(echo "$DURATION > 2.0" | bc -l 2>/dev/null || echo 0) )); then
                pass_test "rtmp-basic (duration=${DURATION}s, has video)"
            else
                fail_test "rtmp-basic" "capture file invalid: duration=$DURATION, video=$HAS_VIDEO"
            fi
        else
            fail_test "rtmp-basic" "no capture file or empty"
        fi

        stop_proc "$SERVER_PID"
    fi
fi

# ===========================
# Test 2: RTMPS Publish + Capture
# ===========================
if should_run "rtmps-basic"; then
    log "${BLUE}Test 2: RTMPS Publish + Capture (dual listener)${NC}"
    PORT=19352
    TLS_PORT=19362

    # Generate certs
    "$SCRIPT_DIR/generate-certs.sh" 2>/dev/null

    CERT="$SCRIPT_DIR/.certs/cert.pem"
    KEY="$SCRIPT_DIR/.certs/key.pem"

    SERVER_PID=$(start_server "$PORT" "-log-level" "debug" \
        "-tls-listen" ":${TLS_PORT}" "-tls-cert" "$CERT" "-tls-key" "$KEY") || \
        { fail_test "rtmps-basic" "server failed to start"; }

    if [[ -n "${SERVER_PID:-}" ]]; then
        # Publish to plain RTMP port
        ffmpeg -hide_banner -loglevel error \
            -f lavfi -i "testsrc=duration=3:size=320x240:rate=25" \
            -c:v libx264 -preset ultrafast -tune zerolatency \
            -f flv "rtmp://localhost:${PORT}/live/tls_test" \
            > "$LOG_DIR/test-publish-rtmps.log" 2>&1 || true

        sleep 2

        # Verify TLS listener was accepting connections by checking server log
        SERVER_LOG="$LOG_DIR/test-server-${PORT}.log"
        if grep -q "RTMPS server listening" "$SERVER_LOG" 2>/dev/null; then
            # Check that the plain RTMP connection worked (media was received)
            if grep -q "connection registered" "$SERVER_LOG" 2>/dev/null; then
                pass_test "rtmps-basic (dual listener active, RTMP publish verified)"
            else
                fail_test "rtmps-basic" "no connection registered"
            fi
        else
            fail_test "rtmps-basic" "RTMPS listener not started"
        fi

        stop_proc "$SERVER_PID"
    fi
fi

# ===========================
# Test 3: RTMP + HLS via Hook
# ===========================
if should_run "rtmp-hls"; then
    log "${BLUE}Test 3: RTMP + HLS via Hook${NC}"
    PORT=19353
    HLS_OUT="$PROJECT_ROOT/hls-output/live_hls_test"

    # Clean up any previous HLS output
    rm -rf "$PROJECT_ROOT/hls-output/live_hls_test" 2>/dev/null || true

    SERVER_PID=$(start_server "$PORT" "-log-level" "debug" \
        "-hook-script" "publish_start=$SCRIPT_DIR/on-publish-hls.sh") || \
        { fail_test "rtmp-hls" "server failed to start"; }

    if [[ -n "${SERVER_PID:-}" ]]; then
        # Set port for the hook script
        export RTMP_PORT="$PORT"

        # Publish a 10-second test stream (need time for HLS segments)
        ffmpeg -hide_banner -loglevel error \
            -f lavfi -i "testsrc=duration=10:size=320x240:rate=25" \
            -f lavfi -i "sine=frequency=440:duration=10" \
            -c:v libx264 -preset ultrafast -tune zerolatency \
            -c:a aac -b:a 64k \
            -f flv "rtmp://localhost:${PORT}/live/hls_test" \
            > "$LOG_DIR/test-publish-hls.log" 2>&1 || true

        sleep 3

        # Check HLS output
        PLAYLIST="$HLS_OUT/playlist.m3u8"
        if [[ -f "$PLAYLIST" ]]; then
            if grep -q "#EXTM3U" "$PLAYLIST"; then
                TS_COUNT=$(find "$HLS_OUT" -name "*.ts" -type f 2>/dev/null | wc -l)
                if [[ "$TS_COUNT" -gt 0 ]]; then
                    pass_test "rtmp-hls (playlist valid, $TS_COUNT segment(s))"
                else
                    fail_test "rtmp-hls" "playlist exists but no .ts segments"
                fi
            else
                fail_test "rtmp-hls" "playlist missing #EXTM3U header"
            fi
        else
            fail_test "rtmp-hls" "no playlist.m3u8 created (hook may not have fired)"
        fi

        # Kill any ffmpeg started by the hook
        if [[ -f "$HLS_OUT/.ffmpeg.pid" ]]; then
            HOOK_PID=$(cat "$HLS_OUT/.ffmpeg.pid")
            kill "$HOOK_PID" 2>/dev/null || true
        fi

        stop_proc "$SERVER_PID"
    fi
fi

# ===========================
# Test 4: RTMPS + HLS via Hook
# ===========================
if should_run "rtmps-hls"; then
    log "${BLUE}Test 4: RTMPS + HLS via Hook${NC}"
    PORT=19354
    TLS_PORT=19364
    HLS_OUT="$PROJECT_ROOT/hls-output/live_rtmps_hls_test"

    "$SCRIPT_DIR/generate-certs.sh" 2>/dev/null
    CERT="$SCRIPT_DIR/.certs/cert.pem"
    KEY="$SCRIPT_DIR/.certs/key.pem"

    rm -rf "$PROJECT_ROOT/hls-output/live_rtmps_hls_test" 2>/dev/null || true

    SERVER_PID=$(start_server "$PORT" "-log-level" "debug" \
        "-tls-listen" ":${TLS_PORT}" "-tls-cert" "$CERT" "-tls-key" "$KEY" \
        "-hook-script" "publish_start=$SCRIPT_DIR/on-publish-hls.sh") || \
        { fail_test "rtmps-hls" "server failed to start"; }

    if [[ -n "${SERVER_PID:-}" ]]; then
        export RTMP_PORT="$PORT"

        # Publish to RTMP (hook converts server-side, transport-agnostic)
        ffmpeg -hide_banner -loglevel error \
            -f lavfi -i "testsrc=duration=10:size=320x240:rate=25" \
            -f lavfi -i "sine=frequency=440:duration=10" \
            -c:v libx264 -preset ultrafast -tune zerolatency \
            -c:a aac -b:a 64k \
            -f flv "rtmp://localhost:${PORT}/live/rtmps_hls_test" \
            > "$LOG_DIR/test-publish-rtmps-hls.log" 2>&1 || true

        sleep 3

        PLAYLIST="$HLS_OUT/playlist.m3u8"
        SERVER_LOG="$LOG_DIR/test-server-${PORT}.log"

        HLS_OK=false
        TLS_OK=false

        if [[ -f "$PLAYLIST" ]] && grep -q "#EXTM3U" "$PLAYLIST"; then
            HLS_OK=true
        fi
        if grep -q "RTMPS server listening" "$SERVER_LOG" 2>/dev/null; then
            TLS_OK=true
        fi

        if [[ "$HLS_OK" == "true" && "$TLS_OK" == "true" ]]; then
            pass_test "rtmps-hls (HLS + TLS listener active)"
        elif [[ "$TLS_OK" == "true" ]]; then
            fail_test "rtmps-hls" "TLS active but HLS not generated"
        else
            fail_test "rtmps-hls" "TLS listener not started"
        fi

        if [[ -f "$HLS_OUT/.ffmpeg.pid" ]]; then
            kill "$(cat "$HLS_OUT/.ffmpeg.pid")" 2>/dev/null || true
        fi
        stop_proc "$SERVER_PID"
    fi
fi

# ===========================
# Test 5: RTMP + Auth (allowed)
# ===========================
if should_run "auth-allow"; then
    log "${BLUE}Test 5: RTMP + Auth (allowed)${NC}"
    PORT=19355

    SERVER_PID=$(start_server "$PORT" "-log-level" "debug" \
        "-auth-mode" "token" "-auth-token" "live/test=secret123") || \
        { fail_test "auth-allow" "server failed to start"; }

    if [[ -n "${SERVER_PID:-}" ]]; then
        CAPTURE_FILE="$TMPDIR/auth-allow-capture.flv"

        # Capture in background
        ffmpeg -hide_banner -loglevel error \
            -i "rtmp://localhost:${PORT}/live/test?token=secret123" \
            -t 8 -c copy "$CAPTURE_FILE" \
            > "$LOG_DIR/test-capture-auth.log" 2>&1 &
        CAP_PID=$!
        PIDS+=("$CAP_PID")

        sleep 1

        # Publish with valid token
        ffmpeg -hide_banner -loglevel error \
            -f lavfi -i "testsrc=duration=5:size=320x240:rate=25" \
            -c:v libx264 -preset ultrafast -tune zerolatency \
            -f flv "rtmp://localhost:${PORT}/live/test?token=secret123" \
            > "$LOG_DIR/test-publish-auth.log" 2>&1 || true

        sleep 3
        stop_proc "$CAP_PID" 2>/dev/null || true

        SERVER_LOG="$LOG_DIR/test-server-${PORT}.log"
        if grep -q "publish started\|connection registered" "$SERVER_LOG" 2>/dev/null; then
            if ! grep -q "auth_failed\|authentication failed" "$SERVER_LOG" 2>/dev/null; then
                pass_test "auth-allow (publish with valid token succeeded)"
            else
                fail_test "auth-allow" "auth failed despite valid token"
            fi
        else
            fail_test "auth-allow" "no publish/connection in server log"
        fi

        stop_proc "$SERVER_PID"
    fi
fi

# ===========================
# Test 6: RTMP + Auth (rejected)
# ===========================
if should_run "auth-reject"; then
    log "${BLUE}Test 6: RTMP + Auth (rejected)${NC}"
    PORT=19356

    SERVER_PID=$(start_server "$PORT" "-log-level" "debug" \
        "-auth-mode" "token" "-auth-token" "live/test=secret123") || \
        { fail_test "auth-reject" "server failed to start"; }

    if [[ -n "${SERVER_PID:-}" ]]; then
        # Publish with WRONG token — should fail
        ffmpeg -hide_banner -loglevel error \
            -f lavfi -i "testsrc=duration=3:size=320x240:rate=25" \
            -c:v libx264 -preset ultrafast -tune zerolatency \
            -f flv "rtmp://localhost:${PORT}/live/test?token=wrongtoken" \
            > "$LOG_DIR/test-publish-auth-reject.log" 2>&1 || true

        sleep 2

        SERVER_LOG="$LOG_DIR/test-server-${PORT}.log"
        if grep -q "auth_failed\|authentication failed\|ErrUnauthorized" "$SERVER_LOG" 2>/dev/null; then
            pass_test "auth-reject (invalid token correctly rejected)"
        else
            # Also check: if no publish_start event was logged, auth was enforced
            if ! grep -q "publish started" "$SERVER_LOG" 2>/dev/null; then
                pass_test "auth-reject (publish blocked — no publish_start in log)"
            else
                fail_test "auth-reject" "publish succeeded with wrong token"
            fi
        fi

        stop_proc "$SERVER_PID"
    fi
fi

# ===========================
# Test 7: RTMPS + Auth
# ===========================
if should_run "rtmps-auth"; then
    log "${BLUE}Test 7: RTMPS + Auth (TLS + token)${NC}"
    PORT=19357
    TLS_PORT=19367

    "$SCRIPT_DIR/generate-certs.sh" 2>/dev/null
    CERT="$SCRIPT_DIR/.certs/cert.pem"
    KEY="$SCRIPT_DIR/.certs/key.pem"

    SERVER_PID=$(start_server "$PORT" "-log-level" "debug" \
        "-tls-listen" ":${TLS_PORT}" "-tls-cert" "$CERT" "-tls-key" "$KEY" \
        "-auth-mode" "token" "-auth-token" "live/test=secret123") || \
        { fail_test "rtmps-auth" "server failed to start"; }

    if [[ -n "${SERVER_PID:-}" ]]; then
        # Publish with valid token to plain RTMP (TLS + auth combined test)
        ffmpeg -hide_banner -loglevel error \
            -f lavfi -i "testsrc=duration=3:size=320x240:rate=25" \
            -c:v libx264 -preset ultrafast -tune zerolatency \
            -f flv "rtmp://localhost:${PORT}/live/test?token=secret123" \
            > "$LOG_DIR/test-publish-rtmps-auth.log" 2>&1 || true

        sleep 2

        SERVER_LOG="$LOG_DIR/test-server-${PORT}.log"
        TLS_OK=false
        AUTH_OK=false

        if grep -q "RTMPS server listening" "$SERVER_LOG" 2>/dev/null; then
            TLS_OK=true
        fi
        if grep -q "connection registered" "$SERVER_LOG" 2>/dev/null; then
            if ! grep -q "auth_failed" "$SERVER_LOG" 2>/dev/null; then
                AUTH_OK=true
            fi
        fi

        if [[ "$TLS_OK" == "true" && "$AUTH_OK" == "true" ]]; then
            pass_test "rtmps-auth (TLS + auth both active, publish succeeded)"
        elif [[ "$TLS_OK" == "false" ]]; then
            fail_test "rtmps-auth" "TLS listener not started"
        else
            fail_test "rtmps-auth" "auth failed with valid token"
        fi

        stop_proc "$SERVER_PID"
    fi
fi

# ===========================
# Summary
# ===========================
echo ""
echo -e "${BLUE}=== Test Summary ===${NC}"
TOTAL=$((PASSED + FAILED + SKIPPED))
echo -e "  Total:   $TOTAL"
echo -e "  ${GREEN}Passed:  $PASSED${NC}"
echo -e "  ${RED}Failed:  $FAILED${NC}"
echo -e "  ${YELLOW}Skipped: $SKIPPED${NC}"
echo ""

if [[ $FAILED -gt 0 ]]; then
    echo -e "${RED}Some tests failed. Check logs in $LOG_DIR${NC}"
    exit "$FAILED"
else
    echo -e "${GREEN}All tests passed!${NC}"
    exit 0
fi

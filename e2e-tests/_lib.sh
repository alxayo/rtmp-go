#!/usr/bin/env bash
# ============================================================================
# _lib.sh — Shared helper library for go-rtmp E2E tests
#
# This file is sourced by every test script. It provides:
#   - Color output helpers
#   - Server build/start/stop management
#   - FFmpeg publish/capture helpers
#   - Assertion functions (file exists, codec check, duration, decodability)
#   - Unique port allocation per test
#   - Cleanup and temp directory management
#   - Test result reporting
#
# USAGE (in a test script):
#   source "$SCRIPT_DIR/_lib.sh"
#   setup "test-name"
#   start_server "$PORT" [extra flags...]
#   ... test logic ...
#   teardown
#   report_result "test-name"
# ============================================================================

set -euo pipefail

# ---- Paths ----
E2E_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$E2E_DIR/.." && pwd)"
LOG_DIR="$E2E_DIR/logs"
CERTS_DIR="$E2E_DIR/.certs"
BINARY="$PROJECT_ROOT/rtmp-server"

# Windows detection
if [[ "$(uname -s)" == *MINGW* ]] || [[ "$(uname -s)" == *MSYS* ]]; then
    BINARY="$PROJECT_ROOT/rtmp-server.exe"
fi

# ---- Colors ----
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# ---- State ----
_PIDS=()
_CHECKS_PASSED=0
_CHECKS_FAILED=0
_TEST_NAME=""
TMPDIR=""

# ---- Logging ----

log_info()  { echo -e "$(date '+%H:%M:%S') ${GREEN}✓${NC} $1"; }
log_error() { echo -e "$(date '+%H:%M:%S') ${RED}✗${NC} $1"; }
log_warn()  { echo -e "$(date '+%H:%M:%S') ${YELLOW}⚠${NC} $1"; }
log_step()  { echo -e "$(date '+%H:%M:%S') ${BLUE}→${NC} $1"; }

# ---- Port Allocation ----
# Generates a unique port based on test name hash to avoid conflicts.
# Range: 19400-19599 (200 ports available)
unique_port() {
    local name="$1"
    local hash
    # Use cksum for cross-platform hash (available on macOS/Linux)
    hash=$(echo -n "$name" | cksum | awk '{print $1}')
    echo $(( 19400 + (hash % 200) ))
}

# ---- Setup / Teardown ----

setup() {
    _TEST_NAME="$1"
    _CHECKS_PASSED=0
    _CHECKS_FAILED=0
    _PIDS=()

    mkdir -p "$LOG_DIR"
    TMPDIR="$E2E_DIR/.test-tmp/$_TEST_NAME"
    rm -rf "$TMPDIR"
    mkdir -p "$TMPDIR"

    echo ""
    echo -e "${BLUE}=== E2E Test: $_TEST_NAME ===${NC}"
}

cleanup() {
    for pid in "${_PIDS[@]+"${_PIDS[@]}"}"; do
        if kill -0 "$pid" 2>/dev/null; then
            # SIGINT lets FFmpeg finalize files properly
            kill -INT "$pid" 2>/dev/null || true
        fi
    done
    # Wait for graceful exit
    sleep 2
    for pid in "${_PIDS[@]+"${_PIDS[@]}"}"; do
        if kill -0 "$pid" 2>/dev/null; then
            kill -9 "$pid" 2>/dev/null || true
        fi
    done
    _PIDS=()
}

teardown() {
    cleanup
    # Clean temp files unless KEEP_TMP is set (for debugging)
    if [[ -z "${KEEP_TMP:-}" ]]; then
        rm -rf "$TMPDIR" 2>/dev/null || true
    fi
}

trap cleanup EXIT INT TERM

# ---- Server Management ----

build_server() {
    if [[ ! -f "$BINARY" ]] || [[ -n "$(find "$PROJECT_ROOT/cmd" "$PROJECT_ROOT/internal" -newer "$BINARY" -name '*.go' 2>/dev/null | head -1)" ]]; then
        log_step "Building server..."
        cd "$PROJECT_ROOT"
        go build -o "$BINARY" ./cmd/rtmp-server
        log_info "Server built: $BINARY"
    fi
}

# Start the server and wait for it to be ready.
# Usage: start_server PORT [extra flags...]
# Sets SERVER_PID and SERVER_LOG globals.
SERVER_PID=""
SERVER_LOG=""

start_server() {
    local port="$1"; shift
    build_server

    SERVER_LOG="$LOG_DIR/${_TEST_NAME}-server.log"
    "$BINARY" -listen ":${port}" "$@" > "$SERVER_LOG" 2>&1 &
    SERVER_PID=$!
    _PIDS+=("$SERVER_PID")

    if ! wait_for_log "$SERVER_LOG" "server started\|server listening\|RTMP server listening" 10; then
        log_error "Server failed to start on port $port"
        if [[ -f "$SERVER_LOG" ]]; then
            echo "--- Server log ---"
            tail -20 "$SERVER_LOG"
            echo "--- End log ---"
        fi
        return 1
    fi
    log_info "Server started (PID $SERVER_PID, port $port)"
    return 0
}

stop_server() {
    local pid="${1:-$SERVER_PID}"
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
        kill "$pid" 2>/dev/null || true
        sleep 1
        kill -9 "$pid" 2>/dev/null || true
    fi
    # Remove from tracked PIDs
    local new_pids=()
    for p in "${_PIDS[@]+"${_PIDS[@]}"}"; do
        [[ "$p" != "$pid" ]] && new_pids+=("$p")
    done
    _PIDS=("${new_pids[@]+"${new_pids[@]}"}")
}

# ---- Log Waiting ----

wait_for_log() {
    local file="$1" pattern="$2" timeout="${3:-15}"
    for _i in $(seq 1 "$timeout"); do
        if [[ -f "$file" ]] && grep -q "$pattern" "$file" 2>/dev/null; then
            return 0
        fi
        sleep 1
    done
    return 1
}

# ---- FFmpeg Helpers ----

# Publish a synthetic test pattern (H.264+AAC) to an RTMP/SRT URL.
# Usage: publish_test_pattern URL DURATION [extra ffmpeg flags...]
publish_test_pattern() {
    local url="$1" duration="$2"; shift 2
    local log_file="$LOG_DIR/${_TEST_NAME}-publish.log"

    ffmpeg -hide_banner -loglevel error \
        -re \
        -f lavfi -i "testsrc=duration=${duration}:size=320x240:rate=25" \
        -f lavfi -i "sine=frequency=440:duration=${duration}" \
        -c:v libx264 -preset ultrafast -tune zerolatency \
        -c:a aac -b:a 64k \
        "$@" \
        -f flv "$url" \
        > "$log_file" 2>&1 || true
}

# Publish H.265 test pattern via Enhanced RTMP.
# Requires FFmpeg 6.1+ with libx265.
publish_h265_test_pattern() {
    local url="$1" duration="$2"; shift 2
    local log_file="$LOG_DIR/${_TEST_NAME}-publish-h265.log"

    ffmpeg -hide_banner -loglevel warning \
        -re \
        -f lavfi -i "testsrc2=duration=${duration}:size=640x480:rate=30" \
        -f lavfi -i "sine=frequency=440:duration=${duration}" \
        -c:v libx265 -preset ultrafast \
        -c:a aac -b:a 64k \
        "$@" \
        -f flv "$url" \
        > "$log_file" 2>&1 || true
}

# Publish audio-only test pattern (AAC).
publish_audio_only() {
    local url="$1" duration="$2"; shift 2
    local log_file="$LOG_DIR/${_TEST_NAME}-publish-audio.log"

    ffmpeg -hide_banner -loglevel error \
        -re \
        -f lavfi -i "sine=frequency=440:duration=${duration}" \
        -c:a aac -b:a 64k \
        "$@" \
        -f flv "$url" \
        > "$log_file" 2>&1 || true
}

# Publish H.264 via SRT (MPEG-TS).
publish_srt_h264() {
    local url="$1" duration="$2"; shift 2
    local log_file="$LOG_DIR/${_TEST_NAME}-publish-srt.log"

    ffmpeg -hide_banner -loglevel error \
        -re \
        -f lavfi -i "testsrc=duration=${duration}:size=320x240:rate=25" \
        -f lavfi -i "sine=frequency=440:duration=${duration}" \
        -c:v libx264 -preset ultrafast -tune zerolatency \
        -c:a aac -b:a 64k \
        "$@" \
        -f mpegts "$url" \
        > "$log_file" 2>&1 || true
}

# Publish H.265 via SRT (MPEG-TS).
publish_srt_h265() {
    local url="$1" duration="$2"; shift 2
    local log_file="$LOG_DIR/${_TEST_NAME}-publish-srt-h265.log"

    ffmpeg -hide_banner -loglevel error \
        -re \
        -f lavfi -i "testsrc2=duration=${duration}:size=640x480:rate=30" \
        -f lavfi -i "sine=frequency=440:duration=${duration}" \
        -c:v libx265 -preset ultrafast \
        -c:a aac -b:a 64k \
        "$@" \
        -f mpegts "$url" \
        > "$log_file" 2>&1 || true
}

# Start a background FFmpeg subscriber/capture.
# Usage: start_capture URL OUTPUT_FILE TIMEOUT_SECONDS
# Sets CAPTURE_PID global.
CAPTURE_PID=""

start_capture() {
    local url="$1" output="$2" timeout="$3"
    local log_file="$LOG_DIR/${_TEST_NAME}-capture.log"

    ffmpeg -hide_banner -loglevel error \
        -i "$url" \
        -t "$timeout" -c copy "$output" \
        > "$log_file" 2>&1 &
    CAPTURE_PID=$!
    _PIDS+=("$CAPTURE_PID")
}

# Wait for capture to finish and clean up PID tracking.
# Sends SIGINT first (FFmpeg finalizes FLV/file on INT), waits, then SIGKILL.
wait_and_stop_capture() {
    local pid="${1:-$CAPTURE_PID}"
    local timeout="${2:-10}"

    # Wait up to timeout for capture to finish naturally
    for _i in $(seq 1 "$timeout"); do
        if ! kill -0 "$pid" 2>/dev/null; then
            wait "$pid" 2>/dev/null || true
            break
        fi
        sleep 1
    done

    # Graceful stop: SIGINT lets FFmpeg finalize the file
    if kill -0 "$pid" 2>/dev/null; then
        kill -INT "$pid" 2>/dev/null || true
        # Wait up to 5 seconds for graceful exit
        for _i in 1 2 3 4 5; do
            if ! kill -0 "$pid" 2>/dev/null; then
                break
            fi
            sleep 1
        done
        wait "$pid" 2>/dev/null || true
    fi

    # Force stop if still running
    if kill -0 "$pid" 2>/dev/null; then
        kill -9 "$pid" 2>/dev/null || true
        wait "$pid" 2>/dev/null || true
    fi

    # Remove from tracked PIDs
    local new_pids=()
    for p in "${_PIDS[@]+"${_PIDS[@]}"}"; do
        [[ "$p" != "$pid" ]] && new_pids+=("$p")
    done
    _PIDS=("${new_pids[@]+"${new_pids[@]}"}")
}

# ---- TLS Certificates ----

generate_certs() {
    if [[ -f "$CERTS_DIR/cert.pem" && -f "$CERTS_DIR/key.pem" ]]; then
        return 0
    fi
    mkdir -p "$CERTS_DIR"
    log_step "Generating TLS certificates..."
    openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 \
        -nodes -keyout "$CERTS_DIR/key.pem" -out "$CERTS_DIR/cert.pem" \
        -days 365 -subj "/CN=localhost" \
        -addext "subjectAltName=DNS:localhost,IP:127.0.0.1" \
        2>/dev/null
    log_info "Certificates generated"
}

# ---- Assertions ----

pass_check() {
    echo -e "  ${GREEN}✓${NC} $1"
    _CHECKS_PASSED=$((_CHECKS_PASSED + 1))
}

fail_check() {
    echo -e "  ${RED}✗${NC} $1"
    if [[ -n "${2:-}" ]]; then
        echo -e "    ${RED}$2${NC}"
    fi
    _CHECKS_FAILED=$((_CHECKS_FAILED + 1))
}

skip_check() {
    echo -e "  ${YELLOW}⊘${NC} $1 (skipped: ${2:-})"
}

# Assert file exists and is non-empty.
assert_file_exists() {
    local file="$1" label="${2:-File exists}"
    if [[ -f "$file" ]] && [[ -s "$file" ]]; then
        local size
        size=$(stat -f%z "$file" 2>/dev/null || stat --format=%s "$file" 2>/dev/null || echo "?")
        pass_check "$label ($(basename "$file"), ${size} bytes)"
    else
        fail_check "$label" "File not found or empty: $file"
    fi
}

# Assert video codec matches expected value.
assert_video_codec() {
    local file="$1" expected="$2"
    local codec
    codec=$(ffprobe -v error -select_streams v:0 \
        -show_entries stream=codec_name -of csv=p=0 "$file" 2>/dev/null || echo "")
    if [[ "$codec" == "$expected" ]]; then
        pass_check "Video codec is $expected"
    else
        fail_check "Video codec is $expected" "got: '${codec:-<none>}'"
    fi
}

# Assert audio codec matches expected value.
assert_audio_codec() {
    local file="$1" expected="$2"
    local codec
    codec=$(ffprobe -v error -select_streams a:0 \
        -show_entries stream=codec_name -of csv=p=0 "$file" 2>/dev/null || echo "")
    if [[ "$codec" == "$expected" ]]; then
        pass_check "Audio codec is $expected"
    else
        fail_check "Audio codec is $expected" "got: '${codec:-<none>}'"
    fi
}

# Assert file has video stream (any codec).
assert_has_video() {
    local file="$1"
    local codec
    codec=$(ffprobe -v error -select_streams v:0 \
        -show_entries stream=codec_type -of csv=p=0 "$file" 2>/dev/null || echo "")
    if [[ "$codec" == "video" ]]; then
        pass_check "File has video stream"
    else
        fail_check "File has video stream" "no video stream found"
    fi
}

# Assert file has audio stream (any codec).
assert_has_audio() {
    local file="$1"
    local codec
    codec=$(ffprobe -v error -select_streams a:0 \
        -show_entries stream=codec_type -of csv=p=0 "$file" 2>/dev/null || echo "")
    if [[ "$codec" == "audio" ]]; then
        pass_check "File has audio stream"
    else
        fail_check "File has audio stream" "no audio stream found"
    fi
}

# Assert recording duration is within range.
assert_duration() {
    local file="$1" min="$2" max="$3"
    local duration
    duration=$(ffprobe -v error -show_entries format=duration -of csv=p=0 "$file" 2>/dev/null || echo "0")

    local ok
    ok=$(awk "BEGIN { d=$duration+0; print (d >= $min && d <= $max) ? 1 : 0 }" 2>/dev/null || echo "0")
    if [[ "$ok" == "1" ]]; then
        pass_check "Duration in range [${min}s, ${max}s] (got ${duration}s)"
    else
        fail_check "Duration in range [${min}s, ${max}s]" "got ${duration}s"
    fi
}

# Assert file is fully decodable (no corrupt frames).
assert_decodable() {
    local file="$1"
    local decode_log="$LOG_DIR/${_TEST_NAME}-decode.log"
    if ffmpeg -hide_banner -v error -i "$file" -f null - > "$decode_log" 2>&1; then
        pass_check "Full decode test passed"
    else
        fail_check "Full decode test" "decode errors (see $decode_log)"
    fi
}

# Assert server log contains a pattern.
assert_log_contains() {
    local file="$1" pattern="$2" label="${3:-Log contains '$pattern'}"
    if grep -q "$pattern" "$file" 2>/dev/null; then
        pass_check "$label"
    else
        fail_check "$label" "pattern '$pattern' not found in $(basename "$file")"
    fi
}

# Assert server log does NOT contain a pattern.
assert_log_not_contains() {
    local file="$1" pattern="$2" label="${3:-Log does not contain '$pattern'}"
    if grep -q "$pattern" "$file" 2>/dev/null; then
        fail_check "$label" "pattern '$pattern' was found in $(basename "$file")"
    else
        pass_check "$label"
    fi
}

# Assert HTTP JSON response contains a field with value > 0.
assert_http_json_field() {
    local url="$1" field="$2"
    local response
    response=$(curl -s "$url" 2>/dev/null || echo "{}")
    if echo "$response" | grep -q "\"$field\""; then
        pass_check "HTTP response contains '$field'"
    else
        fail_check "HTTP response contains '$field'" "field not found in response"
    fi
}

# ---- Result Reporting ----

report_result() {
    local name="${1:-$_TEST_NAME}"
    echo ""
    if [[ $_CHECKS_FAILED -eq 0 ]]; then
        echo -e "${GREEN}RESULT: PASS${NC} — $name (${_CHECKS_PASSED} checks passed)"
        return 0
    else
        echo -e "${RED}RESULT: FAIL${NC} — $name (${_CHECKS_PASSED} passed, ${_CHECKS_FAILED} failed)"
        echo -e "  Server log: $SERVER_LOG"
        return 1
    fi
}

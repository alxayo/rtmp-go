#!/usr/bin/env bash
# ============================================================================
# TEST: srt-encrypt-per-stream-passphrase
# GROUP: SRT Encryption
#
# WHAT IS TESTED:
#   Per-stream SRT encryption using -srt-passphrase-file. The server loads
#   a JSON file mapping stream keys to individual passphrases. Two FFmpeg
#   publishers connect to different streams, each with its own passphrase.
#   This validates that the per-stream passphrase resolver correctly looks
#   up the passphrase based on the Stream ID during the handshake.
#
# EXPECTED RESULT:
#   - Server log shows "SRT per-stream encryption enabled"
#   - Stream 1 (live/stream1) connects with passphrase1 → succeeds
#   - Stream 2 (live/stream2) connects with passphrase2 → succeeds
#   - Both publish sessions start successfully
#   - No errors or panics in server log
#
# PREREQUISITES:
#   - FFmpeg with SRT protocol support (libsrt)
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/srt-encrypt-per-stream-passphrase.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="srt-encrypt-per-stream-passphrase"
PORT=$(unique_port "$TEST_NAME")
SRT_PORT=$((PORT + 200))

# Check SRT support
if ! ffmpeg -hide_banner -protocols 2>/dev/null | grep -q srt; then
    echo -e "${YELLOW}SKIP: FFmpeg does not have SRT protocol support${NC}"
    exit 2
fi

setup "$TEST_NAME"

# Create per-stream passphrase JSON file
PASSPHRASE_FILE="$TMPDIR/srt-keys.json"
cat > "$PASSPHRASE_FILE" <<'EOF'
{
  "live/stream1": "passphrase-for-stream-one",
  "live/stream2": "passphrase-for-stream-two"
}
EOF

# Start server with per-stream SRT encryption
start_server "$PORT" "-log-level" "debug" \
    "-srt-listen" "localhost:${SRT_PORT}" \
    "-srt-passphrase-file" "$PASSPHRASE_FILE" \
    "-srt-pbkeylen" "16"

# Verify per-stream encryption is logged
assert_log_contains "$SERVER_LOG" "SRT per-stream encryption enabled" "Per-stream encryption enabled in log"

# --- Stream 1: publish with passphrase1 ---
log_step "Publishing stream1 via encrypted SRT (per-stream passphrase, 5s)..."
publish_srt_h264 "srt://localhost:${SRT_PORT}?streamid=publish:live/stream1&latency=200000&passphrase=passphrase-for-stream-one&pbkeylen=16" 5

# Wait for the publish session to appear in server logs
if ! wait_for_log "$SERVER_LOG" "publish session started.*stream1" 10; then
    log_error "Stream 1 publish session not started"
    cat "$SERVER_LOG"
    teardown
    report_result "$TEST_NAME" 1
    exit 1
fi
log_info "Stream 1 publish session started"
sleep 2

# --- Stream 2: publish with passphrase2 ---
log_step "Publishing stream2 via encrypted SRT (different passphrase, 5s)..."
publish_srt_h264 "srt://localhost:${SRT_PORT}?streamid=publish:live/stream2&latency=200000&passphrase=passphrase-for-stream-two&pbkeylen=16" 5

# Wait for stream2 publish session
if ! wait_for_log "$SERVER_LOG" "publish session started.*stream2" 10; then
    log_error "Stream 2 publish session not started"
    cat "$SERVER_LOG"
    teardown
    report_result "$TEST_NAME" 1
    exit 1
fi
log_info "Stream 2 publish session started"
sleep 2

# Verify no errors
assert_log_not_contains "$SERVER_LOG" "panic" "No panics in server log"
assert_log_not_contains "$SERVER_LOG" "unwrap SEK" "No SEK unwrap errors"

teardown
report_result "$TEST_NAME" 0

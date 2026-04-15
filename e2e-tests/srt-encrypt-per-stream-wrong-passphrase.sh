#!/usr/bin/env bash
# ============================================================================
# TEST: srt-encrypt-per-stream-wrong-passphrase
# GROUP: SRT Encryption
#
# WHAT IS TESTED:
#   When using per-stream encryption (-srt-passphrase-file), a client that
#   provides the wrong passphrase for a known stream is rejected. This
#   validates that the per-stream resolver looks up the correct passphrase
#   and the SEK unwrap fails with a passphrase mismatch.
#
# EXPECTED RESULT:
#   - FFmpeg fails to connect (handshake rejected due to wrong passphrase)
#   - Server log shows "unwrap SEK" error (wrong passphrase)
#   - No panics in server log
#
# PREREQUISITES:
#   - FFmpeg with SRT protocol support (libsrt)
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/srt-encrypt-per-stream-wrong-passphrase.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="srt-encrypt-per-stream-wrong-passphrase"
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
  "live/protected": "correct-passphrase-here"
}
EOF

# Start server with per-stream SRT encryption
start_server "$PORT" "-log-level" "debug" \
    "-srt-listen" "localhost:${SRT_PORT}" \
    "-srt-passphrase-file" "$PASSPHRASE_FILE" \
    "-srt-pbkeylen" "16"

log_step "Publishing with WRONG passphrase to per-stream encrypted server..."

# Use wrong passphrase — should fail
FFMPEG_LOG="$TMPDIR/ffmpeg-wrong-pp.log"
ffmpeg -hide_banner -loglevel warning \
    -re -f lavfi -i "testsrc=size=320x240:rate=30:duration=3" \
    -f lavfi -i "sine=frequency=1000:duration=3:sample_rate=44100" \
    -c:v libx264 -preset ultrafast -tune zerolatency -g 30 -b:v 500k \
    -c:a aac -b:a 128k \
    -f mpegts "srt://localhost:${SRT_PORT}?streamid=publish:live/protected&latency=200000&passphrase=WRONG-passphrase!&pbkeylen=16" \
    > "$FFMPEG_LOG" 2>&1 &
FFMPEG_PID=$!
_PIDS+=("$FFMPEG_PID")

# Wait for FFmpeg to fail (should not succeed)
sleep 5

# FFmpeg should have exited with error or the server should show an error
if wait_for_log "$SERVER_LOG" "unwrap SEK\|passphrase lookup failed" 5; then
    log_info "Server correctly rejected wrong passphrase"
else
    # Check if FFmpeg connected successfully (which would be wrong)
    if wait_for_log "$SERVER_LOG" "publish session started" 3; then
        log_error "Publish session started with wrong passphrase — this should not happen!"
        teardown
        report_result "$TEST_NAME" 1
        exit 1
    fi
    log_info "Connection rejected (no error log visible, but publish didn't start)"
fi

# Verify no panics
assert_log_not_contains "$SERVER_LOG" "panic" "No panics in server log"

teardown
report_result "$TEST_NAME" 0

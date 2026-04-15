#!/usr/bin/env bash
# ============================================================================
# TEST: srt-encrypt-wrong-passphrase
# GROUP: SRT Encryption
#
# WHAT IS TESTED:
#   SRT connection with a WRONG passphrase is rejected by the server. The
#   server is started with one passphrase, but the FFmpeg client uses a
#   different passphrase. The handshake should fail because the server
#   cannot unwrap the client's Stream Encrypting Key (SEK) with the wrong
#   Key Encrypting Key (KEK).
#
# EXPECTED RESULT:
#   - FFmpeg publish fails (connection refused or times out)
#   - Server log shows handshake error (e.g., "unwrap SEK" or similar)
#   - No media is delivered (no "publish started" for this stream)
#
# PREREQUISITES:
#   - FFmpeg with SRT protocol support (libsrt)
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/srt-encrypt-wrong-passphrase.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="srt-encrypt-wrong-passphrase"
PORT=$(unique_port "$TEST_NAME")
SRT_PORT=$((PORT + 200))

# Check SRT support
if ! ffmpeg -hide_banner -protocols 2>/dev/null | grep -q srt; then
    echo -e "${YELLOW}SKIP: FFmpeg does not have SRT protocol support${NC}"
    exit 2
fi

setup "$TEST_NAME"

# Start server with SRT encryption
start_server "$PORT" "-log-level" "debug" \
    "-srt-listen" "localhost:${SRT_PORT}" \
    "-srt-passphrase" "server-secret-phrase" \
    "-srt-pbkeylen" "16"

log_step "Publishing with WRONG passphrase (should be rejected)..."
# This should fail — we expect the SRT handshake to reject mismatched keys
set +e
publish_srt_h264 "srt://localhost:${SRT_PORT}?streamid=publish:live/wrong-pass&latency=200000&passphrase=wrong-client-phrase&pbkeylen=16" 3
PUBLISH_EXIT=$?
set -e

sleep 2

# Check that the server detected the passphrase mismatch.
# The server should log an error about failing to unwrap the SEK.
if grep -qiE "unwrap SEK|wrong passphrase|handshake.*fail|handshake.*error|encryption.*fail" "$SERVER_LOG" 2>/dev/null; then
    pass_check "Server rejected wrong passphrase"
elif [[ $PUBLISH_EXIT -ne 0 ]]; then
    pass_check "FFmpeg publish failed as expected (exit code: $PUBLISH_EXIT)"
else
    fail_check "Wrong passphrase rejection" "Publish appeared to succeed despite wrong passphrase"
fi

assert_log_not_contains "$SERVER_LOG" "panic\|FATAL" "No server panics"

teardown
report_result "$TEST_NAME"

# ============================================================================
# MANUAL TESTING
# ============================================================================
# For manual testing without the automation framework, see MANUAL_TESTING.md
# which provides exact commands for:
#   - Starting the server
#   - Publishing streams
#   - Capturing/subscribing
#   - Verifying output with ffprobe
#
# Each test group in MANUAL_TESTING.md includes step-by-step instructions
# with real commands you can copy and paste into your terminal.
# ============================================================================

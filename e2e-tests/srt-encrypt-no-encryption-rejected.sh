#!/usr/bin/env bash
# ============================================================================
# TEST: srt-encrypt-no-encryption-rejected
# GROUP: SRT Encryption
#
# WHAT IS TESTED:
#   When the server requires SRT encryption (has a passphrase configured),
#   an unencrypted client (no passphrase in URL) should be rejected. The
#   server expects a KMREQ extension during the handshake; without it,
#   the handshake fails with "encryption required but client sent no KMREQ".
#
# EXPECTED RESULT:
#   - FFmpeg publish fails (connection refused or handshake error)
#   - Server log shows "encryption required" error
#   - No media is delivered
#
# PREREQUISITES:
#   - FFmpeg with SRT protocol support (libsrt)
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/srt-encrypt-no-encryption-rejected.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="srt-encrypt-no-encryption-rejected"
PORT=$(unique_port "$TEST_NAME")
SRT_PORT=$((PORT + 200))

# Check SRT support
if ! ffmpeg -hide_banner -protocols 2>/dev/null | grep -q srt; then
    echo -e "${YELLOW}SKIP: FFmpeg does not have SRT protocol support${NC}"
    exit 2
fi

setup "$TEST_NAME"

# Start server WITH encryption required
start_server "$PORT" "-log-level" "debug" \
    "-srt-listen" "localhost:${SRT_PORT}" \
    "-srt-passphrase" "server-secret-phrase" \
    "-srt-pbkeylen" "16"

log_step "Publishing WITHOUT encryption (should be rejected)..."
# Publish without any passphrase — server should reject
set +e
publish_srt_h264 "srt://localhost:${SRT_PORT}?streamid=publish:live/no-encrypt&latency=200000" 3
PUBLISH_EXIT=$?
set -e

sleep 2

# Check that the server rejected the unencrypted client.
# The server should log "encryption required but client sent no KMREQ".
if grep -qiE "encryption required|no KMREQ|handshake.*fail|handshake.*error" "$SERVER_LOG" 2>/dev/null; then
    pass_check "Server rejected unencrypted client"
elif [[ $PUBLISH_EXIT -ne 0 ]]; then
    pass_check "FFmpeg publish failed as expected (exit code: $PUBLISH_EXIT)"
else
    fail_check "No-encryption rejection" "Publish appeared to succeed despite missing encryption"
fi

assert_log_not_contains "$SERVER_LOG" "panic\|FATAL" "No server panics"

teardown
report_result "$TEST_NAME"

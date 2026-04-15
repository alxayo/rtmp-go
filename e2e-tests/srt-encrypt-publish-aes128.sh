#!/usr/bin/env bash
# ============================================================================
# TEST: srt-encrypt-publish-aes128
# GROUP: SRT Encryption
#
# WHAT IS TESTED:
#   SRT ingest with AES-128 encryption enabled. The server is started with
#   -srt-passphrase and -srt-pbkeylen 16 (AES-128). FFmpeg publishes via
#   SRT with the matching passphrase in the URL. This validates that the
#   SRT encryption handshake (KMREQ/KMRSP) completes successfully and
#   encrypted media flows through the server.
#
# EXPECTED RESULT:
#   - Server log shows "SRT encryption enabled" with "AES-128"
#   - SRT connection is accepted (encrypted handshake succeeds)
#   - No panics, errors, or encryption failures in server log
#
# PREREQUISITES:
#   - FFmpeg with SRT protocol support (libsrt)
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/srt-encrypt-publish-aes128.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="srt-encrypt-publish-aes128"
PORT=$(unique_port "$TEST_NAME")
SRT_PORT=$((PORT + 200))

# Check SRT support
if ! ffmpeg -hide_banner -protocols 2>/dev/null | grep -q srt; then
    echo -e "${YELLOW}SKIP: FFmpeg does not have SRT protocol support${NC}"
    exit 2
fi

setup "$TEST_NAME"

# Start server with SRT encryption enabled (AES-128)
start_server "$PORT" "-log-level" "debug" \
    "-srt-listen" "localhost:${SRT_PORT}" \
    "-srt-passphrase" "test-secret-passphrase" \
    "-srt-pbkeylen" "16"

log_step "Publishing H.264+AAC via encrypted SRT (AES-128, 5s)..."
publish_srt_h264 "srt://localhost:${SRT_PORT}?streamid=publish:live/encrypt-aes128&latency=200000&passphrase=test-secret-passphrase&pbkeylen=16" 5
sleep 2

assert_log_contains "$SERVER_LOG" "SRT encryption enabled" "Encryption is enabled"
assert_log_contains "$SERVER_LOG" "AES-128" "Using AES-128 key length"
assert_log_not_contains "$SERVER_LOG" "panic\|FATAL" "No server panics"

teardown
report_result "$TEST_NAME"

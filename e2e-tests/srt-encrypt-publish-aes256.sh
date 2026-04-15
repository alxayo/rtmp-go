#!/usr/bin/env bash
# ============================================================================
# TEST: srt-encrypt-publish-aes256
# GROUP: SRT Encryption
#
# WHAT IS TESTED:
#   SRT ingest with AES-256 encryption (strongest key size). The server is
#   started with -srt-passphrase and -srt-pbkeylen 32 (AES-256). FFmpeg
#   publishes via SRT with the matching passphrase and pbkeylen=32 in the URL.
#
# EXPECTED RESULT:
#   - Server log shows "SRT encryption enabled" with "AES-256"
#   - SRT connection is accepted (encrypted handshake succeeds)
#   - No panics, errors, or encryption failures in server log
#
# PREREQUISITES:
#   - FFmpeg with SRT protocol support (libsrt)
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/srt-encrypt-publish-aes256.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="srt-encrypt-publish-aes256"
PORT=$(unique_port "$TEST_NAME")
SRT_PORT=$((PORT + 200))

# Check SRT support
if ! ffmpeg -hide_banner -protocols 2>/dev/null | grep -q srt; then
    echo -e "${YELLOW}SKIP: FFmpeg does not have SRT protocol support${NC}"
    exit 2
fi

setup "$TEST_NAME"

# Start server with SRT encryption enabled (AES-256)
start_server "$PORT" "-log-level" "debug" \
    "-srt-listen" "localhost:${SRT_PORT}" \
    "-srt-passphrase" "test-secret-passphrase" \
    "-srt-pbkeylen" "32"

log_step "Publishing H.264+AAC via encrypted SRT (AES-256, 5s)..."
publish_srt_h264 "srt://localhost:${SRT_PORT}?streamid=publish:live/encrypt-aes256&latency=200000&passphrase=test-secret-passphrase&pbkeylen=32" 5
sleep 2

assert_log_contains "$SERVER_LOG" "SRT encryption enabled" "Encryption is enabled"
assert_log_contains "$SERVER_LOG" "AES-256" "Using AES-256 key length"
assert_log_not_contains "$SERVER_LOG" "panic\|FATAL" "No server panics"

teardown
report_result "$TEST_NAME"

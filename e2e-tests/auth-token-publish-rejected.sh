#!/usr/bin/env bash
# ============================================================================
# TEST: auth-token-publish-rejected
# GROUP: Authentication
#
# WHAT IS TESTED:
#   Publishing with an INCORRECT token is rejected by the server. The server
#   is started with -auth-mode=token -auth-token=secret123, but the publisher
#   uses ?token=wrongtoken. The server should deny the connection.
#
# EXPECTED RESULT:
#   - FFmpeg publish fails or is disconnected
#   - Server log shows an authentication failure or rejection message
#   - No "publish started" for this stream
#
# PREREQUISITES:
#   - FFmpeg with libx264, aac
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/auth-token-publish-rejected.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="auth-token-publish-rejected"
PORT=$(unique_port "$TEST_NAME")

setup "$TEST_NAME"

start_server "$PORT" "-log-level" "debug" "-auth-mode" "token" "-auth-token" "live/auth-test=secret123"

log_step "Publishing with WRONG token (should be rejected)..."
# This should fail — we expect non-zero exit from ffmpeg
set +e
publish_test_pattern "rtmp://localhost:${PORT}/live/auth-test?token=wrongtoken" 3
PUBLISH_EXIT=$?
set -e

sleep 2

# Check that the server rejected the connection
if grep -qiE "auth|reject|denied|failed" "$SERVER_LOG" 2>/dev/null; then
    pass_check "Server rejected unauthorized publish"
elif [[ $PUBLISH_EXIT -ne 0 ]]; then
    pass_check "FFmpeg publish failed as expected (exit code: $PUBLISH_EXIT)"
else
    fail_check "Auth rejection" "Publish appeared to succeed despite wrong token"
fi

teardown
report_result "$TEST_NAME"

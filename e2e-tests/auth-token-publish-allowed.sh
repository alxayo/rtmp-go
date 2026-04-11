#!/usr/bin/env bash
# ============================================================================
# TEST: auth-token-publish-allowed
# GROUP: Authentication
#
# WHAT IS TESTED:
#   Publishing with the correct authentication token succeeds. The server
#   is started with -auth-mode=token -auth-token=secret123, and the publisher
#   includes ?token=secret123 in the URL.
#
# EXPECTED RESULT:
#   - Publish succeeds (no rejection in server log)
#   - Server log shows "connection registered" / publish activity
#   - No "auth" failure messages in the log
#
# PREREQUISITES:
#   - FFmpeg with libx264, aac
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/auth-token-publish-allowed.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="auth-token-publish-allowed"
PORT=$(unique_port "$TEST_NAME")

setup "$TEST_NAME"

start_server "$PORT" "-log-level" "debug" "-auth-mode" "token" "-auth-token" "live/auth-test=secret123"

log_step "Publishing with correct token (5s)..."
publish_test_pattern "rtmp://localhost:${PORT}/live/auth-test?token=secret123" 5
sleep 2

assert_log_contains "$SERVER_LOG" "connection registered" "Publisher connected"
assert_log_not_contains "$SERVER_LOG" "auth_failed\|authentication failed\|rejected" "No auth failures"

teardown
report_result "$TEST_NAME"

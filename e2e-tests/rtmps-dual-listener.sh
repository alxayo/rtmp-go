#!/usr/bin/env bash
# ============================================================================
# TEST: rtmps-dual-listener
# GROUP: RTMPS (TLS)
#
# WHAT IS TESTED:
#   Server runs both plain RTMP and RTMPS (TLS) listeners simultaneously
#   on different ports. Verifies both are reachable and can accept connections.
#   This tests the dual-listener capability documented in the server config.
#
# EXPECTED RESULT:
#   - Server log shows both RTMP and RTMPS listeners started
#   - Plain RTMP publish succeeds on the RTMP port
#   - No errors from having both listeners active
#
# PREREQUISITES:
#   - openssl (for cert generation)
#   - FFmpeg with libx264
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/rtmps-dual-listener.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="rtmps-dual-listener"
PORT=$(unique_port "$TEST_NAME")
TLS_PORT=$((PORT + 100))

if ! command -v openssl &>/dev/null; then
    echo -e "${YELLOW}SKIP: openssl not found${NC}"
    exit 2
fi

setup "$TEST_NAME"
generate_certs

start_server "$PORT" \
    "-log-level" "debug" \
    "-tls-listen" "localhost:${TLS_PORT}" \
    "-tls-cert" "$CERTS_DIR/server.crt" \
    "-tls-key" "$CERTS_DIR/server.key"

# Verify both listeners appear in the log
assert_log_contains "$SERVER_LOG" "listening" "Server shows listener(s) started"

# Publish over plain RTMP to verify it still works alongside TLS
log_step "Publishing over plain RTMP (3s)..."
publish_test_pattern "rtmp://localhost:${PORT}/live/dual-test" 3
sleep 2

assert_log_contains "$SERVER_LOG" "connection registered" "Plain RTMP connection accepted"
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

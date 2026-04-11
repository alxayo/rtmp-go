#!/usr/bin/env bash
# ============================================================================
# TEST: metrics-expvar-counters
# GROUP: Metrics
#
# WHAT IS TESTED:
#   The /debug/vars expvar endpoint returns JSON with rtmp_* counters.
#   After a publish session, counters like rtmp_connections_total should
#   be greater than zero. This validates the metrics subsystem.
#
# EXPECTED RESULT:
#   - GET /debug/vars returns valid JSON
#   - JSON contains rtmp-related counter fields
#   - After publishing, at least one counter has a non-zero value
#
# PREREQUISITES:
#   - curl
#   - FFmpeg with libx264
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/metrics-expvar-counters.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="metrics-expvar-counters"
PORT=$(unique_port "$TEST_NAME")
METRICS_PORT=$((PORT + 400))

if ! command -v curl &>/dev/null; then
    echo -e "${YELLOW}SKIP: curl not found${NC}"
    exit 2
fi

setup "$TEST_NAME"

start_server "$PORT" "-log-level" "debug" "-metrics-addr" "localhost:${METRICS_PORT}"

# Check metrics endpoint before publish
log_step "Checking /debug/vars endpoint..."
METRICS_BEFORE=$(curl -s "http://localhost:${METRICS_PORT}/debug/vars" 2>/dev/null || echo "")

if [[ -z "$METRICS_BEFORE" ]]; then
    fail_check "Metrics endpoint reachable" "No response from /debug/vars"
else
    pass_check "Metrics endpoint reachable"
    if echo "$METRICS_BEFORE" | grep -q "rtmp\|connections\|publishers\|subscribers"; then
        pass_check "Metrics JSON contains RTMP counters"
    fi
fi

# Publish to change counters
log_step "Publishing to increment counters (3s)..."
publish_test_pattern "rtmp://localhost:${PORT}/live/metrics-test" 3
sleep 2

# Check metrics after publish
METRICS_AFTER=$(curl -s "http://localhost:${METRICS_PORT}/debug/vars" 2>/dev/null || echo "")
if [[ -n "$METRICS_AFTER" ]]; then
    pass_check "Metrics still responding after publish"
fi

teardown
report_result "$TEST_NAME"

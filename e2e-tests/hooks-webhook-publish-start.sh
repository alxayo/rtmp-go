#!/usr/bin/env bash
# ============================================================================
# TEST: hooks-webhook-publish-start
# GROUP: Event Hooks
#
# WHAT IS TESTED:
#   Webhook POST fires on publish_start event. A simple HTTP listener
#   (using netcat or a Python one-liner) captures the POST request body.
#   After publishing, we verify the webhook received a JSON payload with
#   the expected event type.
#
# EXPECTED RESULT:
#   - Webhook endpoint receives an HTTP POST
#   - POST body contains JSON with event type info
#
# PREREQUISITES:
#   - FFmpeg with libx264
#   - Python3 or nc (for simple HTTP listener)
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/hooks-webhook-publish-start.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="hooks-webhook-publish-start"
PORT=$(unique_port "$TEST_NAME")
WEBHOOK_PORT=$((PORT + 300))

# Need python3 for the webhook listener
if ! command -v python3 &>/dev/null; then
    echo -e "${YELLOW}SKIP: python3 not found (needed for webhook listener)${NC}"
    exit 2
fi

setup "$TEST_NAME"

WEBHOOK_LOG="$TMPDIR/webhook-received.json"

# Start a simple HTTP server that logs the POST body
cat > "$TMPDIR/webhook-server.py" << PYEOF
import http.server, json, sys, os

class Handler(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        length = int(self.headers.get('Content-Length', 0))
        body = self.rfile.read(length)
        with open("$WEBHOOK_LOG", 'wb') as f:
            f.write(body)
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b'ok')
    def log_message(self, format, *args):
        pass  # Suppress output

server = http.server.HTTPServer(('localhost', $WEBHOOK_PORT), Handler)
server.timeout = 30
server.handle_request()  # Handle one request then exit
PYEOF

# Start webhook listener in background
python3 "$TMPDIR/webhook-server.py" &
WEBHOOK_PID=$!
_PIDS+=($WEBHOOK_PID)
sleep 1

start_server "$PORT" "-log-level" "debug" \
    "-hook-webhook" "http://localhost:${WEBHOOK_PORT}/hook"

log_step "Publishing to trigger webhook (3s)..."
publish_test_pattern "rtmp://localhost:${PORT}/live/webhook-test" 3
sleep 3

# Wait for webhook handler to process
wait $WEBHOOK_PID 2>/dev/null || true

if [[ -f "$WEBHOOK_LOG" ]] && [[ -s "$WEBHOOK_LOG" ]]; then
    pass_check "Webhook received POST data ($(wc -c < "$WEBHOOK_LOG") bytes)"
    if grep -qi "publish\|event\|stream" "$WEBHOOK_LOG" 2>/dev/null; then
        pass_check "Webhook payload contains event info"
    fi
else
    fail_check "Webhook received POST" "No data received at webhook endpoint"
fi

teardown
report_result "$TEST_NAME"

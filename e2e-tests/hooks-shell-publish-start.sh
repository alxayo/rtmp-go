#!/usr/bin/env bash
# ============================================================================
# TEST: hooks-shell-publish-start
# GROUP: Event Hooks
#
# WHAT IS TESTED:
#   Shell hook fires on publish_start event. The server is configured with
#   -hook-script pointing to a small shell script that writes a marker file
#   when invoked. After publishing, the test checks for the marker file.
#
# EXPECTED RESULT:
#   - Hook script is executed when publish starts
#   - Marker file is created by the hook script
#   - Hook receives event data (JSON via stdin or args)
#
# PREREQUISITES:
#   - FFmpeg with libx264
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/hooks-shell-publish-start.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="hooks-shell-publish-start"
PORT=$(unique_port "$TEST_NAME")

setup "$TEST_NAME"

MARKER_FILE="$TMPDIR/hook-fired.txt"
HOOK_SCRIPT="$TMPDIR/hook.sh"

# Create a simple hook script that writes a marker file
cat > "$HOOK_SCRIPT" << 'HOOKEOF'
#!/usr/bin/env bash
# Hook script: reads event JSON from stdin and writes to marker file
cat > "$HOOK_MARKER_FILE"
HOOKEOF
chmod +x "$HOOK_SCRIPT"

# Export marker file path so hook can access it
export HOOK_MARKER_FILE="$MARKER_FILE"

start_server "$PORT" "-log-level" "debug" "-hook-script" "$HOOK_SCRIPT"

log_step "Publishing to trigger hook (3s)..."
publish_test_pattern "rtmp://localhost:${PORT}/live/hook-test" 3
sleep 3

if [[ -f "$MARKER_FILE" ]]; then
    pass_check "Hook script fired (marker file created)"
    if [[ -s "$MARKER_FILE" ]]; then
        pass_check "Hook received event data ($(wc -c < "$MARKER_FILE") bytes)"
    fi
else
    fail_check "Hook script fired" "Marker file not found at $MARKER_FILE"
fi

teardown
report_result "$TEST_NAME"

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

# Create a simple hook script that writes event data to marker file
# Note: The server passes RTMP_* environment variables to hooks, but does NOT
# inherit the parent shell's environment. We embed the marker path directly.
cat > "$HOOK_SCRIPT" << HOOKEOF
#!/usr/bin/env bash
# Hook script: writes event env vars to marker file to prove it fired
env | grep RTMP_ > "${MARKER_FILE}"
HOOKEOF
chmod +x "$HOOK_SCRIPT"

start_server "$PORT" "-log-level" "debug" "-hook-script" "publish_start=$HOOK_SCRIPT"

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

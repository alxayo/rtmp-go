#!/usr/bin/env bash
# ============================================================================
# TEST: hooks-hls-conversion
# GROUP: Event Hooks
#
# WHAT IS TESTED:
#   HLS output generation during a live stream. Verifies that when the
#   server is configured with HLS output (via the built-in HLS hook or
#   -hls flag), a .m3u8 playlist and .ts segment files are generated.
#
# NOTE: This test requires the server's HLS feature to be enabled.
#   If HLS is not supported or not configured, the test skips.
#
# EXPECTED RESULT:
#   - An .m3u8 playlist file is created
#   - At least one .ts segment file is created
#   - Playlist contains #EXTM3U header
#
# PREREQUISITES:
#   - FFmpeg with libx264, aac
#   - Go 1.21+
#
# USAGE:
#   ./e2e-tests/hooks-hls-conversion.sh
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

TEST_NAME="hooks-hls-conversion"
PORT=$(unique_port "$TEST_NAME")

setup "$TEST_NAME"

HLS_DIR="$TMPDIR/hls-output"
mkdir -p "$HLS_DIR"

# Check if server supports HLS flags
if ! "$BINARY" -help 2>&1 | grep -qi "hls"; then
    echo -e "${YELLOW}SKIP: Server does not appear to support HLS output${NC}"
    teardown
    exit 2
fi

start_server "$PORT" "-log-level" "debug" "-hls" "true" "-hls-dir" "$HLS_DIR"

log_step "Publishing for HLS generation (8s)..."
publish_test_pattern "rtmp://localhost:${PORT}/live/hls-test" 8
sleep 5

# Check for HLS output
M3U8=$(find "$HLS_DIR" -name "*.m3u8" -type f | head -n 1)
TS_COUNT=$(find "$HLS_DIR" -name "*.ts" -type f | wc -l)

if [[ -n "$M3U8" ]]; then
    pass_check "HLS playlist created: $(basename "$M3U8")"
    if grep -q "#EXTM3U" "$M3U8" 2>/dev/null; then
        pass_check "Playlist has #EXTM3U header"
    else
        fail_check "Playlist format" "Missing #EXTM3U header"
    fi
else
    fail_check "HLS playlist created" "No .m3u8 file found in $HLS_DIR"
fi

if [[ "$TS_COUNT" -gt 0 ]]; then
    pass_check "HLS segments created ($TS_COUNT .ts files)"
else
    fail_check "HLS segments created" "No .ts files found"
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

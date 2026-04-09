#!/usr/bin/env bash
# run-all-tests.sh — Run the complete go-rtmp E2E test suite
# Usage: ./run-all-tests.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "============================================"
echo "  go-rtmp — Full E2E Test Suite"
echo "============================================"
echo ""

# Clean up previous test artifacts
rm -rf "$SCRIPT_DIR/.test-tmp" 2>/dev/null || true
rm -rf "$SCRIPT_DIR/logs/test-*.log" 2>/dev/null || true

# Run the E2E test suite
"$SCRIPT_DIR/test-e2e.sh"
EXIT_CODE=$?

echo ""
if [[ $EXIT_CODE -eq 0 ]]; then
    echo "All E2E tests completed successfully."
else
    echo "$EXIT_CODE test(s) failed."
fi

exit $EXIT_CODE

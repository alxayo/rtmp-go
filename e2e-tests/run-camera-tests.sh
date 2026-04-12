#!/usr/bin/env bash
# ============================================================================
# Run only camera E2E tests
#
# These tests require a live camera device (macOS: avfoundation, Linux: v4l2).
# Tests will auto-skip if no camera is detected on the current platform.
#
# USAGE:
#   ./e2e-tests/run-camera-tests.sh           # Run all camera tests
#   ./e2e-tests/run-camera-tests.sh --list    # List camera tests
#   ./e2e-tests/run-camera-tests.sh --filter srt  # Filter by pattern
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

FILTER=""
LIST_ONLY=false

while [[ $# -gt 0 ]]; do
    case "$1" in
        --filter) FILTER="$2"; shift 2 ;;
        --list)   LIST_ONLY=true; shift ;;
        --help|-h)
            echo "Usage: $0 [--filter PATTERN] [--list]"
            echo "  --filter PATTERN   Only run camera tests whose filename contains PATTERN"
            echo "  --list             List camera tests without running them"
            exit 0
            ;;
        *) echo "Unknown option: $1"; exit 1 ;;
    esac
done

# Discover camera test scripts only
TESTS=()
for f in "$SCRIPT_DIR"/camera-*.sh; do
    [[ -f "$f" ]] || continue
    if [[ -n "$FILTER" ]] && [[ "$(basename "$f")" != *"$FILTER"* ]]; then
        continue
    fi
    TESTS+=("$f")
done

echo -e "${BLUE}============================================${NC}"
echo -e "${BLUE}  go-rtmp — Camera E2E Tests${NC}"
echo -e "${BLUE}============================================${NC}"
echo ""
echo "Tests found: ${#TESTS[@]}"

if [[ -n "$FILTER" ]]; then
    echo "Filter: $FILTER"
fi
echo ""

if $LIST_ONLY; then
    for t in "${TESTS[@]}"; do
        echo "  $(basename "$t")"
    done
    exit 0
fi

# Build server once before running tests
build_server

# Clean up previous test artifacts
rm -rf "$SCRIPT_DIR/.test-tmp" 2>/dev/null || true
rm -f "$SCRIPT_DIR/logs/"*.log 2>/dev/null || true

# Run tests
TOTAL=0
PASSED=0
FAILED=0
SKIPPED=0
FAILED_TESTS=()

for test_script in "${TESTS[@]}"; do
    TOTAL=$((TOTAL + 1))
    test_name="$(basename "$test_script" .sh)"

    if bash "$test_script"; then
        PASSED=$((PASSED + 1))
    else
        exit_code=$?
        if [[ $exit_code -eq 2 ]]; then
            SKIPPED=$((SKIPPED + 1))
        else
            FAILED=$((FAILED + 1))
            FAILED_TESTS+=("$test_name")
        fi
    fi
done

# Summary
echo ""
echo -e "${BLUE}=== Camera Test Suite Summary ===${NC}"
echo -e "  Total:   $TOTAL"
echo -e "  ${GREEN}Passed:  $PASSED${NC}"
echo -e "  ${RED}Failed:  $FAILED${NC}"
echo -e "  ${YELLOW}Skipped: $SKIPPED${NC}"

if [[ $FAILED -gt 0 ]]; then
    echo ""
    echo -e "${RED}Failed tests:${NC}"
    for t in "${FAILED_TESTS[@]}"; do
        echo -e "  ${RED}✗ $t${NC}"
    done
    echo ""
    echo -e "${RED}Check logs in $SCRIPT_DIR/logs/${NC}"
    exit 1
else
    echo ""
    echo -e "${GREEN}All camera tests passed!${NC}"
    exit 0
fi

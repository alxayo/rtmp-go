#!/usr/bin/env bash
# ============================================================================
# Run all E2E tests WITHOUT camera tests (for CI/headless environments)
#
# USAGE:
#   ./e2e-tests/run-all-no-camera.sh [--filter PATTERN]
# ============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Export environment variable to skip camera tests
export SKIP_CAMERA_TESTS=1

# Run all tests via run-all.sh with the environment variable set
"$SCRIPT_DIR/run-all.sh" "$@"

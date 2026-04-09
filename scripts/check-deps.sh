#!/usr/bin/env bash
# check-deps.sh — Check availability of required tools for go-rtmp scripts
# Usage: ./check-deps.sh
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

missing=0

check_tool() {
    local name="$1"
    local cmd="$2"
    local version_flag="${3:---version}"

    if command -v "$cmd" &>/dev/null; then
        local ver
        ver=$("$cmd" $version_flag 2>&1 | head -1) || ver="(version unknown)"
        echo -e "${GREEN}✓${NC} $name: $ver"
    else
        echo -e "${RED}✗${NC} $name: not found in PATH"
        missing=$((missing + 1))
    fi
}

echo "=== go-rtmp Dependency Check ==="
echo ""

# Check ffmpeg tools
check_tool "ffmpeg"  "ffmpeg"  "-version"
check_tool "ffplay"  "ffplay"  "-version"
check_tool "ffprobe" "ffprobe" "-version"

# Check Go compiler
check_tool "go" "go" "version"

# Check rtmp-server binary
BINARY="$PROJECT_ROOT/rtmp-server"
if [[ "$(uname -s)" == *MINGW* ]] || [[ "$(uname -s)" == *MSYS* ]] || [[ "$(uname -s)" == *CYGWIN* ]]; then
    BINARY="$PROJECT_ROOT/rtmp-server.exe"
fi

if [[ -f "$BINARY" ]]; then
    ver=$("$BINARY" -version 2>&1 || echo "(version unknown)")
    echo -e "${GREEN}✓${NC} rtmp-server: $ver (at $BINARY)"
else
    echo -e "${YELLOW}!${NC} rtmp-server: binary not found at $BINARY"
    echo "  Build with: go build -o rtmp-server ./cmd/rtmp-server"
    # Not counted as fatal — scripts can build it
fi

echo ""
if [[ $missing -gt 0 ]]; then
    echo -e "${RED}$missing required tool(s) missing. Install them before running tests.${NC}"
    exit 1
else
    echo -e "${GREEN}All required tools are available.${NC}"
    exit 0
fi

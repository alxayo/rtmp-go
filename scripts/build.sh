#!/usr/bin/env bash
# build.sh - Build go-rtmp binaries locally (Linux/macOS)
# Usage: ./build.sh [OPTIONS]
#   --target server|client|all   Which binary to build (default: all)
#   --output DIR                 Output directory (default: ./bin)
#   --clean                      Remove existing binaries before building
#   --race                       Build with race detector
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

TARGET="all"
OUTPUT_DIR="$PROJECT_ROOT/bin"
CLEAN=false
RACE=false

while [[ $# -gt 0 ]]; do
    case "$1" in
        --target) TARGET="$2"; shift 2 ;;
        --output) OUTPUT_DIR="$2"; shift 2 ;;
        --clean)  CLEAN=true; shift ;;
        --race)   RACE=true; shift ;;
        -h|--help)
            cat <<EOF
Usage: ./build.sh [OPTIONS]

Options:
  --target server|client|all   Which binary to build (default: all)
  --output DIR                 Output directory (default: ./bin)
  --clean                      Remove existing binaries before building
  --race                       Build with race detector
  -h, --help                   Show this help
EOF
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

case "$TARGET" in
    server|client|all) ;;
    *)
        echo "Invalid --target value: $TARGET (expected server|client|all)"
        exit 1
        ;;
esac

if ! command -v go >/dev/null 2>&1; then
    echo "ERROR: Go is not installed or not on PATH."
    exit 1
fi

mkdir -p "$OUTPUT_DIR"

build_one() {
    local name="$1"
    local pkg="$2"
    local out="$OUTPUT_DIR/$name"

    if [[ "$CLEAN" == "true" ]]; then
        rm -f "$out"
    fi

    local args=("build" "-trimpath")
    if [[ "$RACE" == "true" ]]; then
        args+=("-race")
    fi
    args+=("-o" "$out" "$pkg")

    echo "Building $name..."
    (cd "$PROJECT_ROOT" && go "${args[@]}")
    echo "Built: $out"
}

echo "============================================"
echo "  go-rtmp - Local Build"
echo "============================================"
echo "  Target: $TARGET"
echo "  Output: $OUTPUT_DIR"
echo ""

if [[ "$TARGET" == "server" || "$TARGET" == "all" ]]; then
    build_one "rtmp-server" "./cmd/rtmp-server"
fi

if [[ "$TARGET" == "client" || "$TARGET" == "all" ]]; then
    build_one "rtmp-client" "./cmd/rtmp-client"
fi

echo ""
echo "Build completed successfully."

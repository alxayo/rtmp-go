#!/usr/bin/env bash
# serve-docs.sh — Build and serve the go-rtmp documentation site locally.
# Usage: ./serve-docs.sh [--build-only] [--port 1313]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SITE_DIR="$SCRIPT_DIR/site"
PORT=1313
BUILD_ONLY=false

for arg in "$@"; do
  case "$arg" in
    --build-only) BUILD_ONLY=true ;;
    --port)       shift; PORT="${1:-1313}" ;;
    [0-9]*)       PORT="$arg" ;;
  esac
  shift 2>/dev/null || true
done

# --- Check Hugo ---
if ! command -v hugo &>/dev/null; then
  echo "Hugo not found. Installing..."
  if command -v brew &>/dev/null; then
    brew install hugo
  elif command -v apt-get &>/dev/null; then
    sudo apt-get update && sudo apt-get install -y hugo
  elif command -v snap &>/dev/null; then
    sudo snap install hugo --channel=extended
  else
    echo "ERROR: Cannot auto-install Hugo. Install it manually: https://gohugo.io/installation/"
    exit 1
  fi
fi

echo "Using $(hugo version)"

# --- Init submodule (theme) ---
if [ ! -f "$SITE_DIR/themes/hugo-book/theme.toml" ]; then
  echo "Initializing hugo-book theme submodule..."
  git -C "$SCRIPT_DIR" submodule update --init --recursive
fi

# --- Build ---
echo "Building site..."
hugo --source "$SITE_DIR" --gc --minify
echo "Build complete: $SITE_DIR/public/"

if [ "$BUILD_ONLY" = true ]; then
  echo "Done (build-only mode)."
  exit 0
fi

# --- Serve ---
echo ""
echo "Starting local server on http://localhost:$PORT/"
echo "Press Ctrl+C to stop."
hugo server --source "$SITE_DIR" --port "$PORT" --buildDrafts --navigateToChanged

#!/usr/bin/env bash
# run-secure-macos.sh
# Generates self-signed TLS certificates (if missing) and starts RTMP + RTMPS.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "This script is intended for macOS." >&2
  echo "Use run-secure-linux.sh on Linux." >&2
  exit 1
fi

cd "$PROJECT_ROOT"

"$SCRIPT_DIR/start-server.sh" \
  --mode both \
  --port 1935 \
  --tls-port 1936 \
  --log-level debug \
  --foreground

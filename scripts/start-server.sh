#!/usr/bin/env bash
# start-server.sh — Start the go-rtmp server with configurable options
# Usage: ./start-server.sh [OPTIONS]
#   --mode plain|tls|both    Transport mode (default: plain)
#   --enable-hls             Enable HLS conversion hook on publish
#   --enable-auth            Enable token auth with test tokens
#   --port PORT              RTMP listen port (default: 1935)
#   --tls-port PORT          RTMPS listen port (default: 1936)
#   --log-level LEVEL        Log level: debug|info|warn|error (default: info)
#   --foreground             Run in foreground (default: background)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Defaults
MODE="plain"
ENABLE_HLS=false
ENABLE_AUTH=false
PORT=1935
TLS_PORT=1936
LOG_LEVEL="info"
FOREGROUND=false

# Parse arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
        --mode)       MODE="$2"; shift 2 ;;
        --enable-hls) ENABLE_HLS=true; shift ;;
        --enable-auth) ENABLE_AUTH=true; shift ;;
        --port)       PORT="$2"; shift 2 ;;
        --tls-port)   TLS_PORT="$2"; shift 2 ;;
        --log-level)  LOG_LEVEL="$2"; shift 2 ;;
        --foreground) FOREGROUND=true; shift ;;
        *) echo "Unknown option: $1"; exit 1 ;;
    esac
done

# Validate mode
case "$MODE" in
    plain|tls|both) ;;
    *) echo "Invalid mode: $MODE (expected plain|tls|both)"; exit 1 ;;
esac

# Check dependencies
echo "Checking dependencies..."
"$SCRIPT_DIR/check-deps.sh" || exit 1
echo ""

# Build server if needed
BINARY="$PROJECT_ROOT/rtmp-server"
if [[ "$(uname -s)" == *MINGW* ]] || [[ "$(uname -s)" == *MSYS* ]]; then
    BINARY="$PROJECT_ROOT/rtmp-server.exe"
fi

if [[ ! -f "$BINARY" ]] || [[ -n "$(find "$PROJECT_ROOT/cmd" "$PROJECT_ROOT/internal" -newer "$BINARY" -name '*.go' 2>/dev/null | head -1)" ]]; then
    echo "Building rtmp-server..."
    cd "$PROJECT_ROOT"
    go build -o "$BINARY" ./cmd/rtmp-server
    echo "Built: $BINARY"
fi

# Generate certs if TLS mode
if [[ "$MODE" == "tls" || "$MODE" == "both" ]]; then
    CERT_FILE="$SCRIPT_DIR/.certs/cert.pem"
    KEY_FILE="$SCRIPT_DIR/.certs/key.pem"
    if [[ ! -f "$CERT_FILE" || ! -f "$KEY_FILE" ]]; then
        echo "Generating TLS certificates..."
        "$SCRIPT_DIR/generate-certs.sh"
    fi
fi

# Build command arguments
ARGS=("-listen" ":${PORT}" "-log-level" "$LOG_LEVEL")

case "$MODE" in
    tls|both)
        ARGS+=("-tls-listen" ":${TLS_PORT}")
        ARGS+=("-tls-cert" "$SCRIPT_DIR/.certs/cert.pem")
        ARGS+=("-tls-key" "$SCRIPT_DIR/.certs/key.pem")
        ;;
esac

if [[ "$ENABLE_HLS" == "true" ]]; then
    ARGS+=("-hook-script" "publish_start=$SCRIPT_DIR/on-publish-hls.sh")
fi

if [[ "$ENABLE_AUTH" == "true" ]]; then
    ARGS+=("-auth-mode" "token")
    ARGS+=("-auth-token" "live/test=secret123")
    ARGS+=("-auth-token" "live/secure=mytoken456")
fi

# Create log directory
LOG_DIR="$SCRIPT_DIR/logs"
mkdir -p "$LOG_DIR"
LOG_FILE="$LOG_DIR/rtmp-server.log"

echo "=== Starting go-rtmp Server ==="
echo "  Mode:     $MODE"
echo "  RTMP:     :$PORT"
[[ "$MODE" == "tls" || "$MODE" == "both" ]] && echo "  RTMPS:    :$TLS_PORT"
[[ "$ENABLE_HLS" == "true" ]] && echo "  HLS:      enabled (hook on publish)"
[[ "$ENABLE_AUTH" == "true" ]] && echo "  Auth:     token mode"
echo "  Log:      $LOG_FILE"
echo ""

if [[ "$FOREGROUND" == "true" ]]; then
    exec "$BINARY" "${ARGS[@]}" 2>&1 | tee "$LOG_FILE"
else
    "$BINARY" "${ARGS[@]}" > "$LOG_FILE" 2>&1 &
    SERVER_PID=$!
    echo "$SERVER_PID" > "$SCRIPT_DIR/.server.pid"

    # Wait for server to be ready
    echo "Waiting for server to start (PID: $SERVER_PID)..."
    for i in $(seq 1 15); do
        if grep -q "server listening\|server started" "$LOG_FILE" 2>/dev/null; then
            echo "Server started successfully."
            echo "  PID: $SERVER_PID"
            echo "  Stop with: kill $SERVER_PID"
            exit 0
        fi
        if ! kill -0 "$SERVER_PID" 2>/dev/null; then
            echo "ERROR: Server process exited unexpectedly."
            cat "$LOG_FILE"
            exit 1
        fi
        sleep 1
    done

    echo "WARNING: Server may not be ready yet. Check $LOG_FILE"
    exit 0
fi

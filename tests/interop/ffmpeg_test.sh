#!/usr/bin/env bash
# FFmpeg / ffplay interoperability automated test script (T054) - Bash version
# Cross-platform (Linux/macOS) counterpart to PowerShell script.
# Requires: bash, go toolchain, ffmpeg, ffplay
# Exit code: number of failed tests (0 == success)
set -euo pipefail

INCLUDE=${INCLUDE:-PublishOnly,PublishAndPlay,Concurrency,Recording}
FFMPEG_EXE=${FFMPEG_EXE:-ffmpeg}
FFPLAY_EXE=${FFPLAY_EXE:-ffplay}
SERVER_PORT=${SERVER_PORT:-1935}
SERVER_FLAGS=${SERVER_FLAGS:-}
KEEP_WORK_DIR=${KEEP_WORK_DIR:-0}
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")"/../../.. && pwd)"
SERVER_BIN="$ROOT_DIR/rtmp-server"
WORK_DIR="${TMPDIR:-/tmp}/go-rtmp-interop-$(uuidgen 2>/dev/null || cat /proc/sys/kernel/random/uuid)"
RECORDING_DIR="$WORK_DIR/recordings"
FAILURES=()

log() { echo -e "\033[36m[$(date +%H:%M:%S)]\033[0m $*"; }
fail() { echo -e "\033[31mERROR:\033[0m $*" >&2; }

need_cmd() { command -v "$1" >/dev/null 2>&1 || { fail "Missing required command: $1"; exit 1; }; }
need_cmd go; need_cmd "$FFMPEG_EXE"; need_cmd "$FFPLAY_EXE"; need_cmd mktemp

mkdir -p "$WORK_DIR" "$RECORDING_DIR"
log "Work dir: $WORK_DIR"

if [[ ! -x "$SERVER_BIN" ]]; then
  log "Building server binary..."
  (cd "$ROOT_DIR" && go build -o rtmp-server ./cmd/rtmp-server)
fi

PIDS=()
cleanup() {
  for p in "${PIDS[@]:-}"; do
    if kill -0 "$p" 2>/dev/null; then kill "$p" 2>/dev/null || true; fi
  done
  if [[ ${#FAILURES[@]} -eq 0 && $KEEP_WORK_DIR -eq 0 ]]; then rm -rf "$WORK_DIR"; else log "Keeping work dir: $WORK_DIR"; fi
}
trap cleanup EXIT INT TERM

start_server() {
  local extra="$1"
  log "Starting server on :$SERVER_PORT"
  ("$SERVER_BIN" -listen ":$SERVER_PORT" $extra $SERVER_FLAGS & echo $! >"$WORK_DIR/server.pid")
  sleep 0.6
  PIDS+=($(cat "$WORK_DIR/server.pid"))
}

stop_server() {
  for p in "${PIDS[@]}"; do kill "$p" 2>/dev/null || true; done
  PIDS=()
}

TEST_MP4="$WORK_DIR/test.mp4"
if [[ ! -f "$TEST_MP4" ]]; then
  log "Generating synthetic test.mp4"
  "$FFMPEG_EXE" -hide_banner -loglevel error -f lavfi -i testsrc=size=640x360:rate=30 -f lavfi -i sine=frequency=1000:sample_rate=44100 -t 1 -c:v libx264 -pix_fmt yuv420p -c:a aac "$TEST_MP4"
fi

publish() { # usage: publish rtmp://... input.mp4
  local url="$1"; local in="$2"
  log "Publishing -> $url"
  "$FFMPEG_EXE" -hide_banner -loglevel error -re -i "$in" -c copy -f flv "$url"
}

publish_bg() { # usage: publish_bg url input
  local url="$1"; local in="$2"
  ( "$FFMPEG_EXE" -hide_banner -loglevel error -re -i "$in" -c copy -f flv "$url" ) &
  PIDS+=($!)
  sleep 0.2
}

play_headless() { # usage: play_headless rtmp://...
  local url="$1"; log "Playing -> $url"
  "$FFPLAY_EXE" -hide_banner -loglevel error -autoexit -nodisp "$url" >/dev/null 2>&1 || return 1
}

run_test() { # name command...
  local name="$1"; shift
  log "=== $name ==="
  if ! "$@"; then
    fail "$name failed"
    FAILURES+=("$name")
  fi
  stop_server
}

Test_PublishOnly() {
  start_server ""
  publish "rtmp://localhost:$SERVER_PORT/live/test" "$TEST_MP4"
}

Test_PublishAndPlay() {
  start_server ""
  publish_bg "rtmp://localhost:$SERVER_PORT/live/play1" "$TEST_MP4"
  sleep 0.8
  play_headless "rtmp://localhost:$SERVER_PORT/live/play1"
}

Test_Concurrency() {
  start_server ""
  publish_bg "rtmp://localhost:$SERVER_PORT/live/a" "$TEST_MP4"
  publish_bg "rtmp://localhost:$SERVER_PORT/live/b" "$TEST_MP4"
  sleep 1
  play_headless "rtmp://localhost:$SERVER_PORT/live/a"
  play_headless "rtmp://localhost:$SERVER_PORT/live/b"
}

Test_Recording() {
  start_server "-record-all -record-dir $RECORDING_DIR"
  publish "rtmp://localhost:$SERVER_PORT/live/rec1" "$TEST_MP4"
  stop_server
  local flv
  flv=$(ls -1t "$RECORDING_DIR"/*.flv 2>/dev/null | head -n1 || true)
  [[ -n "$flv" ]] || { fail "No FLV recording found"; return 1; }
  log "Verifying recording: $(basename "$flv")"
  "$FFMPEG_EXE" -hide_banner -loglevel error -i "$flv" -f null - >/dev/null 2>&1 || return 1
}

IFS=',' read -ra SELECTED <<<"$INCLUDE"
for t in "${SELECTED[@]}"; do
  case "$t" in
    PublishOnly) run_test PublishOnly Test_PublishOnly ;;
    PublishAndPlay) run_test PublishAndPlay Test_PublishAndPlay ;;
    Concurrency) run_test Concurrency Test_Concurrency ;;
    Recording) run_test Recording Test_Recording ;;
    *) log "Skipping unknown test '$t'" ;;
  esac
done

if [[ ${#FAILURES[@]} -gt 0 ]]; then
  fail "FAILED: ${FAILURES[*]}"
  exit ${#FAILURES[@]}
fi
log "All interop tests passed."
exit 0

# Manual Testing Guide for go-rtmp E2E Tests

This guide provides exact FFmpeg, ffprobe, ffplay, and rtmp-go commands you can run manually to verify functionality without the test automation framework.

## Prerequisites

```bash
# Build the rtmp-server binary
cd /path/to/rtmp-go
go build -o rtmp-server ./cmd/rtmp-server

# Verify tools are available
ffmpeg -version       # FFmpeg (with libx264, libx265, aac, opus, etc.)
ffprobe -version      # FFprobe (included with FFmpeg)
ffplay -version       # FFplay (included with FFmpeg)
```

## Quick Reference: Common Commands

### Start Server
```bash
# Basic RTMP listener on port 1935
./rtmp-server -listen localhost:1935 -log-level debug

# RTMP + RTMPS (TLS)
./rtmp-server -listen localhost:1935 -tls-listen localhost:9935 \
  -tls-cert /path/to/cert.pem -tls-key /path/to/key.pem

# With SRT listener
./rtmp-server -listen localhost:1935 -srt-listen localhost:9000

# With recording enabled
./rtmp-server -listen localhost:1935 -record-all true -record-dir ./recordings

# With authentication
./rtmp-server -listen localhost:1935 -auth-mode token \
  -auth-token "live/stream1=secret123"

# With metrics endpoint
./rtmp-server -listen localhost:1935 -metrics-addr localhost:8080

# With relay
./rtmp-server -listen localhost:1935 \
  -relay-to "rtmp://destination-server:1935"

# With event hooks
./rtmp-server -listen localhost:1935 \
  -hook-script "/path/to/hook.sh" \
  -hook-webhook "http://localhost:8000/webhook"
```

### Publish (FFmpeg)

```bash
# Publish H.264+AAC test pattern (5 seconds, real-time)
ffmpeg -hide_banner -loglevel error -re \
  -f lavfi -i "testsrc=duration=5:size=320x240:rate=25" \
  -f lavfi -i "sine=frequency=440:duration=5" \
  -c:v libx264 -preset ultrafast -tune zerolatency \
  -c:a aac -b:a 64k \
  -f flv "rtmp://localhost:1935/live/stream1"

# Publish H.265 (Enhanced RTMP)
ffmpeg -hide_banner -loglevel error -re \
  -f lavfi -i "testsrc=duration=5:size=320x240:rate=25" \
  -f lavfi -i "sine=frequency=440:duration=5" \
  -c:v libx265 -preset ultrafast \
  -c:a aac -b:a 64k \
  -f flv "rtmp://localhost:1935/live/stream1"

# Publish audio-only
ffmpeg -hide_banner -loglevel error -re \
  -f lavfi -i "sine=frequency=440:duration=5" \
  -c:a aac -b:a 64k \
  -f flv "rtmp://localhost:1935/live/audio-stream"

# Publish with authentication token
ffmpeg -hide_banner -loglevel error -re \
  -f lavfi -i "testsrc=duration=5:size=320x240:rate=25" \
  -f lavfi -i "sine=frequency=440:duration=5" \
  -c:v libx264 -preset ultrafast -tune zerolatency \
  -c:a aac -b:a 64k \
  -f flv "rtmp://localhost:1935/live/stream1?token=secret123"
```

### Subscribe/Capture (FFmpeg)

```bash
# Capture stream to FLV file (5 seconds)
ffmpeg -hide_banner -loglevel error -t 5 \
  -i "rtmp://localhost:1935/live/stream1" \
  -c copy \
  capture.flv

# Capture via ffplay (real-time display)
ffplay -hide_banner -autoexit "rtmp://localhost:1935/live/stream1"

# Capture with authentication token
ffmpeg -hide_banner -loglevel error -t 5 \
  -i "rtmp://localhost:1935/live/stream1?token=secret123" \
  -c copy \
  capture.flv
```

### Verify Files (FFprobe)

```bash
# Show all streams
ffprobe -hide_banner -show_streams capture.flv

# Get video codec
ffprobe -v error -select_streams v:0 -show_entries stream=codec_name \
  -of csv=p=0 capture.flv

# Get audio codec
ffprobe -v error -select_streams a:0 -show_entries stream=codec_name \
  -of csv=p=0 capture.flv

# Get duration
ffprobe -v error -show_entries format=duration -of csv=p=0 capture.flv

# Full decode test (verify file is decodable)
ffmpeg -v error -i capture.flv -f null -
```

### Query Metrics (curl)

```bash
# Get expvar metrics
curl -s "http://localhost:8080/debug/vars" | jq '.rtmp_connections_total'

# Pretty-print all metrics
curl -s "http://localhost:8080/debug/vars" | jq '.'
```

---

## Per-Test Manual Commands

### RTMP: publish-h264

**What**: Basic RTMP publish with H.264+AAC

**Terminal 1 - Start Server**:
```bash
./rtmp-server -listen localhost:1935 -log-level debug
```

**Terminal 2 - Publish**:
```bash
ffmpeg -hide_banner -loglevel error -re \
  -f lavfi -i "testsrc=duration=5:size=320x240:rate=25" \
  -f lavfi -i "sine=frequency=440:duration=5" \
  -c:v libx264 -preset ultrafast -tune zerolatency \
  -c:a aac -b:a 64k \
  -f flv "rtmp://localhost:1935/live/test"
```

**Verify**: Check server log for `"connection registered"` and `"publish"` messages.

---

### RTMP: publish-play-h264

**What**: Full RTMP publish→subscribe cycle with capture

**Terminal 1 - Start Server**:
```bash
./rtmp-server -listen localhost:1935 -log-level debug
```

**Terminal 2 - Publish** (background):
```bash
ffmpeg -hide_banner -loglevel error -re \
  -f lavfi -i "testsrc=duration=8:size=320x240:rate=25" \
  -f lavfi -i "sine=frequency=440:duration=8" \
  -c:v libx264 -preset ultrafast -tune zerolatency \
  -c:a aac -b:a 64k \
  -f flv "rtmp://localhost:1935/live/test" &
PUB_PID=$!
sleep 2
```

**Terminal 2 - Subscribe/Capture** (same terminal):
```bash
# Capture for 5 seconds
ffmpeg -hide_banner -loglevel error -t 5 \
  -i "rtmp://localhost:1935/live/test" \
  -c copy capture.flv

# Wait for publisher to finish
wait $PUB_PID
```

**Verify**:
```bash
# Check video codec
ffprobe -v error -select_streams v:0 -show_entries stream=codec_name \
  -of csv=p=0 capture.flv
# Expected: h264

# Check audio codec
ffprobe -v error -select_streams a:0 -show_entries stream=codec_name \
  -of csv=p=0 capture.flv
# Expected: aac

# Check duration
ffprobe -v error -show_entries format=duration -of csv=p=0 capture.flv
# Expected: ~5 seconds
```

---

### RTMP: publish-audio-only

**What**: Audio-only stream (no video)

**Terminal 1 - Start Server**:
```bash
./rtmp-server -listen localhost:1935 -log-level debug
```

**Terminal 2 - Publish**:
```bash
ffmpeg -hide_banner -loglevel error -re \
  -f lavfi -i "sine=frequency=440:duration=5" \
  -c:a aac -b:a 64k \
  -f flv "rtmp://localhost:1935/live/audio-stream"
```

**Verify**:
```bash
ffmpeg -hide_banner -loglevel error -t 3 \
  -i "rtmp://localhost:1935/live/audio-stream" \
  -c copy capture.flv

ffprobe -v error -show_streams capture.flv
# Expected: only audio stream (no video)
```

---

### RTMP: concurrent-subscribers

**What**: One publisher, three simultaneous subscribers

**Terminal 1 - Start Server**:
```bash
./rtmp-server -listen localhost:1935 -log-level debug
```

**Terminal 2 - Publish** (background):
```bash
ffmpeg -hide_banner -loglevel error -re \
  -f lavfi -i "testsrc=duration=10:size=320x240:rate=25" \
  -f lavfi -i "sine=frequency=440:duration=10" \
  -c:v libx264 -preset ultrafast -tune zerolatency \
  -c:a aac -b:a 64k \
  -f flv "rtmp://localhost:1935/live/fanout" &
PUB_PID=$!
sleep 2
```

**Terminal 3, 4, 5 - Subscribe** (in separate terminals or background):
```bash
# Subscriber 1
ffmpeg -hide_banner -loglevel error -t 6 \
  -i "rtmp://localhost:1935/live/fanout" \
  -c copy sub1.flv &

# Subscriber 2
ffmpeg -hide_banner -loglevel error -t 6 \
  -i "rtmp://localhost:1935/live/fanout" \
  -c copy sub2.flv &

# Subscriber 3
ffmpeg -hide_banner -loglevel error -t 6 \
  -i "rtmp://localhost:1935/live/fanout" \
  -c copy sub3.flv &

# Wait for all captures to finish
wait

# Wait for publisher
wait $PUB_PID
```

**Verify**:
```bash
for f in sub{1,2,3}.flv; do
  echo "=== $f ==="
  ffprobe -v error -show_entries format=size -of csv=p=0 "$f"
done
# All should have similar size (all received same stream)
```

---

### RTMPS: publish-play

**What**: RTMP over TLS (RTMPS) publish and subscribe

**Prerequisites - Generate TLS Certificate** (one-time):
```bash
mkdir -p .certs
openssl req -x509 -newkey rsa:2048 -keyout .certs/key.pem \
  -out .certs/cert.pem -days 365 -nodes \
  -subj "/CN=localhost"
```

**Terminal 1 - Start Server with TLS**:
```bash
./rtmp-server -listen localhost:1935 -tls-listen localhost:9935 \
  -tls-cert .certs/cert.pem -tls-key .certs/key.pem \
  -log-level debug
```

**Terminal 2 - Publish via TLS**:
```bash
ffmpeg -hide_banner -loglevel error -re \
  -f lavfi -i "testsrc=duration=5:size=320x240:rate=25" \
  -f lavfi -i "sine=frequency=440:duration=5" \
  -c:v libx264 -preset ultrafast -tune zerolatency \
  -c:a aac -b:a 64k \
  -f flv "rtmps://localhost:9935/live/tls-stream"
```

**Terminal 3 - Subscribe via TLS**:
```bash
# Note: FFmpeg/FFplay may require -rtmp_swfurl or -rtmp_flashver to verify cert
ffmpeg -hide_banner -loglevel error -t 5 \
  -rtmp_swfverify 0 \
  -i "rtmps://localhost:9935/live/tls-stream" \
  -c copy capture.flv
```

---

### Enhanced RTMP: H.265

**What**: H.265 (HEVC) via Enhanced RTMP (E-RTMP v2)

**Terminal 1 - Start Server**:
```bash
./rtmp-server -listen localhost:1935 -log-level debug
```

**Terminal 2 - Publish H.265**:
```bash
ffmpeg -hide_banner -loglevel error -re \
  -f lavfi -i "testsrc=duration=5:size=320x240:rate=25" \
  -f lavfi -i "sine=frequency=440:duration=5" \
  -c:v libx265 -preset ultrafast \
  -c:a aac -b:a 64k \
  -f flv "rtmp://localhost:1935/live/h265-stream"
```

**Terminal 3 - Capture**:
```bash
ffmpeg -hide_banner -loglevel error -t 5 \
  -i "rtmp://localhost:1935/live/h265-stream" \
  -c copy capture.flv
```

**Verify**:
```bash
ffprobe -v error -select_streams v:0 -show_entries stream=codec_name \
  -of csv=p=0 capture.flv
# Expected: hevc
```

---

### SRT: publish-h264

**What**: SRT ingest with H.264

**Terminal 1 - Start Server with SRT**:
```bash
./rtmp-server -listen localhost:1935 -srt-listen localhost:9000 \
  -log-level debug
```

**Terminal 2 - Publish via SRT** (requires SRT support):
```bash
# Using ffmpeg with SRT (if libsrt available)
ffmpeg -hide_banner -loglevel error -re \
  -f lavfi -i "testsrc=duration=5:size=320x240:rate=25" \
  -f lavfi -i "sine=frequency=440:duration=5" \
  -c:v libx264 -preset ultrafast -tune zerolatency \
  -c:a aac -b:a 64k \
  -f mpegts "srt://localhost:9000?streamid=publish:live/srt-stream&latency=200000"
```

**Terminal 3 - Subscribe via RTMP** (cross-protocol):
```bash
ffmpeg -hide_banner -loglevel error -t 5 \
  -i "rtmp://localhost:1935/live/srt-stream" \
  -c copy capture.flv
```

---

### Recording: FLV H.264

**What**: Record published streams to FLV files

**Terminal 1 - Start Server with Recording**:
```bash
mkdir -p recordings
./rtmp-server -listen localhost:1935 \
  -record-all true -record-dir ./recordings \
  -log-level debug
```

**Terminal 2 - Publish**:
```bash
ffmpeg -hide_banner -loglevel error -re \
  -f lavfi -i "testsrc=duration=5:size=320x240:rate=25" \
  -f lavfi -i "sine=frequency=440:duration=5" \
  -c:v libx264 -preset ultrafast -tune zerolatency \
  -c:a aac -b:a 64k \
  -f flv "rtmp://localhost:1935/live/rec-stream"
```

**Verify**:
```bash
ls -lh recordings/
# You should see a .flv file with timestamp in name
# e.g., live_rec-stream_20260411_120000.flv

ffprobe -v error -show_entries stream=codec_name -of csv=p=0 \
  recordings/live_rec-stream_*.flv
# Expected: h264, aac
```

---

### Auth: Token Publish Allowed

**What**: Token-based authentication for publish

**Terminal 1 - Start Server with Auth**:
```bash
./rtmp-server -listen localhost:1935 \
  -auth-mode token -auth-token "live/auth-test=secret123" \
  -log-level debug
```

**Terminal 2 - Publish with Correct Token**:
```bash
ffmpeg -hide_banner -loglevel error -re \
  -f lavfi -i "testsrc=duration=5:size=320x240:rate=25" \
  -f lavfi -i "sine=frequency=440:duration=5" \
  -c:v libx264 -preset ultrafast -tune zerolatency \
  -c:a aac -b:a 64k \
  -f flv "rtmp://localhost:1935/live/auth-test?token=secret123"
```

**Verify**: Check server log for `"publish authenticated"` message.

---

### Auth: Token Publish Rejected

**What**: Token-based auth with wrong token

**Terminal 1 - Start Server with Auth**:
```bash
./rtmp-server -listen localhost:1935 \
  -auth-mode token -auth-token "live/auth-test=secret123" \
  -log-level debug
```

**Terminal 2 - Publish with Wrong Token**:
```bash
ffmpeg -hide_banner -loglevel error -re \
  -f lavfi -i "testsrc=duration=5:size=320x240:rate=25" \
  -f lavfi -i "sine=frequency=440:duration=5" \
  -c:v libx264 -preset ultrafast -tune zerolatency \
  -c:a aac -b:a 64k \
  -f flv "rtmp://localhost:1935/live/auth-test?token=wrongtoken"
```

**Verify**: Check server log for `"auth_failed"` or connection rejection.

---

### Relay: Single Destination

**What**: Relay stream to another RTMP server

**Terminal 1 - Start Primary Server**:
```bash
./rtmp-server -listen localhost:1935 \
  -relay-to "rtmp://localhost:1936" \
  -log-level debug
```

**Terminal 2 - Start Destination Server**:
```bash
./rtmp-server -listen localhost:1936 -log-level debug
```

**Terminal 3 - Publish to Primary**:
```bash
ffmpeg -hide_banner -loglevel error -re \
  -f lavfi -i "testsrc=duration=5:size=320x240:rate=25" \
  -f lavfi -i "sine=frequency=440:duration=5" \
  -c:v libx264 -preset ultrafast -tune zerolatency \
  -c:a aac -b:a 64k \
  -f flv "rtmp://localhost:1935/live/relay-stream"
```

**Terminal 4 - Subscribe from Destination**:
```bash
ffmpeg -hide_banner -loglevel error -t 5 \
  -i "rtmp://localhost:1936/live/relay-stream" \
  -c copy capture.flv
```

---

### Metrics: Expvar Counters

**What**: Query expvar metrics endpoint

**Terminal 1 - Start Server with Metrics**:
```bash
./rtmp-server -listen localhost:1935 \
  -metrics-addr localhost:8080 \
  -log-level debug
```

**Terminal 2 - Publish**:
```bash
ffmpeg -hide_banner -loglevel error -re \
  -f lavfi -i "testsrc=duration=5:size=320x240:rate=25" \
  -f lavfi -i "sine=frequency=440:duration=5" \
  -c:v libx264 -preset ultrafast -tune zerolatency \
  -c:a aac -b:a 64k \
  -f flv "rtmp://localhost:1935/live/metric-stream" &
sleep 2
```

**Terminal 2 - Query Metrics**:
```bash
curl -s "http://localhost:8080/debug/vars" | jq '.rtmp_connections_total'
# Should show > 0 after publish starts
```

---

## Notes

- **Real-time streaming**: All examples use `-re` flag to limit encoding to real-time speed. Remove it to encode as fast as possible.
- **Test patterns**: `testsrc` generates test video. Replace with `-i input.mp4` to use actual files.
- **Preset levels**: `ultrafast` for tests, `faster`/`fast` for quality.
- **Timing**: Tests assume 2-3 second startup latency. Adjust delays as needed.
- **Audio**: `sine` generates test audio. Replace with `-i` microphone/file input.
- **Verify files**: Always check codecs and duration with ffprobe after capture.


# Getting Started

This guide walks you through building, running, and testing the go-rtmp server from scratch.

## Prerequisites

- **Go 1.21+** installed ([download](https://go.dev/dl/))
- **FFmpeg** installed (provides `ffmpeg` and `ffplay` for testing)
- Optionally: **OBS Studio** for live streaming from a camera/screen

## Build

```bash
cd go-rtmp
go build -o rtmp-server ./cmd/rtmp-server
```

On Windows this produces `rtmp-server.exe`.

## Run

### Basic Server

```bash
./rtmp-server -listen :1935 -log-level info
```

The server is now accepting RTMP connections on port 1935.

### With Recording

```bash
mkdir -p recordings
./rtmp-server -listen :1935 -log-level info -record-all true -record-dir ./recordings
```

Every published stream will be saved as an FLV file in the `recordings/` directory.

### With Relay (Multi-Destination)

```bash
./rtmp-server -listen :1935 -relay-to rtmp://cdn1.example.com/live/key -relay-to rtmp://cdn2.example.com/live/key
```

The server will forward all incoming media to the specified destinations.

### With Event Hooks

```bash
# Log all events as JSON to stderr (for log pipelines)
./rtmp-server -listen :1935 -hook-stdio-format json

# Call a webhook when a stream starts publishing
./rtmp-server -listen :1935 -hook-webhook "publish_start=https://api.example.com/on-publish"

# Run a script when a client connects
./rtmp-server -listen :1935 -hook-script "connection_accept=/opt/scripts/on-connect.sh"

# Combine multiple hooks
./rtmp-server -listen :1935 \
  -hook-stdio-format json \
  -hook-webhook "publish_start=https://api.example.com/on-publish" \
  -hook-script "connection_accept=/opt/scripts/on-connect.sh"
```

Available event types: `connection_accept`, `connection_close`, `publish_start`, `play_start`, `codec_detected`.

### All CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-listen` | `:1935` | TCP address to listen on |
| `-log-level` | `info` | Log verbosity: `debug`, `info`, `warn`, `error` |
| `-record-all` | `false` | Record all published streams to FLV files |
| `-record-dir` | `recordings` | Directory for FLV recordings |
| `-chunk-size` | `4096` | Outbound chunk payload size (1-65536 bytes) |
| `-relay-to` | (none) | RTMP URL to relay streams to (repeatable) |
| `-hook-script` | (none) | Shell hook: `event_type=/path/to/script` (repeatable) |
| `-hook-webhook` | (none) | Webhook: `event_type=https://url` (repeatable) |
| `-hook-stdio-format` | (disabled) | Stdio output format: `json` or `env` |
| `-hook-timeout` | `30s` | Hook execution timeout |
| `-hook-concurrency` | `10` | Max concurrent hook executions |
| `-version` | | Print version and exit |

## Test with FFmpeg

### Publish a Test Stream

```bash
# Stream a local video file to the server
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test

# Or generate a test pattern (no video file needed)
ffmpeg -re -f lavfi -i testsrc=size=640x480:rate=30 \
       -f lavfi -i sine=frequency=440:sample_rate=44100 \
       -c:v libx264 -preset ultrafast -tune zerolatency \
       -c:a aac -f flv rtmp://localhost:1935/live/test
```

### Subscribe (Watch the Stream)

```bash
ffplay rtmp://localhost:1935/live/test
```

### Multiple Subscribers

Open several terminals and run `ffplay` in each — they all receive the same stream independently:

```bash
# Terminal 2
ffplay rtmp://localhost:1935/live/test

# Terminal 3
ffplay rtmp://localhost:1935/live/test
```

## Test with OBS Studio

1. Open OBS Studio → Settings → Stream
2. Set **Service** to "Custom"
3. Set **Server** to `rtmp://localhost:1935/live`
4. Set **Stream Key** to `mystream`
5. Click "Start Streaming"

The server will log the connection and begin recording/relaying.

## Verify Recording

```bash
# List recorded files
ls recordings/

# Play a recording
ffplay recordings/live_mystream_20260302_143000.flv

# Check file details
ffprobe recordings/live_mystream_20260302_143000.flv
```

## Run the Test Suite

```bash
# All tests
go test ./...

# With verbose output
go test -v ./internal/rtmp/chunk/

# Specific package
go test ./internal/rtmp/server/

# Integration tests only
go test ./tests/integration/
```

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| "connection refused" | Server not running | Start the server first |
| Black screen in ffplay | Missing sequence headers | Restart the publisher — the server caches headers for late joiners |
| "stream not found" in play | Wrong stream key | Ensure publisher and subscriber use the same `app/streamName` |
| High CPU usage | Debug logging | Use `-log-level info` instead of `debug` |
| Recording file empty | Publisher disconnected before keyframe | Stream for at least a few seconds |

## Next Steps

- Read the [Architecture Guide](architecture.md) to understand the system design
- Read the [RTMP Protocol Reference](rtmp-protocol.md) for wire-level details
- Read the [Implementation Guide](implementation.md) for code-level walkthrough

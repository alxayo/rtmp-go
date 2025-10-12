# Quick Start Guide: Testing RTMP Server Relay Functionality

**Date:** October 12, 2025  
**Purpose:** Stream from OBS → RTMP Server → ffplay (with recording)

## Overview

This guide demonstrates the complete RTMP relay functionality:
- **Publisher:** OBS Studio streaming to the server
- **Subscribers:** Multiple ffplay instances playing the stream
- **Recording:** Server automatically saves streams to FLV files

## Prerequisites

1. **OBS Studio** installed (for streaming)
2. **ffplay** installed (part of FFmpeg package)
3. **go-rtmp server** built

## Step 1: Build the RTMP Server

```powershell
cd c:\code\alxayo\go-rtmp
go build -o rtmp-server.exe .\cmd\rtmp-server\
```

## Step 2: Start the RTMP Server with Recording Enabled

```powershell
# Create a directory for recordings
mkdir recordings -ErrorAction SilentlyContinue

# Start the server with recording enabled
.\rtmp-server.exe -addr "127.0.0.1:1935" -record-dir ".\recordings" -record-all
```

**Server flags explained:**
- `-addr "127.0.0.1:1935"` - Listen on localhost port 1935 (standard RTMP port)
- `-record-dir ".\recordings"` - Directory where FLV files will be saved
- `-record-all` - Automatically record all published streams

You should see output like:
```
INFO RTMP server listening addr=127.0.0.1:1935
INFO Recording enabled dir=.\recordings record_all=true
```

## Step 3: Configure OBS Studio

1. **Open OBS Studio**

2. **Go to Settings → Stream**
   - Service: `Custom...`
   - Server: `rtmp://127.0.0.1:1935/live`
   - Stream Key: `mystream` (or any name you prefer)

3. **Configure your scene** (add sources like Display Capture, Video Capture Device, etc.)

4. **Click "Start Streaming"** in OBS

You should see in the server logs:
```
INFO Client connected peer_addr=127.0.0.1:xxxxx conn_id=xxx
INFO Handshake completed conn_id=xxx
INFO Stream published stream_key=live/mystream conn_id=xxx
INFO Recording started file=.\recordings\live_mystream_20251012_xxxxxx.flv
```

## Step 4: Play the Stream with ffplay

Open a **new PowerShell terminal** and run:

```powershell
ffplay -fflags nobuffer -flags low_delay -framedrop rtmp://127.0.0.1:1935/live/mystream
```

**ffplay flags explained:**
- `-fflags nobuffer` - Reduce buffering for lower latency
- `-flags low_delay` - Enable low-delay mode
- `-framedrop` - Drop frames if necessary to maintain sync

You should see:
- The video playing in an ffplay window
- Server logs showing the subscriber connection:
```
INFO Client connected peer_addr=127.0.0.1:xxxxx conn_id=yyy
INFO Stream subscribed stream_key=live/mystream conn_id=yyy
```

## Step 5: Test Multiple Subscribers

Open **another PowerShell terminal** and start a second ffplay instance:

```powershell
ffplay -fflags nobuffer -flags low_delay rtmp://127.0.0.1:1935/live/mystream
```

Both players should display the same stream simultaneously, demonstrating the relay functionality.

## Step 6: Verify Recording

While streaming, check the recordings directory:

```powershell
ls .\recordings\
```

You should see an FLV file like:
```
live_mystream_20251012_143052.flv
```

## Step 7: Stop Streaming and Verify Recording

1. **Stop streaming in OBS** (click "Stop Streaming")

2. The recording file will be finalized automatically

3. **Play the recorded file** to verify:

```powershell
ffplay .\recordings\live_mystream_20251012_143052.flv
```

## Architecture Flow

```
OBS (Publisher)
    ↓
    ↓ [RTMP Publish: rtmp://127.0.0.1:1935/live/mystream]
    ↓
RTMP Server
    ├→ Relay to Subscriber 1 (ffplay)
    ├→ Relay to Subscriber 2 (ffplay)
    └→ Record to FLV file (.\recordings\live_mystream_*.flv)
```

## Troubleshooting

### OBS won't connect
- Verify server is running on port 1935
- Check firewall settings
- Ensure stream key matches (case-sensitive)

### ffplay shows error
- Wait a few seconds after OBS starts streaming (allow time for metadata)
- Try without low-latency flags first: `ffplay rtmp://127.0.0.1:1935/live/mystream`

### No recording file
- Verify `-record-all` flag is set
- Check `-record-dir` path exists and is writable
- Look for errors in server logs

### High latency
- In OBS: Settings → Output → Set "Keyframe Interval" to 1-2 seconds
- Use the low-latency flags in ffplay command

## Advanced Testing

### Test with different stream keys

```powershell
# OBS: Stream Key = "test1"
# Terminal 1:
ffplay rtmp://127.0.0.1:1935/live/test1

# OBS: Stream Key = "test2"  
# Terminal 2:
ffplay rtmp://127.0.0.1:1935/live/test2
```

### Monitor server performance

The server logs will show connection counts, stream keys, and recording status in real-time.

## Testing Checklist

- [ ] Server starts without errors
- [ ] OBS connects successfully
- [ ] ffplay subscriber receives stream
- [ ] Multiple ffplay instances work simultaneously
- [ ] Recording file is created
- [ ] Recording file plays back correctly
- [ ] Server logs show all connections and events

## Related Documentation

- `tests/integration/relay_test.go` - Automated integration tests
- `specs/002-rtmp-relay-feature/` - Feature specification
- `docs/RELAY_TESTING_GUIDE.md` - Additional testing scenarios

---

**Last Updated:** October 12, 2025  
**Validated:** RTMP Server v1.0.0 with OBS Studio and ffplay

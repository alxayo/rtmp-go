# Quick Start Guide: RTMP Server Recording & Relay

**Date:** October 13, 2025  
**Status:** ✅ Fully Operational  
**Purpose:** Stream from OBS → RTMP Server → ffplay (with simultaneous recording)

## Overview

This guide demonstrates the complete RTMP server functionality:
- **Publisher:** OBS Studio streaming to the server
- **Recording:** Server automatically saves streams to FLV files
- **Relay:** Multiple subscribers (ffplay, VLC, etc.) can play the live stream
- **Late-Join Support:** Subscribers joining mid-stream receive codec initialization

### Key Features

✅ **Simultaneous Recording & Relay** - Record to file while streaming to live viewers  
✅ **Late-Join Support** - Subscribers joining mid-stream receive H.264 SPS/PPS and AAC config  
✅ **Multiple Subscribers** - Support unlimited concurrent viewers  
✅ **Thread-Safe** - Independent payload copies prevent corruption between connections

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
.\rtmp-server.exe -listen localhost:1935 -log-level info -record-all true -record-dir ./recordings
```

**Server flags explained:**
- `-listen localhost:1935` - Listen address and port (standard RTMP port is 1935)
- `-log-level info` - Set log verbosity (debug, info, warn, error)
- `-record-all true` - Automatically record all published streams
- `-record-dir ./recordings` - Directory where FLV files will be saved

You should see output like:
```json
{"level":"INFO","msg":"RTMP server listening","addr":"127.0.0.1:1935"}
{"level":"INFO","msg":"server started","addr":"127.0.0.1:1935","version":"dev"}
```

### Advanced Options

```powershell
# Debug mode with detailed logging (for troubleshooting)
.\rtmp-server.exe -listen localhost:1935 -log-level debug -record-all true -record-dir ./recordings > debug.log

# Production mode with minimal logging
.\rtmp-server.exe -listen localhost:1935 -log-level warn -record-all true -record-dir ./recordings
```

## Step 3: Configure OBS Studio

1. **Open OBS Studio**

2. **Go to Settings → Stream**
   - Service: `Custom...`
   - Server: `rtmp://localhost:1935/live`
   - Stream Key: `test` (or any name you prefer)

3. **Configure Video Settings** (Settings → Output)
   - Encoder: **x264** (H.264)
   - Keyframe Interval: **2 seconds** (for lower latency)
   - Video Bitrate: 2500 Kbps (or as desired)

4. **Configure Audio Settings** (Settings → Audio)
   - Sample Rate: **48000 Hz**
   - Encoder: **AAC** (default)

5. **Configure your scene** (add sources like Display Capture, Video Capture Device, etc.)

6. **Click "Start Streaming"** in OBS

You should see in the server logs:
```json
{"level":"INFO","msg":"Connection accepted","conn_id":"c000001","peer_addr":"127.0.0.1:xxxxx"}
{"level":"INFO","msg":"Handshake completed","phase":"handshake"}
{"level":"INFO","msg":"recorder initialized","stream_key":"live/test","file":"recordings\\live_test_20251013_121100.flv"}
{"level":"INFO","msg":"recording started","stream_key":"live/test"}
{"level":"INFO","msg":"Cached audio sequence header","stream_key":"live/test","size":7}
{"level":"INFO","msg":"Cached video sequence header","stream_key":"live/test","size":52}
{"level":"INFO","msg":"Codecs detected","stream_key":"live/test","videoCodec":"H264","audioCodec":"AAC"}
```

**What's happening:**
- Server accepts the connection and completes RTMP handshake
- Recording starts automatically (if `-record-all true` is set)
- Server caches the **sequence headers** (H.264 SPS/PPS and AAC AudioSpecificConfig)
- These cached headers will be sent to any subscriber joining later

## Step 4: Play the Stream with ffplay (Subscriber)

**Important:** Wait 2-3 seconds after OBS starts streaming to ensure sequence headers are cached.

Open a **new PowerShell terminal** and run:

```powershell
ffplay rtmp://localhost:1935/live/test
```

### Alternative Players

**VLC Media Player:**
```powershell
vlc rtmp://localhost:1935/live/test
```

**ffplay with low-latency flags:**
```powershell
ffplay -fflags nobuffer -flags low_delay -framedrop rtmp://localhost:1935/live/test
```

You should see:
- The video playing in a window
- Server logs showing the subscriber connection:
```json
{"level":"INFO","msg":"Connection accepted","conn_id":"c000002","peer_addr":"127.0.0.1:xxxxx"}
{"level":"INFO","msg":"play command","stream_key":"live/test"}
{"level":"INFO","msg":"Subscriber added","stream_key":"live/test","total_subscribers":1}
{"level":"INFO","msg":"Sent cached audio sequence header to subscriber","stream_key":"live/test","size":7}
{"level":"INFO","msg":"Sent cached video sequence header to subscriber","stream_key":"live/test","size":52}
```

**What's happening:**
- Server accepts subscriber connection
- Server sends **cached sequence headers** (H.264 SPS/PPS, AAC config) to the subscriber
- This ensures the subscriber's decoder can initialize correctly even though it joined mid-stream
- Server then relays ongoing media packets to the subscriber

### Expected ffplay Output

```
Input #0, flv, from 'rtmp://localhost:1935/live/test':
  Duration: N/A, start: 6.956000, bitrate: N/A
  Stream #0:0: Audio: aac (LC), 48000 Hz, stereo, fltp
  Stream #0:1: Video: h264 (High), yuv420p(tv, bt709, progressive), 1280x720, 30.30 fps
```

### About "mmco: unref short failure" Warning

You may see a single warning:
```
[h264 @ ...] mmco: unref short failure
```

**This is normal and expected** when joining a live H.264 stream mid-GOP (between keyframes). The decoder recovers automatically within < 1 second and playback continues normally. This is the same behavior you'd see on YouTube Live or Twitch.

**See:** `RELAY_MMCO_ERROR_ANALYSIS.md` for detailed explanation.

## Step 5: Test Multiple Subscribers

Open **another PowerShell terminal** and start a second ffplay instance:

```powershell
ffplay rtmp://localhost:1935/live/test
```

Open **a third terminal** for a third subscriber:

```powershell
ffplay rtmp://localhost:1935/live/test
```

**All players should display the same stream simultaneously**, demonstrating the relay functionality.

Server logs will show:
```json
{"level":"INFO","msg":"Subscriber added","stream_key":"live/test","total_subscribers":2}
{"level":"INFO","msg":"Sent cached audio sequence header to subscriber"}
{"level":"INFO","msg":"Sent cached video sequence header to subscriber"}
{"level":"INFO","msg":"Subscriber added","stream_key":"live/test","total_subscribers":3}
{"level":"INFO","msg":"Sent cached audio sequence header to subscriber"}
{"level":"INFO","msg":"Sent cached video sequence header to subscriber"}
```

### Key Technical Details

**Payload Independence:**
- Each subscriber receives an **independent copy** of media packets
- Prevents memory corruption between connections
- Thread-safe broadcasting with proper mutex locking

**Sequence Header Caching:**
- Server caches H.264 SPS/PPS and AAC AudioSpecificConfig on first receipt
- Late-joining subscribers receive cached headers before media packets
- Ensures decoder initialization regardless of join time

## Step 6: Verify Recording

While streaming, check the recordings directory:

```powershell
ls .\recordings\
```

You should see an FLV file like:
```
live_test_20251013_121100.flv
```

**Recording format:**
- Container: **FLV** (Flash Video)
- Video: **H.264** (AVC)
- Audio: **AAC**
- Filename pattern: `{app}_{stream}_{YYYYMMDD}_{HHMMSS}.flv`

## Step 7: Stop Streaming and Verify Recording

1. **Stop streaming in OBS** (click "Stop Streaming")

2. The recording file will be finalized automatically

3. **Play the recorded file** to verify:

```powershell
ffplay .\recordings\live_test_20251013_121100.flv
```

### Verify Recording Quality

**Check file info:**
```powershell
ffprobe .\recordings\live_test_20251013_121100.flv
```

**Convert to MP4** (if needed):
```powershell
ffmpeg -i .\recordings\live_test_20251013_121100.flv -c copy output.mp4
```

### Simultaneous Operation Verification

✅ **Recording continues** while subscribers are connected  
✅ **Relay continues** while recording is active  
✅ **Both features work independently** without interference

## Architecture Flow

### Data Flow Diagram

```
OBS Studio (Publisher)
    ↓
    ↓ [RTMP Publish: rtmp://localhost:1935/live/test]
    ↓ [Sends: H.264 video + AAC audio]
    ↓
┌───────────────────────────────────────────────────────┐
│ RTMP Server                                           │
│                                                       │
│ 1. Handshake & Connect                               │
│ 2. Receive Media Packets                             │
│ 3. Cache Sequence Headers (SPS/PPS, AudioConfig)     │
│ 4. Codec Detection (H264, AAC)                       │
│                                                       │
│ ┌─────────────────────────────────────────────┐     │
│ │ BroadcastMessage()                          │     │
│ │ - Clone payload for each subscriber         │     │
│ │ - Thread-safe mutex locking                 │     │
│ │ - Independent delivery to all subscribers   │     │
│ └─────────────────────────────────────────────┘     │
│                                                       │
│         ├→ Recording Thread                          │
│         │   └→ Write to FLV file                     │
│         │                                             │
│         ├→ Subscriber 1 (c000002)                    │
│         │   └→ Send cached headers + live packets    │
│         │                                             │
│         ├→ Subscriber 2 (c000003)                    │
│         │   └→ Send cached headers + live packets    │
│         │                                             │
│         └→ Subscriber N (c00000N)                    │
│             └→ Send cached headers + live packets    │
└───────────────────────────────────────────────────────┘
    │           │               │               │
    ↓           ↓               ↓               ↓
Recording    ffplay        ffplay          VLC
  .flv      Window 1      Window 2      Player
```

### Sequence Header Lifecycle

```
Time: T=0 (Stream Start)
┌─────────────────────────────────────────────────────┐
│ OBS sends:                                          │
│ 1. Audio Seq Header (AAC config, 7 bytes)          │
│ 2. Video Seq Header (H.264 SPS/PPS, 52 bytes)      │
└─────────────────────────────────────────────────────┘
               ↓
┌─────────────────────────────────────────────────────┐
│ Server caches in Stream struct:                    │
│ - AudioSequenceHeader: *chunk.Message              │
│ - VideoSequenceHeader: *chunk.Message              │
└─────────────────────────────────────────────────────┘

Time: T=30s (Late Subscriber Joins)
┌─────────────────────────────────────────────────────┐
│ ffplay connects (play command)                     │
└─────────────────────────────────────────────────────┘
               ↓
┌─────────────────────────────────────────────────────┐
│ HandlePlay() sends:                                 │
│ 1. NetStream.Play.Start                            │
│ 2. Cached Audio Seq Header → Subscriber           │
│ 3. Cached Video Seq Header → Subscriber           │
│ 4. Then ongoing live media packets                 │
└─────────────────────────────────────────────────────┘
               ↓
┌─────────────────────────────────────────────────────┐
│ ffplay decoder initializes:                        │
│ - H.264 decoder configured with SPS/PPS           │
│ - AAC decoder configured with AudioSpecificConfig  │
│ - Ready to decode media frames                     │
└─────────────────────────────────────────────────────┘
```

## Troubleshooting

### OBS won't connect
- ✅ Verify server is running: Look for "RTMP server listening" log message
- ✅ Check port: Server should be on port 1935
- ✅ Check firewall: Allow incoming connections on port 1935
- ✅ Verify URL: `rtmp://localhost:1935/live` (app name is "live")
- ✅ Check stream key: Case-sensitive, matches between OBS and ffplay

### ffplay shows H.264 errors (continuous)
If you see repeated errors like:
```
[h264] No start code is found
[h264] Error splitting the input into NAL units
```

**Solutions:**
- ✅ **CRITICAL:** Start OBS first, wait 2-3 seconds, THEN start ffplay
- ✅ Ensure server logs show "Cached video sequence header" before starting ffplay
- ✅ If already running, restart ffplay (the subscriber connection)

### Single "mmco: unref short failure" warning
**This is normal and expected!** See explanation in Step 4. No action required.

### No recording file
- ✅ Verify `-record-all true` flag is set when starting server
- ✅ Check `-record-dir ./recordings` path exists and is writable
- ✅ Look for "recording started" in server logs
- ✅ Verify OBS is actually streaming (check OBS status indicator)

### Recording stops/corrupted
- ✅ Don't kill server with Ctrl+C during recording (graceful shutdown needed)
- ✅ Check disk space
- ✅ Verify write permissions on recordings directory

### High latency
- ✅ In OBS: Settings → Output → Set "Keyframe Interval" to 1-2 seconds
- ✅ In OBS: Settings → Output → Disable "Look-ahead" and "Psycho Visual Tuning"
- ✅ Use low-latency flags in ffplay: `-fflags nobuffer -flags low_delay`

### Video plays but with artifacts
- ✅ Check OBS bitrate settings (2500 Kbps recommended for 720p)
- ✅ Verify CPU usage isn't maxed out (lower OBS encoding preset if needed)
- ✅ Check network stability (even on localhost, system resources matter)

## Advanced Testing

### Test with Different Stream Keys (Multiple Streams)

The server supports **multiple simultaneous streams** with different keys:

```powershell
# Stream 1: OBS instance 1 with Stream Key = "stream1"
# Terminal 1: Subscribe to stream1
ffplay rtmp://localhost:1935/live/stream1

# Stream 2: OBS instance 2 with Stream Key = "stream2"  
# Terminal 2: Subscribe to stream2
ffplay rtmp://localhost:1935/live/stream2
```

Each stream will have its own:
- Independent recording file: `live_stream1_*.flv`, `live_stream2_*.flv`
- Independent subscriber list
- Independent sequence header cache

### Monitor Server Performance

**Real-time monitoring with debug logging:**
```powershell
.\rtmp-server.exe -listen localhost:1935 -log-level debug -record-all true -record-dir ./recordings
```

**Filter logs for specific connection:**
```powershell
# In PowerShell (if logging to file)
Select-String -Path debug.log -Pattern "c000001"
```

### Test Late-Join Scenario

This tests the sequence header caching fix:

1. Start OBS streaming
2. **Wait 30-60 seconds** (let stream run)
3. Start ffplay (late joiner)
4. ✅ Video should play immediately with sequence headers

Server logs should show:
```json
{"msg":"Sent cached audio sequence header to subscriber"}
{"msg":"Sent cached video sequence header to subscriber"}
```

### Stress Test (Multiple Subscribers)

```powershell
# Terminal 1-10: Start 10 concurrent subscribers
1..10 | ForEach-Object { 
    Start-Process ffplay -ArgumentList "rtmp://localhost:1935/live/test"
}
```

Monitor server for:
- Memory usage stability
- No payload corruption between subscribers
- All windows show same video

## Testing Checklist

### Basic Functionality
- [ ] Server starts without errors
- [ ] Server logs show "RTMP server listening"
- [ ] OBS connects successfully
- [ ] Server logs show "Cached audio sequence header"
- [ ] Server logs show "Cached video sequence header"
- [ ] Recording file is created in `./recordings/`
- [ ] Server logs show "recording started"

### Relay Functionality
- [ ] ffplay subscriber connects successfully
- [ ] Server logs show "Subscriber added"
- [ ] Server logs show "Sent cached audio sequence header to subscriber"
- [ ] Server logs show "Sent cached video sequence header to subscriber"
- [ ] Video plays in ffplay window
- [ ] Audio plays correctly
- [ ] Multiple ffplay instances work simultaneously
- [ ] Server logs show correct subscriber count

### Late-Join Test
- [ ] Start OBS and wait 30 seconds
- [ ] Start ffplay (late joiner)
- [ ] Video plays immediately without errors
- [ ] Server sends cached sequence headers to late joiner

### Recording Verification
- [ ] Recording file plays back correctly with ffplay
- [ ] Recording contains both audio and video
- [ ] Recording quality matches live stream
- [ ] Filename format is correct: `{app}_{stream}_{timestamp}.flv`

### Cleanup
- [ ] Stop OBS streaming
- [ ] All ffplay instances close cleanly
- [ ] Recording file is finalized (no corruption)
- [ ] Server continues running without errors

## Implementation Details

### Sequence Header Caching (Critical Feature)

**Problem Solved:** Late-joining subscribers need codec initialization packets that were sent at stream start.

**Solution:** Server caches sequence headers and sends them to new subscribers.

**Files Modified:**
- `internal/rtmp/server/registry.go` - Added caching logic
- `internal/rtmp/server/play_handler.go` - Added delivery to subscribers

**Technical Details:**
- **Video Sequence Header:** H.264 SPS/PPS (AVC packet type = 0)
- **Audio Sequence Header:** AAC AudioSpecificConfig (AAC packet type = 0)
- **Detection:** Byte-level inspection of payload (`msg.TypeID` and `payload[1]`)
- **Delivery:** Sent immediately after `NetStream.Play.Start` response

### Payload Cloning (Thread Safety)

Each subscriber receives an **independent copy** of media packets:

```go
relayMsg := &chunk.Message{
    // ... copy metadata ...
    Payload: make([]byte, len(msg.Payload)),
}
copy(relayMsg.Payload, msg.Payload)
```

This prevents memory corruption when multiple subscribers read payloads concurrently.

## Related Documentation

### Technical Documentation
- **`RELAY_FIX_SEQUENCE_HEADERS.md`** - Detailed technical explanation of sequence header fix
- **`RELAY_MMCO_ERROR_ANALYSIS.md`** - Analysis of "mmco: unref short failure" warning
- **`RELAY_COMPLETE.md`** - Executive summary of relay implementation
- **`RELAY_FIX_QUICKSTART.md`** - Alternative quick start guide

### Code References
- `internal/rtmp/server/registry.go` - Stream management and broadcasting
- `internal/rtmp/server/play_handler.go` - Subscriber handling
- `internal/rtmp/media/recorder.go` - FLV recording implementation
- `tests/integration/relay_test.go` - Automated integration tests

### Specifications
- `specs/002-rtmp-relay-feature/` - Feature specification
- `docs/000-constitution.md` - Project principles and guidelines

---

## Summary

**The rtmp-server now supports:**
✅ **Recording** - Automatic FLV recording with H.264/AAC  
✅ **Relay** - Live streaming to multiple subscribers  
✅ **Late-Join Support** - Sequence header caching for mid-stream joiners  
✅ **Simultaneous Operation** - Recording and relay work together  
✅ **Thread Safety** - Independent payload copies prevent corruption

**Last Updated:** October 13, 2025  
**Status:** ✅ Production-Ready  
**Validated:** RTMP Server with OBS Studio and ffplay

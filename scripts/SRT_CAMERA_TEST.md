# SRT Camera Ingest Test Guide

This guide walks you through testing SRT ingest with your integrated camera.

## Quick Start (3 terminals)

### Terminal 1: Start the Server

```bash
cd /Users/alex/Code/rtmp-go
./rtmp-server -listen :1935 -srt-listen :10080 -record-all true -record-dir ./recordings -log-level info
```

Expected output:
```
{"time":"...","level":"INFO","msg":"RTMP server listening","addr":"127.0.0.1:1935"}
{"time":"...","level":"INFO","msg":"SRT server listening","addr":"127.0.0.1:10080"}
```

### Terminal 2: Capture Camera and Stream via SRT

**macOS** (with integrated camera):
```bash
ffmpeg \
  -f avfoundation \
  -video_size 1280x720 \
  -framerate 30 \
  -i "0:1" \
  -c:v libx264 \
  -preset ultrafast \
  -tune zerolatency \
  -b:v 2500k \
  -c:a aac \
  -b:a 128k \
  -f mpegts \
  "srt://localhost:10080?streamid=publish:live/camera-test"
```

**Linux** (with v4l2 camera):
```bash
ffmpeg \
  -f v4l2 \
  -video_size 1280x720 \
  -framerate 30 \
  -i /dev/video0 \
  -c:v libx264 \
  -preset ultrafast \
  -tune zerolatency \
  -b:v 2500k \
  -f mpegts \
  "srt://localhost:10080?streamid=publish:live/camera-test"
```

**Windows** (with dshow camera):
```bash
ffmpeg.exe `
  -f dshow `
  -i "video=""Integrated Camera""" `
  -c:v libx264 `
  -preset ultrafast `
  -tune zerolatency `
  -b:v 2500k `
  -c:a aac `
  -b:a 128k `
  -f mpegts `
  "srt://localhost:10080?streamid=publish:live/camera-test"
```

### Terminal 3: Watch via RTMP

```bash
ffplay rtmp://localhost:1935/live/camera-test
```

Or use any RTMP player (VLC, OBS, etc.)

## What's Happening

1. **Server** (`terminal 1`):
   - Listens on port 1935 (RTMP) and 10080 (SRT)
   - Records all streams to `./recordings/` as FLV files
   - Logs all connections

2. **FFmpeg Publisher** (`terminal 2`):
   - Captures camera frames (1280x720 @ 30fps)
   - Encodes to H.264 video + AAC audio
   - Wraps in MPEG-TS container (required by SRT)
   - Sends to SRT server at `localhost:10080`
   - Stream ID `publish:live/camera-test` tells server:
     - `publish` = this is a publisher (not a subscriber)
     - `live/camera-test` = the stream name/key

3. **Server Bridge** (internal):
   - Receives SRT packets over UDP
   - Performs SRT handshake v5
   - Applies reliability (ACK/NAK)
   - Demuxes MPEG-TS packets
   - Extracts H.264 & AAC frames
   - Converts H.264 from Annex B to AVCC format
   - Converts AAC from ADTS to raw frames
   - Converts timestamps from 90kHz to 1ms (RTMP)
   - Feeds into stream registry as `chunk.Message`

4. **RTMP Subscribers** (`terminal 3`):
   - Connect via RTMP (or RTMPS if enabled)
   - Request stream `live/camera-test`
   - Receive H.264/AAC frames transparently
   - Don't know the stream came from SRT

5. **Recording** (automatic):
   - Server simultaneously writes all media to FLV file
   - File saved as `recordings/live_camera-test_YYYYMMDD_HHMMSS.flv`

## Listing Available Cameras

**macOS** — see all avfoundation devices:
```bash
ffmpeg -f avfoundation -list_devices true -i "" 2>&1 | grep -A 20 "AVFoundation video"
```

Output example:
```
[AVFoundation @ ...] [0] Integrated Camera
[AVFoundation @ ...] [1] (other device)
```

Use `"0:1"` to capture device 0 (video) + device 1 (audio).

**Linux** — see all v4l2 devices:
```bash
ls -la /dev/video*
```

**Windows** — list dshow devices:
```powershell
ffmpeg.exe -f dshow -list_devices true -i dummy 2>&1 | Select-String "video"
```

## Troubleshooting

### "Connection refused" when publishing
- Is the server running? (check `terminal 1`)
- Is the server listening on the right port? (default: 10080)
- Try `netstat -an | grep 10080` to verify port is open

### "Pixel format not supported"
- This is a warning, not an error. FFmpeg will auto-convert the pixel format.
- Safe to ignore if you see video streaming.

### Recording not created
- Check that `-record-all true` flag is set
- Check `./recordings/` directory exists
- Look for files matching `live_camera-test_*.flv`

### Can't see video in ffplay
- Wait 2-3 seconds for the SRT handshake and codec headers to flow
- Check server logs for "stream registered" messages
- Try verbose logging: `-log-level debug`

## SRT with Encryption (Optional)

Add a passphrase to both sides:

**Server:**
```bash
./rtmp-server -listen :1935 -srt-listen :10080 -srt-passphrase "mysecretkey"
```

**FFmpeg Publisher:**
```bash
ffmpeg -f avfoundation -i "0:1" -c copy -f mpegts \
  "srt://localhost:10080?streamid=publish:live/camera-test&passphrase=mysecretkey"
```

## What This Demonstrates

✅ **SRT Ingest** — UDP-based protocol can deliver media  
✅ **MPEG-TS Demuxing** — Server extracts H.264 and AAC from transport stream  
✅ **Codec Conversion** — Annex B → AVCC and ADTS → raw happen transparently  
✅ **Dual Protocol** — Same stream available to RTMP subscribers  
✅ **Recording** — Works with SRT just like RTMP  
✅ **Live Streaming** — Sub-second latency from camera to viewer

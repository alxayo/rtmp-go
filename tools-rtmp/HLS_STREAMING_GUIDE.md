# RTMP to HLS Streaming Guide

This guide shows you how to convert RTMP streams from your Go RTMP server to HLS format for web-based adaptive streaming.

## 🎯 Complete Workflow

### 1. Start Your RTMP Server
```bash
# Build and start the RTMP server
go build -o rtmp-server ./cmd/rtmp-server
./rtmp-server -listen localhost:1935 -log-level info -record-all true -record-dir ./recordings
```

### 2. Publish to RTMP Server

**Option A: OBS Studio**
- Settings → Stream
- Service: `Custom...`
- Server: `rtmp://localhost:1935/live`
- Stream Key: `test`

**Option B: FFmpeg**
```bash
# Stream a video file
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test

# Stream webcam + microphone (macOS)
ffmpeg -f avfoundation -i "0:0" -c:v libx264 -preset ultrafast -c:a aac rtmp://localhost:1935/live/test
```

### 3. Convert RTMP to HLS

**Basic Mode (Fastest - Copy Codecs):**
```bash
./rtmp-to-hls.sh basic test
```

**Quality Mode (Re-encode for Better Compatibility):**
```bash
./rtmp-to-hls.sh quality test
```

**Adaptive Mode (Multi-bitrate for Adaptive Streaming):**
```bash
./rtmp-to-hls.sh adaptive test
```

### 4. Serve HLS Files

```bash
# Start simple HTTP server
python3 -m http.server 8080

# Or use Node.js
npx http-server -p 8080 -c-1

# Or use Go
go run -C /tmp -c 'package main; import("net/http"; "log"); func main(){log.Fatal(http.ListenAndServe(":8080", http.FileServer(http.Dir("."))))}'
```

### 5. Play HLS Stream

**Option A: HTML Player**
```bash
open hls-player.html
# or
open http://localhost:8080/hls-player.html
```

**Option B: VLC Media Player**
```
Open Network Stream: http://localhost:8080/hls-output/playlist.m3u8
```

**Option C: FFplay**
```bash
ffplay http://localhost:8080/hls-output/playlist.m3u8
```

## 🔧 HLS Configuration Options

### Segment Duration and List Size
```bash
-hls_time 10          # 10-second segments
-hls_list_size 6      # Keep 6 segments in playlist (60 seconds of content)
```

### Segment Management
```bash
-hls_flags delete_segments    # Delete old segments automatically
-hls_flags append_list        # Append to existing playlist
-hls_flags single_file        # Single file output
```

### Advanced Options
```bash
-hls_start_number_source epoch    # Use timestamp for segment numbering
-hls_allow_cache 0               # Disable caching
-hls_segment_type mpegts         # Segment format (default)
```

## 📊 Multi-bitrate Adaptive Streaming

The adaptive mode creates multiple quality levels:

| Quality | Resolution | Video Bitrate | Audio Bitrate |
|---------|------------|---------------|---------------|
| High    | 1920x1080  | 5000k         | 128k          |
| Medium  | 1280x720   | 2500k         | 128k          |
| Low     | 854x480    | 1000k         | 96k           |

Client players automatically switch between qualities based on bandwidth.

## 🎥 File Structure

**Basic/Quality Mode:**
```
hls-output/
├── playlist.m3u8      # Main playlist
├── segment_000.ts     # Video segments
├── segment_001.ts
└── segment_002.ts
```

**Adaptive Mode:**
```
hls-output/
├── master.m3u8        # Master playlist (points to quality variants)
├── stream_0.m3u8      # High quality playlist
├── stream_1.m3u8      # Medium quality playlist
├── stream_2.m3u8      # Low quality playlist
├── stream_0/          # High quality segments
│   ├── segment_000.ts
│   └── segment_001.ts
├── stream_1/          # Medium quality segments
└── stream_2/          # Low quality segments
```

## 🚀 Performance Tips

### For Low Latency
```bash
-hls_time 2           # Shorter segments (2 seconds)
-hls_list_size 3      # Smaller playlist
-tune zerolatency     # For libx264
-preset ultrafast     # Faster encoding
```

### For Quality
```bash
-hls_time 10          # Longer segments (better compression)
-preset slow          # Better compression
-crf 20               # Higher quality
```

### For Bandwidth Efficiency
```bash
-b:v 2000k            # Fixed bitrate
-maxrate 2000k        # Max bitrate
-bufsize 4000k        # Buffer size
-g 50                 # GOP size (affects seeking)
```

## 🔍 Troubleshooting

### Common Issues

**"Connection refused" errors:**
- Ensure RTMP server is running on port 1935
- Check if stream is actively publishing

**"No such file or directory" for segments:**
- Ensure output directory exists and is writable
- Check disk space

**Player shows "Stream not found":**
- Verify HTTP server is serving the correct directory
- Check browser console for CORS errors

**High CPU usage:**
- Use `-c copy` instead of re-encoding when possible
- Reduce video resolution or bitrate
- Use hardware acceleration if available

### Debug Commands

```bash
# Check RTMP stream info
ffprobe rtmp://localhost:1935/live/test

# Monitor HLS segments
watch -n 1 'ls -la hls-output/'

# Test HTTP server response
curl -I http://localhost:8080/hls-output/playlist.m3u8
```

## 🌐 Integration with Web Applications

### JavaScript HLS.js Example
```javascript
import Hls from 'hls.js';

const video = document.getElementById('video');
const hls = new Hls({
  lowLatencyMode: true,
  liveMaxLatencyDurationCount: 3
});

hls.loadSource('http://localhost:8080/hls-output/playlist.m3u8');
hls.attachMedia(video);
```

### React Component Example
```jsx
import { useEffect, useRef } from 'react';
import Hls from 'hls.js';

function VideoPlayer({ src }) {
  const videoRef = useRef();
  
  useEffect(() => {
    const hls = new Hls();
    hls.loadSource(src);
    hls.attachMedia(videoRef.current);
    
    return () => hls.destroy();
  }, [src]);
  
  return <video ref={videoRef} controls autoPlay muted />;
}
```

## 📱 Mobile Considerations

- iOS Safari has native HLS support (no hls.js needed)
- Android requires hls.js for broader compatibility
- Use adaptive streaming for varying mobile bandwidth
- Consider lower bitrate options for mobile networks

## 🔒 Security Notes

- Serve HLS over HTTPS in production
- Implement token-based authentication if needed
- Consider DRM for premium content
- Use CDN for better performance and security
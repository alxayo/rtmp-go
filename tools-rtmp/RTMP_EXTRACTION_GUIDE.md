# 🎬 RTMP Stream Extraction Guide

## Overview
The `extract-rtmp-stream.sh` script allows you to extract specific media components from live RTMP streams using FFmpeg. Perfect for analyzing streams, creating highlights, or separating audio/video content.

## 🚀 Quick Start

### Basic Usage
```bash
# Extract only video data
./extract-rtmp-stream.sh video test

# Extract only audio data  
./extract-rtmp-stream.sh audio test

# Extract only I-frames (keyframes)
./extract-rtmp-stream.sh iframes test
```

### Advanced Usage
```bash
# Extract 60 seconds of video
./extract-rtmp-stream.sh video test --duration 60

# Extract audio with custom filename
./extract-rtmp-stream.sh audio mystream --output "podcast_audio.aac"

# Extract I-frames with specific quality
./extract-rtmp-stream.sh iframes livestream --quality slow --format mkv
```

## 📋 Available Options

### Modes
| Mode | Description | Output | Use Case |
|------|-------------|--------|----------|
| `video` | Video stream only | H.264 MP4 file | Video analysis, silent clips |
| `audio` | Audio stream only | AAC/MP3 file | Podcasts, music extraction |
| `iframes` | Keyframes only | MP4 with I-frames | Thumbnails, scene detection |

### Command Line Options
| Option | Description | Example |
|--------|-------------|---------|
| `--duration SEC` | Record for specific time | `--duration 120` |
| `--output FILE` | Custom output filename | `--output "my_video.mp4"` |
| `--format FORMAT` | Output format | `--format mkv` |
| `--quality PRESET` | Encoding quality | `--quality slow` |

### Quality Presets
- `ultrafast` - Fastest encoding, larger files
- `fast` - Good speed/quality balance
- `medium` - Default balanced option
- `slow` - Better compression, slower encoding
- `veryslow` - Best compression, slowest encoding

## 🎯 Detailed Examples

### 1. Extract Video Only

**Basic video extraction:**
```bash
./extract-rtmp-stream.sh video test
```
**Output:** `extracted-media/video_test_20251018_153000.mp4`

**60-second video clip:**
```bash
./extract-rtmp-stream.sh video livestream --duration 60 --output "highlight.mp4"
```

**High-quality video:**
```bash
./extract-rtmp-stream.sh video test --quality slow --format mkv
```

### 2. Extract Audio Only

**Basic audio extraction:**
```bash
./extract-rtmp-stream.sh audio test
```
**Output:** `extracted-media/audio_test_20251018_153000.aac`

**Extract as MP3:**
```bash
./extract-rtmp-stream.sh audio mystream --format mp3 --output "stream_audio.mp3"
```

**5-minute audio clip:**
```bash
./extract-rtmp-stream.sh audio podcast --duration 300 --format wav
```

### 3. Extract I-Frames Only

**Basic I-frame extraction:**
```bash
./extract-rtmp-stream.sh iframes test
```
**Output:** `extracted-media/iframes_test_20251018_153000.mp4`

**I-frames for thumbnail generation:**
```bash
./extract-rtmp-stream.sh iframes stream --duration 30 --quality ultrafast
```

**High-quality keyframes:**
```bash
./extract-rtmp-stream.sh iframes demo --quality slow --output "keyframes_hq.mp4"
```

## 📊 Understanding the Outputs

### Video Extraction
- **Contains:** H.264 video stream only
- **No audio:** Silent video file
- **Use cases:** 
  - Video quality analysis
  - Creating silent B-roll footage
  - Analyzing motion and visual content
  - Creating video-only clips for editing

### Audio Extraction  
- **Contains:** AAC audio stream only
- **No video:** Audio-only file
- **Use cases:**
  - Podcast extraction from live streams
  - Music performance recording
  - Audio quality analysis
  - Creating audio-only content

### I-Frame Extraction
- **Contains:** Only keyframes (I-frames) from video
- **Playback:** Choppy/jerky (this is normal!)
- **Frame rate:** Much lower than original (only keyframes)
- **Use cases:**
  - Scene change detection
  - Thumbnail generation
  - Content analysis
  - Studying GOP structure

**Note:** I-frame videos appear jerky because they only contain keyframes, not the smooth P and B frames in between.

## 🔧 Technical Details

### Default Settings
- **RTMP Source:** `rtmp://localhost:1935/live/[stream_name]`
- **Output Directory:** `./extracted-media/`
- **Video Codec:** Copy (no re-encoding) for video/audio modes
- **I-Frame Mode:** Re-encodes with libx264 (required for frame filtering)

### File Naming Convention
```
Mode_StreamName_Timestamp.Extension
├── video_test_20251018_153000.mp4
├── audio_mystream_20251018_153000.aac  
└── iframes_demo_20251018_153000.mp4
```

### Stream Requirements
- Active RTMP stream on specified server
- H.264 video codec (for video/iframe modes)
- AAC audio codec (for audio mode)
- Network connectivity to RTMP server

## 🛠️ Troubleshooting

### Common Issues

**"RTMP stream not available"**
- Check if RTMP server is running: `ps aux | grep rtmp-server`
- Verify stream is publishing: Check server logs
- Test stream accessibility: `ffplay rtmp://localhost:1935/live/test`

**"FFmpeg not found"** 
- Install FFmpeg: `brew install ffmpeg` (macOS)
- Verify installation: `ffmpeg -version`

**"Permission denied"**
- Make script executable: `chmod +x extract-rtmp-stream.sh`
- Check output directory permissions

**Large file sizes**
- Use shorter durations: `--duration 30`
- Use faster presets: `--quality ultrafast`
- Choose efficient formats: `--format mp4`

### Quality vs Speed Trade-offs

| Quality | Speed | File Size | Use Case |
|---------|-------|-----------|----------|
| ultrafast | Very Fast | Large | Real-time processing |
| fast | Fast | Medium | Live streaming |
| medium | Moderate | Medium | General purpose |
| slow | Slow | Small | Archival quality |

## 📈 Performance Tips

### For Real-time Processing:
```bash
./extract-rtmp-stream.sh video test --quality ultrafast --duration 10
```

### For Best Quality:
```bash
./extract-rtmp-stream.sh video test --quality slow --format mkv
```

### For Analysis Workflows:
```bash
# Extract 30 seconds of each type
./extract-rtmp-stream.sh video test --duration 30
./extract-rtmp-stream.sh audio test --duration 30  
./extract-rtmp-stream.sh iframes test --duration 30
```

## 🎨 Integration Examples

### With Your RTMP Server
```bash
# Start RTMP server
./rtmp-server -listen localhost:1935 -log-level info

# Start streaming (OBS/FFmpeg)
# Then extract components:
./extract-rtmp-stream.sh video mystream --duration 120
```

### With HLS Conversion
```bash
# Extract video, then convert to HLS
./extract-rtmp-stream.sh video test --duration 60 --output "source.mp4"
ffmpeg -i extracted-media/source.mp4 -f hls -hls_time 10 hls-output/playlist.m3u8
```

### Batch Processing
```bash
#!/bin/bash
# Extract all components from stream
STREAM="mystream"
DURATION=60

./extract-rtmp-stream.sh video $STREAM --duration $DURATION
./extract-rtmp-stream.sh audio $STREAM --duration $DURATION  
./extract-rtmp-stream.sh iframes $STREAM --duration $DURATION

echo "All extractions completed for stream: $STREAM"
```

## 📄 Output Files

The script creates several files:

### Media Files
- **Video:** `.mp4` files with H.264 video
- **Audio:** `.aac`, `.mp3`, or `.wav` audio files
- **I-frames:** `.mp4` files with keyframes only

### Reports
- **Analysis Report:** Text file with extraction details
- **Stream Info:** Codec and quality information
- **File Statistics:** Size and duration data

## 🎯 Next Steps

1. **Test with your stream:** `./extract-rtmp-stream.sh video test --duration 10`
2. **Analyze outputs:** Check file sizes and quality
3. **Integrate into workflow:** Use for highlights, analysis, or archiving
4. **Customize:** Modify script for specific needs

The script provides a solid foundation for RTMP stream analysis and content extraction! 🎬✨
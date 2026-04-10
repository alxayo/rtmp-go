# H.265/HEVC Codec Support

## Overview

The RTMP/SRT server now supports H.265 (High Efficiency Video Coding, also known as HEVC) for both ingest and distribution. H.265 is the successor to H.264 and provides approximately **50% better compression** at the same video quality.

## Supported Directions

### ✅ SRT Ingest (NEW)
You can now stream H.265 video to the server via SRT protocol:

```bash
ffmpeg -i input.mp4 -c:v libx265 -f mpegts \
  'srt://server:10080?streamid=publish:live/h265-stream'
```

**Requirements:**
- ffmpeg with libsrt and libx265 support
- Camera or encoder capable of H.265 output
- Network bandwidth for H.265 stream (typically 40-60% of H.264 equivalent)

### ✅ RTMP Ingest
H.265 can be published via RTMP from any RTMP client:

```bash
ffmpeg -i input.mp4 -c:v libx265 -f flv rtmp://server/live/stream
```

### ✅ RTMP Distribution
H.265 subscribers can play H.265 streams via RTMP:

```bash
ffplay rtmp://server/live/h265-stream
```

### ✅ Recording
H.265 streams are recorded to FLV files with the original codec preserved:

```
./rtmp-server -srt-listen :10080 -record-all true
```

## H.265 vs H.264: Key Differences

### Parameter Sets
**H.264** requires 2 parameter sets:
- SPS (Sequence Parameter Set)
- PPS (Picture Parameter Set)

**H.265** requires 3 parameter sets:
- **VPS (Video Parameter Set)** ← New in H.265
- SPS (Sequence Parameter Set)
- PPS (Picture Parameter Set)

The VPS contains encoder options that can vary across multiple profiles and levels.

### Profile Support

Currently supported H.265 profiles:
- **Main Profile** (8-bit)
- **Main 10 Profile** (10-bit, HDR)

Profiles not yet tested (but likely working):
- Range extensions profiles

### NAL Unit Types

| Type Range | Purpose |
|-----------|---------|
| 32 | VPS (Video Parameter Set) |
| 33 | SPS (Sequence Parameter Set) |
| 34 | PPS (Picture Parameter Set) |
| 35 | AUD (Access Unit Delimiter) |
| 16-21 | IDR (Keyframe) NAL units |
| 0-15 | Inter-frame NAL units |

## Technical Details

### MPEG-TS Stream Type
H.265 is identified in MPEG-TS by:
- **Stream Type**: 0x24 (as defined in ISO/IEC 13818-1)
- **Elementary Streams**: H.265 bitstream in Annex B format

### RTMP Format
H.265 in RTMP uses **Enhanced RTMP (E-RTMP v2)** format:
- **FourCC**: `"hvc1"` — identifies HEVC codec in the Enhanced RTMP video tag
- **IsExHeader**: bit 7 = 1, signaling Enhanced RTMP format
- **Decoder Configuration**: HEVCDecoderConfigurationRecord (ISO/IEC 14496-15)

The Enhanced RTMP video tag header is:
```
Byte 0: [IsExHeader:1][FrameType:3][PacketType:4]
  SequenceStart (seq header): 0x90 (IsEx=1, Keyframe=1, PktType=0)
  CodedFrames (keyframe):     0x91 (IsEx=1, Keyframe=1, PktType=1)
  CodedFrames (inter):        0xA1 (IsEx=1, Inter=2, PktType=1)
Bytes 1-4: FourCC "hvc1"
```

> **Note:** Some older implementations use legacy CodecID=12 (0x1C/0x2C). This server
> uses the standard Enhanced RTMP format which is supported by ffmpeg, ffplay, and VLC.

### Sequence Header Structure

The HEVCDecoderConfigurationRecord (ISO/IEC 14496-15 §8.3.3.1) contains:
- Configuration version (1 byte, always 1)
- General profile/tier/level information (12 bytes, extracted from SPS[3:15])
- Min spatial segmentation info (2 bytes, 4 reserved bits + 12-bit value)
- Parallelism type (1 byte, 6 reserved bits + 2-bit value)
- Chroma format (1 byte, 6 reserved bits + 2-bit value)
- Bit depth information (2 bytes, 5 reserved bits + 3-bit value each)
- Frame rate info (2 bytes)
- Constant frame rate flags (1 byte)

> **Important:** All reserved bits in the record MUST be set to 1 per the spec.
- NAL unit length size minus one (1 byte)
- Array of VPS/SPS/PPS parameter sets

## Testing H.265 Streams

### Test with Camera (macOS/Linux)

Use the provided test script:

```bash
# Test SRT H.265 ingest
./scripts/test-srt-h265.sh 30  # 30 second test

# Windows PowerShell
.\scripts\test-srt-h265.ps1 -Duration 30
```

The script will:
1. Start the RTMP/SRT server
2. Stream H.265 from your camera via SRT
3. Record the stream locally
4. Validate the codec in the recording

### Test with File Input

```bash
# Start server
./rtmp-server -listen :1935 -srt-listen :10080 -record-all true

# Stream H.265 file via SRT
ffmpeg -re -i h265-file.mp4 -c:v copy -f mpegts \
  'srt://localhost:10080?streamid=publish:live/test'

# Play the stream
ffplay rtmp://localhost:1935/live/test

# Verify recording
ffprobe recordings/h265-file.flv
```

## Performance Considerations

### Bandwidth Savings
H.265 typically achieves:
- **40-50% smaller file size** vs H.264 at same quality
- **30-40% lower bitrate** for live streaming

Example: A 1080p30 stream might use:
- **H.264**: 3-5 Mbps
- **H.265**: 1.5-2.5 Mbps

### CPU Overhead
- **Encoding**: H.265 encoding is **2-3x slower** than H.264
  - Use `-preset ultrafast` or `veryfast` for live capture
  - Consider GPU encoding (NVENC, HEVC_AMF, etc.)
- **Decoding**: H.265 decoding is **1.5-2x slower** than H.264
  - Less critical for playback unless many concurrent subscribers

### Compatibility
- **Modern clients**: Full support (Chrome 64+, Firefox 55+, Safari 11+)
- **Older/legacy clients**: May need fallback to H.264
  - Use RTMP for clients that don't support H.265
  - Transcode if necessary

## Troubleshooting

### "H.265 frames are being dropped"
**Cause**: ffmpeg doesn't have H.265 encoder (libx265).

**Solution**:
```bash
# Install libx265
# macOS
brew install x265

# Ubuntu/Debian
apt-get install libx265-dev

# Verify
ffmpeg -codecs | grep hevc
```

### "SRT connection fails when streaming H.265"
**Cause**: Server may have been built with an older version before H.265 support.

**Solution**:
```bash
# Rebuild the server
go build -o rtmp-server ./cmd/rtmp-server
```

### "Recording has no video"
**Cause**: If parameter sets aren't detected early enough, video frames may be dropped.

**Solution**:
- Ensure the stream starts with a keyframe
- Use a modern ffmpeg version
- Try with H.264 first to rule out other issues

## Future Enhancements

Potential improvements (not yet implemented):
- [ ] H.266 (VVC) support
- [ ] AV1 codec support
- [ ] Alternative codec profiles (Range Extensions, etc.)
- [ ] HDR metadata forwarding
- [ ] Adaptive bitrate based on network conditions

## References

- [ISO/IEC 23090-3 (H.265 Codec)](https://www.itu.int/rec/T-REC-H.265-202108-I/en)
- [ISO/IEC 14496-15 (HEVCDecoderConfigurationRecord)](https://www.iso.org/standard/73025.html)
- [MPEG-TS Stream Types](https://en.wikipedia.org/wiki/Elementary_stream#Elementary_stream_types)
- [FFmpeg H.265 Encoding Guide](https://trac.ffmpeg.org/wiki/Encode/H.265)

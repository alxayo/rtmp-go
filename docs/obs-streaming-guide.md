# OBS Studio Streaming Guide

Optimal OBS Studio settings for streaming to the rtmp-go RTMP server with HLS ABR transcoding.

These settings produce a clean source stream that the HLS transcoder can process without artifacts. The key principles are: **no B-frames**, **fixed keyframe intervals**, and **constant bitrate**.

## Quick Setup

1. Open OBS Studio → **Settings**
2. Apply the settings below in each tab
3. Set your stream server and key under **Stream**
4. Start streaming

## Stream Settings

| Setting | Value |
|---------|-------|
| Service | Custom |
| Server | `rtmp://stream.port-80.com/live` |
| Stream Key | `your_stream_key?token=YOUR_TOKEN` |

> Replace `stream.port-80.com` with your RTMP server's FQDN or Azure Container Apps FQDN.

## Output Settings

### Output Mode: Advanced

Select **Output Mode: Advanced** at the top of the Output tab.

#### Streaming Tab

| Setting | Value | Notes |
|---------|-------|-------|
| Encoder | x264 | Software encoder — most compatible |
| Rate Control | CBR | Constant bitrate — essential for stable transcoding |
| Bitrate | 4500 Kbps | Good balance for 1080p source; transcoder downscales to 720p/480p |
| Keyframe Interval | 2 s | Must match transcoder's `-force_key_frames` (every 2 seconds) |
| CPU Usage Preset | veryfast | Matches transcoder; use `faster` or `medium` if CPU allows |
| Profile | baseline | **Critical**: prevents B-frames and reference frame errors |
| Tune | zerolatency | Reduces encoder latency for live streaming |
| x264 Options | `bframes=0` | **Critical**: explicitly disable B-frames |

> **Why baseline profile?** The ABR transcoder re-encodes your stream into 3 renditions. B-frames from Main/High profiles cause non-monotonic DTS timestamps, producing `[hls] Non-monotonic DTS` warnings and choppy playback. Baseline profile eliminates this entirely.

#### If using NVENC (NVIDIA GPU):

| Setting | Value |
|---------|-------|
| Encoder | NVIDIA NVENC H.264 |
| Rate Control | CBR |
| Bitrate | 4500 Kbps |
| Keyframe Interval | 2 s |
| Preset | P4: Medium (Low Latency) |
| Profile | baseline |
| B-frames | 0 |

## Video Settings

| Setting | Value | Notes |
|---------|-------|-------|
| Base (Canvas) Resolution | 1920×1080 | Match your monitor or capture source |
| Output (Scaled) Resolution | 1920×1080 | Send full resolution; transcoder handles downscaling |
| Downscale Filter | Lanczos | Only matters if Base ≠ Output resolution |
| FPS Type | Common FPS Values | |
| Common FPS Values | 30 | Matches transcoder's `-r 30`; use 60 only if transcoder is configured for it |

> **Why 30 FPS?** The ABR transcoder forces 30 FPS (`-r 30`). Sending 60 FPS wastes upload bandwidth since the transcoder discards half the frames. If you need 60 FPS output, update the transcoder's `-r` flag first.

## Audio Settings

| Setting | Value | Notes |
|---------|-------|-------|
| Sample Rate | 48 kHz | Matches transcoder audio output (48000 Hz) |
| Channels | Stereo | |
| Audio Bitrate (Output tab) | 128 Kbps | Source audio; transcoder re-encodes per rendition |

## Advanced Settings

| Setting | Value | Notes |
|---------|-------|-------|
| Process Priority | Above Normal | Helps prevent frame drops on busy systems |
| Network: Enable Dynamic Bitrate | Disabled | CBR is more predictable for the transcoder |
| Network: Enable Optimizations | Enabled | |

## Bandwidth Requirements

| Source Bitrate | Upload Speed Required | Notes |
|---------------|----------------------|-------|
| 3000 Kbps | ≥4 Mbps | Good for 720p source, transcoder upscales to 1080p |
| 4500 Kbps | ≥6 Mbps | **Recommended** for 1080p source |
| 6000 Kbps | ≥8 Mbps | High quality 1080p; more headroom for transcoder |

> Include ~20% headroom over your video bitrate for audio + RTMP overhead.

## Troubleshooting

### OBS shows "Encoding overloaded!"

Your CPU can't keep up with the x264 preset. Solutions:
1. Change CPU Usage Preset from `veryfast` to `ultrafast`
2. Lower Output Resolution to 1280×720 (transcoder still produces 1080p/720p/480p renditions)
3. Switch to NVENC if you have an NVIDIA GPU

### Choppy playback despite smooth OBS preview

1. **Check profile**: Must be `baseline`, not `main` or `high`
2. **Check B-frames**: Must be `0` — verify in x264 Options field: `bframes=0`
3. **Check keyframe interval**: Must be exactly `2` seconds (not `0` which means auto)
4. **Check bitrate**: CBR, not VBR — variable bitrate causes transcoder buffer fluctuations

### Stream disconnects frequently

1. Check upload bandwidth (run a speed test)
2. Lower bitrate to 3000 Kbps
3. Check OBS → Stats window for dropped frames
4. If on WiFi, switch to Ethernet

### Audio out of sync

1. Verify Sample Rate is 48 kHz (not 44.1 kHz)
2. Check Advanced Audio Properties: ensure no sync offset is set
3. The transcoder's `-async 1` flag corrects minor audio drift, but large offsets (>100ms) from the source need fixing in OBS

## FFmpeg Equivalent

For testing without OBS, these FFmpeg flags produce an equivalent source stream:

```bash
ffmpeg -re -i input.mp4 \
  -c:v libx264 -profile:v baseline -bf 0 \
  -g 60 -keyint_min 60 \
  -b:v 4500k -maxrate 5000k -bufsize 9000k \
  -preset veryfast -tune zerolatency \
  -c:a aac -b:a 128k -ar 48000 \
  -f flv "rtmp://stream.port-80.com/live/stream?token=YOUR_TOKEN"
```

| FFmpeg Flag | OBS Equivalent |
|-------------|---------------|
| `-profile:v baseline` | Profile: baseline |
| `-bf 0` | x264 Options: `bframes=0` |
| `-g 60 -keyint_min 60` | Keyframe Interval: 2s (at 30fps, 2×30=60 frames) |
| `-b:v 4500k` | Bitrate: 4500 Kbps |
| `-preset veryfast` | CPU Usage Preset: veryfast |
| `-tune zerolatency` | Tune: zerolatency |
| `-ar 48000` | Audio Sample Rate: 48 kHz |

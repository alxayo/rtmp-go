---
title: "OBS Studio Setup"
weight: 1
---

# OBS Studio Setup

This guide walks through configuring [OBS Studio](https://obsproject.com/) to stream to your go-rtmp server.

## Prerequisites

- go-rtmp server running (see [Quick Start]({{< relref "/docs/quick-start" >}}))
- OBS Studio installed

## Step-by-Step Configuration

### 1. Stream Settings

Open OBS → **Settings** → **Stream**:

| Setting | Value |
|---------|-------|
| Service | **Custom...** |
| Server | `rtmp://localhost:1935/live` |
| Stream Key | `mystream` |

> **Tip**: Replace `localhost` with your server's IP address if streaming from a different machine.

### 2. Output Settings

Open OBS → **Settings** → **Output** → **Streaming** tab:

| Setting | Recommended Value |
|---------|-------------------|
| Encoder | **x264** (H.264), **x265** (H.265/HEVC), or hardware encoders (NVENC, QuickSync, AMF) |
| Rate Control | CBR |
| Bitrate | 2500 Kbps (adjust for your upload speed) |
| Keyframe Interval | **2 seconds** (critical for late-join performance) |
| CPU Usage Preset | `veryfast` (for x264) |
| Profile | `high` |

> **Important**: A 2-second keyframe interval ensures subscribers can join within 2 seconds. Longer intervals mean longer wait times for late-joining viewers.

### 3. Audio Settings

Open OBS → **Settings** → **Audio**:

| Setting | Recommended Value |
|---------|-------------------|
| Sample Rate | **48000 Hz** |
| Channels | Stereo |

### 4. Add Sources

In the main OBS window, click **+** under Sources to add:

- **Display Capture** — screen share
- **Video Capture Device** — webcam
- **Audio Input Capture** — microphone
- **Audio Output Capture** — desktop audio

### 5. Start Streaming

Click **Start Streaming** in the bottom-right corner of OBS.

You should see the server log output:

```
INF connection registered conn_id=1 remote=127.0.0.1:54321
INF publisher started conn_id=1 stream_key=live/mystream
```

### 6. Verify

Open a subscriber in another terminal:

```bash
ffplay rtmp://localhost:1935/live/mystream
```

You should see your OBS output with minimal latency.

## With Authentication

If the server requires authentication (`-auth-mode token`), include the token in the Stream Key field:

```
mystream?token=secret123
```

The full OBS stream settings become:

| Setting | Value |
|---------|-------|
| Server | `rtmp://localhost:1935/live` |
| Stream Key | `mystream?token=secret123` |

## Troubleshooting

### Connection Refused

**Symptom**: OBS shows "Could not access the specified channel or stream key" or "Failed to connect to server".

**Fix**: Ensure the go-rtmp server is running and listening on the correct address/port. Check that no firewall is blocking port 1935.

### Black Screen on Subscriber

**Symptom**: ffplay connects but shows a black screen.

**Fix**:
1. OBS can use **H.264**, **H.265/HEVC**, or **AV1** encoders. go-rtmp supports all of them via Enhanced RTMP.
2. Restart the stream in OBS
3. Wait 2–3 seconds before starting the subscriber

### High CPU Usage

**Symptom**: Server or OBS using excessive CPU.

**Fix**:
- In OBS, switch from **x264** to a hardware encoder (NVENC for NVIDIA, QuickSync for Intel, AMF for AMD)
- On the server, use `-log-level info` instead of `debug` (debug logs every media message)
- Lower the stream bitrate

### Audio/Video Out of Sync

**Symptom**: Audio is ahead of or behind video.

**Fix**:
- Ensure OBS keyframe interval is set to **2 seconds** (not 0 or auto)
- Restart the stream

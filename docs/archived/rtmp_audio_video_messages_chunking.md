# RTMP Audio/Video Synchronization and Chunking

## Overview
This document describes how audio and video data are transmitted and synchronized in RTMP (Real-Time Messaging Protocol) sessions. The information is grounded in the RTMP handshake and data exchange specifications.

---

## RTMP Chunking Mechanism
RTMP uses a **chunk-based protocol** to transmit messages. Each message (e.g., video frame, audio frame, command) is split into **chunks** for transmission.

### Chunk Format
Each chunk consists of:
1. **Basic Header** (1–3 bytes)
2. **Message Header** (0, 3, 7, or 11 bytes depending on format)
3. **Extended Timestamp** (optional, 4 bytes)
4. **Chunk Data** (up to `chunk_size` bytes)

### Message Types
Each RTMP message has a **Type ID**:
- `8`: Audio Message (encoded audio data)
- `9`: Video Message (encoded video data)

Audio and video are sent as **separate messages**, not combined in a single packet.

---

## Synchronization of Audio and Video

### Timestamps
Each audio and video message includes a **timestamp** in its header. This timestamp indicates the presentation time (in milliseconds) relative to the start of the stream.

### Playback Synchronization
The client (e.g., media player) uses these timestamps to align audio and video frames during playback:
- Audio and video frames with the same timestamp are played together.
- The player buffers incoming data and uses timestamps to correct for network jitter or delay.

### Multiplexing
Audio and video messages are **interleaved** over the same RTMP connection:
- Example sequence: `[video chunk][audio chunk][video chunk][audio chunk]`
- Each message is independent and has its own headers.

---

## Summary Table
| Aspect                | Audio & Video Together? | How Synchronized?         | Who Ensures Sync?      |
|-----------------------|------------------------|---------------------------|------------------------|
| RTMP Message          | No                     | Timestamps in headers     | Player/client          |
| RTMP Chunk            | No                     | N/A                       | N/A                    |
| RTMP Packet           | No                     | N/A                       | N/A                    |
| Playback              | N/A                    | Uses timestamps to align  | Player/client          |

---

## Implementation Notes
- RTMP delivers timestamped messages; synchronization is handled by the player.
- Proper buffering and timestamp alignment are essential to avoid lag.
- RTMP does not guarantee sync; it provides the data and timing info.

---

*Generated on: YYYY-MM-DD HH:MM:SS*
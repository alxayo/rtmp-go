
# RTMP Protocol Technical Reference

This document provides a comprehensive technical overview of the RTMP (Real-Time Messaging Protocol) for expert software engineers. It covers message types, session handling, stream multiplexing, and synchronization mechanisms.

---

## RTMP Message Types

RTMP messages are transmitted in chunks and identified by a Type ID. Below is a summary of message types and their usage:

| Type ID | Name                        | Description / Usage                                 | Required? |
|---------|-----------------------------|-----------------------------------------------------|-----------|
| 1       | Set Chunk Size              | Changes the maximum chunk size for transmission     | Yes       |
| 2       | Abort Message               | Aborts a chunk stream                              | Optional  |
| 3       | Acknowledgement             | Sent in response to received bytes (flow control)   | Yes       |
| 4       | User Control Message        | Ping, stream begin, stream EOF, etc.                | Yes       |
| 5       | Window Acknowledgement Size | Flow control, window size negotiation               | Yes       |
| 6       | Set Peer Bandwidth          | Bandwidth negotiation                               | Yes       |
| 8       | Audio Message               | Encoded audio data                                  | If audio  |
| 9       | Video Message               | Encoded video data                                  | If video  |
| 18      | Command Message (AMF0)      | Commands like `connect`, `play`, `publish`          | Yes       |
| 20      | Command Message (AMF3)      | Same as above, but AMF3 encoding                    | Optional  |

---

## RTMP Sessions

After the handshake (C0-C2, S0-S2), the connection enters the RTMP session phase:

- A session is tied to a single TCP connection.
- All control, command, audio, and video messages are exchanged within this session.
- Multiple logical streams can be created using `createStream`, each with its own stream ID.
- The session persists until the TCP connection is closed.

---

## Multiple Sessions per Connection?

**No**, RTMP does not support multiple sessions over a single TCP connection.

- One TCP connection = One RTMP session.
- To initiate multiple sessions, separate TCP connections must be established.
- Within a session, multiple logical streams can exist, but they are not independent sessions.

---

## Multiple Logical Streams and Media Pairing

RTMP supports multiplexing of audio and video messages over a single session:

- Audio and video are sent as separate messages, interleaved over the connection.
- Each message includes a timestamp for synchronization.
- The client/player uses timestamps to align playback.

### Pairing Rules

- RTMP does not enforce pairing of one audio stream to one video stream.
- You can send multiple video streams with one audio stream, but this is non-standard.
- Most implementations expect one audio and one video stream per logical stream.

### Synchronization Table

| Aspect        | Audio & Video Together? | How Synchronized?         | Who Ensures Sync? |
|---------------|--------------------------|---------------------------|--------------------|
| RTMP Message  | No                       | Timestamps in headers     | Player/client      |
| RTMP Chunk    | No                       | N/A                       | N/A                |
| RTMP Packet   | No                       | N/A                       | N/A                |
| Playback      | N/A                      | Uses timestamps to align  | Player/client      |

---

## Implementation Notes

- RTMP delivers timestamped messages; synchronization is handled by the player.
- Proper buffering and timestamp alignment are essential to avoid lag.
- RTMP does not guarantee sync; it provides the data and timing info.

---

## References

- RTMP Audio/Video Synchronization and Chunking
- RTMP Basic Handshake Deep Dive
- RTMP Data Exchange Technical Breakdown
- RTMP Overview and Architecture


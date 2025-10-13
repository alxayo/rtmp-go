
# RTMP Protocol End-to-End Session and Technical Breakdown

This document provides a comprehensive technical overview of the RTMP (Real-Time Messaging Protocol) for expert software engineers. It includes message types, session handling, stream multiplexing, synchronization, and a full end-to-end session breakdown.

---

## RTMP Message Types

RTMP messages are sent in a chunked format, and each message has a Type ID that defines its purpose.

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

Audio (8) and Video (9) messages are only required if you are transmitting media. For a minimal RTMP implementation (e.g., for a control or metadata-only session), you could technically omit them.
Most Important (Must-Implement) Message Types
For a successful RTMP session (e.g., for live streaming), you must implement at least:

Set Chunk Size (1)
Acknowledgement (3)
User Control Message (4)
Window Acknowledgement Size (5)
Set Peer Bandwidth (6)
Command Message (AMF0, 18)
Audio (8) and Video (9) (if you are sending media)

---
## What Are RTMP Sessions?
After the handshake (C0-C2, S0-S2), the connection enters the RTMP session phase. In this phase:

The client and server exchange RTMP messages (audio, video, commands, control).
The session is stateful and persists as long as the TCP connection is open.
Each session is uniquely identified by the TCP connection and the negotiated parameters (e.g., stream key, app name).

## How Are Sessions Used?

Session Initialization: After handshake, the client sends a connect command (AMF0/AMF3 message) to start a session.
Stream Control: The client can then send createStream, publish, play, etc., to control streams within the session.
Data Exchange: Audio, video, and metadata messages are exchanged within the session.
Session Termination: Closing the TCP connection or sending a closeStream command ends the session.

## How Are Sessions Implemented?

State Machine: The RTMP server/client maintains a state machine per session (e.g., handshake, connect, streaming, teardown).
Session Context: Each session tracks negotiated parameters (chunk size, bandwidth, stream IDs, etc.).
Multiplexing: Multiple logical streams (audio, video, metadata) are multiplexed over a single session using chunk stream IDs.

---

## RTMP Session Model

One TCP connection = One RTMP session context.
After the handshake, all commands, control messages, and media streams are multiplexed over this single session.
Within a session, you can have multiple logical streams (e.g., audio, video, metadata), each identified by a unique stream ID. But these are not separate sessions—they are sub-streams within the same session context.


##  Supporting Evidence from the Files

The RTMP handshake (C0-C2, S0-S2) establishes a single session context for the connection. All subsequent messages (commands, audio, video, control) are part of this session.
The RTMP Data Exchange file describes how, after the handshake, the connection enters the “RTMP session phase,” where all data is exchanged within that session.
The multiplexing described in the chunking mechanism is about interleaving different types of messages (audio, video, control) over the same session—not about having multiple sessions.


## Multiple Streams vs. Multiple Sessions

Multiple Streams: You can create multiple logical streams (e.g., for different tracks or purposes) within a single session using commands like createStream. Each has its own stream ID.
Multiple Sessions: To have multiple independent sessions (e.g., for different users or stream keys), you must open separate TCP connections—each with its own handshake and session state.


## In summary:
One TCP connection = One RTMP session.
Multiple logical streams (audio/video) can exist within a session, but not multiple sessions.

---

## Multiple Logical Streams in RTMP

RTMP allows multiplexing of different types of messages (audio, video, metadata, control) over a single session (TCP connection).
Each message (audio or video) is independent and has its own headers, including a timestamp for synchronization.
Audio and video are sent as separate messages, not combined in a single packet. They are interleaved over the same connection, e.g.:
[video chunk][audio chunk][video chunk][audio chunk]. [rtmp_audio...s_chunking]


## How Many Media Streams Can You Have in a Session?

RTMP itself does not strictly limit the number of audio or video streams per session at the protocol level.
In practice, most implementations (like OBS, Wowza, nginx-rtmp) use one audio stream and one video stream per logical stream (e.g., per publish or play command).
Each logical stream is identified by a stream ID, and you can create multiple logical streams within a session using commands like createStream. However, each logical stream typically carries one audio and one video track.


##  Pairing of Audio and Video Streams

Pairing is not enforced by RTMP: You could, in theory, send multiple video streams and one audio stream, or vice versa, as long as each message is properly timestamped.
Synchronization is handled by the client/player, which uses the timestamps in the message headers to align audio and video frames during playback. [rtmp_audio...s_chunking]
Typical Usage: Most players and servers expect one audio and one video stream per logical stream for proper synchronization and playback. Sending multiple video streams with a single audio stream is non-standard and may not be supported by all players.


##  Example Scenarios

Standard Live Stream:

1 audio stream (e.g., microphone)
1 video stream (e.g., camera)


## Advanced (Non-Standard):

2 video streams (e.g., main camera + screen share) and 1 audio stream
This is possible at the protocol level, but most players will not know how to synchronize or present them unless custom logic is implemented.




## Key Points

RTMP multiplexes audio and video messages over a single session.
Each message is timestamped for synchronization.
One audio + one video per logical stream is the norm.
Multiple logical streams can exist, but each is usually a separate media track (not multiple videos in one stream).
Pairing is not enforced, but practical playback compatibility depends on the player.

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
- Each message includes a timestamp.
- The client/player uses timestamps to synchronize playback.

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

## End-to-End RTMP Session Breakdown

### 1. Handshake Phase

- **C0 (Client Version)**: 1 byte, usually 0x03
- **C1 (Client Challenge)**: 1536 bytes (timestamp + zero + random data)
- **S0 (Server Version)**: 1 byte
- **S1 (Server Challenge)**: 1536 bytes
- **S2 (Server Response)**: Echoes C1 random data
- **C2 (Client Response)**: Echoes S1 random data

### 2. Session Establishment Phase

- **Set Chunk Size (Type 1)**
- **Window Acknowledgement Size (Type 5)**
- **Set Peer Bandwidth (Type 6)**
- **Acknowledgement (Type 3)**

### 3. Command/Control Phase

- **connect (Type 18)**: Client initiates session
- **_result (Type 18)**: Server responds
- **createStream (Type 18)**: Client requests stream ID
- **publish/play (Type 18)**: Start media flow
- **User Control Message (Type 4)**: Stream Begin, Ping/Pong

### 4. Media Transmission Phase

- **Audio Message (Type 8)**: Encoded audio
- **Video Message (Type 9)**: Encoded video
- **Metadata (Type 18)**: Optional stream metadata
- **Chunking**: Messages split into chunks
- **Acknowledgement (Type 3)**: Flow control
- **User Control Message (Type 4)**: Buffer events, ping/pong

### 5. Session Teardown Phase

- **closeStream (Type 18)**
- **Stream EOF (Type 4)**
- **TCP Connection Closed**

---

## 

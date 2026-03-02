# 1. RTMP Handshake Phase
Purpose: Establishes the session context over TCP.
Packet Exchange

Step | Sender | Packet Name | Structure (bytes) | Description
--- | --- | --- | --- | ---
1 | Client | C0 (Version) | 1 byte | Usually 0x03 (RTMP version)
2 | Client | C1 (Challenge) | 1536 bytes | Timestamp (4) + Zero (4) + Random (1528)
3 | Server | S0 (Version) | 1 byte | Echoes C0
4 | Server | S1 (Challenge) | 1536 bytes | Timestamp (4) + Zero (4) + Random (1528)
5 | Server | S2 (Response) | 1536 bytes | Echoes C1 random data
6 | Client | C2 (Response) | 1536 bytes | Echoes S1 random data

Pseudo code
```c

// Send C0+C1
send(socket, 0x03, 1); // C0
send(socket, c1_data, 1536); // C1

// Receive S0+S1+S2
recv(socket, s0, 1);
recv(socket, s1, 1536);
recv(socket, s2, 1536);

// Send C2
send(socket, s1_random, 1536); // C2

```

# 2. Session Establishment Phase
## Key RTMP Message Types

Type ID | Name | Structure (header + payload) | Usage
--- | --- | --- | ---
1 | Set Chunk Size | 1 + 3 bytes (header + chunk size) | Sets max chunk size
5 | Window Ack Size | 1 + 4 bytes (header + window size) | Flow control
6 | Set Peer Bandwidth | 1 + 4 + 1 bytes (header + bandwidth + limit type) | Bandwidth negotiation
3 | Acknowledgement | 1 + 4 bytes (header + sequence num) | Flow control

### Packet Structure Example:

- RTMP Header: Basic (1-12 bytes, depending on format)
- Payload: Varies by message type

### Set Chunk Size (Type 1):
```
| Header (fmt+csid) | Timestamp | Msg Length | Msg Type (1) | Msg Stream ID | Chunk Size (4 bytes) |
```

### Code Example

```c
// Set Chunk Size
uint8_t header[] = {0x02}; // fmt=0, csid=2
uint8_t msg_type = 0x01;
uint32_t chunk_size = htonl(4096);
send(socket, header, sizeof(header));
send(socket, &msg_type, 1);
send(socket, &chunk_size, 4);

```

# 3. Command/Control Phase
## AMF0 Command Messages (Type 18)

- connect, createStream, publish, play, _result, etc.
- Encoded using AMF0 (Action Message Format)

### Packet Structure:
```
| Header | Timestamp | Msg Length | Msg Type (18) | Msg Stream ID | AMF0 Payload |
```

### AMF0 Payload Example (connect):
```
["connect", 1, {app:"live", flashVer:"FMLE/3.0", tcUrl:"rtmp://server/live", ...}]
```

### Code Example (Python, using PyAMF):

``` python
from pyamf import AMF0
payload = AMF0.encode([
    "connect", 1, {
        "app": "live",
        "flashVer": "FMLE/3.0",
        "tcUrl": "rtmp://server/live"
    }
])
send(socket, rtmp_header)
send(socket, payload)
```

# 4. Media Transmission Phase
## Audio (Type 8) & Video (Type 9) Messages

- Each media frame is sent as a separate RTMP message.
- Interleaved over the connection.

### Packet Structure:
```
| Header | Timestamp | Msg Length | Msg Type (8/9) | Msg Stream ID | Encoded Media Data |
```

### Audio Example:
```
Header: fmt=0, csid=4, timestamp, length, type=8, stream_id
Payload: AAC/MP3/PCM frame
```

### Video Example:
```
Header: fmt=0, csid=6, timestamp, length, type=9, stream_id
Payload: H.264/VP6 frame
```

### Code Example (pseudo):

```c
// Send audio frame
send(socket, audio_header, header_len);
send(socket, audio_data, audio_len);

// Send video frame
send(socket, video_header, header_len);
send(socket, video_data, video_len);
```

# 5. Session Teardown Phase

Type ID | Name | Structure | Usage
--- | --- | --- | ---
18 | closeStream | AMF0 encoded | End logical stream
4 | Stream EOF | 6 bytes (header+event) | End of stream signal
 `-` | TCP FIN | TCP packet | Close connection

## Code Example:

```c
# Send closeStream command (AMF0)
payload = AMF0.encode(["closeStream", 0, None])
send(socket, rtmp_header)
send(socket, payload)

# Close TCP connection
socket.close()
```

# RTMP Chunking

- All RTMP messages are split into chunks.
- Each chunk has its own header (fmt, csid, timestamp, etc.).
- Chunk size negotiated via Set Chunk Size message.

### Chunk Header Formats:

- Format 0: Full header (12 bytes)
- Format 1: No stream ID (8 bytes)
- Format 2: Only timestamp delta (4 bytes)
- Format 3: No header (1 byte)

### Code Example (chunking):

```c
// Split message into chunks
for (int i = 0; i < msg_len; i += chunk_size) {
    send(socket, chunk_header, header_len);
    send(socket, msg_data + i, min(chunk_size, msg_len - i));
}

```

# Summary Table: RTMP Session Packet Flow

Phase | Client Packet(s) | Server Packet(s)
--- | --- | ---
Handshake | C0, C1, C2 | S0, S1, S2
Establishment | connect (18), Set Chunk Size (1), Window Ack Size (5), Set Peer Bandwidth (6) | _result (18), Set Chunk Size (1), Window Ack Size (5), Set Peer Bandwidth (6)
Control | createStream (18), publish/play (18) | _result (18), User Control (4)
Media | Audio (8), Video (9) | Audio (8), Video (9)
Teardown | closeStream (18) | Stream EOF (4)
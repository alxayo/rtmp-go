
# 🔄 RTMP Data Exchange – Technical Breakdown

Once the handshake (`C0-C2` and `S0-S2`) is complete, the connection enters the **RTMP session phase**, where actual data (audio, video, control messages) is exchanged.

## 📦 RTMP Chunking Mechanism

RTMP uses a **chunk-based protocol** to transmit messages. Each message (e.g., video frame, command) is split into **chunks** for transmission.

### 🔹 Chunk Format

Each chunk consists of:

1. **Basic Header** (1–3 bytes)
2. **Message Header** (0, 3, 7, or 11 bytes depending on format)
3. **Extended Timestamp** (optional, 4 bytes)
4. **Chunk Data** (up to `chunk_size` bytes)

### 🔹 1. Basic Header

- **Format**: 1–3 bytes
- **Fields**:
  - **fmt** (2 bits): Determines the message header format.
  - **csid** (6 bits or more): Chunk Stream ID.

```
Byte 1:
+--------+--------+
| fmt(2) | csid(6)|
+--------+--------+
```

- **fmt values**:
  - `0`: Full header (timestamp, length, type, stream ID)
  - `1`: No stream ID
  - `2`: Only timestamp delta
  - `3`: No header (same as previous chunk)

### 🔹 2. Message Header

Depending on `fmt`, the message header includes:

| fmt | Timestamp | Msg Length | Msg Type ID | Stream ID |
|-----|-----------|------------|-------------|-----------|
| 0   | ✓         | ✓          | ✓           | ✓         |
| 1   | ✓         | ✓          | ✓           | ✗         |
| 2   | ✓         | ✗          | ✗           | ✗         |
| 3   | ✗         | ✗          | ✗           | ✗         |

### 🔹 3. Extended Timestamp

- **Only present** if timestamp ≥ `0xFFFFFF`
- 4 bytes, big-endian

### 🔹 4. Chunk Data

- Contains part of the RTMP message payload.
- Max size is negotiated via `Set Chunk Size` message (default: 128 bytes).

## 🧾 RTMP Message Types

Each RTMP message has a **Type ID** (1 byte) that defines its purpose.

| Type ID | Name                  | Description                          |
|---------|-----------------------|--------------------------------------|
| 1       | Set Chunk Size        | Changes max chunk size               |
| 2       | Abort Message         | Aborts a chunk stream                |
| 3       | Acknowledgement       | Sent in response to received bytes   |
| 4       | User Control Message  | Ping, stream begin, etc.             |
| 5       | Window Acknowledgement| Flow control                         |
| 6       | Set Peer Bandwidth    | Bandwidth negotiation                |
| 8       | Audio Message         | Encoded audio data                   |
| 9       | Video Message         | Encoded video data                   |
| 18      | Command Message (AMF0)| Commands like `connect`, `play`, etc.|
| 20      | Command Message (AMF3)| Same as above, but AMF3 encoding     |

## 🧠 Example: Sending a Video Frame

1. **Create a Video Message** (Type ID `9`)
2. **Split into chunks** (e.g., 128 bytes each)
3. **Send each chunk with appropriate headers**

## 🧪 Pseudocode: Chunking a Message

```python
def chunk_message(message, chunk_size, csid, fmt):
    chunks = []
    timestamp = message.timestamp
    header = create_header(fmt, csid, timestamp, len(message.data), message.type_id, message.stream_id)

    for i in range(0, len(message.data), chunk_size):
        chunk_data = message.data[i:i+chunk_size]
        if i == 0:
            chunks.append(header + chunk_data)
        else:
            chunks.append(create_header(3, csid) + chunk_data)  # fmt=3: no header
    return chunks
```

## 🧩 AMF Encoding (for Commands)

RTMP uses **AMF0 or AMF3** (Action Message Format) to encode command messages like `connect`, `play`, `publish`.

- **AMF0**: Older, widely supported
- **AMF3**: Newer, used in Flash Player 9+

Example: `connect` command in AMF0

```
[
  "connect",         // Command name (string)
  1,                 // Transaction ID (number)
  {                  // Command object (object)
    app: "live",
    flashVer: "FMLE/3.0",
    tcUrl: "rtmp://localhost/live"
  }
]
```

## ✅ Implementation Tips

- **Start with AMF0** for compatibility.
- **Use a state machine** to manage chunk parsing.
- **Handle control messages** like ping/pong and bandwidth.
- **Implement flow control** using `Window Acknowledgement` and `Set Peer Bandwidth`

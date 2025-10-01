# Chunking Contract

**Feature**: 001-rtmp-server-implementation  
**Package**: `internal/rtmp/chunk`  
**Date**: 2025-10-01

## Overview

This contract defines the RTMP chunk stream protocol used to multiplex and fragment messages over a single TCP connection. Chunking enables interleaving of audio, video, and control messages while respecting bandwidth constraints.

**Reference**: Adobe RTMP Specification Section 5.3 (Chunking)

---

## Chunk Format

Every RTMP message (except handshake) is transmitted as one or more chunks:

```
Chunk = BasicHeader + MessageHeader + [ExtendedTimestamp] + ChunkData
```

**Default Chunk Size**: 128 bytes (negotiable via Set Chunk Size message, max 65536 bytes)

---

## Basic Header

**Purpose**: Identifies chunk stream ID (CSID) and message header format type (FMT)

**Size**: 1, 2, or 3 bytes (depends on CSID value)

### Format

**Byte 0**:
```
Bits 0-1 (2 bits): fmt (format type, 0-3)
Bits 2-7 (6 bits): cs id (chunk stream ID, 0-63)
```

```
+--------+--------+
| fmt(2) | csid(6)|
+--------+--------+
```

### CSID Encoding Rules

| CSID Range | Basic Header Size | Encoding |
|------------|-------------------|----------|
| 2-63       | 1 byte            | Byte 0: fmt << 6 \| csid |
| 64-319     | 2 bytes           | Byte 0: fmt << 6 \| 0<br>Byte 1: (csid - 64) |
| 320-65599  | 3 bytes           | Byte 0: fmt << 6 \| 1<br>Byte 1: (csid - 64) & 0xFF (low byte)<br>Byte 2: ((csid - 64) >> 8) & 0xFF (high byte) |

**Reserved CSIDs**:
- 0: Signals 2-byte form (csid 64-319)
- 1: Signals 3-byte form (csid 320-65599)
- 2: Reserved for protocol control messages (Set Chunk Size, Ack, etc.)

### Examples

**CSID=2, FMT=0** (1 byte):
```
Hex: 02
Binary: 00 000010
        ↑↑ ↑↑↑↑↑↑
        fmt  csid
```

**CSID=64, FMT=0** (2 bytes):
```
Hex: 00 00
Byte 0: 00 (fmt=0, csid=0 signals 2-byte form)
Byte 1: 00 (64 - 64 = 0)
```

**CSID=320, FMT=1** (3 bytes):
```
Hex: 41 00 01
Byte 0: 41 (fmt=1, csid=1 signals 3-byte form)
Byte 1: 00 (low byte of 320-64=256)
Byte 2: 01 (high byte of 256)
```

---

## Message Header

**Purpose**: Contains message metadata (timestamp, length, type, stream ID)

**Size**: 0, 3, 7, or 11 bytes (depends on FMT)

### FMT 0: Full Header (11 bytes)

**Used when**:
- Starting a new message on a chunk stream
- Timestamp goes backwards (e.g., seeking)
- Message stream ID changes

**Format**:
```
Bytes 0-2:   Timestamp (3 bytes, big-endian, milliseconds)
Bytes 3-5:   Message Length (3 bytes, big-endian, payload size)
Byte 6:      Message Type ID (1 byte)
Bytes 7-10:  Message Stream ID (4 bytes, LITTLE-ENDIAN)
```

**Note**: Message Stream ID is **little-endian** (RTMP quirk), all other multi-byte fields are big-endian.

**Example** (Audio message, timestamp=1234, length=128, type=8, stream ID=1):
```
Hex: 00 04 D2  00 00 80  08  01 00 00 00
     --------  --------  --  -----------
     timestamp  length   type  stream ID (LE)
```

### FMT 1: No Message Stream ID (7 bytes)

**Used when**:
- Continuing message stream on same chunk stream
- Message stream ID unchanged from previous chunk

**Format**:
```
Bytes 0-2:   Timestamp Delta (3 bytes, big-endian, milliseconds since last chunk)
Bytes 3-5:   Message Length (3 bytes, big-endian)
Byte 6:      Message Type ID (1 byte)
```

**Inherited from previous chunk**:
- Message Stream ID (last value on this CSID)

**Example** (Delta=100ms, length=128, type=8):
```
Hex: 00 00 64  00 00 80  08
     --------  --------  --
      delta     length   type
```

### FMT 2: Timestamp Delta Only (3 bytes)

**Used when**:
- Continuing same message type with same length
- Only timestamp changes

**Format**:
```
Bytes 0-2:   Timestamp Delta (3 bytes, big-endian)
```

**Inherited from previous chunk**:
- Message Length
- Message Type ID
- Message Stream ID

**Example** (Delta=33ms, typical for 30fps video):
```
Hex: 00 00 21
     --------
      delta
```

### FMT 3: No Header (0 bytes)

**Used when**:
- Continuing fragmented message (message length > chunk size)
- All fields identical to previous chunk

**Inherited from previous chunk**:
- Timestamp (or timestamp delta continues)
- Message Length
- Message Type ID
- Message Stream ID

**No additional bytes** - only Basic Header + Chunk Data

---

## Extended Timestamp

**Purpose**: Handle timestamps >= 16777215 (0xFFFFFF)

**Trigger**: When timestamp or timestamp delta in Message Header == 0xFFFFFF

**Size**: 4 bytes (big-endian uint32)

**Format**:
```
Bytes 0-3: Extended Timestamp (4 bytes, big-endian, full timestamp value)
```

**Placement**: Immediately after Message Header, before Chunk Data

**Rules**:
- If timestamp field in Message Header == 0xFFFFFF, Extended Timestamp is present
- Extended Timestamp contains the full timestamp value (not delta, even for FMT 1/2)
- For FMT 3 continuation chunks, Extended Timestamp is present if it was present in FMT 0/1/2

**Example** (Timestamp=20000000ms, type=9, length=1024):
```
Basic Header: 06 (fmt=0, csid=6)
Message Header: FF FF FF  00 04 00  09  01 00 00 00
                --------  --------  --  -----------
                0xFFFFFF   length   type stream ID
Extended Timestamp: 01 31 2D 00 (20000000 in hex)
Chunk Data: (1024 bytes video payload)
```

---

## Chunk Data

**Purpose**: Actual message payload bytes

**Size**: Up to `chunkSize` bytes (default 128, max 65536)

**Rules**:
- If message length <= chunk size: entire message in one chunk
- If message length > chunk size: split into multiple chunks
- Continuation chunks use FMT 3 (no header) with same CSID

**Example** (Message length=384 bytes, chunk size=128):
```
Chunk 1 (FMT 0): BasicHeader + MessageHeader(11) + ChunkData(128) = 140 bytes
Chunk 2 (FMT 3): BasicHeader + ChunkData(128) = 129 bytes
Chunk 3 (FMT 3): BasicHeader + ChunkData(128) = 129 bytes
Total: 398 bytes transmitted for 384-byte message
```

---

## Per-CSID State Cache

**Purpose**: Enable header compression (FMT 1, 2, 3) by remembering previous chunk fields

**State per CSID**:
- Last timestamp (absolute, uint32)
- Last message length (uint32)
- Last message type ID (uint8)
- Last message stream ID (uint32, little-endian)
- Partial message buffer (if message spans multiple chunks)
- Bytes received so far (uint32)

**Initialization**: All fields zero until first FMT 0 chunk received on CSID

**Updates**:
- FMT 0: Update all fields
- FMT 1: Update timestamp (add delta to last), message length, type ID; reuse stream ID
- FMT 2: Update timestamp (add delta); reuse length, type, stream ID
- FMT 3: Update timestamp (add previous delta if applicable); reuse all other fields

---

## Dechunking Algorithm (Reader)

**Purpose**: Reassemble complete messages from interleaved chunks

**Pseudocode**:
```go
chunkStreams := make(map[uint32]*ChunkStreamState)

for {
    // Read Basic Header
    fmt, csid := readBasicHeader(conn)
    
    // Get or create chunk stream state
    state := chunkStreams[csid]
    if state == nil {
        state = &ChunkStreamState{csid: csid}
        chunkStreams[csid] = state
    }
    
    // Read Message Header (based on FMT)
    switch fmt {
    case 0: // Full header
        timestamp := readUint24(conn)
        msgLength := readUint24(conn)
        msgTypeID := readUint8(conn)
        msgStreamID := readUint32LE(conn)
        
        if timestamp == 0xFFFFFF {
            timestamp = readUint32(conn) // Extended timestamp
        }
        
        state.lastTimestamp = timestamp
        state.lastMsgLength = msgLength
        state.lastMsgTypeID = msgTypeID
        state.lastMsgStreamID = msgStreamID
        state.buffer = make([]byte, 0, msgLength)
        state.bytesReceived = 0
        
    case 1: // No stream ID
        timestampDelta := readUint24(conn)
        msgLength := readUint24(conn)
        msgTypeID := readUint8(conn)
        
        if timestampDelta == 0xFFFFFF {
            timestampDelta = readUint32(conn)
        }
        
        state.lastTimestamp += timestampDelta
        state.lastMsgLength = msgLength
        state.lastMsgTypeID = msgTypeID
        state.buffer = make([]byte, 0, msgLength)
        state.bytesReceived = 0
        
    case 2: // Timestamp delta only
        timestampDelta := readUint24(conn)
        
        if timestampDelta == 0xFFFFFF {
            timestampDelta = readUint32(conn)
        }
        
        state.lastTimestamp += timestampDelta
        
    case 3: // No header
        // Reuse all previous fields
    }
    
    // Read chunk data
    bytesToRead := min(chunkSize, state.lastMsgLength - state.bytesReceived)
    chunkData := make([]byte, bytesToRead)
    io.ReadFull(conn, chunkData)
    
    state.buffer = append(state.buffer, chunkData...)
    state.bytesReceived += uint32(bytesToRead)
    
    // Check if message complete
    if state.bytesReceived == state.lastMsgLength {
        msg := &Message{
            csid:         csid,
            timestamp:    state.lastTimestamp,
            msgLength:    state.lastMsgLength,
            msgTypeID:    state.lastMsgTypeID,
            msgStreamID:  state.lastMsgStreamID,
            payload:      state.buffer,
        }
        
        dispatchMessage(msg)
        
        // Reset for next message on this CSID
        state.buffer = nil
        state.bytesReceived = 0
    }
}
```

---

## Chunking Algorithm (Writer)

**Purpose**: Fragment outgoing messages into chunks

**Pseudocode**:
```go
func sendMessage(conn net.Conn, msg *Message, chunkSize uint32) error {
    payloadRemaining := msg.payload
    offset := 0
    isFirstChunk := true
    
    for len(payloadRemaining) > 0 {
        chunkDataLen := min(chunkSize, uint32(len(payloadRemaining)))
        
        if isFirstChunk {
            // FMT 0 (full header)
            writeBasicHeader(conn, 0, msg.csid)
            
            timestamp := msg.timestamp
            if timestamp >= 0xFFFFFF {
                writeUint24(conn, 0xFFFFFF)
            } else {
                writeUint24(conn, timestamp)
            }
            
            writeUint24(conn, msg.msgLength)
            writeUint8(conn, msg.msgTypeID)
            writeUint32LE(conn, msg.msgStreamID)
            
            if timestamp >= 0xFFFFFF {
                writeUint32(conn, timestamp) // Extended timestamp
            }
            
            isFirstChunk = false
        } else {
            // FMT 3 (no header)
            writeBasicHeader(conn, 3, msg.csid)
            
            // Extended timestamp for continuation chunks (if used in first chunk)
            if msg.timestamp >= 0xFFFFFF {
                writeUint32(conn, msg.timestamp)
            }
        }
        
        // Write chunk data
        conn.Write(payloadRemaining[:chunkDataLen])
        
        payloadRemaining = payloadRemaining[chunkDataLen:]
        offset += int(chunkDataLen)
    }
    
    return nil
}
```

---

## Test Scenarios

### Test 1: Single Chunk (FMT 0, message fits in one chunk)

**Input Message**:
- CSID: 2
- Timestamp: 1000ms
- Type: 1 (Set Chunk Size)
- Stream ID: 0
- Payload: [0x00, 0x00, 0x10, 0x00] (4 bytes, chunk size 4096)

**Expected Output**:
```
Hex: 02 00 03 E8 00 00 04 01 00 00 00 00 00 00 10 00
     -- ---------- -------- -- ----------- -----------
     BH  timestamp   length  T   stream ID    payload
     
BH = Basic Header (fmt=0, csid=2)
```

**Byte Breakdown**:
- 0x02: Basic header (fmt=0, csid=2)
- 0x00 0x03 0xE8: Timestamp 1000
- 0x00 0x00 0x04: Message length 4
- 0x01: Type ID 1
- 0x00 0x00 0x00 0x00: Stream ID 0 (LE)
- 0x00 0x00 0x10 0x00: Payload

### Test 2: Multiple Chunks (FMT 0 + FMT 3, chunk size 128)

**Input Message**:
- CSID: 6
- Timestamp: 2000ms
- Type: 9 (Video)
- Stream ID: 1
- Payload: 384 bytes (zeros for simplicity)

**Expected Output**:
```
Chunk 1 (FMT 0):
  Hex: 06 00 07 D0 00 01 80 09 01 00 00 00 [128 bytes payload]
       -- ---------- -------- -- -----------
       BH  timestamp   length  T   stream ID

Chunk 2 (FMT 3):
  Hex: C6 [128 bytes payload]
       --
       BH (fmt=3, csid=6)

Chunk 3 (FMT 3):
  Hex: C6 [128 bytes payload]
       --
       BH
```

**Validation**:
- Chunk 1 basic header: 0x06 (fmt=0, csid=6)
- Chunk 2 basic header: 0xC6 (fmt=3, csid=6) = 11 000110
- Chunk 3 basic header: 0xC6 (same)
- Total payload bytes: 384 (128 + 128 + 128)

### Test 3: Extended Timestamp (timestamp >= 0xFFFFFF)

**Input Message**:
- CSID: 4
- Timestamp: 20000000ms (0x01312D00)
- Type: 8 (Audio)
- Stream ID: 1
- Payload: 64 bytes

**Expected Output**:
```
Hex: 04 FF FF FF 00 00 40 08 01 00 00 00 01 31 2D 00 [64 bytes payload]
     -- ---------- -------- -- ----------- -----------
     BH  0xFFFFFF    length  T   stream ID  ext timestamp

Basic Header: 0x04 (fmt=0, csid=4)
Timestamp: 0xFFFFFF (signals extended timestamp)
Extended Timestamp: 0x01312D00 (20000000)
```

### Test 4: Interleaved Chunks (Audio + Video)

**Scenario**: Audio and video messages interleaved

**Input**:
1. Audio message (CSID=4, 256 bytes, chunk size 128)
2. Video message (CSID=6, 256 bytes, chunk size 128)

**Expected Chunk Sequence**:
```
1. Audio FMT 0 + 128 bytes (CSID=4)
2. Video FMT 0 + 128 bytes (CSID=6)
3. Audio FMT 3 + 128 bytes (CSID=4, continuation)
4. Video FMT 3 + 128 bytes (CSID=6, continuation)
```

**Validation**:
- Dechunker maintains separate state for CSID=4 and CSID=6
- Audio message reassembled: 128 + 128 = 256 bytes
- Video message reassembled: 128 + 128 = 256 bytes

### Test 5: FMT 1 (Timestamp Delta, No Stream ID Change)

**Scenario**: Second audio message on same CSID, 33ms later

**Previous Message** (CSID=4):
- Timestamp: 1000ms, Type: 8, Stream ID: 1

**Current Message**:
- Timestamp: 1033ms (delta=33ms), Type: 8, Stream ID: 1, Payload: 64 bytes

**Expected Output**:
```
Hex: 44 00 00 21 00 00 40 08 [64 bytes payload]
     -- ---------- -------- --
     BH    delta     length  T

Basic Header: 0x44 (fmt=1, csid=4)
Timestamp Delta: 0x000021 (33ms)
Stream ID: Inherited from previous chunk (1)
```

### Test 6: FMT 2 (Timestamp Delta Only)

**Scenario**: Third audio message, same length and type, 33ms later

**Expected Output**:
```
Hex: 84 00 00 21 [64 bytes payload]
     -- --------
     BH   delta

Basic Header: 0x84 (fmt=2, csid=4)
Timestamp Delta: 0x000021 (33ms)
Length, Type, Stream ID: Inherited
```

---

## Performance Considerations

### Chunk Size Trade-offs

| Chunk Size | Overhead | Latency | CPU | Recommendation |
|------------|----------|---------|-----|----------------|
| 128        | High     | Low     | High| Default, low latency |
| 4096       | Low      | Medium  | Low | Recommended for throughput |
| 65536      | Very Low | High    | Very Low | Bulk transfer |

**Calculation** (1MB message, 30 fps video):
- 128-byte chunks: 1MB / 128 = 8192 chunks × 1 byte header = 8KB overhead (~0.8%)
- 4096-byte chunks: 1MB / 4096 = 256 chunks × 1 byte header = 256 bytes overhead (~0.025%)

### Memory Management

**Buffer Pooling**:
- Allocate buffers from pool for chunk sizes: 128, 4096, 65536
- Return buffers after message dispatch
- Reduce GC pressure for high-throughput streams

---

## References

- Adobe RTMP Specification 1.0, Section 5.3 (Chunking)
- FFmpeg libavformat/rtmppkt.c (chunk encoding/decoding)
- nginx-rtmp-module ngx_rtmp_receive.c (dechunking implementation)

---

**Status**: Contract complete. Ready for implementation and golden test generation.

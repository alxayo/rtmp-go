# Control Messages Contract

**Feature**: 001-rtmp-server-implementation  
**Package**: `internal/rtmp/control`  
**Date**: 2025-10-01

## Overview

This contract defines RTMP protocol control messages (types 1-6) used for flow control, bandwidth negotiation, and connection management. All control messages are sent on CSID=2, MSID=0.

**Reference**: Adobe RTMP Specification Section 5.4 (Protocol Control Messages)

---

## Message Type Summary

| Type ID | Name | Payload Size | Direction | Purpose |
|---------|------|--------------|-----------|---------|
| 1 | Set Chunk Size | 4 bytes | Both | Negotiate chunk size |
| 2 | Abort Message | 4 bytes | Both | Abort chunk stream |
| 3 | Acknowledgement | 4 bytes | Both | Flow control ACK |
| 4 | User Control Message | 6+ bytes | Server→Client | Stream events |
| 5 | Window Acknowledgement Size | 4 bytes | Both | Flow control window |
| 6 | Set Peer Bandwidth | 5 bytes | Server→Client | Bandwidth limit |

**Common Properties**:
- CSID: 2 (protocol control channel)
- MSID: 0 (no stream association)
- Chunk format: Typically FMT 0 (full header) on first send

---

## Type 1: Set Chunk Size

**Purpose**: Change the maximum chunk size for subsequent chunks sent by this endpoint

**Payload**: 4 bytes (big-endian uint32)

**Format**:
```
Bytes 0-3: Chunk Size (uint32, big-endian)
           Bit 31 must be 0 (max value 2147483647)
           Practical range: 128-65536
```

**Constraints**:
- Chunk size must be >= 1
- Bit 31 must be 0 (0x80000000 bit not set)
- Default chunk size: 128 bytes
- Recommended chunk size: 4096 bytes (better throughput)
- Maximum chunk size: 65536 bytes (practical limit)

**Example** (Set chunk size to 4096):
```
Hex: 00 00 10 00
     -----------
     Chunk size 4096 (big-endian)
```

**Complete Message** (CSID=2, MSID=0, Type=1):
```
Chunk:
  Basic Header: 02 (fmt=0, csid=2)
  Message Header: 00 00 00 00 00 04 01 00 00 00 00
                  ---------- -------- -- -----------
                  timestamp=0 length=4 T=1 stream=0
  Payload: 00 00 10 00
```

**Effect**:
- Sender updates its `sendChunkSize` to 4096
- Receiver must update its `receiveChunkSize` to 4096 for decoding subsequent chunks
- Each direction tracks its own chunk size independently

**Error Cases**:
- Chunk size == 0: Reject with error, close connection
- Bit 31 set: Reject with error, close connection
- Chunk size > 65536: Warning (accept but may cause memory issues)

**Test Scenarios**:
- Set chunk size to 128 (default): Verify parsing
- Set chunk size to 4096 (typical): Verify dechunker respects new size
- Set chunk size to 65536 (max): Verify large messages handled
- Set chunk size to 0: Expect error and connection close
- Set chunk size with bit 31 set: Expect error and connection close

---

## Type 2: Abort Message

**Purpose**: Discard a partially received message on a specific chunk stream

**Payload**: 4 bytes (big-endian uint32)

**Format**:
```
Bytes 0-3: Chunk Stream ID (uint32, big-endian)
           The CSID to abort
```

**Example** (Abort CSID=4):
```
Hex: 00 00 00 04
     -----------
     CSID to abort
```

**Effect**:
- Receiver discards any buffered data for specified CSID
- Resets chunk stream state for that CSID
- Next chunk on that CSID must be FMT 0 (new message)

**Use Cases**:
- Sender detects error and wants to abort current message
- Sender wants to skip remainder of large message
- Rare in practice (most implementations don't use this)

**Test Scenarios**:
- Send partial message (e.g., 200 bytes of 500-byte message), then Abort: Verify buffer cleared
- Send Abort for CSID with no active message: No-op, no error

---

## Type 3: Acknowledgement

**Purpose**: Notify peer that a certain number of bytes have been received (flow control)

**Payload**: 4 bytes (big-endian uint32)

**Format**:
```
Bytes 0-3: Sequence Number (uint32, big-endian)
           Total bytes received so far
```

**Example** (Acknowledge 1,000,000 bytes received):
```
Hex: 00 0F 42 40
     -----------
     1000000 bytes
```

**Trigger**:
- When `bytesReceived - lastAckSent >= windowAckSize`
- Prevents sender from transmitting beyond receiver's buffer capacity

**Pseudocode**:
```go
conn.bytesReceived += uint64(chunkDataLen)

if conn.bytesReceived - conn.lastAckSent >= uint64(conn.windowAckSize) {
    sendAcknowledgement(conn, conn.bytesReceived)
    conn.lastAckSent = conn.bytesReceived
}
```

**Test Scenarios**:
- Set window size 2,500,000; send 2,500,001 bytes: Expect ACK
- Send ACK with sequence number 0: Valid (initial state)
- Receive ACK: Server may track for flow control (implementation-specific)

---

## Type 4: User Control Message

**Purpose**: Send stream lifecycle events and ping/pong for keepalive

**Payload**: 2 bytes event type + event-specific data (4+ bytes)

**Format**:
```
Bytes 0-1: Event Type (uint16, big-endian)
Bytes 2+:  Event Data (varies by event type)
```

### Event Types

#### Event 0: Stream Begin

**Purpose**: Notify client that stream is ready for playback

**Payload**:
```
Bytes 0-1: 0x0000 (Stream Begin)
Bytes 2-5: Stream ID (uint32, big-endian)
```

**Example** (Stream ID=1):
```
Hex: 00 00 00 00 00 01
     ----- -----------
     event  stream ID
```

**When to Send**: After client sends `play` command, before sending media messages

#### Event 1: Stream EOF

**Purpose**: Notify client that stream has ended (publisher disconnected)

**Payload**:
```
Bytes 0-1: 0x0001 (Stream EOF)
Bytes 2-5: Stream ID (uint32, big-endian)
```

**Example** (Stream ID=1):
```
Hex: 00 01 00 00 00 01
     ----- -----------
     event  stream ID
```

**When to Send**: Publisher disconnects or calls `deleteStream`

#### Event 2: Stream Dry

**Purpose**: Notify client that no data is currently available (buffer empty)

**Payload**:
```
Bytes 0-1: 0x0002 (Stream Dry)
Bytes 2-5: Stream ID (uint32, big-endian)
```

**Use Case**: Rare, informational only

#### Event 3: Set Buffer Length

**Purpose**: Client tells server how much to buffer (in milliseconds)

**Payload**:
```
Bytes 0-1: 0x0003 (Set Buffer Length)
Bytes 2-5: Stream ID (uint32, big-endian)
Bytes 6-9: Buffer Length (uint32, big-endian, milliseconds)
```

**Example** (Stream ID=1, buffer 3000ms):
```
Hex: 00 03 00 00 00 01 00 00 0B B8
     ----- ----------- -----------
     event  stream ID   buffer ms
```

**Direction**: Client → Server (advisory, server may ignore)

#### Event 4: Stream Is Recorded

**Purpose**: Notify client that stream is being recorded

**Payload**:
```
Bytes 0-1: 0x0004 (Stream Is Recorded)
Bytes 2-5: Stream ID (uint32, big-endian)
```

**When to Send**: When server starts recording a stream

#### Event 6: Ping Request

**Purpose**: Keepalive / latency measurement

**Payload**:
```
Bytes 0-1: 0x0006 (Ping Request)
Bytes 2-5: Timestamp (uint32, big-endian, milliseconds)
```

**Example** (Timestamp=123456):
```
Hex: 00 06 00 01 E2 40
     ----- -----------
     event  timestamp
```

**Expected Response**: Ping Response (event 7) echoing timestamp

#### Event 7: Ping Response

**Purpose**: Response to Ping Request

**Payload**:
```
Bytes 0-1: 0x0007 (Ping Response)
Bytes 2-5: Timestamp (uint32, big-endian, echo from Ping Request)
```

**Example** (Echo timestamp=123456):
```
Hex: 00 07 00 01 E2 40
     ----- -----------
     event  timestamp
```

**Test Scenarios**:
- Send Stream Begin before media: Verify client receives
- Send Ping Request: Expect Ping Response with same timestamp
- Send Stream EOF on publisher disconnect: Verify players receive

---

## Type 5: Window Acknowledgement Size

**Purpose**: Negotiate the window size for flow control

**Payload**: 4 bytes (big-endian uint32)

**Format**:
```
Bytes 0-3: Window Size (uint32, big-endian, bytes)
```

**Example** (Window size 2,500,000 bytes):
```
Hex: 00 26 25 A0
     -----------
     2500000 bytes
```

**Complete Message**:
```
Chunk:
  Basic Header: 02 (fmt=0, csid=2)
  Message Header: 00 00 00 00 00 04 05 00 00 00 00
                  ---------- -------- -- -----------
                  timestamp=0 length=4 T=5 stream=0
  Payload: 00 26 25 A0
```

**Effect**:
- Receiver must send Acknowledgement (type 3) after receiving this many bytes
- Sender uses ACK to determine when it's safe to send more data (backpressure)

**Typical Values**:
- 2,500,000 bytes (common default)
- 5,000,000 bytes (higher throughput)

**Negotiation**:
- Server typically sends WAS immediately after handshake
- Client may send its own WAS (advisory)

**Test Scenarios**:
- Set WAS 2,500,000; send 2,500,001 bytes: Expect ACK from peer
- Set WAS 0: Invalid, reject with error

---

## Type 6: Set Peer Bandwidth

**Purpose**: Inform peer of bandwidth limit and how to enforce it

**Payload**: 5 bytes (4-byte bandwidth + 1-byte limit type)

**Format**:
```
Bytes 0-3: Bandwidth (uint32, big-endian, bytes per second)
Byte 4:    Limit Type (uint8)
           0 = Hard (peer must limit output to this value)
           1 = Soft (peer should limit, but may exceed if needed)
           2 = Dynamic (peer should adjust based on network conditions)
```

**Example** (Bandwidth 2,500,000 bytes/sec, Dynamic limit):
```
Hex: 00 26 25 A0 02
     ----------- --
     bandwidth   limit type
```

**Complete Message**:
```
Chunk:
  Basic Header: 02 (fmt=0, csid=2)
  Message Header: 00 00 00 00 00 05 06 00 00 00 00
                  ---------- -------- -- -----------
                  timestamp=0 length=5 T=6 stream=0
  Payload: 00 26 25 A0 02
```

**Limit Type Semantics**:

| Limit Type | Name | Behavior |
|------------|------|----------|
| 0 | Hard | Peer MUST NOT exceed this bandwidth |
| 1 | Soft | Peer SHOULD limit, but may exceed temporarily |
| 2 | Dynamic | Peer should adjust dynamically (recommended) |

**Effect**:
- Receiver adjusts its send rate accordingly (implementation-specific)
- Sender uses this for bandwidth estimation

**Typical Values**:
- Bandwidth: 2,500,000 bytes/sec (~20 Mbps)
- Limit Type: 2 (Dynamic)

**Negotiation**:
- Server sends SPB immediately after handshake
- Client may respond with its own SPB (rare)

**Test Scenarios**:
- Send SPB with Dynamic limit: Verify parsing
- Send SPB with Hard limit: Verify rate limiting (implementation-specific)
- Send SPB with bandwidth=0: Invalid, reject

---

## Control Burst Sequence (Post-Handshake)

**Purpose**: Immediately after handshake, server establishes flow control parameters

**Recommended Order** (Server → Client):
1. Window Acknowledgement Size (type 5)
2. Set Peer Bandwidth (type 6)
3. Set Chunk Size (type 1, optional but recommended)

**Example Sequence**:
```
Message 1: Window Ack Size = 2,500,000
Message 2: Set Peer Bandwidth = 2,500,000, Dynamic
Message 3: Set Chunk Size = 4096
```

**Timing**: Send immediately after handshake completion, before any other messages

**Client Response**:
- Client may send its own control messages (WAS, SPB, SCS)
- Client must honor server's chunk size for sending
- Client must send ACK when bytes received exceeds window

---

## Error Handling

### Invalid Control Message

**Scenarios**:
- Set Chunk Size with bit 31 set
- Set Chunk Size == 0
- Window Ack Size == 0
- Set Peer Bandwidth with invalid limit type (>2)

**Action**:
- Log error with context (message type, payload, connection ID)
- Optionally close connection (depends on severity)

### Missing Control Messages

**Scenarios**:
- Client doesn't send ACK when required
- Server doesn't send WAS/SPB after handshake

**Action**:
- Proceed with defaults (chunk size 128, no flow control)
- Log warning (non-fatal, some clients/servers skip control messages)

---

## Implementation Notes

### Encoding Control Messages

**Pseudocode**:
```go
func sendSetChunkSize(conn *Connection, chunkSize uint32) error {
    if chunkSize == 0 || (chunkSize & 0x80000000) != 0 {
        return errors.New("invalid chunk size")
    }
    
    payload := make([]byte, 4)
    binary.BigEndian.PutUint32(payload, chunkSize)
    
    msg := &Message{
        csid:        2,
        timestamp:   0,
        msgLength:   4,
        msgTypeID:   1,
        msgStreamID: 0,
        payload:     payload,
    }
    
    return conn.SendMessage(msg)
}
```

### Decoding Control Messages

**Pseudocode**:
```go
func handleControlMessage(conn *Connection, msg *Message) error {
    switch msg.msgTypeID {
    case 1: // Set Chunk Size
        chunkSize := binary.BigEndian.Uint32(msg.payload)
        if chunkSize == 0 || (chunkSize & 0x80000000) != 0 {
            return fmt.Errorf("invalid chunk size: %d", chunkSize)
        }
        conn.readChunkSize = chunkSize
        
    case 3: // Acknowledgement
        seqNum := binary.BigEndian.Uint32(msg.payload)
        // Track peer ACK for flow control (optional)
        
    case 4: // User Control Message
        eventType := binary.BigEndian.Uint16(msg.payload[0:2])
        switch eventType {
        case 6: // Ping Request
            timestamp := binary.BigEndian.Uint32(msg.payload[2:6])
            sendPingResponse(conn, timestamp)
        case 7: // Ping Response
            // Measure latency (optional)
        }
        
    case 5: // Window Ack Size
        windowSize := binary.BigEndian.Uint32(msg.payload)
        conn.windowAckSize = windowSize
        
    case 6: // Set Peer Bandwidth
        bandwidth := binary.BigEndian.Uint32(msg.payload[0:4])
        limitType := msg.payload[4]
        conn.peerBandwidth = bandwidth
        conn.limitType = limitType
    }
    
    return nil
}
```

---

## Test Coverage Requirements

| Test Case | Type | Priority |
|-----------|------|----------|
| Set Chunk Size (valid) | Unit | Critical |
| Set Chunk Size (invalid, bit 31 set) | Error | High |
| Set Chunk Size (zero) | Error | High |
| Acknowledgement (trigger at window boundary) | Integration | High |
| User Control Stream Begin | Unit | High |
| User Control Ping Request → Ping Response | Integration | Medium |
| Window Ack Size | Unit | High |
| Set Peer Bandwidth (all limit types) | Unit | Medium |
| Control burst sequence | Integration | Critical |

---

## References

- Adobe RTMP Specification 1.0, Section 5.4 (Protocol Control Messages)
- FFmpeg libavformat/rtmpproto.c (control message handling)
- nginx-rtmp-module ngx_rtmp_handler.c (control flow)

---

**Status**: Contract complete. Ready for implementation and test generation.

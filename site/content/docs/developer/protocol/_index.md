---
title: "RTMP Protocol Reference"
weight: 3
bookCollapseSection: true
---

# RTMP Protocol Reference

This is a practical reference for the RTMP protocol as implemented by go-rtmp. It covers the wire format details you need to understand the code.

## Protocol Overview

RTMP (Real-Time Messaging Protocol) is a TCP-based protocol for streaming audio, video, and data. A session goes through four phases:

1. **TCP Connect** — standard TCP three-way handshake to port 1935
2. **RTMP Handshake** — version negotiation and key exchange (C0/C1/C2 ↔ S0/S1/S2)
3. **Commands** — AMF0-encoded RPC calls to set up streams (`connect`, `createStream`, `publish`/`play`)
4. **Media** — continuous audio and video message flow

---

## Handshake

The handshake establishes the RTMP session. It's a fixed-size exchange with no negotiation — both sides must send exactly the right number of bytes.

### Packet Format

| Packet | Size | Contents |
|--------|------|----------|
| **C0/S0** | 1 byte | RTMP version: always `0x03` |
| **C1/S1** | 1536 bytes | 4-byte timestamp + 4 zero bytes + 1528 bytes random data |
| **C2/S2** | 1536 bytes | Echo of the peer's C1/S1 (timestamp + random) |

### Sequence Diagram

```
  Client                          Server
    │                               │
    │──── C0 + C1 (1537 bytes) ────►│
    │                               │
    │◄── S0 + S1 + S2 (3073 bytes)──│
    │                               │
    │──── C2 (1536 bytes) ─────────►│
    │                               │
    │     Handshake Complete         │
```

**Timing**: Both C0+C1 and S0+S1+S2 are sent as single writes. The server waits for C0+C1 before sending its response. Total bytes exchanged: **6145 bytes** (1537 + 3073 + 1536 - 1 shared). Timeout: **5 seconds**.

---

## Chunks

### Why Chunks Exist

RTMP multiplexes multiple message streams over a single TCP connection. A video keyframe might be 50KB, but audio packets are typically 200-500 bytes. Without chunking, a large video frame would block audio delivery. Chunks break large messages into fixed-size fragments that can be interleaved.

### Chunk Format

```
┌─────────────┬─────────────────┬────────────────────┬─────────┐
│ Basic Header│  Message Header │ Extended Timestamp  │ Payload │
│  (1-3 bytes)│  (0/3/7/11 bytes)│    (0/4 bytes)     │         │
└─────────────┴─────────────────┴────────────────────┴─────────┘
```

### Basic Header

The first byte contains the **FMT** (2 bits) and **CSID** (6 bits):

```
 7 6 5 4 3 2 1 0
┌─────┬───────────┐
│ FMT │   CSID    │
└─────┴───────────┘
```

- CSID 0: 2-byte header form (CSID = byte2 + 64)
- CSID 1: 3-byte header form (CSID = byte2 + byte3×256 + 64)
- CSID 2: reserved for control messages
- CSID 3-63: single-byte header form

### Message Header (FMT Types)

| FMT | Header Size | Fields | Use Case |
|-----|-------------|--------|----------|
| **0** | 11 bytes | timestamp (3) + msg length (3) + type ID (1) + msg stream ID (4) | First message on a CSID, or when timestamp wraps |
| **1** | 7 bytes | timestamp delta (3) + msg length (3) + type ID (1) | Same stream ID, different size/type |
| **2** | 3 bytes | timestamp delta (3) | Same stream ID, same size, same type (common for audio) |
| **3** | 0 bytes | (none) | Continuation chunk (same message, next fragment) |

> **Important**: The Message Stream ID (MSID) in FMT 0 is encoded as **little-endian**. This is the **only little-endian field** in the entire RTMP protocol. Everything else is big-endian.

### Extended Timestamp

When the timestamp or timestamp delta value is **≥ 0xFFFFFF** (16777215), the 3-byte field is set to `0xFFFFFF` and the actual value is stored in a 4-byte **extended timestamp** field immediately after the message header.

---

## Message Types

| TypeID | Name | CSID | MSID | Description |
|--------|------|------|------|-------------|
| **1** | Set Chunk Size | 2 | 0 | New maximum chunk payload size (1-16777215) |
| **2** | Abort Message | 2 | 0 | Discard partially received message on a CSID |
| **3** | Acknowledgement | 2 | 0 | Bytes received since last ACK |
| **4** | User Control | 2 | 0 | Stream events (StreamBegin, StreamEOF, etc.) |
| **5** | Window Ack Size | 2 | 0 | How many bytes before the peer must send an ACK |
| **6** | Set Peer Bandwidth | 2 | 0 | Output bandwidth limit + limit type |
| **8** | Audio | 3+ | 1+ | Audio data (AAC, MP3, etc.) |
| **9** | Video | 3+ | 1+ | Video data (H.264, H.265, etc.) |
| **20** | Command (AMF0) | 3+ | 0/1+ | RPC commands encoded in AMF0 |

Control messages (TypeID 1-6) always use **CSID 2** and **MSID 0**.

---

## Control Burst

After the handshake completes, both client and server exchange control messages to configure the session:

1. **Window Acknowledgement Size** (TypeID 5) — tells the peer to send an ACK after receiving this many bytes (typically 2500000)
2. **Set Peer Bandwidth** (TypeID 6) — limits the peer's outbound bandwidth (typically 2500000, limit type = Dynamic)
3. **Set Chunk Size** (TypeID 1) — increases chunk payload from the 128-byte default (go-rtmp uses 4096 by default)

---

## AMF0 Encoding

AMF0 (Action Message Format version 0) is a binary format for encoding structured data. RTMP commands use AMF0 for all RPC messages.

### Type Markers

| Marker | Type | Encoding |
|--------|------|----------|
| `0x00` | Number | 8-byte IEEE 754 double (big-endian) |
| `0x01` | Boolean | 1 byte (0x00 = false, 0x01 = true) |
| `0x02` | String | 2-byte length (big-endian) + UTF-8 bytes |
| `0x03` | Object | Key-value pairs until end marker |
| `0x05` | Null | No payload |
| `0x0A` | Strict Array | 4-byte count (big-endian) + N values |

### Object Format

An AMF0 object is a sequence of key-value pairs terminated by the **object end marker**: `0x00 0x00 0x09`.

```
0x03                          ← Object marker
  0x00 0x03 "app"             ← Key: 2-byte length + UTF-8
  0x02 0x00 0x04 "live"       ← Value: String marker + 2-byte length + UTF-8
  0x00 0x05 "tcUrl"           ← Key
  0x02 ... "rtmp://..."       ← Value
  0x00 0x00 0x09              ← Object end marker
```

---

## Command Flow

### Connect

```
Client → Server:  connect(txnID=1, {app:"live", tcUrl:"rtmp://host/live", ...})
Server → Client:  _result(txnID=1, {fmsVer:"FMS/3,0,1,123"}, {code:"NetConnection.Connect.Success"})
```

### Create Stream

```
Client → Server:  createStream(txnID=2, null)
Server → Client:  _result(txnID=2, null, streamID=1.0)
```

### Publish

```
Client → Server:  publish(txnID=0, null, "mystream", "live")
Server → Client:  onStatus(txnID=0, null, {code:"NetStream.Publish.Start"})
```

### Play

```
Client → Server:  play(txnID=0, null, "mystream")
Server → Client:  onStatus(txnID=0, null, {code:"NetStream.Play.Start"})
```

---

## Audio Format

Audio messages (TypeID 8) encode format information in the first byte:

```
 7 6 5 4 3 2 1 0
┌───────┬───┬───┬─┐
│Format │Rate│Sz │Ch│
└───────┴───┴───┴─┘
```

| Bits | Field | Values |
|------|-------|--------|
| 7-4 | SoundFormat | 2=MP3, 10=AAC, 11=Speex |
| 3-2 | SampleRate | 0=5.5kHz, 1=11kHz, 2=22kHz, 3=44kHz |
| 1 | SampleSize | 0=8-bit, 1=16-bit |
| 0 | Channels | 0=Mono, 1=Stereo |

### AAC Specifics

For AAC (SoundFormat=10), the second byte indicates the packet type:
- **0x00** = AAC sequence header (AudioSpecificConfig) — must be sent first, cached for late-join
- **0x01** = AAC raw data frame

---

## Video Format

Video messages (TypeID 9) encode format information in the first byte:

```
 7 6 5 4 3 2 1 0
┌───────┬─────────┐
│Frame  │ CodecID │
│Type   │         │
└───────┴─────────┘
```

| Bits | Field | Values |
|------|-------|--------|
| 7-4 | FrameType | 1=keyframe, 2=inter-frame, 5=video info |
| 3-0 | CodecID | 2=Sorenson H.263, 7=H.264 (AVC), 12=H.265 (HEVC) |

### H.264 (AVC) Specifics

For H.264 (CodecID=7), the second byte indicates the packet type:
- **0x00** = AVC sequence header (SPS/PPS) — must be sent first, cached for late-join
- **0x01** = AVC NALU (actual video data)
- **0x02** = AVC end of sequence

---

## Stream Keys

Stream keys are derived from the RTMP URL:

```
rtmp://host:port/app/streamName
                 └─┘ └─────────┘
                 app   stream name
```

The server combines these into a registry key: `app/streamName`. For example:
- URL: `rtmp://localhost:1935/live/mystream` → Key: `live/mystream`
- URL: `rtmp://localhost:1935/live/mystream?token=abc` → Key: `live/mystream` (query params stripped)

Publishers and subscribers must use the same key to share a stream. The query string is parsed separately for authentication tokens but is not part of the stream key.

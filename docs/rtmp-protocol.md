# RTMP Protocol Reference

A concise technical reference for the RTMP protocol as implemented by this server. For the full specification, see Adobe's [RTMP Specification](https://rtmp.veriskope.com/docs/spec/).

## Protocol Overview

RTMP (Real-Time Messaging Protocol) is a TCP-based protocol for streaming audio, video, and data. A typical session has four phases:

```
1. TCP Connect     (port 1935)
2. Handshake       (version exchange + random data echo)
3. Command Phase   (negotiate application, create streams, start publishing/playing)
4. Media Phase     (continuous audio/video frame transmission)
```

## Handshake

The handshake verifies both sides speak RTMP v3. Each party sends three pieces of data:

| Packet | Size | Contents |
|--------|------|----------|
| C0/S0 | 1 byte | Version (must be `0x03`) |
| C1/S1 | 1536 bytes | 4-byte timestamp + 4 zero bytes + 1528 random bytes |
| C2/S2 | 1536 bytes | Echo of the peer's C1/S1 (verifies connectivity) |

**Sequence:**
```
Client              Server
──────              ──────
C0+C1  ──────────►
       ◄──────────  S0+S1+S2
C2     ──────────►
```

After the handshake, both sides switch to chunk-based communication.

## Chunks

RTMP does not send complete messages over TCP. Instead, each message is split into **chunks** with a maximum payload size (default 128 bytes, negotiated up to 4096+).

### Why Chunks?

Large video keyframes (50+ KB) would block the connection for audio data. By interleaving small chunks from different streams, RTMP ensures low-latency audio/video multiplexing.

### Chunk Format

```
┌──────────────┬──────────────────┬─────────────────────┬──────────┐
│ Basic Header │  Message Header  │ Extended Timestamp?  │ Payload  │
│  (1-3 bytes) │ (0/3/7/11 bytes) │    (0 or 4 bytes)   │ (≤chunk  │
│              │                  │                      │   size)  │
└──────────────┴──────────────────┴─────────────────────┴──────────┘
```

### Basic Header

The first byte encodes two values:
- **Bits 7-6**: FMT (header format type, 0-3)
- **Bits 5-0**: Chunk Stream ID (CSID)

CSID encoding:
- Values 2-63: 1-byte form (CSID in bits 5-0)
- Value 0 in bits 5-0: 2-byte form (next byte + 64)
- Value 1 in bits 5-0: 3-byte form (next 2 bytes + 64)

### Message Header (FMT Types)

FMT controls how much header information is present. Higher FMT values omit unchanged fields:

| FMT | Header Size | Fields Present | When Used |
|-----|-------------|----------------|-----------|
| 0 | 11 bytes | Timestamp (abs), Length, TypeID, StreamID | First message on CSID |
| 1 | 7 bytes | Timestamp (delta), Length, TypeID | Same stream, different size/type |
| 2 | 3 bytes | Timestamp (delta) | Same stream, same size/type |
| 3 | 0 bytes | (none — all inherited) | Continuation chunks |

### Extended Timestamp

When the 3-byte timestamp field equals `0xFFFFFF` (16,777,215), an additional 4-byte timestamp follows the message header. This supports timestamps beyond ~4.66 hours.

### Message Stream ID Quirk

The 4-byte Message Stream ID in FMT 0 headers is encoded in **little-endian** — the only little-endian field in RTMP. All other multi-byte integers are big-endian.

## Message Types

| TypeID | Name | Purpose |
|--------|------|---------|
| 1 | Set Chunk Size | Change maximum chunk payload size |
| 2 | Abort Message | Discard a partially received message |
| 3 | Acknowledgement | Report bytes received (flow control) |
| 4 | User Control | Stream lifecycle events (Begin, Ping) |
| 5 | Window Ack Size | Set acknowledgement window |
| 6 | Set Peer Bandwidth | Limit output rate |
| 8 | Audio | Audio data (AAC, MP3, Speex) |
| 9 | Video | Video data (H.264, H.265) |
| 20 | Command (AMF0) | Application commands (connect, publish, play) |

## Control Burst

After the handshake, the server sends three control messages:

1. **Window Acknowledgement Size** (2,500,000 bytes) — flow control
2. **Set Peer Bandwidth** (2,500,000 bytes, Dynamic) — output rate hint
3. **Set Chunk Size** (4096 bytes) — increase from default 128

## AMF0 Encoding

Commands are serialized in AMF0 (Action Message Format version 0):

| Marker | Type | Example |
|--------|------|---------|
| `0x00` | Number | `42.0` (IEEE 754 double) |
| `0x01` | Boolean | `true` / `false` (1 byte) |
| `0x02` | String | `"live"` (2-byte length + UTF-8) |
| `0x03` | Object | `{"app":"live"}` (key-value pairs, ends with `0x00 0x00 0x09`) |
| `0x05` | Null | (no payload) |
| `0x0A` | Array | `[1, "x", true]` (4-byte count + elements) |

## Command Flow

### Connect

```
Client → Server:  ["connect", 1.0, {"app":"live", "tcUrl":"rtmp://host/live", ...}]
Server → Client:  ["_result", 1.0, {fmsVer, capabilities}, {code:"NetConnection.Connect.Success"}]
```

### Create Stream

```
Client → Server:  ["createStream", 2.0, null]
Server → Client:  ["_result", 2.0, null, 1.0]     // stream ID = 1
Server → Client:  UserControl StreamBegin(1)
```

### Publish

```
Client → Server:  ["publish", 0, null, "mystream", "live"]
Server → Client:  ["onStatus", 0, null, {code:"NetStream.Publish.Start"}]
```

After this, the client sends audio (TypeID 8) and video (TypeID 9) messages.

### Play

```
Client → Server:  ["play", 0, null, "mystream", -2]     // -2 = live
Server → Client:  UserControl StreamBegin(1)
Server → Client:  ["onStatus", 0, null, {code:"NetStream.Play.Start"}]
Server → Client:  (cached audio sequence header, if available)
Server → Client:  (cached video sequence header, if available)
```

After this, the server forwards media messages from the publisher.

## Audio Message Format

The first byte of an audio message payload:

```
Bits 7-4: SoundFormat (codec)     Bits 3-2: SampleRate
Bit 1:    SampleSize              Bit 0:    Channels
```

Key codec IDs: 2=MP3, 10=AAC, 11=Speex

For AAC, byte 2 distinguishes:
- `0x00` = Sequence Header (AudioSpecificConfig — decoder initialization)
- `0x01` = Raw AAC frame data

## Video Message Format

The first byte of a video message payload:

```
Bits 7-4: FrameType              Bits 3-0: CodecID
```

Key values: FrameType 1=Keyframe, 2=Inter-frame. CodecID 7=H.264 (AVC), 12=H.265 (HEVC).

For H.264, byte 2 distinguishes:
- `0x00` = Sequence Header (SPS/PPS — decoder initialization)
- `0x01` = NALU (actual video data)

## Sequence Headers

The first audio and video messages from a publisher are typically **sequence headers** — they contain codec configuration data that decoders need before processing any media frames:

- **H.264 Video Sequence Header**: Contains SPS (Sequence Parameter Set) and PPS (Picture Parameter Set) — resolution, profile, frame rate parameters
- **AAC Audio Sequence Header**: Contains AudioSpecificConfig — sample rate, channel count, codec profile

The server caches these so late-joining subscribers can immediately initialize their decoders.

## Stream Keys

RTMP identifies streams using an **application name** + **stream name**:

```
URL: rtmp://host:1935/live/mystream
         └── host ──┘ └app┘ └stream┘

Stream Key: "live/mystream"
```

The application name is sent in the `connect` command. The stream name is sent in `publish` or `play`.

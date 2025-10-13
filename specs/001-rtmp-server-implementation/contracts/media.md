# RTMP Media Message Contracts

**Feature**: 001-rtmp-server-implementation  
**Package**: `internal/rtmp/media`  
**Date**: 2025-10-01

## Overview

This contract defines RTMP Audio (Type 8) and Video (Type 9) message formats. Media messages carry encoded audio/video frames using FLV tag format, transmitted after `publish` command.

**Reference**: FLV File Format Specification v10.1, RTMP Specification Section 7.1

**Scope**: Message format parsing only. **No transcoding, codec implementation, or frame parsing**. Server acts as transparent relay for encoded media.

---

## Audio Message (Type 8)

**Purpose**: Carry encoded audio frames

**Message Type**: 8 (Audio Data)  
**CSID**: 6 (audio channel)  
**MSID**: Stream ID (e.g., 1)  
**Timestamp**: Relative time in milliseconds (DTS, not PTS)

### Audio Tag Format

```
Byte 0:     Audio Tag Header (1 byte)
            Bits 7-4:  Sound Format (4 bits)
            Bits 3-2:  Sound Rate (2 bits)
            Bit 1:     Sound Size (1 bit)
            Bit 0:     Sound Type (1 bit)
Byte 1+:    Audio Data (codec-specific, variable length)
```

### Audio Tag Header Breakdown

**Bits 7-4: Sound Format**

| Value | Codec | Description |
|-------|-------|-------------|
| 0 | Linear PCM, platform endian | Uncompressed |
| 1 | ADPCM | Compressed |
| 2 | MP3 | MPEG-1 Layer 3 |
| 3 | Linear PCM, little endian | Uncompressed |
| 4 | Nellymoser 16kHz mono | Speech codec |
| 5 | Nellymoser 8kHz mono | Speech codec |
| 6 | Nellymoser | General |
| 7 | G.711 A-law | Telephony |
| 8 | G.711 mu-law | Telephony |
| 9 | Reserved | - |
| 10 | AAC | MPEG-4 AAC |
| 11 | Speex | Open source |
| 14 | MP3 8kHz | Low bitrate |
| 15 | Device-specific | - |

**Most Common**: AAC (10), MP3 (2)

**Bits 3-2: Sound Rate** (sample rate)

| Value | Sample Rate |
|-------|-------------|
| 0 | 5.5 kHz |
| 1 | 11 kHz |
| 2 | 22 kHz |
| 3 | 44 kHz |

**Note**: For AAC, this field is ignored (sample rate in AudioSpecificConfig).

**Bit 1: Sound Size** (sample width)

| Value | Bit Depth |
|-------|-----------|
| 0 | 8-bit |
| 1 | 16-bit |

**Note**: Compressed formats (AAC, MP3) always use 16-bit.

**Bit 0: Sound Type** (channels)

| Value | Channels |
|-------|----------|
| 0 | Mono |
| 1 | Stereo |

### Audio Data Payload

**For AAC (Sound Format = 10)**:

```
Byte 0:     Audio Tag Header (0xAF typical: AAC, 44kHz, 16-bit, Stereo)
Byte 1:     AAC Packet Type
            0x00 = AAC Sequence Header (AudioSpecificConfig)
            0x01 = AAC Raw (compressed audio frame)
Byte 2+:    AAC Data
```

**AAC Packet Type 0x00** (Sequence Header):
- Sent **once** at start of stream
- Contains AudioSpecificConfig (2+ bytes, ISO 14496-3)
- Example: `0xAF 0x00 <AudioSpecificConfig bytes>`

**AAC Packet Type 0x01** (Raw Frame):
- Sent for each audio frame
- Contains compressed AAC data
- Example: `0xAF 0x01 <AAC compressed data>`

**For MP3 (Sound Format = 2)**:

```
Byte 0:     Audio Tag Header (0x2F typical: MP3, 44kHz, 16-bit, Stereo)
Byte 1+:    MP3 Frame Data (starts with sync word 0xFFF)
```

**No sequence header needed for MP3** (self-describing format).

### Example: AAC Sequence Header

```
Audio Tag Header: 0xAF
  Bits 7-4: 1010 (10 = AAC)
  Bits 3-2: 11 (3 = 44kHz)
  Bit 1:    1 (16-bit)
  Bit 0:    1 (Stereo)

AAC Packet Type:  0x00 (Sequence Header)

AudioSpecificConfig: 0x12 0x10
  (2 bytes, decoded per ISO 14496-3)
  Profile: AAC-LC
  Sample Rate: 44.1kHz
  Channels: 2 (Stereo)
```

**Full Message Payload**:
```
Hex: AF 00 12 10
     -- -- -----
     Audio AAC  AudioSpecificConfig
     Header Seq
```

### Example: AAC Raw Frame

```
Audio Tag Header: 0xAF
AAC Packet Type:  0x01 (Raw)
AAC Data:         [compressed audio bytes, e.g., 200 bytes]
```

**Full Message Payload**:
```
Hex: AF 01 [200 bytes of compressed AAC data]
     -- --
     Audio AAC Raw
     Header
```

### Audio Message Structure

**RTMP Message**:
- **Type**: 8 (Audio)
- **CSID**: 6
- **Timestamp**: Milliseconds (e.g., 0, 23, 46, ...)
- **MSID**: 1 (stream ID)
- **Payload**: Audio Tag Header (1 byte) + [AAC Packet Type (1 byte)] + Audio Data

**Chunk Format**: Typically FMT 1 (delta timestamp) or FMT 3 (continuation) after first frame.

---

## Video Message (Type 9)

**Purpose**: Carry encoded video frames

**Message Type**: 9 (Video Data)  
**CSID**: 7 (video channel)  
**MSID**: Stream ID (e.g., 1)  
**Timestamp**: Relative time in milliseconds (DTS, not PTS)

### Video Tag Format

```
Byte 0:     Video Tag Header (1 byte)
            Bits 7-4:  Frame Type (4 bits)
            Bits 3-0:  Codec ID (4 bits)
Byte 1+:    Video Data (codec-specific, variable length)
```

### Video Tag Header Breakdown

**Bits 7-4: Frame Type**

| Value | Type | Description |
|-------|------|-------------|
| 1 | Keyframe (I-frame) | Seekable, full frame |
| 2 | Inter frame (P-frame) | Predicted frame |
| 3 | Disposable inter frame (B-frame) | Bidirectional |
| 4 | Generated keyframe | Server-generated |
| 5 | Video info / command frame | Metadata |

**Most Common**: 1 (Keyframe), 2 (Inter frame)

**Bits 3-0: Codec ID**

| Value | Codec | Description |
|-------|-------|-------------|
| 1 | JPEG | Motion JPEG |
| 2 | Sorenson H.263 | Flash-era codec |
| 3 | Screen video | Screen sharing |
| 4 | On2 VP6 | Flash video |
| 5 | On2 VP6 with alpha | Transparency |
| 6 | Screen video v2 | Screen sharing v2 |
| 7 | AVC (H.264) | MPEG-4 Part 10 |
| 12 | HEVC (H.265) | High Efficiency |

**Most Common**: AVC/H.264 (7)

### Video Data Payload

**For AVC/H.264 (Codec ID = 7)**:

```
Byte 0:     Video Tag Header (0x17 = Keyframe + AVC, or 0x27 = Inter + AVC)
Byte 1:     AVC Packet Type
            0x00 = AVC Sequence Header (SPS/PPS)
            0x01 = AVC NALU (compressed video frame)
            0x02 = AVC End of Sequence
Bytes 2-4:  Composition Time Offset (int24, big-endian, signed)
            CTS offset in milliseconds (PTS = DTS + CTS)
            Typically 0 for simple streams
Byte 5+:    AVC Data
```

**AVC Packet Type 0x00** (Sequence Header):
- Sent **once** at start of stream (before first video frame)
- Contains AVCDecoderConfigurationRecord (SPS/PPS, ISO 14496-15)
- Example: `0x17 0x00 0x00 0x00 0x00 <AVCDecoderConfigurationRecord>`

**AVC Packet Type 0x01** (NALU):
- Sent for each video frame
- Contains one or more NAL Units (H.264 compressed data)
- Format: Length-prefixed NALUs (4-byte length + NALU data)
- Example: `0x17 0x01 0x00 0x00 0x00 <NALUs>`

**Composition Time Offset (CTS)**:
- Difference between PTS and DTS (for B-frames)
- Most streams use 0 (PTS = DTS)
- Signed int24 in milliseconds

### Example: AVC Sequence Header

```
Video Tag Header: 0x17
  Bits 7-4: 0001 (1 = Keyframe)
  Bits 3-0: 0111 (7 = AVC)

AVC Packet Type:  0x00 (Sequence Header)

Composition Time: 0x00 0x00 0x00 (0 ms offset)

AVCDecoderConfigurationRecord:
  [SPS/PPS data, e.g., 50 bytes]
```

**Full Message Payload**:
```
Hex: 17 00 00 00 00 [50 bytes of AVCDecoderConfigurationRecord]
     -- -- -------- 
     Video AVC CTS=0
     Header Seq
```

### Example: AVC Keyframe (I-frame)

```
Video Tag Header: 0x17 (Keyframe + AVC)
AVC Packet Type:  0x01 (NALU)
Composition Time: 0x00 0x00 0x00 (0 ms)
AVC Data:         [NALUs, e.g., 1500 bytes]
  Length-prefixed NALUs:
    0x00 0x00 0x05 0xA0 [NALU 1, 1440 bytes]
    0x00 0x00 0x00 0x20 [NALU 2, 32 bytes]
```

**Full Message Payload**:
```
Hex: 17 01 00 00 00 [1500 bytes of NALUs]
     -- -- --------
     Video AVC CTS=0
     Header NALU
```

### Example: AVC Inter Frame (P-frame)

```
Video Tag Header: 0x27 (Inter frame + AVC)
AVC Packet Type:  0x01 (NALU)
Composition Time: 0x00 0x00 0x00 (0 ms)
AVC Data:         [NALUs, e.g., 500 bytes]
```

**Full Message Payload**:
```
Hex: 27 01 00 00 00 [500 bytes of NALUs]
     -- -- --------
     Video AVC CTS=0
     Header NALU
```

### Video Message Structure

**RTMP Message**:
- **Type**: 9 (Video)
- **CSID**: 7
- **Timestamp**: Milliseconds (e.g., 0, 40, 80, ... for 25fps)
- **MSID**: 1 (stream ID)
- **Payload**: Video Tag Header (1 byte) + [AVC Packet Type (1 byte) + CTS (3 bytes)] + Video Data

**Chunk Format**: Typically FMT 1 (delta timestamp) or FMT 3 (continuation) after first frame.

---

## Media Message Flow

### Publish Stream (FFmpeg)

```
1. Handshake (C0/C1/S0/S1/S2/C2)
2. connect command
3. _result (connect)
4. createStream command
5. _result (stream_id=1)
6. publish command (stream_id=1, key="test")
7. onStatus (NetStream.Publish.Start)

--- Media Data ---

8. Audio Message (Type 8):
   - Timestamp: 0
   - Payload: 0xAF 0x00 <AudioSpecificConfig> (AAC Sequence Header)

9. Video Message (Type 9):
   - Timestamp: 0
   - Payload: 0x17 0x00 0x00 0x00 0x00 <AVCDecoderConfigurationRecord> (AVC Sequence Header)

10. Video Message (Type 9):
    - Timestamp: 0
    - Payload: 0x17 0x01 0x00 0x00 0x00 <NALUs> (Keyframe)

11. Audio Message (Type 8):
    - Timestamp: 23
    - Payload: 0xAF 0x01 <AAC data>

12. Video Message (Type 9):
    - Timestamp: 40
    - Payload: 0x27 0x01 0x00 0x00 0x00 <NALUs> (Inter frame)

[Repeat audio/video messages...]

--- End ---

13. deleteStream command (stream_id=1)
```

### Play Stream (ffplay)

```
1. Handshake
2. connect command
3. _result (connect)
4. createStream command
5. _result (stream_id=1)
6. play command (stream_id=1, key="test", start=-2)
7. onStatus (NetStream.Play.Start)

--- Server Sends Media Data ---

8. Audio Message (AAC Sequence Header)
9. Video Message (AVC Sequence Header)
10. Video Message (Keyframe)
11. Audio Message (AAC frame)
12. Video Message (Inter frame)
[...]
```

---

## Server Implementation Notes

### Media Message Parsing

**Extract Metadata** (do not decode codec data):

**Audio**:
```go
func parseAudioTag(payload []byte) (AudioInfo, error) {
    if len(payload) < 1 {
        return AudioInfo{}, errors.New("audio tag too short")
    }
    
    header := payload[0]
    soundFormat := (header >> 4) & 0x0F
    soundRate := (header >> 2) & 0x03
    soundSize := (header >> 1) & 0x01
    soundType := header & 0x01
    
    info := AudioInfo{
        Codec:     soundFormat,  // 10 = AAC
        Rate:      soundRate,    // 3 = 44kHz
        BitDepth:  soundSize,    // 1 = 16-bit
        Channels:  soundType,    // 1 = Stereo
    }
    
    if soundFormat == 10 && len(payload) >= 2 {
        info.AACPacketType = payload[1]  // 0x00 = Seq Header, 0x01 = Raw
    }
    
    return info, nil
}
```

**Video**:
```go
func parseVideoTag(payload []byte) (VideoInfo, error) {
    if len(payload) < 1 {
        return VideoInfo{}, errors.New("video tag too short")
    }
    
    header := payload[0]
    frameType := (header >> 4) & 0x0F
    codecID := header & 0x0F
    
    info := VideoInfo{
        FrameType: frameType,  // 1 = Keyframe, 2 = Inter
        Codec:     codecID,    // 7 = AVC
    }
    
    if codecID == 7 && len(payload) >= 5 {
        info.AVCPacketType = payload[1]  // 0x00 = Seq Header, 0x01 = NALU
        // CTS: int24 big-endian at bytes 2-4
        cts := int32(payload[2])<<16 | int32(payload[3])<<8 | int32(payload[4])
        if cts >= 0x800000 {
            cts -= 0x1000000  // Sign extend
        }
        info.CompositionTime = cts
    }
    
    return info, nil
}
```

### Sequence Header Detection

**First Audio/Video Messages**:
- AAC Sequence Header: `payload[0] = 0xAF, payload[1] = 0x00`
- AVC Sequence Header: `payload[0] = 0x17, payload[1] = 0x00`

**Cache Sequence Headers**:
```go
type Stream struct {
    AudioSeqHeader []byte  // Store first audio message with 0xAF 0x00
    VideoSeqHeader []byte  // Store first video message with 0x17 0x00
}

// On new subscriber:
func (s *Stream) SendMetadata(conn *Connection) {
    if s.AudioSeqHeader != nil {
        conn.SendAudioMessage(0, s.AudioSeqHeader)  // Timestamp=0
    }
    if s.VideoSeqHeader != nil {
        conn.SendVideoMessage(0, s.VideoSeqHeader)  // Timestamp=0
    }
}
```

### Timestamp Handling

- **DTS (Decoding Time Stamp)**: RTMP message timestamp
- **PTS (Presentation Time Stamp)**: DTS + CTS (Composition Time Offset)
- **Server Role**: Pass through timestamps unchanged (no rewriting)
- **Monotonic Check**: Optionally warn if timestamps are non-monotonic

### Relay Logic (No Transcoding)

**Publisher → Subscribers**:
```go
func (s *Stream) PublishMessage(msg *Message) {
    // Store sequence headers
    if msg.Type == 8 && len(msg.Payload) >= 2 {
        if msg.Payload[0] == 0xAF && msg.Payload[1] == 0x00 {
            s.AudioSeqHeader = msg.Payload
        }
    }
    if msg.Type == 9 && len(msg.Payload) >= 2 {
        if msg.Payload[0] == 0x17 && msg.Payload[1] == 0x00 {
            s.VideoSeqHeader = msg.Payload
        }
    }
    
    // Relay to all subscribers
    for _, subscriber := range s.Subscribers {
        subscriber.SendMessage(msg)
    }
}
```

---

## Codec Support Matrix

| Codec | Type | Tag Header | Server Support |
|-------|------|------------|----------------|
| AAC | Audio | 0xAF | ✅ Relay only |
| MP3 | Audio | 0x2F | ✅ Relay only |
| H.264 (AVC) | Video | 0x17/0x27 | ✅ Relay only |
| H.265 (HEVC) | Video | 0x1C/0x2C | ⚠️ Relay (untested) |
| Opus | Audio | Custom | ❌ Out of scope |

**Note**: Server does not decode/encode media. All codecs are relayed as-is.

---

## Test Scenarios

### Golden Tests

| Test Case | Input | Expected Parse |
|-----------|-------|----------------|
| AAC Seq Header | `0xAF 0x00 0x12 0x10` | Codec=AAC, PacketType=SeqHeader |
| AAC Raw Frame | `0xAF 0x01 [data]` | Codec=AAC, PacketType=Raw |
| AVC Seq Header | `0x17 0x00 0x00 0x00 0x00 [SPS/PPS]` | FrameType=Keyframe, Codec=AVC, PacketType=SeqHeader |
| AVC Keyframe | `0x17 0x01 0x00 0x00 0x00 [NALUs]` | FrameType=Keyframe, Codec=AVC, PacketType=NALU |
| AVC Inter Frame | `0x27 0x01 0x00 0x00 0x00 [NALUs]` | FrameType=Inter, Codec=AVC, PacketType=NALU |

### Integration Tests

- **Publish AAC+AVC**: Verify sequence headers cached
- **Relay to Subscriber**: Verify sequence headers sent first
- **Timestamp Continuity**: Verify monotonic timestamps
- **Codec Detection**: Verify correct codec ID parsing

### FFmpeg Interop

**Publish Test**:
```bash
ffmpeg -re -i test.mp4 -c:v copy -c:a copy -f flv rtmp://localhost:1935/live/test
```

**Expected Messages**:
1. Audio: 0xAF 0x00 (AAC Seq Header)
2. Video: 0x17 0x00 (AVC Seq Header)
3. Video: 0x17 0x01 (Keyframe)
4. Audio: 0xAF 0x01 (AAC frame)
5. Video: 0x27 0x01 (Inter frame)
[...]

**Play Test**:
```bash
ffplay rtmp://localhost:1935/live/test
```

**Expected Behavior**: Playback with <3s latency, no video corruption.

---

## Error Handling

### Invalid Media Messages

- **Payload too short** (< 1 byte): Drop message, log warning
- **Unknown codec**: Relay anyway (don't block)
- **Missing sequence header**: Allow (some codecs don't require it)

### Timestamp Anomalies

- **Non-monotonic timestamps**: Log warning, relay unchanged
- **Large timestamp jumps** (> 10s): Log warning, continue
- **Negative timestamps**: Invalid, but relay unchanged

---

## References

- FLV File Format Specification v10.1 (Adobe)
- ISO 14496-3 (AAC - AudioSpecificConfig)
- ISO 14496-10 (H.264/AVC)
- ISO 14496-15 (AVC in MP4/FLV - AVCDecoderConfigurationRecord)
- FFmpeg libavformat/flvenc.c (FLV tag encoding)

---

**Status**: Contract complete. Ready for implementation (parser only, no codec implementation).

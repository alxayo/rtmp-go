# RTMP Command Message Contracts

**Feature**: 001-rtmp-server-implementation  
**Package**: `internal/rtmp/rpc`  
**Date**: 2025-10-01

## Overview

This contract defines RTMP NetConnection and NetStream command messages used for establishing connections and controlling streams. Commands use AMF0 encoding for RPC-style communication.

**Reference**: RTMP Specification Section 7 (Command Messages)

**Message Type**: 20 (AMF0 Command), CSID: 3, MSID: 0 (for NetConnection) or stream MSID (for NetStream)

---

## Command Message Structure

All commands follow this format:

```
[Command Name]  String (AMF0)
[Transaction ID] Number (AMF0)
[Command Object] Object/Null (AMF0)
[Additional Arguments...] (AMF0 values)
```

**Fields**:
- **Command Name**: String identifying the command (e.g., `"connect"`, `"publish"`)
- **Transaction ID**: Number for matching requests/responses (0 for notifications)
- **Command Object**: Context data (Object or Null depending on command)
- **Additional Arguments**: Command-specific parameters

**Encoding**: All fields are AMF0-encoded, concatenated in a single message payload.

---

## NetConnection Commands

### Command: `connect`

**Purpose**: Establish connection to application

**Direction**: Client → Server

**Message Type**: 20 (AMF0 Command), CSID: 3, MSID: 0

**Format**:
```
Command Name:   "connect" (String)
Transaction ID: 1 (Number, always 1 for connect)
Command Object: {
    "app": "live",               // Application name (String)
    "flashVer": "FMLE/3.0",      // Client version (String)
    "tcUrl": "rtmp://host/live", // Connection URL (String)
    "fpad": false,               // Use proxy? (Boolean, optional)
    "audioCodecs": 3575,         // Supported audio codecs (Number, optional)
    "videoCodecs": 252,          // Supported video codecs (Number, optional)
    "videoFunction": 1,          // Seeking capability (Number, optional)
    "objectEncoding": 0          // AMF version (Number, 0=AMF0, 3=AMF3)
}
Optional:       null (no additional arguments)
```

**Example Payload** (AMF0 encoded):
```
02 00 07 63 6F 6E 6E 65 63 74    // "connect" (String)
00 3F F0 00 00 00 00 00 00        // 1.0 (Transaction ID)
03                                // Object marker
  00 03 61 70 70                  // "app"
  02 00 04 6C 69 76 65            // "live"
  00 08 66 6C 61 73 68 56 65 72  // "flashVer"
  02 00 08 46 4D 4C 45 2F 33 2E 30 // "FMLE/3.0"
  00 05 74 63 55 72 6C            // "tcUrl"
  02 00 13 72 74 6D 70 3A 2F 2F 6C 6F 63 61 6C 68 6F 73 74 2F 6C 69 76 65 // "rtmp://localhost/live"
  00 0E 6F 62 6A 65 63 74 45 6E 63 6F 64 69 6E 67 // "objectEncoding"
  00 00 00 00 00 00 00 00 00      // 0.0 (AMF0)
00 00 09                          // End of object
```

**Server Response**: `_result` or `_error` command

---

### Command: `_result` (connect response)

**Purpose**: Acknowledge successful connection

**Direction**: Server → Client

**Message Type**: 20 (AMF0 Command), CSID: 3, MSID: 0

**Format**:
```
Command Name:   "_result" (String)
Transaction ID: 1 (Number, matches connect transaction ID)
Properties:     {
    "fmsVer": "FMS/3,5,7,7009",  // Server version (String)
    "capabilities": 31,           // Server capabilities (Number)
    "mode": 1                     // Server mode (Number, 1=publish)
}
Information:    {
    "level": "status",            // Status level (String)
    "code": "NetConnection.Connect.Success", // Status code (String)
    "description": "Connection succeeded.",  // Description (String)
    "objectEncoding": 0           // AMF version (Number)
}
```

**Example Payload** (AMF0 encoded):
```
02 00 07 5F 72 65 73 75 6C 74    // "_result" (String)
00 3F F0 00 00 00 00 00 00        // 1.0 (Transaction ID)
03                                // Properties object
  00 06 66 6D 73 56 65 72          // "fmsVer"
  02 00 0F 46 4D 53 2F 33 2C 35 2C 37 2C 37 30 30 39 // "FMS/3,5,7,7009"
  00 0C 63 61 70 61 62 69 6C 69 74 69 65 73 // "capabilities"
  00 40 3F 00 00 00 00 00 00      // 31.0
00 00 09                          // End of object
03                                // Information object
  00 05 6C 65 76 65 6C            // "level"
  02 00 06 73 74 61 74 75 73      // "status"
  00 04 63 6F 64 65                // "code"
  02 00 1D 4E 65 74 43 6F 6E 6E 65 63 74 69 6F 6E 2E 43 6F 6E 6E 65 63 74 2E 53 75 63 63 65 73 73 // "NetConnection.Connect.Success"
00 00 09                          // End of object
```

**Status Codes**:
- `NetConnection.Connect.Success`: Connection successful
- `NetConnection.Connect.Rejected`: Connection rejected
- `NetConnection.Connect.Failed`: Connection failed

---

### Command: `_error` (connect failure)

**Purpose**: Indicate connection failure

**Direction**: Server → Client

**Message Type**: 20 (AMF0 Command), CSID: 3, MSID: 0

**Format**:
```
Command Name:   "_error" (String)
Transaction ID: 1 (Number, matches connect transaction ID)
Properties:     null
Information:    {
    "level": "error",             // Status level (String)
    "code": "NetConnection.Connect.Rejected", // Status code (String)
    "description": "Connection rejected: app not found." // Description (String)
}
```

**Example Payload** (AMF0 encoded):
```
02 00 06 5F 65 72 72 6F 72        // "_error" (String)
00 3F F0 00 00 00 00 00 00        // 1.0 (Transaction ID)
05                                // null (Properties)
03                                // Information object
  00 05 6C 65 76 65 6C            // "level"
  02 00 05 65 72 72 6F 72          // "error"
  00 04 63 6F 64 65                // "code"
  02 00 20 4E 65 74 43 6F 6E 6E 65 63 74 69 6F 6E 2E 43 6F 6E 6E 65 63 74 2E 52 65 6A 65 63 74 65 64 // "NetConnection.Connect.Rejected"
00 00 09                          // End of object
```

---

### Command: `releaseStream`

**Purpose**: Release exclusive stream (publish prep)

**Direction**: Client → Server

**Message Type**: 20 (AMF0 Command), CSID: 3, MSID: 0

**Format**:
```
Command Name:   "releaseStream" (String)
Transaction ID: 2 (Number, non-zero)
Command Object: null
Stream Key:     "test" (String, stream name)
```

**Server Response**: `_result` with null/null (optional, can be ignored)

**Note**: This is a courtesy call; server may ignore.

---

### Command: `FCPublish`

**Purpose**: Flash Communication publish notification

**Direction**: Client → Server

**Message Type**: 20 (AMF0 Command), CSID: 3, MSID: 0

**Format**:
```
Command Name:   "FCPublish" (String)
Transaction ID: 3 (Number, non-zero)
Command Object: null
Stream Key:     "test" (String, stream name)
```

**Server Response**: `onFCPublish` status message (optional)

**Note**: Flash-specific, can be acknowledged or ignored.

---

### Command: `createStream`

**Purpose**: Create new message stream

**Direction**: Client → Server

**Message Type**: 20 (AMF0 Command), CSID: 3, MSID: 0

**Format**:
```
Command Name:   "createStream" (String)
Transaction ID: 4 (Number, non-zero)
Command Object: null
```

**Example Payload** (AMF0 encoded):
```
02 00 0C 63 72 65 61 74 65 53 74 72 65 61 6D // "createStream"
00 40 10 00 00 00 00 00 00        // 4.0 (Transaction ID)
05                                // null (Command Object)
```

**Server Response**: `_result` with stream ID

---

### Command: `_result` (createStream response)

**Purpose**: Return allocated stream ID

**Direction**: Server → Client

**Message Type**: 20 (AMF0 Command), CSID: 3, MSID: 0

**Format**:
```
Command Name:   "_result" (String)
Transaction ID: 4 (Number, matches createStream transaction ID)
Command Object: null
Stream ID:      1 (Number, allocated MSID)
```

**Example Payload** (AMF0 encoded):
```
02 00 07 5F 72 65 73 75 6C 74    // "_result" (String)
00 40 10 00 00 00 00 00 00        // 4.0 (Transaction ID)
05                                // null (Command Object)
00 3F F0 00 00 00 00 00 00        // 1.0 (Stream ID)
```

**Typical Stream IDs**: Start at 1, increment per stream

---

## NetStream Commands

### Command: `publish`

**Purpose**: Start publishing to stream

**Direction**: Client → Server

**Message Type**: 20 (AMF0 Command), CSID: 8 (or 4), MSID: 1 (stream ID)

**Format**:
```
Command Name:    "publish" (String)
Transaction ID:  0 (Number, notification - no response expected)
Command Object:  null
Publishing Name: "test" (String, stream key)
Publishing Type: "live" (String, "live" | "record" | "append")
```

**Example Payload** (AMF0 encoded):
```
02 00 07 70 75 62 6C 69 73 68    // "publish" (String)
00 00 00 00 00 00 00 00 00        // 0.0 (Transaction ID)
05                                // null (Command Object)
02 00 04 74 65 73 74              // "test" (Publishing Name)
02 00 04 6C 69 76 65              // "live" (Publishing Type)
```

**Publishing Types**:
- `"live"`: Live stream (default)
- `"record"`: Record to file
- `"append"`: Append to existing recording

**Server Response**: `onStatus` with `NetStream.Publish.Start`

---

### Command: `play`

**Purpose**: Start playing stream

**Direction**: Client → Server

**Message Type**: 20 (AMF0 Command), CSID: 8 (or 4), MSID: 1 (stream ID)

**Format**:
```
Command Name:   "play" (String)
Transaction ID: 0 (Number, notification)
Command Object: null
Stream Name:    "test" (String, stream key)
Start:          -2 (Number, -2=live, -1=recorded, >=0=offset in seconds)
Duration:       -1 (Number, -1=all, >0=duration in seconds, optional)
Reset:          true (Boolean, flush previous playlist, optional)
```

**Example Payload** (AMF0 encoded):
```
02 00 04 70 6C 61 79              // "play" (String)
00 00 00 00 00 00 00 00 00        // 0.0 (Transaction ID)
05                                // null (Command Object)
02 00 04 74 65 73 74              // "test" (Stream Name)
00 C0 00 00 00 00 00 00 00        // -2.0 (Start, live stream)
00 BF F0 00 00 00 00 00 00        // -1.0 (Duration, all)
01 01                             // true (Reset)
```

**Start Values**:
- `-2`: Live stream (subscribe to current broadcast)
- `-1`: Recorded stream (VOD)
- `>= 0`: Start offset in seconds

**Server Response**: `onStatus` with `NetStream.Play.Start`

---

### Command: `deleteStream`

**Purpose**: Delete stream (cleanup)

**Direction**: Client → Server

**Message Type**: 20 (AMF0 Command), CSID: 3, MSID: 0

**Format**:
```
Command Name:   "deleteStream" (String)
Transaction ID: 0 (Number, notification)
Command Object: null
Stream ID:      1 (Number, MSID to delete)
```

**Example Payload** (AMF0 encoded):
```
02 00 0C 64 65 6C 65 74 65 53 74 72 65 61 6D // "deleteStream"
00 00 00 00 00 00 00 00 00        // 0.0 (Transaction ID)
05                                // null (Command Object)
00 3F F0 00 00 00 00 00 00        // 1.0 (Stream ID)
```

**Server Response**: None (notification)

---

### Command: `closeStream`

**Purpose**: Close stream (stop publish/play)

**Direction**: Client → Server

**Message Type**: 20 (AMF0 Command), CSID: 8, MSID: 1 (stream ID)

**Format**:
```
Command Name:   "closeStream" (String)
Transaction ID: 0 (Number, notification)
Command Object: null
```

**Server Response**: None (notification)

---

## Server Status Messages

### Command: `onStatus`

**Purpose**: Notify client of stream status changes

**Direction**: Server → Client

**Message Type**: 20 (AMF0 Command), CSID: 5 (or 4), MSID: 1 (stream ID)

**Format**:
```
Command Name:   "onStatus" (String)
Transaction ID: 0 (Number, always 0)
Command Object: null
Info Object:    {
    "level": "status",                     // "status" | "warning" | "error"
    "code": "NetStream.Publish.Start",     // Status code
    "description": "Publishing test.",     // Human-readable description
    "details": "test"                      // Stream key (optional)
}
```

**Example Payload** (AMF0 encoded):
```
02 00 08 6F 6E 53 74 61 74 75 73  // "onStatus" (String)
00 00 00 00 00 00 00 00 00        // 0.0 (Transaction ID)
05                                // null (Command Object)
03                                // Info Object
  00 05 6C 65 76 65 6C            // "level"
  02 00 06 73 74 61 74 75 73      // "status"
  00 04 63 6F 64 65                // "code"
  02 00 18 4E 65 74 53 74 72 65 61 6D 2E 50 75 62 6C 69 73 68 2E 53 74 61 72 74 // "NetStream.Publish.Start"
00 00 09                          // End of object
```

**Common Status Codes**:

**Publish**:
- `NetStream.Publish.Start`: Publishing started
- `NetStream.Publish.BadName`: Invalid stream name
- `NetStream.Publish.Idle`: Publisher idle (no data)

**Play**:
- `NetStream.Play.Start`: Playback started
- `NetStream.Play.Stop`: Playback stopped
- `NetStream.Play.StreamNotFound`: Stream not found
- `NetStream.Play.Reset`: Playlist reset

**General**:
- `NetStream.Data.Start`: Data transmission started
- `NetStream.Unpublish.Success`: Unpublish succeeded

---

## Command Flow Examples

### Publish Flow

```
Client → Server: connect (transaction_id=1)
Server → Client: Window Acknowledgement Size
Server → Client: Set Peer Bandwidth
Server → Client: Set Chunk Size
Server → Client: _result (transaction_id=1)

Client → Server: releaseStream (transaction_id=2, stream_key="test")
Client → Server: FCPublish (transaction_id=3, stream_key="test")
Client → Server: createStream (transaction_id=4)
Server → Client: _result (transaction_id=4, stream_id=1)

Client → Server: publish (transaction_id=0, stream_id=1, stream_key="test", type="live")
Server → Client: onStatus (NetStream.Publish.Start)

[Client sends Audio/Video messages...]

Client → Server: deleteStream (transaction_id=0, stream_id=1)
```

### Play Flow

```
Client → Server: connect (transaction_id=1)
Server → Client: _result (transaction_id=1)

Client → Server: createStream (transaction_id=2)
Server → Client: _result (transaction_id=2, stream_id=1)

Client → Server: play (transaction_id=0, stream_id=1, stream_key="test", start=-2)
Server → Client: onStatus (NetStream.Play.Start)

[Server sends Audio/Video messages...]

Client → Server: deleteStream (transaction_id=0, stream_id=1)
```

---

## Implementation Notes

### Transaction ID Management

- **Request Commands**: Non-zero transaction IDs (1, 2, 3, ...)
- **Responses**: Match request transaction ID
- **Notifications**: Transaction ID = 0 (no response expected)

**Server Logic**:
```go
type PendingCall struct {
    TransactionID float64
    ResponseChan  chan *Message
}

// On client command:
if transactionID > 0 {
    // Store pending call
    pendingCalls[transactionID] = &PendingCall{...}
    // Send response later
    sendResponse("_result", transactionID, result)
}
```

### Error Handling

- **Invalid Command**: Send `_error` response
- **App Not Found**: `NetConnection.Connect.Rejected`
- **Stream Not Found**: `NetStream.Play.StreamNotFound`
- **Invalid Arguments**: `_error` with description

### AMF0 Encoding Tips

- Use `encodeValue()` from AMF0 contract for each field
- Concatenate encoded values (no delimiters)
- String fields: Always encode with 0x02 marker + length + bytes
- Null fields: Always encode as 0x05

---

## Test Scenarios

### Golden Tests

| Test Case | Command | Expected Payload (Hex) |
|-----------|---------|------------------------|
| connect with app="live" | `connect` | `02 00 07 63 6F 6E 6E 65 63 74 00 3F F0...` |
| _result (connect) | `_result` | `02 00 07 5F 72 65 73 75 6C 74 00 3F F0...` |
| createStream | `createStream` | `02 00 0C 63 72 65 61 74 65 53 74 72 65 61 6D...` |
| publish (live) | `publish` | `02 00 07 70 75 62 6C 69 73 68 00 00...` |
| play (live) | `play` | `02 00 04 70 6C 61 79 00 00...` |

### Integration Tests

- **connect → _result**: Verify transaction ID matching
- **createStream → _result**: Verify stream ID allocation
- **publish → onStatus**: Verify status code
- **play → onStatus**: Verify status code

---

## References

- RTMP Specification: Section 7 (Command Messages)
- FFmpeg libavformat/rtmpproto.c (command handling)

---

**Status**: Contract complete. Ready for implementation and golden test generation.

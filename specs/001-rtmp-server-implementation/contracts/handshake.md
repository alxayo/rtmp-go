# Handshake Contract

**Feature**: 001-rtmp-server-implementation  
**Package**: `internal/rtmp/handshake`  
**Date**: 2025-10-01

## Overview

This contract defines the RTMP version 3 simple handshake protocol. The handshake establishes the connection before any chunk stream or RTMP messages are exchanged.

**Reference**: Adobe RTMP Specification Section 5.2 (Handshake)

---

## Protocol Sequence

### Client → Server: C0 + C1

**C0** (1 byte):
```
Byte 0: RTMP version (0x03 for version 3)
```

**C1** (1536 bytes):
```
Bytes 0-3:   Time (4 bytes, big-endian uint32, epoch seconds or zero)
Bytes 4-7:   Zero (4 bytes, must be 0x00000000)
Bytes 8-1535: Random data (1528 bytes, for session uniqueness)
```

### Server → Client: S0 + S1 + S2

**S0** (1 byte):
```
Byte 0: RTMP version (0x03, must match C0)
```

**S1** (1536 bytes):
```
Bytes 0-3:   Time (4 bytes, server timestamp or zero)
Bytes 4-7:   Zero (4 bytes, 0x00000000)
Bytes 8-1535: Random data (1528 bytes, server-generated)
```

**S2** (1536 bytes):
```
Bytes 0-3:   Time (4 bytes, echo from C1 bytes 0-3)
Bytes 4-7:   Time2 (4 bytes, echo from C1 bytes 4-7, should be zero)
Bytes 8-1535: Random data (1528 bytes, echo from C1 bytes 8-1535)
```

**Note**: S2 is a complete echo of C1. Server must copy C1 exactly into S2.

### Client → Server: C2

**C2** (1536 bytes):
```
Bytes 0-3:   Time (4 bytes, echo from S1 bytes 0-3)
Bytes 4-7:   Time2 (4 bytes, echo from S1 bytes 4-7)
Bytes 8-1535: Random data (1528 bytes, echo from S1 bytes 8-1535)
```

**Note**: C2 is a complete echo of S1. Client must copy S1 exactly into C2.

---

## State Machine (Server)

```
┌─────────────┐
│   Initial   │ (TCP accepted)
└──────┬──────┘
       │ recv C0+C1
       v
┌─────────────┐
│  RecvC0C1   │ (validate version, store C1)
└──────┬──────┘
       │ send S0+S1+S2
       v
┌─────────────┐
│ SentS0S1S2  │ (waiting for C2)
└──────┬──────┘
       │ recv C2
       v
┌─────────────┐
│  RecvC2     │ (validate C2 echoes S1)
└──────┬──────┘
       │ validate
       v
┌─────────────┐
│  Completed  │ (transition to chunk mode)
└─────────────┘
```

**Error Transitions**:
- Any state → Closed (timeout, version mismatch, truncated message)

---

## State Machine (Client)

```
┌─────────────┐
│   Initial   │ (TCP connected)
└──────┬──────┘
       │ send C0+C1
       v
┌─────────────┐
│ SentC0C1    │ (waiting for S0+S1+S2)
└──────┬──────┘
       │ recv S0+S1
       v
┌─────────────┐
│ RecvS0S1    │ (validate version, store S1)
└──────┬──────┘
       │ send C2
       v
┌─────────────┐
│  SentC2     │ (waiting for S2 or proceeding)
└──────┬──────┘
       │ recv S2
       v
┌─────────────┐
│  Completed  │ (transition to chunk mode)
└─────────────┘
```

---

## Validation Rules

### Version Validation

**Rule**: C0 and S0 must be 0x03  
**Action on Mismatch**: Server rejects connection (close TCP socket)  
**Error**: "Unsupported RTMP version: {hex(version)}"

**Supported Versions**:
- 0x03: RTMP version 3 (simple handshake)

**Unsupported Versions**:
- 0x06: RTMPE (encrypted RTMP, out of scope)
- 0x08: RTMPS (RTMP over TLS, out of scope)
- Other values: Invalid

### Size Validation

**Rule**: C1, S1, S2, C2 must be exactly 1536 bytes  
**Action on Mismatch**: Close connection with timeout or protocol error  
**Implementation**: Use `io.ReadFull` to ensure exact byte count

### Echo Validation

**Rule**: S2 must be byte-for-byte copy of C1  
**Action on Mismatch**: Log warning (optional validation, not enforced by all clients/servers)  
**Note**: Some implementations skip echo validation for performance

**Rule**: C2 must be byte-for-byte copy of S1  
**Action on Mismatch**: Server may log warning but typically does not reject  
**Note**: Most servers do not validate C2 contents

### Timeout

**Rule**: Each handshake step must complete within 5 seconds  
**Action on Timeout**: Close connection, log "Handshake timeout"  
**Implementation**: Use `net.Conn.SetReadDeadline` and `SetWriteDeadline`

---

## Test Scenarios

### Valid Handshake (Golden Test)

**Input** (C0 + C1):
```
Hex:
03                                   // C0: version 3
00 00 01 5F 00 00 00 00              // C1 bytes 0-7: time=351, zero=0
A3 4F B2 7C ... (1528 random bytes)  // C1 bytes 8-1535
```

**Expected Output** (S0 + S1 + S2):
```
Hex:
03                                   // S0: version 3
00 00 02 1A 00 00 00 00              // S1 bytes 0-7: time=538, zero=0
1F 8D C4 E3 ... (1528 random bytes)  // S1 bytes 8-1535 (server random)
00 00 01 5F 00 00 00 00              // S2 bytes 0-7: echo C1 time+zero
A3 4F B2 7C ... (1528 bytes)         // S2 bytes 8-1535: echo C1 random
```

**Validation**:
- S2 bytes 0-1535 exactly match C1 bytes 0-1535

### Invalid Version (Error Test)

**Input**:
```
Hex:
06                                   // C0: version 6 (RTMPE)
00 00 01 5F 00 00 00 00 A3 4F ...   // C1 (ignored)
```

**Expected Behavior**:
- Server reads C0, detects version 0x06
- Server closes TCP connection immediately
- Server logs: "Unsupported RTMP version: 0x06"
- No S0/S1/S2 sent

### Truncated C1 (Timeout Test)

**Input**:
```
Hex:
03                                   // C0: version 3
00 00 01 5F 00 00 00 00 A3 4F ...   // C1: only 1000 bytes (incomplete)
(connection idle, no more data)
```

**Expected Behavior**:
- Server reads C0 successfully
- Server attempts to read 1536 bytes for C1
- `io.ReadFull` blocks until timeout (5 seconds)
- Server closes connection
- Server logs: "Handshake timeout reading C1"

### Zero Timestamp (Edge Case)

**Input**:
```
Hex:
03                                   // C0: version 3
00 00 00 00 00 00 00 00              // C1 bytes 0-7: time=0, zero=0
F4 21 D8 9A ... (1528 random bytes)  // C1 bytes 8-1535
```

**Expected Behavior**:
- Server accepts zero timestamp (valid per spec)
- S2 echoes C1 exactly (including zero timestamp)
- Handshake completes successfully

---

## Implementation Notes

### Random Data Generation

**Client**:
```go
c1Random := make([]byte, 1528)
_, err := rand.Read(c1Random) // crypto/rand for security
if err != nil {
    return err
}
```

**Server**:
```go
s1Random := make([]byte, 1528)
_, err := rand.Read(s1Random)
if err != nil {
    return err
}
```

**Note**: Use `crypto/rand` for cryptographically secure random data (prevents session hijacking in theory, though simple handshake has no real security).

### Timestamp Handling

**Flexibility**: Timestamp can be:
- Current Unix time in seconds (e.g., `time.Now().Unix()`)
- Current Unix time in milliseconds (e.g., `time.Now().UnixMilli()`)
- Zero (0x00000000)

**Recommendation**: Use zero for simplicity (no clock synchronization needed).

### I/O Operations

**Reading**:
```go
// Set 5-second deadline
conn.SetReadDeadline(time.Now().Add(5 * time.Second))

// Read exactly 1 byte (C0)
c0 := make([]byte, 1)
if _, err := io.ReadFull(conn, c0); err != nil {
    return fmt.Errorf("read C0: %w", err)
}

// Read exactly 1536 bytes (C1)
c1 := make([]byte, 1536)
if _, err := io.ReadFull(conn, c1); err != nil {
    return fmt.Errorf("read C1: %w", err)
}
```

**Writing**:
```go
// Set 5-second deadline
conn.SetWriteDeadline(time.Now().Add(5 * time.Second))

// Write S0+S1+S2 in one call (atomic)
s0s1s2 := make([]byte, 1+1536+1536)
s0s1s2[0] = 0x03 // S0
copy(s0s1s2[1:1537], s1)
copy(s0s1s2[1537:3073], c1) // S2 = C1
if _, err := conn.Write(s0s1s2); err != nil {
    return fmt.Errorf("write S0+S1+S2: %w", err)
}
```

### Echo Validation (Optional)

**Server validates C2 echoes S1**:
```go
// After reading C2
if !bytes.Equal(c2, s1) {
    log.Warn("C2 does not match S1 (non-fatal)")
}
```

**Note**: Most implementations skip this check for performance.

---

## Performance Considerations

### Memory Allocation

- Allocate C1, S1, S2, C2 buffers once per connection (3 × 1536 = 4608 bytes)
- Reuse buffers if possible (connection pool scenario)

### Network I/O

- Total bytes exchanged: 1 + 1536 + 1 + 1536 + 1536 + 1536 = 6146 bytes
- Over local network: <10ms
- Over WAN (100ms RTT): ~200-300ms
- Target: <50ms local, <200ms WAN (meets performance goal)

### Concurrency

- Handshake is synchronous (blocking I/O)
- One goroutine per connection handles handshake sequentially
- No shared state between connections (no mutex needed)

---

## Security Considerations

### Simple Handshake Limitations

- **No encryption**: Data sent in plaintext
- **No authentication**: No client/server verification
- **Replay attacks possible**: C1/S1 random data not validated
- **Session hijacking**: No protection

**Mitigation**: Deploy in trusted network or use RTMPS (TLS layer, out of scope).

### Version Downgrade Attack

- Client could request unsupported version (e.g., 0x06) to probe server capabilities
- Server must reject unsupported versions immediately (no fallback)

---

## References

- Adobe RTMP Specification 1.0, Section 5.2 (Handshake)
- FFmpeg libavformat/rtmpproto.c (reference implementation)
- nginx-rtmp-module ngx_rtmp_handshake.c (reference implementation)

---

## Test Coverage Requirements

| Test Case                  | Type       | Priority |
|----------------------------|------------|----------|
| Valid handshake            | Golden     | Critical |
| Invalid version (0x06)     | Error      | Critical |
| Truncated C1               | Timeout    | High     |
| Truncated C2               | Timeout    | High     |
| Zero timestamp             | Edge case  | Medium   |
| C2 mismatch S1             | Warning    | Low      |
| Loopback (client+server)   | Integration| High     |
| FFmpeg interop             | Integration| Critical |

**Golden Test Files**:
- `tests/golden/handshake_valid_c0c1.bin` (C0+C1 input)
- `tests/golden/handshake_valid_s0s1s2.bin` (S0+S1+S2 expected output)
- `tests/golden/handshake_valid_c2.bin` (C2 input)

---

**Status**: Contract complete. Ready for implementation and test generation.

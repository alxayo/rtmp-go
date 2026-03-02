# Fix: OBS Studio Connection Timeout After Handshake

**Date**: October 10, 2025  
**Issue**: Connection timeout after successful handshake  
**Status**: ✅ RESOLVED

## Problem Description

When streaming from OBS Studio to the RTMP server, the connection would timeout approximately 5 seconds after the handshake completed successfully:

```json
{"time":"2025-10-10T19:23:29.798661+03:00","level":"INFO","msg":"Handshake completed","phase":"handshake","side":"server","c1_ts":373527294,"s1_ts":3471785733}
{"time":"2025-10-10T19:23:29.7991772+03:00","level":"INFO","msg":"Connection accepted","conn_id":"c000001","peer_addr":"[::1]:58172","handshake_ms":1}
{"time":"2025-10-10T19:23:29.8034219+03:00","level":"INFO","msg":"Control sent: Set Chunk Size","conn_id":"c000001","peer_addr":"[::1]:58172","size":4096}
{"time":"2025-10-10T19:23:34.7993346+03:00","level":"DEBUG","msg":"readLoop error","conn_id":"c000001","peer_addr":"[::1]:58172","error":"chunk error: reader.basic_header: read tcp [::1]:1935->[::1]:58172: i/o timeout"}
```

### Symptoms
- RTMP handshake completed successfully ✅
- Control burst sent successfully ✅
- Exactly 5 seconds later, `readLoop` fails with "i/o timeout"
- Connection terminates before receiving any commands from OBS

## Root Cause Analysis

The handshake implementation in `internal/rtmp/handshake/server.go` sets aggressive 5-second deadlines during the handshake process for security (preventing slowloris attacks):

```go
const (
    serverReadTimeout  = 5 * time.Second // per spec: 5s deadline for each blocking read phase
    serverWriteTimeout = 5 * time.Second
)
```

These deadlines are set before each read/write operation during the handshake:

1. Before reading C0+C1: `SetReadDeadline(time.Now().Add(5 * time.Second))`
2. Before writing S0+S1+S2: `SetWriteDeadline(time.Now().Add(5 * time.Second))`
3. Before reading C2: `SetReadDeadline(time.Now().Add(5 * time.Second))`

**The critical bug**: After the handshake completed successfully, these deadlines were **never cleared**. 

When the connection transitioned to normal operation (`readLoop` in `conn.go`), the 5-second read deadline remained active. Since OBS Studio typically takes a few seconds to prepare and send the `connect` command after establishing the connection, the read timeout would expire before any data arrived.

### Timeline
```
T+0ms:  Handshake starts
T+1ms:  Handshake completes (deadline still active: expires at T+5000ms)
T+1ms:  Control burst sent
T+1ms:  readLoop starts, tries to read first chunk
T+1ms to T+5000ms: OBS preparing to send connect command
T+5000ms: Read deadline expires → i/o timeout error
T+5000ms: Connection terminated
```

## Solution

Modified `ServerHandshake()` in `internal/rtmp/handshake/server.go` to clear both read and write deadlines after the handshake completes successfully:

```go
if err := h.Complete(); err != nil {
    return err
}

// Clear deadlines after successful handshake so subsequent chunk reads
// can operate without timeout constraints (T016 integration requirement).
// This prevents spurious "i/o timeout" errors when client delays sending
// the connect command after handshake (common with OBS Studio).
if err := conn.SetReadDeadline(time.Time{}); err != nil {
    log.Warn("Failed to clear read deadline", "error", err)
}
if err := conn.SetWriteDeadline(time.Time{}); err != nil {
    log.Warn("Failed to clear write deadline", "error", err)
}

log.Info("Handshake completed", "c1_ts", h.C1Timestamp(), "s1_ts", h.S1Timestamp())
return nil
```

**Key insight**: Setting a deadline to the zero time value (`time.Time{}`) removes any active deadline, allowing subsequent reads/writes to block indefinitely until data arrives or the connection is closed.

## Testing Instructions

1. **Rebuild the server**:
   ```powershell
   go build -o rtmp-server.exe ./cmd/rtmp-server
   ```

2. **Start the server with debug logging**:
   ```powershell
   .\rtmp-server.exe -log-level debug
   ```

3. **Configure OBS Studio**:
   - Open Settings → Stream
   - Service: Custom
   - Server: `rtmp://localhost:1935/live`
   - Stream Key: `test` (or any key)

4. **Test streaming**:
   - Click "Start Streaming" in OBS
   - Connection should now persist beyond handshake
   - Server should receive and log the `connect` command from OBS

## Expected Behavior After Fix

```json
{"level":"INFO","msg":"Handshake completed",...}
{"level":"INFO","msg":"Connection accepted",...}
{"level":"INFO","msg":"Control sent: Window Acknowledgement Size",...}
{"level":"INFO","msg":"Control sent: Set Peer Bandwidth",...}
{"level":"INFO","msg":"Control sent: Set Chunk Size",...}
// No timeout - connection remains active waiting for commands
{"level":"DEBUG","msg":"Received message","type_id":20,"csid":3,...} // connect command
```

## Files Modified

- `internal/rtmp/handshake/server.go` - Added deadline clearing after handshake completion (lines 123-131)

## Design Rationale

### Why Have Handshake Timeouts?
- **Security**: Prevent slowloris-style attacks where clients open connections but never complete handshake
- **Resource protection**: Ensure malicious clients can't hold server connections indefinitely
- **RTMP spec compliance**: Handshake should complete quickly (< 5 seconds is generous)

### Why Remove Timeouts After Handshake?
- **Streaming nature**: RTMP streaming can have natural pauses (keyframe intervals, network conditions)
- **Client behavior**: Legitimate clients like OBS may take several seconds to prepare media streams
- **Protocol design**: The chunk layer has its own mechanisms for detecting dead connections (acknowledgements, ping/pong)

### Future Enhancements
Consider implementing:
1. **Configurable idle timeout** for the chunk reading phase (e.g., 30-60 seconds)
2. **Acknowledgement tracking** to detect silent connections
3. **Ping/Pong mechanism** (User Control Messages type 6/7) for keepalive

## Related Issues

- Initial issue [1346b536dda48dbf02ba8a2012b77d3e683f8f90](./Fix_1346b536dda48dbf02ba8a2012b77d3e683f8f90.md) - Invalid CSID 0 in control messages
- Both issues occurred during OBS Studio integration testing

## References

- RTMP Specification: Handshake section (C0+C1 → S0+S1+S2 → C2)
- Go `net` package: `SetReadDeadline()` documentation
- Constitutional principle: **Protocol-First** - respect RTMP timing expectations while supporting real-world clients

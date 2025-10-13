# Wireshark RTMP Capture Guide

## Quick Setup

### 1. Start Wireshark Capture
```powershell
# On Windows, capture loopback traffic
# In Wireshark UI:
# 1. Select "Adapter for loopback traffic capture" or "Npcap Loopback Adapter"
# 2. Enter capture filter: tcp port 1935
# 3. Click start (shark fin icon)
```

### 2. Start Your RTMP Server
```powershell
# Terminal 1: Start the server
cd c:\code\alxayo\go-rtmp
.\rtmp-server.exe -addr :1935 -debug
```

### 3. Run FFmpeg Test
```powershell
# Terminal 2: Publish stream
ffmpeg -re -f lavfi -i testsrc=duration=10:size=1280x720:rate=30 `
       -f lavfi -i sine=frequency=1000:duration=10 `
       -c:v libx264 -preset ultrafast -tune zerolatency -b:v 1500k `
       -c:a aac -b:a 128k `
       -f flv rtmp://localhost:1935/live/test
```

### 4. Stop Capture
- Press Ctrl+E in Wireshark or click the red stop button
- Save the capture: File → Save As → `rtmp_capture_YYYYMMDD_HHMMSS.pcapng`

## Analyzing RTMP Traffic

### Display Filters (apply AFTER capture)

```wireshark
# Show only RTMP packets
rtmpt

# Show TCP streams for connection analysis
tcp.stream eq 0

# Show specific RTMP message types
rtmpt.header.message_type == 0x14  # AMF0 Command
rtmpt.header.message_type == 0x08  # Audio
rtmpt.header.message_type == 0x09  # Video

# Show handshake phase (first few packets)
tcp.len == 1 || tcp.len == 1536

# Filter by direction
ip.src == 127.0.0.1 && tcp.srcport == 1935  # Server → Client
ip.dst == 127.0.0.1 && tcp.dstport == 1935  # Client → Server
```

### What to Look For

#### **Handshake Phase** (First 3 packets from client perspective)
1. **C0+C1** (1537 bytes): Version byte (0x03) + C1 timestamp + random data
2. **S0+S1+S2** (3073 bytes): Server response + echo
3. **C2** (1536 bytes): Client echo of S1

**Check:**
- C0 version = 0x03
- S0 version = 0x03
- Timestamps are present
- S2 exactly echoes C1 (bytes 1-1536)
- C2 exactly echoes S1 (bytes 1-1536)

#### **RTMP Messages** (After handshake)
Look at chunk headers:
- **fmt** (2 bits): Format type (0-3)
- **csid** (6+ bits): Chunk Stream ID
- **timestamp**: Message timestamp
- **message length**: Payload size
- **message type id**: What kind of message (0x14=Command, 0x08=Audio, 0x09=Video)
- **message stream id** (msid): Stream identifier

**Common message sequence:**
1. Set Chunk Size (type 1)
2. Window Acknowledgement Size (type 5)
3. Set Peer Bandwidth (type 6)
4. connect command (type 0x14, AMF0)
5. _result response
6. createStream command
7. _result with stream ID
8. publish/play command
9. Audio/Video data (types 0x08/0x09)

#### **Protocol Errors to Spot**
- ❌ Handshake bytes wrong (not 1537, 3073, 1536)
- ❌ Version not 0x03
- ❌ S2 doesn't match C1, or C2 doesn't match S1
- ❌ Chunk size changes without Set Chunk Size message
- ❌ Missing extended timestamp when timestamp >= 0xFFFFFF
- ❌ Wrong endianness (msid is little-endian, others big-endian)
- ❌ Malformed AMF0 (wrong type markers, incorrect string lengths)

## Advanced Techniques

### Follow TCP Stream
1. Right-click any RTMP packet
2. Select "Follow" → "TCP Stream"
3. See raw bytes of entire connection
4. Use "Show data as" dropdown: C Arrays (for test vectors), Raw (hex dump)

### Extract Test Vectors
```powershell
# Export specific packets for golden tests
# In Wireshark:
# 1. Select packet(s) of interest
# 2. File → Export Packet Dissections → As Plain Text
# 3. Or right-click → Copy → Bytes → Hex Stream
```

### Statistics
```
Statistics → Conversations → TCP
  - See connection duration, bytes transferred
  
Statistics → Protocol Hierarchy
  - See RTMP vs other traffic breakdown
  
Statistics → I/O Graphs
  - Visualize throughput over time
```

### Decryption (if using RTMPS)
If you implement RTMPS later:
```
Edit → Preferences → Protocols → TLS
  - Add server private key for decryption
```

## Troubleshooting Scenarios

### Scenario 1: Handshake Fails
**Filter:** `frame.number <= 10`
**Check:**
- Are C0/C1 sent together? (Should be 1537 bytes)
- Does server respond with S0/S1/S2? (Should be 3073 bytes)
- Does C2 echo S1 correctly?
- Any TCP retransmissions? (Look for `[TCP Retransmission]` in packet info)

### Scenario 2: Connection Drops Mid-Stream
**Filter:** `tcp.flags.reset == 1 or tcp.flags.fin == 1`
**Check:**
- Who sent RST/FIN? Client or server?
- What was the last RTMP message before disconnect?
- Check for Window Ack Size violations (too many bytes without ack)

### Scenario 3: Audio/Video Not Playing
**Filter:** `rtmpt.header.message_type == 0x08 || rtmpt.header.message_type == 0x09`
**Check:**
- Are audio/video packets arriving?
- What are the timestamps? (Should be monotonically increasing)
- Are packets on correct CSID? (Typically audio=4, video=6)
- Check FLV codec tags (first byte of audio/video payload)

### Scenario 4: High Latency
**Filter:** `rtmpt`
**Check Statistics:**
- Statistics → I/O Graphs → Y Axis: Bytes
- Look for buffering (gaps in data flow)
- Check acknowledgement frequency
- Inspect Set Peer Bandwidth / Window Ack Size values

## Export for Sharing

### Save filtered capture
```powershell
# File → Export Specified Packets
# Choose: "Displayed" (saves only filtered packets)
# Format: .pcapng (keeps timestamps, interfaces)
```

### Create test vectors
```powershell
# Select packet → Right-click → Copy → Bytes → Hex Stream
# Paste into Go test file:
testData := []byte{
    0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    // ... paste hex bytes
}
```

## Sample Commands Reference

### Publish Test Stream
```powershell
# Video + Audio test pattern
ffmpeg -re -f lavfi -i testsrc=duration=30:size=1280x720:rate=30 `
       -f lavfi -i sine=frequency=1000:duration=30 `
       -c:v libx264 -preset ultrafast -b:v 1500k `
       -c:a aac -b:a 128k `
       -f flv rtmp://localhost:1935/live/test

# From file
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test
```

### Play Stream
```powershell
# ffplay
ffplay rtmp://localhost:1935/live/test

# Record to file
ffmpeg -i rtmp://localhost:1935/live/test -c copy output.flv
```

## Tips

1. **Use display filters, not capture filters** for analysis
   - Capture filters drop packets permanently
   - Display filters just hide them temporarily

2. **Save captures early and often**
   - Captures can be large; save before running out of RAM

3. **Use packet comments** for annotations
   - Right-click packet → Edit Packet Comment
   - Useful for marking protocol violations

4. **Compare captures**
   - Save "working" capture as baseline
   - Compare against "broken" capture to spot differences

5. **Automate with tshark** (CLI version)
   ```powershell
   # Capture to file
   tshark -i "Npcap Loopback Adapter" -f "tcp port 1935" -w rtmp.pcapng
   
   # Extract stats
   tshark -r rtmp.pcapng -q -z conv,tcp
   
   # Filter and export
   tshark -r rtmp.pcapng -Y "rtmpt" -w rtmp_only.pcapng
   ```

## References
- Wireshark RTMP Dissector: https://wiki.wireshark.org/RTMP
- RTMP Specification: Adobe RTMP v1.0 (specs/001-rtmp-server-implementation/spec.md)
- FFmpeg RTMP: https://ffmpeg.org/ffmpeg-protocols.html#rtmp

---

*Last updated: 2025-10-10*

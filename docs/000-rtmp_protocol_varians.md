# 1. RTMP (Original / RTMP Proper)
Transport: TCP (default port: 1935).
Purpose: Streaming audio, video, and data between Flash Player and Flash Media Server.
Handshake: Basic 3-step handshake (C0/C1 → S0/S1/S2 → C2).
Chunking: Messages split into chunks (default size: 128 bytes).
Multiplexing: Supports multiple streams over a single connection.
Codec Support: Originally designed for FLV container with codecs like Spark (video) and MP3 (audio).
Limitations:
- No encryption.
- No native support for modern codecs.
- No adaptive bitrate streaming.




# 2. RTMPS (RTMP Secure)
Transport: RTMP over TLS/SSL (typically port 443).
Purpose: Adds encryption for secure transmission.
Use Case: Secure live streaming over public networks.
Technical Difference:
- Wraps RTMP packets in a TLS session.
- Requires SSL certificate setup on the server.
- Same chunking and message format as RTMP.




# 3. RTMPE (RTMP Encrypted)
Transport: RTMP with Adobe's proprietary encryption.
Purpose: Lightweight encryption alternative to RTMPS.
Technical Details:
- Uses Diffie-Hellman key exchange and RC4 stream cipher.
- Not based on standard SSL/TLS.
- Less secure than RTMPS.
Use Case: Flash-based DRM and content protection.


# 4. RTMPT (RTMP Tunneled)
Transport: RTMP encapsulated in HTTP requests (ports 80/443).
Purpose: Bypass firewalls and proxies.
Technical Details:
- RTMP packets are wrapped in HTTP POST requests.
- Server responds with HTTP responses containing RTMP data.
- Adds latency due to HTTP overhead.
Use Case: Corporate networks with strict firewall rules.


# 5. RTMFP (RTMP over UDP)
Transport: UDP.
Purpose: Peer-to-peer communication and low-latency streaming.
Technical Details:
- Replaces RTMP chunk stream with Secure Real-Time Media Flow Protocol.
- Supports NAT traversal, encryption, and multicast.
- Designed for Flash Player 10 and Adobe Cirrus.
Use Case: Video conferencing, multiplayer games, P2P streaming.


# 6. E-RTMP (Enhanced RTMP)
Introduced by: Veovera Software Organization (VSO) in collaboration with Adobe, Twitch, YouTube. [GitHub - v...roject ...]
Purpose: Modernize RTMP for today's streaming needs.
Key Enhancements:
- Advanced Audio Codecs: AC-3, E-AC-3, Opus, FLAC.
- Advanced Video Codecs: VP8, VP9, HEVC, AV1 with HDR.
- Multitrack Support: Multiple audio/video tracks in one stream.
- FourCC Signaling: Codec identification for compatibility.
- Reconnect Request Feature: Improves connection resilience.
- Nanosecond Timestamp Precision: Better sync across formats.
- Expanded Metadata Support: For richer stream descriptions.
Compatibility: Backward-compatible with legacy RTMP infrastructure.


🧠 Summary Table

| Version  | Transport     | Encryption      | Codec Support     | Use Case                  | Key Feature                |
|----------|--------------|-----------------|-------------------|---------------------------|----------------------------|
| RTMP     | TCP          | ❌              | Spark, MP3        | Basic live/VOD streaming  | Chunking, multiplexing     |
| RTMPS    | TCP (TLS)    | ✅ TLS          | Same as RTMP      | Secure streaming          | SSL encryption             |
| RTMPE    | TCP          | ✅ (Adobe)      | Same as RTMP      | Flash DRM                 | Proprietary encryption     |
| RTMPT    | HTTP         | ❌              | Same as RTMP      | Firewall bypass           | HTTP tunneling             |
| RTMFP    | UDP          | ✅              | Flash codecs      | P2P, conferencing         | NAT traversal, multicast   |
| E-RTMP   | TCP          | ✅ (modern)     | Modern codecs     | Advanced streaming        | Multitrack, reconnect, HDR |
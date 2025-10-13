RTMP Handshake – Step-by-Step Breakdown
The handshake consists of three main steps involving three messages exchanged between the client and server:
Step 1: C0 + C1 (Client → Server)

C0: 1 byte – RTMP version (usually 0x03 for version 3).
C1: 1536 bytes – Contains:

Timestamp (4 bytes): Time when the message was sent.
Zero field (4 bytes): Reserved.
Random data (1528 bytes): Used for session uniqueness.



Step 2: S0 + S1 + S2 (Server → Client)

S0: 1 byte – RTMP version (should match C0).
S1: 1536 bytes – Similar structure to C1:

Timestamp
Zero field
Random data


S2: 1536 bytes – Echoes back the client’s C1 data to confirm receipt and integrity.

Step 3: C2 (Client → Server)

C2: 1536 bytes – Echoes back the server’s S1 data to complete the handshake.

Once this exchange is complete, the connection is established, and the client can begin sending RTMP messages (audio, video, metadata).

🧠 Additional Negotiation After Handshake
After the basic handshake, the client and server negotiate:
✅ Version Negotiation

Ensures both sides support the same RTMP version (usually v3).

📦 Chunk Size Agreement

RTMP breaks data into chunks (default size: 128 bytes).
The client and server can send a Set Chunk Size message to increase this (e.g., 4096 bytes) for performance.

📶 Bandwidth Check

The client sends a Bandwidth Check message.
The server responds with its bandwidth capacity.
Helps optimize streaming quality and avoid buffering.


🔐 Security Considerations

RTMP itself is not encrypted.
For secure transmission, RTMPS (RTMP over TLS/SSL) is used.
Authentication can be added via tokens or signed URLs.


Excellent question! The RTMP protocol version commonly referred to as version 3 is actually the version byte used during the handshake process, not a formal versioning system like HTTP/2 or TLS 1.3. However, there are different RTMP handshake versions and modes, each with technical differences that affect compatibility, security, and codec support.
Let’s break this down precisely:

🔢 RTMP Version Byte (C0/S0)
During the handshake, the first byte sent by the client (C0) and server (S0) indicates the protocol version:

0x03 → RTMP version 3 (standard, most widely used)
0x06 → RTMPE (Adobe's encrypted variant)
0x08 → RTMPS (RTMP over SSL/TLS)

These are not protocol versions in the traditional sense, but indicators of the handshake mode and encryption type.

🧩 RTMP Handshake Modes
There are two main handshake types:
1. Simple Handshake (Standard RTMP v3)

Used by: Most encoders (OBS, FFmpeg, FMLE), servers (nginx-rtmp, Wowza).
Structure:

C0/S0: 1 byte (version, usually 0x03)
C1/S1: 1536 bytes

4 bytes: timestamp
4 bytes: zero field
1528 bytes: random data


C2/S2: 1536 bytes

Echoes the peer’s random data




Purpose: Establishes a basic session with no encryption.
Codec Support: VP6, MP3, Speex (Flash-era codecs).


2. Complex Handshake (Encrypted RTMP)

Used by: Adobe Flash Player, RTMPE/RTMPS servers.
Structure:

C1/S1: Includes digest and key blocks for cryptographic verification.
Two schemas:

Schema 0: Key first, digest second.
Schema 1: Digest first, key second.




Cryptography:

Uses HMAC-SHA256 for digest verification.
Diffie-Hellman key exchange.
RC4 stream cipher for encryption.


Purpose: Adds DRM and secure session establishment.
Codec Support: H.264, AAC (modern codecs).
Version bytes:

C1[4–7] and S1[4–7] contain version identifiers (e.g., 0x80 00 07 02 or 0x04 05 00 01). [Rtmp proto...) detailed]




🆕 Enhanced RTMP (E-RTMP)

Introduced by: Veovera Software Organization (2023+).
Not a handshake version, but an extension of RTMP capabilities.
Enhancements:

Multitrack support (multiple audio/video tracks).
Advanced codecs: VP9, HEVC, AV1, Opus, FLAC.
FourCC signaling for codec identification.
Reconnect request feature.
Nanosecond timestamp precision.


Compatibility: Backward-compatible with RTMP v3 handshake


Complex HandshakeRTMPE/RTMPS✅H.264, AACFlash Player, encrypted sessionsE-RTMPExtensionOptionalAV1, VP9, Opus, etcModern streaming platforms


| Mode/Version Byte | Type | Encryption | Codec Support | Use Case |
|---|---|---|---|---|
| 0x03 | Simple | ❌ | VP6, MP3, Speex | Standard RTMP ingest |
| 0x06 | RTMPE | ✅ (Adobe) | H.264, AAC | Flash DRM, secure streaming |
| 0x08 | RTMPS | ✅ (TLS) | H.264, AAC | Secure RTMP over SSL |
| Complex Handshake | RTMPE/RTMPS | ✅ | H.264, AAC | Flash Player, encrypted sessions |
| E-RTMP | Extension | Optional | AV1, VP9, Opus, etc | Modern streaming platforms |

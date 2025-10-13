What is RTMP?
RTMP is a protocol originally developed by Macromedia (now Adobe) for streaming audio, video, and data over the internet between a media server and a client. While it's no longer used for playback (due to Flash being deprecated), it's still widely used for video ingestion—especially for live streaming to platforms like YouTube Live, Twitch, and Facebook Live.

⚙️ How RTMP Works – Step by Step
1. Capture & Encode

A camera or screen capture tool records video/audio.
An encoder (software like OBS, vMix, Wirecast, or hardware encoders) compresses the media using codecs like H.264 (video) and AAC (audio).

2. RTMP Push to Server

The encoder sends the stream to a RTMP server (e.g., Wowza, nginx with RTMP module, or cloud services like YouTube Live).
This is done via a RTMP URL, typically in the format:
rtmp://<server-address>/live/<stream-key>



3. RTMP Handshake

The client (encoder) and server perform a handshake to establish a connection.
This includes:

Version negotiation
Chunk size agreement
Bandwidth checks



4. Streaming Data in Chunks

RTMP breaks the media stream into chunks.
These chunks are sent over TCP, ensuring reliable delivery.
RTMP uses multiplexing, meaning audio, video, and metadata are interleaved in the same stream.

5. Server Receives & Distributes

The RTMP server receives the stream and can:

Record it.
Transcode it to other formats (e.g., HLS or DASH for playback).
Distribute it to viewers or other servers.




🔐 RTMP Variants

RTMPT: RTMP over HTTP (tunneled through port 80).
RTMPS: RTMP over TLS/SSL (secure).
RTMPE: RTMP with Adobe's encryption (less common now).


📉 Limitations of RTMP

Latency: Typically 2–5 seconds (not ideal for ultra-low latency).
No native adaptive bitrate (ABR) support.
TCP-based: Reliable but not as fast as UDP-based protocols like WebRTC.
Flash dependency for playback (deprecated).


✅ Why RTMP Is Still Used

Simplicity: Easy to set up and widely supported.
Compatibility: Most streaming platforms accept RTMP ingest.
Reliability: TCP ensures data arrives intact.
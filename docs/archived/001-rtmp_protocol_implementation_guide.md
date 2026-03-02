Below is a from‑scratch, step‑by‑step implementation guide for building an RTMP server that can handle end‑to‑end audio/video streaming including chunk-size and bandwidth negotiation. Everything is grounded in the original Adobe specifications (and well‑known open implementations) and includes bit‑level packet layouts, sequencing, and worked examples you can test against tools like FFmpeg/OBS.
Abbreviations used: CSID (Chunk Stream ID), MSID (Message Stream ID), FMT (chunk header format type 0–3), WAS (Window Acknowledgement Size), SPB (Set Peer Bandwidth). All multi‑byte integers are big‑endian unless noted (MSID is little‑endian).

## 0) What you’ll implement (high‑level)
An RTMP server has these core components:
TCP listener on (by convention) port 1935.
Handshake state machine (C0/C1/C2 ↔ S0/S1/S2).
Chunker/De‑chunker to read/write RTMP chunks (Basic Header, Message Header, Extended Timestamp, Data). Default chunk size is 128 bytes until changed.
Protocol control (Set Chunk Size, Abort, Acknowledgement, WAS, SPB) on CSID=2, MSID=0.
AMF0/AMF3 encoder/decoder for command/data messages (“connect”, “createStream”, “play”, “publish”, etc.).
NetConnection/NetStream RPC engine for command sequencing and responses.
Media pipeline: RTMP Video (type=9)/Audio (type=8) messages carry FLV‑tag payloads (H.264/AAC common). You must packetize the AVC/AAC sequence headers followed by frame packets.
Bandwidth/flow control: WAS/ACK counters + SPB limit type (Hard/Soft/Dynamic).
Teardown: deleteStream, user control events (StreamEOF/StreamDry), and socket close.

## 1) Wire format—what you must parse/emit
## 1.1 Handshake (simple/unencrypted)
Sequence (client → server): C0+C1 → (server) S0+S1 → (client) C2 → (server) S2. Both sides must wait for the previous step before proceeding, then switch to chunked RTMP.
C0/S0 (1 byte)
Version for RTMP is 3.
C1/S1 (1536 bytes)

C2/S2 (1536 bytes)

Note: Adobe’s public spec documents the “simple” handshake. Encrypted “complex” handshakes seen in legacy Flash players are not part of the public spec and are typically handled by implementations as an optional capability.

## 1.2 Chunk format (applies to all subsequent traffic)
A chunk = Basic Header + Message Header + (optional) Extended Timestamp + Chunk Data.
Basic Header (1–3 bytes)
CSID encoding rules choose 1, 2, or 3 bytes; CSID=2 is reserved for protocol control.
Message Header (0/3/7/11 bytes) depending on FMT (0..3):
Type 0 (FMT=0) is the “full” 11‑byte header (timestamp, msg length, type id, MSID). MSID is little‑endian.
Extended Timestamp (0 or 4 bytes) appears when the 24‑bit timestamp or delta is 0xFFFFFF.
Default chunk size is 128 bytes until changed by Set Chunk Size. Each direction tracks its own chunk size.

## 1.3 Message type IDs you’ll use
Protocol control:
1 Set Chunk Size, 2 Abort, 3 Acknowledgement, 4 User Control, 5 Window Acknowledgement Size, 6 Set Peer Bandwidth (all on CSID=2, MSID=0).
RTMP command/data:
20 (0x14) Command AMF0, 17 (0x11) Command AMF3, 18 (0x12) Data AMF0, 15 (0x0F) Data AMF3, 19/16 Shared Object.
Media: 8 Audio, 9 Video, 22 Aggregate.

## 2) Session lifecycle (sequenced, both sides)
We’ll cover Handshake → Post‑handshake control → NetConnection → NetStream → Media → Teardown, with bit‑level structures and hex examples.
## 2.1 After handshake: establish flow control & chunk sizes (server → client)
Typical servers send these immediately (order may vary), all on CSID=2, MSID=0:
Window Acknowledgement Size (type=5) – 4‑byte window uint32.
Set Peer Bandwidth (type=6) – 4‑byte window + 1‑byte limit type (0=Hard, 1=Soft, 2=Dynamic).
Set Chunk Size (type=1) – 4‑byte max chunk size this endpoint will use when sending.
Semantics defined here; ordering commonly observed in practice is WAS → SPB, then optionally SCS.
Bit‑level payloads (message payload, big‑endian):
Set Chunk Size (1):

Window Acknowledgement Size (5):

Set Peer Bandwidth (6):

Worked example (hex) – Set Chunk Size = 4096 (server → client).
Basic header: FMT=0, CSID=2 → 0x02 (1‑byte form).
Msg header (Type 0/11‑bytes): ts=000000, len=000004, type=01, MSID=00000000 (LE).
Payload: 00 00 10 00.
Putting together:

Worked example (hex) – WAS=2,500,000 (0x002625A0): type=05; payload 00 26 25 A0.
Worked example (hex) – SPB=2,500,000, limit=Dynamic(2): type=06; payload 00 26 25 A0  02.
Acknowledgement (type=3): The peer must send ACK (type=3, 4‑byte sequence number = total bytes received so far) whenever its receive counter exceeds the last acknowledged boundary of WAS. This is your back‑pressure mechanism.

## 2.2 NetConnection (client ↔ server): AMF‑encoded “connect”
Client → Server: Command Message (AMF0) on CSID=3, MSID=0, type=20 (0x14):
AMF0 body fields:
string "connect"
number transactionId=1 (IEEE‑754 double)
object “command object” with entries like app, tcUrl, fpad, capabilities, audioCodecs, videoCodecs, videoFunction, flashVer, swfUrl (keys/values as needed by your server)
(optional) user args …
Exact members and example values are listed in the spec’s NetConnection.connect section.
AMF0 type markers you’ll need:
0x02 string, 0x00 number (double), 0x03 object, 0x05 null, 0x01 boolean, etc. (lengths as per AMF0).
Example: first bytes of AMF0 “connect”
(AMF0 encoding rules and markers per spec)
Server → Client: (i) WAS/SPB/SCS if not already sent; (ii) reply with Command AMF0 "_result" (txnId=1) carrying status objects e.g., first object (fmsVer, capabilities) and an info object with level:"status", code:"NetConnection.Connect.Success", etc., then (iii) proceed to NetStream creation. See message formats and the connect command spec.

## 2.3 Create a stream then play or publish
Client → Server: createStream (AMF0 command) with a fresh transactionId (e.g., 2). Server → Client: _result with a numeric stream ID (e.g., 1).
From here, messages for that stream use MSID =  (e.g., 1) and usually CSID=8 or 5 for commands (you’re free to choose any non‑reserved CSID).
A) Playback (pull)
Client → Server: play (AMF0) with streamName (and optional start/len).
Server → Client: User Control (type=4) “StreamBegin” with the streamId (2‑byte event type + 4‑byte streamId). Then send onStatus(NetStream.Play.Start) and optional |RtmpSampleAccess / onMetaData data messages.
Server → Client: start sending Video (9)/Audio (8) messages (see §3).
B) Publishing (push)
Client → Server: publish (AMF0) with streamName and type (“live”, etc.).
Server → Client: User Control StreamBegin; then onStatus(NetStream.Publish.Start).
Client → Server: start sending Video (9)/Audio (8) on MSID=streamId.

## 3) Media payloads (inside RTMP type 8/9 messages)
RTMP Audio (8) and Video (9) messages embed FLV tag bodies (no FLV file header). The first bytes of those bodies follow FLV’s Audio/Video TagHeader and then codec‑specific payload (H.264/AVC or AAC most commonly).
## 3.1 Video (type=9) — H.264/AVC in FLV
FLV Video TagHeader:
For H.264, CodecID=7; keyframe+AVC sequence header 0x17 00 00 00 00 precedes AVCDecoderConfigurationRecord.
Sequence Header payload = AVCDecoderConfigurationRecord (SPS/PPS, lengthSizeMinusOne, etc.). After that, send each frame as AVCPacketType=1 followed by length‑prefixed NAL units.
## 3.2 Audio (type=8) — AAC in FLV
FLV Audio TagHeader packs SoundFormat/Rate/Size/Type and for AAC adds AACPacketType (0=SequenceHeader, 1=Raw AAC). First AAC Sequence Header carries AudioSpecificConfig (object type, sample rate index, channel config). Subsequent packets are raw AAC frames.
The FLV 10.1 spec defines these tag formats; AAC’s AudioSpecificConfig comes from MPEG‑4 Audio (ISO/IEC 14496‑3) and must be present once before raw frames.

## 4) Bit‑level examples of key RTMP control/user‑control packets
All below use CSID=2 (basic header 0x02 with FMT chosen as needed) and MSID=0 (little‑endian zero in the Type‑0 header), timestamp=0.
## 4.1 Set Chunk Size (type=1)

## 4.2 Window Acknowledgement Size (type=5)

## 4.3 Set Peer Bandwidth (type=6), Dynamic

## 4.4 Acknowledgement (type=3)

## 4.5 User Control (type=4) – StreamBegin for streamId=1


## 5) Command messages (AMF0/AMF3) you must support
## 5.1 NetConnection
connect → server returns _result and status info.
## 5.2 NetStream
createStream → server returns stream id (number).
play → server sends UserControl StreamBegin, onStatus(NetStream.Play.Start), then media.
publish → server sends UserControl StreamBegin, onStatus(NetStream.Publish.Start).
deleteStream → server may send StreamEOF/StreamDry then close.
AMF encodings: AMF0 and AMF3 are fully specified by Adobe; you’ll mostly use AMF0 for RTMP commands (type=0x14). Keep an AMF0 encoder/decoder handy (strings, numbers, objects, arrays).

## 6) Bandwidth & chunk negotiation (how to implement correctly)
Track two chunk sizes (send and receive) per direction; start at 128 until SCS received. You MUST obey peer’s Set Chunk Size when receiving and use your configured size when sending.
Maintain a byte‑received counter per connection. When bytes_since_last_ack >= WAS, send Acknowledgement (type=3) with that total. Reset the threshold window.
When sending, respect Set Peer Bandwidth from the peer; use its window and limit type:
Hard(0): limit to the given window.
Soft(1): limit to min(current limit, given window).
Dynamic(2): if prior was Hard, treat as Hard else ignore.
The common starting sequence is: after handshake, server sends WAS → SPB → SCS, then answers connect. This is widely observed and simplifies flow control.

## 7) Teardown (graceful)
Client → Server: deleteStream for each active stream.
Server → Client: may send UserControl StreamEOF / StreamDry.
Either may then send NetConnection.close or close TCP.
All event codes and command semantics are defined in the spec’s NetStream & User Control sections.

## 8) Implementation blueprint (server)
Here’s a practical order of work with pseudo‑APIs to keep you unblocked:
## 8.1 Connection handling
Accept TCP; associate a Connection context with:
Handshake FSM state
Rx/Tx chunk size (default 128)
Rx WAS and received‑bytes counter; last‑acked total
Map CSID → (previous chunk header) for delta headers (FMT=1..3)
Map MSID → stream context (per NetStream)
AMF encoder/decoder instances
Send queue with back‑pressure (respect SPB rules)
Perform handshake exactly as §1.1. Upon completion, switch to chunk I/O.
Immediately send WAS/SPB/SCS (recommended) on CSID=2, MSID=0.
## 8.2 Chunk I/O
Reader loop:
Read Basic Header; derive FMT, CSID.
Read Message Header per FMT; reconstruct absolute values using remembered headers per CSID.
If timestamp field was 0xFFFFFF, read Extended Timestamp.
Read chunk data up to current peer → us chunk size; reassemble message by (CSID, message stream, message id) until the full message length is collected.
Bump received‑bytes and send ACK on threshold.
Writer:
Break messages into chunks of our send chunk size (SetChunkSize we announced).
Use Type 0 header for the first chunk of a message on a CSID, then prefer Type 3 for continued chunks if possible.
## 8.3 Protocol control and commands
Control (types 1–6): apply immediately, ignore timestamps.
User Control (type 4): events like StreamBegin, PingRequest/Response; payload is eventType (16b) + data.
Commands: AMF0 AMF/Command on type=20; decode connect, reply _result. Then createStream → stream id; play/publish etc. See full command schemas and examples in §7 of the spec.
## 8.4 Media
Playback path: fetch or generate FLV‑style payloads; first send AVC/AAC sequence headers as RTMP Video/Audio messages, then send frames (with proper timestamps, interleave by DTS, and use CompositionTime for H.264 B‑frames).
Publishing path: accept incoming Video/Audio messages and relay/record as needed. The first video/audio packet per stream must be the sequence header for decoders to initialize.

## 9) End‑to‑end example flows (condensed)
## 9.1 Publish flow (client OBS/FFmpeg → your server)
TCP & RTMP handshake complete.
Server → Client: WAS, SPB, SCS.
Client → Server: connect (AMF0). Server → Client: _result success.
Client → Server: createStream. Server → Client: _result (streamId=1).
Client → Server: publish("live/stream"). Server → Client: UserControl StreamBegin(1), onStatus(NetStream.Publish.Start).
Client → Server: send Video 0x17 00 … (AVC sequence header), Audio AAC sequence header, then regular Video/Audio frames.
Acks happen per WAS as bytes accumulate; server may throttle per SPB.
Teardown: Client deleteStream(1); server sends StreamEOF/StreamDry and closes.
## 9.2 Play flow (your server → player)
Like above, but client issues play and your server emits StreamBegin and starts sending sequence headers then frames.

## 10) Test with real tools
Publish with FFmpeg to your RTMP server:
(FFmpeg supports rtmp, rtmps, etc.; this is a standard publish command.)
Reference server behavior: SRS docs show typical handshake/flow, default configs (chunk size, ack sizes) you can mirror for interop.

Appendix A — Field/packet quick reference (bit‑level)
A.1 Chunk Basic Header (1–3 bytes)
## 1‑byte form (CSID=2..63):
## 2‑byte form (CSID=64..319): first byte’s CSID part is 0; id = secondByte + 64.
## 3‑byte form (CSID up to 65599): id = thirdByte*256 + secondByte + 64.
(Use the smallest form possible.)
A.2 Message Header (by FMT)
FMT=0 (Type‑0): 11B: timestamp(3) length(3) typeId(1) msid(4 LE)
FMT=1 (Type‑1): 7B: delta(3) length(3) typeId(1)
FMT=2 (Type‑2): 3B: delta(3)
FMT=3 (Type‑3): 0B: reused previous header values
Extended Timestamp (4B) present when timestamp/delta is 0xFFFFFF.
A.3 Protocol Control payloads
Set Chunk Size(1): uint32 chunk_size
Abort(2): uint32 csid
Acknowledgement(3): uint32 sequence
User Control(4): uint16 eventType + eventData(…)
Window Acknowledgement Size(5): uint32 window
Set Peer Bandwidth(6): uint32 window + uint8 limitType
(All on CSID=2, MSID=0, timestamps ignored.)
A.4 AMF references
AMF0 markers: number=0x00, boolean=0x01, string=0x02, object=0x03, null=0x05, ecma array=0x08, strict array=0x0A, …; AVM+ marker 0x11 switches to AMF3.
AMF3 markers and U29 encoding rules (integers, strings, arrays, traits) detailed in the AMF3 spec.
A.5 FLV tag formats for H.264/AAC (inside RTMP 9/8)
Video: FrameType|CodecID, for H.264 CodecID=7; then AVCPacketType, CompositionTime. Sequence‐header = AVCDecoderConfigurationRecord; regular frames = length‑prefixed NALs.
Audio: AAC requires AACSequenceHeader first (AudioSpecificConfig), then raw AAC frames with AACPacketType=1.

Appendix B — Minimal server message orderings (ready‑to‑use)
After handshake (server):
WAS(window=2_500_000) → SPB(2_500_000,Dynamic) → SCS(4096) (CSID=2, MSID=0)
Respond to connect with _result (AMF0), then be ready for createStream.
When stream begins (play/publish):
Send UserControl StreamBegin(streamId).
For play: onStatus(NetStream.Play.Start) then onMetaData (AMF data msg type=18), then media.
For publish: onStatus(NetStream.Publish.Start).

Appendix C — Common pitfalls (and the spec lines to re‑read)
Wrong CSID/MSID for control: protocol control must be CSID=2, MSID=0.
Forgetting ACKs: if you don’t send Acknowledgement (3) when passing WAS boundaries, peers may stall.
Chunk header deltas: you must cache prior headers per CSID to decode FMT=1..3.
Missing sequence headers: decoders won’t play H.264/AAC without the initial AVC/AAC sequence headers.
Endian mix‑ups: MSID is little‑endian (everything else is network byte order).

Sources (primary, normative & widely-implemented)
Adobe RTMP 1.0 (machine‑readable HTML, and original PDF) — full chunking, handshake, control, commands, media types.
AMF0 / AMF3 specifications — type markers and encodings for RTMP commands.
Adobe FLV 10.1 — defines FLV tag payloads used in RTMP Audio/Video messages (AVC/AAC sequence headers, etc.).
SRS documentation — practical handshake notes, interop behaviors and configs.
Wikipedia (RTMP) — background & default TCP port reference.
FFmpeg protocols — standard client for publishing to RTMP.


# RTMP server and client solution codded in Go lang version 1.25.1.
The proposed product is a Go-based RTMP (Real-Time Messaging Protocol) server and client implementation, designed for robust, spec-compliant media streaming. Its primary purpose is to enable reliable, low-latency transmission of audio, video, and control data between publishers (clients) and media servers, supporting live and on-demand streaming scenarios.
## Intended Users:
- Developers building streaming platforms or integrating RTMP ingest/playback into their services.
- Operators needing a customizable, testable RTMP stack for interoperability with tools like OBS, FFmpeg, or custom encoders/players.

## Key Features:

1. Modular Go Architecture: The solution is split into clear packages (client, server, connection, handshake, chunk, control, AMF, RPC, media, internal utilities), each mirroring a protocol responsibility. This modularity supports maintainability and extensibility.
2. Spec-Driven State Machines: Handshake, chunk I/O, RPC, and flow control are implemented as explicit state machines, ensuring protocol correctness and simplifying debugging.
3. Wire-Format Fidelity: All protocol messages (handshake, control, media, AMF commands) are encoded/decoded per RTMP spec, including correct chunking, header formats, and endian handling.
4. Adaptive Chunk Size: The implementation supports dynamic tuning of RTMP chunk sizes based on network conditions, balancing throughput and latency while avoiding fragmentation and interleaving issues.
5. Comprehensive Control Handling: All RTMP control messages (Set Chunk Size, Abort, Acknowledgement, User Control, Window Ack Size, Set Peer Bandwidth) are supported, with correct stream/channel assignment and flow control semantics.
AMF0 Command Support: The stack encodes/decodes AMF0 commands for connection management, stream creation, play, publish, and status messaging, supporting transaction mapping and concurrency.
6. Media Payload Handling: Audio and video are encapsulated as FLV tag bodies, with correct handling of sequence headers (AVC/AAC) and timestamping, ensuring compatibility with standard decoders.
7. Concurrency and Backpressure: Each connection runs in its own goroutine, with bounded queues and flow control to prevent resource exhaustion and ensure smooth operation under load.
8. Testability: The plan includes golden test vectors, integration harnesses, and fuzzing for protocol robustness, plus interop tests with common RTMP tools.
10. Clear Implementation Roadmap: Tasks are broken down into small, autonomous units with defined dependencies, exit criteria, and test requirements, supporting incremental delivery and quality assurance.

Overall, the solution is designed for correctness, performance, and ease of integration, providing a production-ready RTMP stack with strong test coverage and clear extensibility points. It is suitable for both server and client roles, and can be used as a foundation for custom streaming solutions or as a reference implementation for RTMP protocol education and testing.


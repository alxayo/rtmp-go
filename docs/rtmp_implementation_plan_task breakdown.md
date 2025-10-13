
 Tasks are intentionally small and autonomous, respect dependencies, and follow the implementation order recommended in the blueprint (Handshake → Control burst → Chunk I/O → AMF0 → RPC → Media → Teardown).

Phase 0 — Repo & Foundations
T0. Repository & Package Scaffolding
Description: Create the module skeleton (go.mod) and packages mirroring the blueprint: rtmp/{client,server,conn}, handshake, chunk, control, amf, rpc, media, internal (bufpool/clock/logger/errors). Establish top‑level Connection and NetStream types.
Initial State: Empty repo.
Exit Criteria: Packages compiled; public types compile; make build (or go build ./...) passes.
Dependencies: None.
Test Requirements: Lint + build CI pipeline succeeds; trivial compile‑time tests pass.

Phase 1 — Handshake
T1. Simple Handshake (Server FSM)
Description: Implement server‑side “simple” RTMP handshake (S0/S1/S2) and state machine: SVRecvC0C1 → SVSentS0S1 → SVRecvC2 → SVSentS2 → SVCompleted. Version byte 3, sizes: S0=1B, S1=1536B, S2=1536B. Transition to chunk mode only after completion.
Initial State: Package skeleton exists (T0).
Exit Criteria: Given C0+C1, server emits S0+S1; upon C2, emits S2 and returns “handshake complete”.
Dependencies: T0.
Test Requirements: Unit tests with golden byte sequences for valid/invalid C0/C1/C2; timeouts; wrong version; truncated buffers.
T2. Simple Handshake (Client FSM)
Description: Implement client‑side “simple” handshake (C0/C1/C2) and state machine: HSSentC0C1 → HSRecvS0S1 → HSSentC2 → HSCompleted. Version 3.
Initial State: T0.
Exit Criteria: Client sends C0+C1; on S0+S1, sends C2; completes after S2.
Dependencies: T0.
Test Requirements: Loopback tests against T1; negative tests (version mismatch, size errors).
T3. Post‑Handshake “Control Burst” (Server)
Description: Immediately after handshake completion, server sends Window Acknowledgement Size (WAS), Set Peer Bandwidth (SPB), then Set Chunk Size (SCS) on CSID=2, MSID=0 as recommended in the blueprint.
Initial State: T1 done.
Exit Criteria: On handshake complete, server emits WAS→SPB→SCS in order with configured values.
Dependencies: T1.
Test Requirements: Integration test: handshake → observe control triplet in correct order; verify default chunk size is 128 before SCS and updated after.

Phase 2 — Chunking (Headers, Reader, Writer)
T4. Chunk Header Types & State Cache
Description: Implement parsing/serialization of Basic+Message+Extended Timestamp headers; maintain per‑CSID last‑header cache to resolve FMT=1..3; handle MSID little‑endian nuance.
Initial State: T0.
Exit Criteria: Given sequences of headers (FMT 0–3), parser emits fully resolved message headers, honoring Extended Timestamp (0xFFFFFF). Serializer can reconstruct headers.
Dependencies: T0.
Test Requirements: Golden tests for all FMTs, CSIDs, extended TS edge cases, MSID endianness.
T5. Dechunker (Reader)
Description: Implement chunk reader that reassembles messages from interleaved chunks across CSIDs; respects receive chunk size; surfaces complete messages to the dispatcher.
Initial State: T4.
Exit Criteria: For synthetic interleaved inputs, emits correct message boundaries with timestamps.
Dependencies: T4.
Test Requirements: Table‑driven tests with fragmented messages, mixed CSIDs, extended timestamps, abort mid‑stream.
T6. Chunker (Writer)
Description: Implement writer that fragments outgoing messages according to SendChunkSize; schedules CSIDs; supports header compression (FMT>0) when valid; emits to net.Conn.
Initial State: T4.
Exit Criteria: Given arbitrary message sizes, produces correct chunk sequences (FMT selection, extended TS as needed).
Dependencies: T4.
Test Requirements: Golden serialization tests; verify reassembly by T5; measure compliance with configured SendChunkSize.
T7. Message Dispatcher
Description: Implement a dispatcher that routes complete messages by MsgTypeID to control, command (AMF), and media handlers; ensure control messages are recognized on CSID=2, MSID=0.
Initial State: T5.
Exit Criteria: Messages are delivered to registered handlers with correct context (CSID, MSID, timestamps).
Dependencies: T5.
Test Requirements: Unit tests feeding mixed control/command/media messages; assert routing & ordering.

Phase 3 — Protocol Control & Flow Control
T8. Control Message Types 1–6
Description: Implement structs/encoders/decoders for: Set Chunk Size(1), Abort(2), Acknowledgement(3), User Control(4), Window Ack Size(5), Set Peer Bandwidth(6); always on CSID=2, MSID=0.
Initial State: T7.
Exit Criteria: Round‑trip encode/decode fidelity for each control message; handlers invoked.
Dependencies: T7.
Test Requirements: Golden vectors; misuse tests (wrong stream/CSID) rejected/ignored as per design.
T9. ACK Window Tracking (WAS) & Acknowledgement
Description: Track bytesReceived; emit Acknowledgement (type=3) when crossing the WAS threshold; allow configuring WAS; reset accounting on ACK. Applies to both roles.
Initial State: T8.
Exit Criteria: Given traffic, ACKs are emitted exactly when boundaries are crossed; counters correct after wrap/ACK.
Dependencies: T8.
Test Requirements: Simulated input byte counts around boundaries; timing independence (tick vs data‑driven).
T10. Set Peer Bandwidth (SPB) Enforcement
Description: Implement SPB handling with limit types Hard(0), Soft(1), Dynamic(2); integrate with writer scheduling/back‑pressure so outbound rate respects peer window.
Initial State: T8.
Exit Criteria: Writer throttles according to SPB; limit type semantics observed.
Dependencies: T6, T8.
Test Requirements: Rate‑limit tests; window updates; dynamic type transitions.
T11. Set Chunk Size Application
Description: Apply SCS to both reader and writer paths (RecvChunkSize/SendChunkSize) and ensure subsequent fragmentation/parsing honors new sizes.
Initial State: T6, T8.
Exit Criteria: Changing chunk size at runtime is honored in both directions.
Dependencies: T6, T8.
Test Requirements: Send/receive with size change mid‑stream; verify chunk boundaries.
T12. Abort Message Handling
Description: Implement Abort Message (type=2) to discard partially received message on selected CSID and clear related dechunker state.
Initial State: T5, T8.
Exit Criteria: After Abort for CSID=X, partial buffers for X are dropped; reassembly continues for others.
Dependencies: T5, T8.
Test Requirements: Inject partial message then Abort; ensure no leak/corruption; other CSIDs unaffected.
T13. User Control Events
Description: Support StreamBegin/EOF/StreamDry, PingRequest/Response, and other User Control subtypes (type=4), with proper payloads and timing.
Initial State: T8.
Exit Criteria: Encode/decode + dispatch of user control events; plumb through to NetStream lifecycle.
Dependencies: T8, T18.
Test Requirements: Unit tests per subtype; integration: UserControl emitted during play/publish sequences.

Phase 4 — AMF0 Codec
T14. AMF0 Core Types & Command/Data Encoding
Description: Implement AMF0 encoder/decoder for primitives, ECMA arrays, objects; provide helpers for connect, _result, and ScriptData messages.
Initial State: T0.
Exit Criteria: Round‑trip encode/decode for representative objects; command frames serialized to RTMP payloads.
Dependencies: T0.
Test Requirements: Golden AMF0 samples (command & script data); fuzz basic types.
T15. Command Transaction Mapping
Description: Implement command name + transaction ID mapping table; txnID → response channel to pair requests with _result/_error.
Initial State: T14.
Exit Criteria: Concurrent commands resolve to the correct responses; timeouts handled.
Dependencies: T14.
Test Requirements: Concurrency tests; out‑of‑order answers; missing responses.

Phase 5 — RPC Layers (NetConnection / NetStream)
T16. NetConnection (Client)
Description: Implement SendConnect(app, tcUrl, ...) to encode AMF0 connect, send over RTMP, await _result, and materialize connection state.
Initial State: T6, T7, T14, T15.
Exit Criteria: Successful connect round‑trip; connection properties captured.
Dependencies: T14, T15, T6, T7.
Test Requirements: Loopback with server stub; error cases (bad app, timeouts).
T17. NetConnection (Server)
Description: Handle incoming connect; validate/apply parameters; respond with _result; trigger control/user‑control as required by your policy.
Initial State: T7, T14, T15.
Exit Criteria: Server accepts/denies connect and transitions connection context to ready.
Dependencies: T14, T15, T7, T3.
Test Requirements: Integration with client T16; negative tests for malformed AMF.
T18. NetStream Lifecycle (createStream/deleteStream)
Description: Implement createStream/deleteStream; maintain MSID → *NetStream map with locking; expose stream state (Idle/Playing/Publishing/Closed). Remember MSID is little‑endian on the wire.
Initial State: T0, T14, T15.
Exit Criteria: Streams can be created/deleted; state transitions audited.
Dependencies: T14, T15.
Test Requirements: Multi‑stream create/delete concurrency; endianness correctness in frames.
T19. Play Flow
Description: Implement client play(name, start, length) and server handling; send appropriate User Control (e.g., StreamBegin) and status messages; start media egress on the stream.
Initial State: T18.
Exit Criteria: Play request produces correct control/AMF responses; server begins sending media on target MSID.
Dependencies: T18, T13.
Test Requirements: Sequence test that matches the blueprint’s Play flow diagram ordering.
T20. Publish Flow
Description: Implement client publish(name, type) and server acceptance; transition stream to Publishing; prepare to receive media from client; send status events as applicable.
Initial State: T18.
Exit Criteria: Publish request accepted, stream state=Publishing; server can ingest media.
Dependencies: T18, T13.
Test Requirements: Sequence test that matches the blueprint’s Publish flow diagram ordering.
T21. Status/OnStatus Messaging
Description: Provide helpers to emit/parse common NetConnection/NetStream onStatus events used during play/publish (e.g., NetStream.Play.Start, NetStream.Publish.Start). (Names per your internal conventions; the blueprint’s sequence diagrams expect ordered status/control frames around play/publish.)
Initial State: T14, T15.
Exit Criteria: Easy APIs to send consistent status messages; decoded on the client.
Dependencies: T14, T15, T19, T20.
Test Requirements: Compare emitted sequences to diagrammed expectations.

Phase 6 — Media Payloads (FLV Tag Bodies in RTMP)
T22. FLV Tag Body Helpers (Audio/Video)
Description: Implement FLV body marshaling for RTMP type 9 (Video) and 8 (Audio), plus 18 (ScriptData) as needed; support AVC and AAC payload shapes per FLV conventions.
Initial State: T0.
Exit Criteria: Helpers construct valid FLV tag bodies for AVC/AAC.
Dependencies: None (can be done in parallel with earlier phases).
Test Requirements: Golden sample bodies; validate against known decoders.
T23. AVC/AAC Sequence Headers
Description: Implement AVCDecoderConfigurationRecord and AudioSpecificConfig helpers and ensure sequence headers are sent once before frames on each stream.
Initial State: T22.
Exit Criteria: First media frames on a stream are sequence headers; subsequent frames reference them.
Dependencies: T22, T20 (publish), T19 (play) as applicable.
Test Requirements: Integration to verify first packet is sequence header; negative test when missing -> consumer fails appropriately.
T24. Media Timestamping & Message Assembly
Description: Map input PTS/DTS to RTMP timestamps; assemble RTMP messages (type 9/8) with proper MsgLength, timestamps, MSID, and chunk via T6.
Initial State: T6, T22, T23.
Exit Criteria: Continuous media stream with monotonically nondecreasing timestamps; chunked per SendChunkSize.
Dependencies: T6, T22, T23.
Test Requirements: Timestamp continuity, rollover/extended TS, mixed audio/video cadence.
T25. Media Ingest/Egress Integration
Description: Wire media path to NetStream states: server egress for Play, ingest for Publish; back‑pressure aware writer.
Initial State: T19, T20, T24.
Exit Criteria: End‑to‑end publish → play loopback works in memory.
Dependencies: T19, T20, T24, T10.
Test Requirements: Loopback test: client publishes H.264/AAC with sequence headers; another client plays and decodes.

Phase 7 — Concurrency & Back‑Pressure
T26. Connection Goroutine Model
Description: Implement one goroutine per connection with select over: readerLoop (T5), writerLoop (T6), ack ticker/byte counters (T9), ctx cancellation; ensure safe shutdown.
Initial State: T5, T6, T9.
Exit Criteria: Connection runs and shuts down cleanly on context cancel or error; no goroutine leaks.
Dependencies: T5, T6, T9.
Test Requirements: Race detector; leak checks; cancellation propagation; high‑volume stability.
T27. TX Queue & SPB‑Aware Scheduler
Description: Implement bounded txQueue with SPB enforcement from T10; prevent unbounded memory growth; apply back‑pressure to producers.
Initial State: T6, T10.
Exit Criteria: Under overload, queue remains bounded; writes respect SPB window; drop/backoff policy defined.
Dependencies: T6, T10.
Test Requirements: Load tests with oversized producers; verify fairness and latency bounds.
T28. Stream/CSID State Safety
Description: Guard streams and per‑CSID header cache with mutexes (as in blueprint); validate no races under FMT compression.
Initial State: T4, T18.
Exit Criteria: -race clean under concurrent publish/play over multiple streams/CSIDs.
Dependencies: T4, T18.
Test Requirements: Stress tests with interleaved control/media across many streams.

Phase 8 — Teardown & Lifecycle
T29. Graceful Teardown
Description: Implement DeleteStream, close/teardown sequences; emit User Control events (e.g., StreamEOF/StreamDry), drain/close queues, free CSID cache.
Initial State: T13, T18, T26.
Exit Criteria: Closing a NetStream or Connection leaves no goroutines/buffers; correct control frames are sent.
Dependencies: T13, T18, T26.
Test Requirements: Teardown while idle and while streaming; verify no double‑frees/leaks.

Phase 9 — Test Vectors, Interop & Quality
T30. Handshake & Control Golden Vectors
Description: Provide golden byte vectors for: complete simple handshake; post‑handshake WAS→SPB→SCS burst; verify exact bytes on the wire.
Initial State: T1–T3, T8.
Exit Criteria: Tests fail if any field/ordering deviates.
Dependencies: T1–T3, T8.
Test Requirements: Byte‑exact assertions against captured vectors.
T31. Publish Path Integration Test
Description: Build a harness that pushes AVC/AAC sequence headers then media frames over a client connection to server; confirm ordering and stream state transitions (Publishing).
Initial State: T20, T22–T25.
Exit Criteria: Server receives headers first, then frames; no reorder; states/logs match expectations.
Dependencies: T20, T22–T25.
Test Requirements: Hex fixtures; timing jitter; loss injections; abort mid‑GOP.
T32. Play Path Integration Test
Description: Start a server stream; client requests play; verify User Control (StreamBegin), status messages, and media egress cadence and chunking.
Initial State: T19, T22–T25.
Exit Criteria: Client decodes sequence headers then media; timestamps monotonic; ACKs emitted past WAS.
Dependencies: T19, T22–T25, T9.
Test Requirements: Bitrate sweeps; chunk size changes mid‑stream; extended TS cases.
T33. Interop Scenarios (OBS/FFmpeg)
Description: Manual/automated interop matrix: OBS/FFmpeg publish to our server; our client plays from common servers; verify compatibility with common chunk sizes and control behaviors.
Initial State: T19–T25.
Exit Criteria: Pass basic publish/play with defaults (chunk=128 → raised by SCS); status/control seen as expected.
Dependencies: T19–T25.
Test Requirements: Scripts to run OBS/FFmpeg in headless mode; log capture and assertions.
T34. Fuzzing the Chunk Parser
Description: Add go‑fuzz style tests for T4/T5 to catch malformed headers, extended TS edge cases, CSID collisions, and abort scenarios.
Initial State: T4, T5.
Exit Criteria: Fuzzer runs find no panics; coverage targets met.
Dependencies: T4, T5.
Test Requirements: Corpus seeded with vectors from T30/T31/T32.
T35. Performance & Back‑Pressure Benchmarks
Description: Benchmarks for reader/writer throughput with varying chunk sizes; verify SPB throttling and txQueue behavior under load.
Initial State: T6, T10, T27.
Exit Criteria: Published baseline QPS/latency; no unbounded memory; stable under 10× target bitrate.
Dependencies: T6, T10, T27.
Test Requirements: testing.B benches; pprof heap/goroutine snapshots.

Dependency‑Respecting Order (Summary)
T0 → Foundations.
Handshake: T1, T2, then T3.
Chunking: T4 → T5 → T6 → T7.
Control/Flow: T8 → T9 → T10 → T11 → T12 → T13.
AMF0: T14 → T15.
RPC: T16, T17, T18, T19, T20, T21.
Media: T22 → T23 → T24 → T25.
Concurrency: T26 → T27 → T28.
Teardown: T29.
Quality: T30–T35 (some parallelizable once core paths exist).

Notes tied to the blueprint (applied above)
Default chunk size 128 until SCS updates it. Control on CSID=2 / MSID=0. MSID is little‑endian; all else big‑endian. WAS/SPB/ACK semantics and SPB limit types 0/1/2 are enforced. These constraints are reflected in T3, T4, T8–T11, T18, T27.
Sequence headers first on each stream (T23/T31/T32) and ordered sequences for Publish/Play (T19–T21).
Goroutine model and locks on streams/CSID caches (T26–T28).

If you want, I can export these as an Azure Boards/Jira CSV (with columns for Name, Description, Initial State, Exit Criteria, Dependencies, Test Requirements) or generate the wire‑level golden vectors + Go test harness the blueprint mentions so you can start validating immediately. Preference? 
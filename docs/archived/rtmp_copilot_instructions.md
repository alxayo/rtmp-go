# Copilot Instruction File: Build a Minimal-Dependency RTMP Server & Client in Go
Purpose: This file instructs GitHub Copilot (within VS Code) to generate a complete, production-grade RTMP server and client in Go, using only the standard library where possible. It emphasizes idiomatic Go, robust error handling, safe concurrency, modular design, testability, and maintainability.
Scope: Implement RTMP handshake, chunking, command/message flow, basic publish/play, and media/data relay (transparent byte-forwarding; no transcoding).
Constraint: Do not reference or copy code from existing RTMP libraries. Implement from first principles based on protocol specifications and observed behaviors with tools like ffmpeg/ffplay.

## 1) Overview
Goal: Implement an RTMP stack with:
Server: Accepts publishers and players; routes streams (app/streamKey), handles handshake, chunking, control messages, AMF0 commands, basic session state, and fan-out to subscribers.
Client: Can publish media to a server and play from a server. Provides a CLI for simple flows.
Protocol:
Handshake: C0/C1 ↔ S0/S1/S2 (version 3, 1536-byte random).
Chunk stream: Basic header (fmt+csid), message header by format (0/1/2/3), extended timestamp rules.
Message types: Set Chunk Size (1), Abort (2), Ack (3), User Control (4), Window Ack Size (5), Set Peer Bandwidth (6), Audio (8), Video (9), Data (18 AMF0), Command (20 AMF0).
Commands: connect, _result, createStream, publish, play, deleteStream, onStatus, releaseStream, FCPublish, FCUnpublish (support minimally—enough for ffmpeg/ffplay interop).
AMF: Implement a minimal AMF0 encoder/decoder (Number, Boolean, String, Object, Null, ECMA Array, Strict Array). Avoid AMF3.
Non-goals:
Transcoding, muxing/demuxing beyond transparent forwarding.
Full RTMP edge cases (we’ll handle common/control paths robustly).
GUI tooling or dashboards.

## 2) Architecture
### 2.1 Repository Layout
### 2.2 Concurrency Model
Per-connection:
One goroutine for readLoop (decode chunks → messages).
One goroutine for writeLoop (consume outbound chan → encode chunks).
Context cancellation propagates shutdown.
Backpressure: bounded outbound queues; drop or disconnect slow consumers per policy.
Server:
accept loop → spawn per-connection worker.
stream registry (“rooms” keyed by app/streamKey) with:
single publisher (or configurable),
many subscribers,
fan-out via per-subscriber channels or ring buffers.
Client:
publish: read from input (stdin/file/socket), wrap into RTMP messages, writeLoop.
play: receive messages and write to output (stdout/file/socket) or simply validate flow.

## 3) Key Components
### 3.1 Handshake (internal/rtmp/handshake.go)
Implement RTMP version 3:
C0: 1 byte version (0x03).
C1, S1, S2: 1536 bytes (time, zero, random; respond with echo semantics).
Use io.ReadFull with deadlines.
Verify version byte; negotiate timeouts; log peer addresses.
### 3.2 Chunking (internal/rtmp/chunk.go)
Parse basic header (fmt: 2 bits; csid: 6/14/22 bits).
Parse message header depending on fmt (0/1/2/3).
Extended timestamp when timestamp >= 0xFFFFFF.
Track chunk stream state per csid (previous headers).
Default chunk size: 128; support Set Chunk Size (type 1).
Writer must split payload into chunks with appropriate headers.
### 3.3 Message Framing (internal/rtmp/message.go)
Define message types and Go structs for:
Control (Set Chunk Size, Abort, Ack, Window Ack Size, Set Peer BW, User Control).
Data (type 18 AMF0).
Command (type 20 AMF0).
Audio (8) and Video (9) as opaque payloads.
Ensure Message Stream ID (msid) semantics:
Command messages often on stream id 0.
Media messages on non-zero stream id.
### 3.4 Commands & Sessions (internal/rtmp/command.go)
Implement minimal AMF0-based commands:
connect → _result (NetConnection.Connect.Success), set window sizes, chunk size.
createStream → _result with stream id (e.g., 1).
publish → onStatus (NetStream.Publish.Start).
play → onStatus (NetStream.Play.Start) then send media as relayed.
Implement FCPublish/FCUnpublish no-ops if needed for client interop.
Maintain per-connection session state (app, tcUrl, flashVer, objectEncoding).
### 3.5 Control Flow (internal/rtmp/control.go)
Window Acknowledgement Size negotiation.
Acknowledgement tracking (bytes read vs. ack window).
User Control events:
Stream Begin/EOF,
PingRequest/Pong (keepalive).
Set Peer Bandwidth handling (respecting hard/soft/dynamic).
### 3.6 AMF0 (internal/amf0/*)
Implement minimal encoder/decoder:
Types: Number(0x00), Boolean(0x01), String(0x02), Object(0x03), Null(0x05), ECMA Array(0x08), Strict Array(0x0A), Date(0x0B) (optional), Object End(0x09).
Provide Encode(values ...any) ([]byte, error) and Decode([]byte) ([]any, error) and lower-level streaming APIs working with io.Reader/Writer.
Avoid reflection overuse; handle strings and maps carefully.
Prefer deterministic encoding for testability.
### 3.7 Server (internal/rtmp/server.go, internal/rtmp/room.go)
Server:
type Server struct { Addr string; TLS *tls.Config; ... }
ListenAndServe(ctx) & Serve(ln net.Listener).
Use net.Listen("tcp", addr) or tls.NewListener when TLS configured (for RTMPS).
Per-conn: wrap with bufio.Reader/Writer, set SetDeadline/SetReadDeadline appropriately.
Room/Registry:
type Hub struct { mu sync.RWMutex; rooms map[string]*Room }
type Room struct { publisher *Conn; subs map[*Conn]*subscriberState; ... }
On publish: set publisher; on play: add subscriber; fan-out media from publisher to all subs.
Backpressure policy: bounded channel per subscriber (e.g., 64 msgs); on overflow: drop frame or disconnect slow subscriber (configurable).
### 3.8 Client (internal/rtmp/client.go)
Dial server, perform handshake, connect → createStream → publish or play.
Publish: read FLV/RTMP-framed media from stdin/file (or accept opaque []byte frames) and send as video/audio messages.
Play: receive video/audio/data messages and write to stdout/file or provide a callback.
### 3.9 URL Parsing (internal/rtmp/url.go)
Parse rtmp://host[:port]/app/streamKey?query.
Extract host, port (default 1935), app, streamKey, tcUrl, optional auth token in query.
### 3.10 Logging & Metrics (internal/util/log.go)
Use log/slog (Go 1.21+) with levels and structured fields (conn id, remote addr, stream key).
Log handshake, commands, errors, state changes, timeouts.
Optional counters with expvar (stdlib) for active conns, publishers, subscribers.

## 4) Implementation Guidelines (Copilot, follow step-by-step)
Principles: Idiomatic Go, small packages, clear interfaces, cohesive files, unit-testable functions, no external dependencies (stdlib only). Use contexts for cancellation, timeouts, and WaitGroups for coordinated shutdown.
### 4.1 Project Initialization
Create the repo structure shown above.
go mod init example.com/rtmp-go (adjust module path).
Add .golangci.yml later if you want, but prefer go vet and go test -race (no external tools).
### 4.2 Types & Constants
Create internal/rtmp/message.go with types and constants:
### 4.3 Handshake
Implement PerformServerHandshake(r io.Reader, w io.Writer, dl time.Duration) error and PerformClientHandshake(...).
Key rules:
Expect/send version 0x03.
C1/S1/S2: 1536 bytes. Echo peer’s random where needed.
Use io.ReadFull, set SetReadDeadline.
Validate lengths; wrap errors with %w.
### 4.4 Chunk Reader/Writer
Implement ChunkReader and ChunkWriter:
Support fmt 0/1/2/3 headers, extended timestamps.
Recover per-chunk-stream previous header values.
Reader returns fully reassembled Message.
Writer splits Message.Payload into chunks ≤ sz.
### 4.5 AMF0
Create internal/amf0/encode.go and decode.go:
API:
func Encode(values ...any) ([]byte, error)
func Decode(b []byte) ([]any, error)
Streamed forms: EncodeTo(w io.Writer, values ...any) error, DecodeFrom(r io.Reader) ([]any, error).
Support minimal types; ensure string length fits AMF0 (uint16) when needed; for long strings, handle appropriately (AMF0 supports Long String 0x0C — implement if necessary).
Unit tests covering round-trips, edge cases, and golden samples.
### 4.6 Connection & State Machine
In internal/rtmp/conn.go, define:
func (c *Conn) run() spawns readLoop() and writeLoop().
readLoop():
handshake, then receive messages, decode AMF0 for commands, update state, route to server/hub.
enforce SetReadDeadline (idle timeout).
on error: log, cancel context, close channels.
writeLoop():
select on out or ctx.Done(), write chunks, flush with bw.Flush() regularly.
handle backpressure: if blocked, drop non-critical frames (configurable).
Flow control:
track bytes read; send Acknowledgement when threshold passed.
handle inbound Set Chunk Size, Window Ack Size, Set Peer Bandwidth.
### 4.7 Server & Hub
In internal/rtmp/server.go:
In internal/rtmp/room.go:
On publish app/streamKey: create/find room; set publisher; start fan-out goroutine reading from publisher (media messages only) and distribute to subs.
On play: register conn as subscriber; send StreamBegin user control; then media stream.
Fan-out design:
Each subscriber has a bounded channel (e.g., chan Message length 64).
When full: either drop oldest (ring buffer) or disconnect slow sub (configurable).
Clean-up: on conn close, remove from room; if publisher leaves, notify subs (onStatus with NetStream.Play.UnpublishNotify), then stop fan-out.
### 4.8 Commands Implementation
connect (csid: 3, msid: 0):
Respond with Window Ack Size, Set Peer Bandwidth, Set Chunk Size, _result (NetConnection.Connect.Success).
createStream:
Respond with _result and assign streamID=1 (or incremental).
publish (msid: stream id):
Mark as publisher; onStatus NetStream.Publish.Start.
play (msid: stream id):
onStatus NetStream.Play.Reset/Start; issue UserControl(StreamIsRecorded?) as needed; begin receiving media.
deleteStream:
Remove subscriber or publisher; cleanup room.
Implement minimal onMetaData pass-through (type 18 Data).
### 4.9 Client
In internal/rtmp/client.go:
Publish: dials, handshake, connect → createStream → publish, then reads FLV-tagged RTMP payloads from r (or just pre-framed RTMP messages in our demo) and forwards as RTMP messages.
Play: receives audio/video/data and writes their payloads to w in sequence (or logs). For demo with ffplay, simply piping FLV is sufficient if you choose to implement simple FLV mux (optional). Otherwise, keep it as “validation” client.
### 4.10 CLI Tools
rtmp-server: flags: -addr :1935, -log-level, -chunk-size, -idle-timeout, -publisher-per-room 1.
rtmp-client:
rtmp-client publish -url rtmp://localhost/live/stream -i input.flv
rtmp-client play -url rtmp://localhost/live/stream -o output.flv

## 5) Best Practices (Apply Throughout)
### 5.1 Go Style & Structure
Keep files ~200–400 lines where sensible; split by concern.
Export only necessary symbols; provide doc comments for exported types/functions.
Prefer interfaces for testing seams (e.g., io.Reader/io.Writer).
### 5.2 Error Handling
Always wrap: fmt.Errorf("context: %w", err).
Use sentinel errors for expected conditions (e.g., ErrClosed, ErrTimeout).
Convert protocol violations into typed errors for better logs and tests.
### 5.3 Concurrency & Resource Management
Context everywhere. Respect ctx.Done().
Close channels from producers only; avoid closing shared channels.
defer close connections; cancel contexts; wg.Wait() on shutdown.
Set deadlines for handshake and I/O; refresh read deadlines on activity.
### 5.4 Performance & Memory
Reuse buffers via sync.Pool (internal/util/bufpool.go).
Avoid excessive allocations in hot paths (chunk parsing/encoding).
Batch bw.Flush() logically; but do not hold large payloads when backpressured.
### 5.5 Logging
Use slog with structured fields: "conn_id", "remote", "app", "stream", "csid", "msid", "type".
Log at Debug for protocol details; Info for state changes; Warn/Error for failures.
### 5.6 Security & Robustness
Validate sizes (chunk size, window size, message length).
Limit maximum message payload (e.g., ≤ 16MB).
Drop connections on malformed headers or abuse patterns.
Optional simple auth via token in RTMP URL query (application validates against a user-provided function).

## 6) Testing Strategy
Aim for high coverage on AMF0, chunking, handshake, and command flows. Use go test -race. Avoid flaky tests by using net.Pipe or bufconn-like patterns (implement with net.Pipe since stdlib only).
### 6.1 Unit Tests
AMF0:
Round-trip for all supported types.
Golden tests for connect command arrays.
Chunker:
Encode→Decode property: random payloads, timestamps edge cases (≥ 0xFFFFFF), fmt transitions (0→3).
Handshake:
Simulate client/server using net.Pipe, verify C0/S0 version and C1/S1/S2 sizes and echoes.
Control Messages:
Window Ack Size negotiation; ack counters; ping/pong.
### 6.2 Integration Tests
Spin up Server on ephemeral port.
Client publish a short test stream (generate synthetic audio/video payloads or minimal valid FLV headers).
Subscriber receives; verify the sequence of message types and onStatus events.
Test publisher disconnect: subscribers get unpublish notify.
### 6.3 Fuzzing (optional, stdlib)
Use testing/fuzz on AMF0 decoder and chunk parser with bounds checks to catch panics.
### 6.4 Tooling
go vet ./...
go test -race ./...
Benchmarks for chunk encode/decode.
### 6.5 Manual Verification (with ffmpeg/ffplay)
Publish:
Play:
Observe server logs and confirm playback.

## 7) Detailed Task List for Copilot (Generate Code in Small, Verifiable Steps)
Step 1 — Logging & Utilities
Create internal/util/log.go:
func NewLogger(level string) *slog.Logger
Create internal/util/bufpool.go with sync.Pool for byte slices.
(Optional) internal/util/ringbuf.go: fixed-size ring buffer for Message references.
Step 2 — AMF0 Minimal Implementation
Implement encoder/decoder with tests:
Encode/Decode for String, Number(float64), Boolean, Null, Object(map[string]any), ECMA Array(map[string]any), Strict Array([]any), Long String.
amf0_test.go with round-trips and golden vectors.
Step 3 — RTMP Handshake
PerformServerHandshake and PerformClientHandshake.
Tests using net.Pipe.
Step 4 — Chunk Reader/Writer
Implement header parsing/serialization, extended timestamp logic, and per-csid state.
Unit tests for combinations and boundaries.
Step 5 — Message Types & Helpers
Constructors for control messages (Window Ack Size, Set Chunk Size, Ack, User Control).
Marshal/unmarshal helpers to/from Message.
Step 6 — Commands
AMF0 command payload structures for connect, _result, createStream, publish, play, onStatus.
Encode/decode helpers with strict validation and tests.
Step 7 — Conn Lifecycle & Loops
Conn.run(), readLoop(), writeLoop(), send(msg Message) bool, closeWith(err error).
Apply deadlines, context cancel, and backpressure policy.
Step 8 — Server & Hub
Accept loop, per-conn spawn, graceful shutdown on ctx.
Hub with Publish/Subscribe/Unsubscribe, and fan-out goroutine per room.
Step 9 — Client
Implement Client.Publish and Client.Play with control flow (connect → createStream → publish/play).
Basic CLI under cmd/rtmp-client.
Step 10 — Integration Tests & Examples
End-to-end tests using in-memory pipes where sensible.
Shell scripts in /examples.

## 8) Code Snippets (Selected Skeletons)
### 8.1 Server main.go
### 8.2 Minimal AMF0 Encode (fragment)
### 8.3 Command Encode Example

## 9) Acceptance Criteria
Server accepts connections, completes handshake, responds to connect/createStream.
Publish path: accepts ffmpeg as publisher; subscribers can ffplay successfully (audio or video flow).
Play path: multiple subscribers can receive; slow subscriber policy enforced without affecting others.
AMF0 tests: ≥ 90% coverage of encoder/decoder paths.
Chunker: enc/dec round-trip tests pass including extended timestamps.
Race detector: go test -race passes.
Graceful shutdown: Ctrl+C closes listeners, waits for goroutines, and logs clean exit.
No external dependencies: go.mod imports only stdlib.

## 10) Suggested Prompts (for iterative Copilot use)
“Generate internal/amf0/encode.go implementing AMF0 encode for Number, Boolean, String (incl. Long String), Object, Null, ECMA Array, and Strict Array, with comprehensive comments and tests in amf0_test.go.”
“Create internal/rtmp/handshake.go with server/client handshake (version 3), using deadlines and io.ReadFull, plus tests with net.Pipe.”
“Implement internal/rtmp/chunk.go reading/writing RTMP chunks with fmt 0–3, extended timestamps, and state per csid; add round-trip tests.”
“Write internal/rtmp/command.go handlers for connect, _result, createStream, publish, play, onStatus using AMF0; include encoder helpers and unit tests.”
“Implement internal/rtmp/conn.go with readLoop/writeLoop, context cancellation, deadlines, flow control, and logging via slog.”
“Add internal/rtmp/server.go and room.go (Hub) to manage publishers and subscribers with bounded queues and backpressure policy.”
“Create cmd/rtmp-server/main.go and cmd/rtmp-client/main.go with CLI flags and basic run logic.”
“Add integration tests that spin up the server and validate publish/play flows with synthetic messages.”

## 11) Notes & Tips
Prefer little-endian for MsgStreamID per RTMP message header spec; other numeric fields are typically big-endian in chunk/message headers—be careful and add tests.
Chunk size: Start at 128; negotiate up (e.g., 4096) after connect to reduce overhead.
Window/Ack: Set reasonable default (e.g., 2–5MB), send Acks as bytes read exceed window thresholds.
User Control: Send StreamBegin when starting a stream; respond to Ping with Pong.
Keep AMF0 minimal but correct; most interop depends on correct command arrays and onStatus.

## 12) What to Build Next (Optional Enhancements)
RTMPS (TLS) with self-signed or provided certs (crypto/tls).
Authentication plug-in (token check) at publish/play time.
Basic FLV mux/demux (still stdlib).
Prometheus-style metrics (would require external dep, so prefer expvar or custom endpoints).
HLS re-streaming (large scope; separate module).

If you want, I can generate the first implementation batch (AMF0 + handshake + chunking skeleton + tests) to get you started. Would you prefer starting with AMF0 or the Handshake/Chunker first, Alex?
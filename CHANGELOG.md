# Changelog

All notable changes to go-rtmp are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **Multitrack sequence header caching**: Per-track video/audio sequence headers are cached in the stream registry for late-join subscribers (E-RTMP v2 multitrack)
- **MP4 recording parity**: Enhanced audio codec recording with proper MP4 sample entries
  - Opus → `Opus` sample entry + `dOps` box (OpusSpecificBox)
  - FLAC → `fLaC` sample entry + `dfLa` box (FLAC metadata blocks)
  - AC-3 → `ac-3` sample entry + `dac3` box (AC3SpecificBox)
  - E-AC-3 → `ec-3` sample entry + `dec3` box (EC3SpecificBox)
  - MP3 → `.mp3` sample entry + `esds` box (MPEG-1 Audio OTI)
- **E-RTMP v2 ModEx support**: Parse ModEx (Modifier Extension) packets for both video and audio, including nanosecond timestamp offset extraction
- **E-RTMP v2 Multitrack support**: Parse multitrack video/audio packets (OneTrack, ManyTracks, ManyTracksManyCodecs)
- **E-RTMP v2 additional packet types**: Handle AudioPacketType.SequenceEnd (4), MultichannelConfig (5), VideoPacketType.MPEG2TSSequenceStart (5)
- **Reconnect Request (E-RTMP v2)**: Server-initiated client reconnection via `NetConnection.Connect.ReconnectRequest`
  - `RequestReconnect(connID, tcUrl, description)` for single-connection redirect
  - `RequestReconnectAll(tcUrl, description)` for server-wide maintenance
  - `Connection.SendReconnectRequest(tcUrl, description)` for connection-level API
  - SIGUSR1 signal handler triggers reconnect-all (with optional `-reconnect-url` redirect)
- **VP8 E-RTMP support**: Added VP8 video codec (FourCC `vp08`) for Enhanced RTMP, completing full E-RTMP v2 video codec parity (H.264, H.265, AV1, VP8, VP9, VVC)
- **SRT Encryption**: Full AES-CTR encryption for SRT ingest streams with passphrase-based authentication
  - KMREQ/KMRSP key exchange during SRT handshake (KM message parser per SRT RFC §3.2.2)
  - AES-128, AES-192, and AES-256 support with PBKDF2-HMAC-SHA1 key derivation
  - Packet-level AES-CTR encryption/decryption with per-packet IV construction
  - Passphrase validation (10-79 characters per SRT specification)
  - PBKDF2 uses LSB 64-bit salt per SRT spec §6.2.1 for libsrt interoperability
- **SRT Key Rotation**: Hitless even/odd key rekeying for long-running encrypted streams
  - Dual-key KeySet with thread-safe even/odd cipher slots
  - Post-handshake KMREQ control packets (type 0x7FFF) for mid-stream key refresh
  - KKBoth support — both even and odd keys can be pre-announced in a single KMREQ
  - Automatic KMRSP acknowledgment sent back to the sender after key installation
  - Strict KM crypto profile validation (rejects unsupported cipher types, auth, KEKI)

### Fixed
- **SRT reconnection**: Second SRT connection with same stream key no longer fails after first disconnects (EvictPublisher fallback, identity-aware cleanup)

### Security
- Drop plaintext data packets on encrypted SRT connections (enforces security contract)
- Drop odd-key packets when only even key is installed (prevents wrong-key decryption)
- Reject KMREQ with key length mismatch during post-handshake rekeying
- Reject KMREQ with unsupported crypto parameters (non-AES-CTR cipher, non-zero auth/KEKI)

## [v0.3.0] — 2026-04-13

### Added
- **Per-stream metrics endpoint** (`rtmp_streams`): Dynamic JSON snapshot showing each active stream's key, subscriber count, video/audio codecs, recording status, and uptime — queryable via `curl localhost:8080/debug/vars | jq '.rtmp_streams'`
- **Per-destination relay endpoint** (`rtmp_relay_destinations`): Dynamic JSON snapshot of each relay destination's URL, connection status, and message/byte counters
- **8 new metrics counters and gauges**:
  - `rtmp_subscriber_drops_total` — messages dropped due to slow subscribers
  - `rtmp_auth_successes_total` / `rtmp_auth_failures_total` — authentication outcomes
  - `rtmp_handshake_failures_total` — failed RTMP handshakes
  - `rtmp_bytes_egress` — total bytes sent to subscribers
  - `rtmp_recordings_active` (gauge) — currently active recordings
  - `rtmp_recording_errors_total` — recorder creation/close errors
  - `rtmp_zombie_connections_total` — connections closed due to read timeout
- **Secure server setup scripts**: Platform-specific scripts for Linux (`scripts/run-secure-linux.sh`), macOS (`scripts/run-secure-macos.sh`), and Windows (`scripts/run-secure-windows.ps1`) with TLS, auth, and recording preconfigured
- **ABR HLS hook scripts**: On-publish event hooks that launch parallel FFmpeg instances for adaptive bitrate HLS output with automatic master playlist generation (`scripts/on-publish-abr.{sh,ps1}`)

### Fixed
- **SRT recorder gauge leak**: SRT publisher teardown path now correctly decrements `recordings_active` and increments `recording_errors_total` on close failure — previously every recorded SRT session left the gauge permanently high
- **Subscriber drop under-counting**: Real RTMP connections using `SendMessage` (with timeout) now correctly increment `subscriber_drops_total` on send failure — previously only the `TrySendMessage` non-blocking path was instrumented
- **Bytes egress over-counting**: `rtmp_bytes_egress` now only increments after successful message delivery, not before the send attempt
- **4 dead SRT metrics wired**: `srt_bytes_received`, `srt_packets_received` (in SRT→RTMP bridge), `srt_packets_retransmit` (NAK handler), and `srt_packets_dropped` (TSBPD reliability loop) were declared in v0.2.0 but never incremented — all four now report accurate values

### Documentation
- **Multi-stream ingest guide**: RTMP + SRT simultaneous operation, per-stream file storage layout, consumer subscription patterns (`docs/multi-stream-guide.md`, `site/content/docs/user-guide/multi-stream.md`)
- **Authentication deep-dive**: File-based token configuration, SIGHUP reload behavior and limitations, webhook authentication flow, TLS token security considerations (`site/content/docs/user-guide/authentication.md`)
- **Parallel FFmpeg ABR HLS guide**: Step-by-step setup for multi-resolution HLS using on-publish hooks with GOP alignment requirements (`site/content/docs/user-guide/hls-streaming.md`)
- **SRT encryption**: Passphrase validation flow and current limitations (`site/content/docs/user-guide/srt-ingest.md`, `docs/srt-protocol.md`)
- **Expanded metrics & monitoring**: SRT metrics section, dynamic endpoint documentation with `jq` query examples, Grafana dashboard panel suggestions (`site/content/docs/user-guide/metrics.md`)
- **README**: Added multi-stream and auth-file reload notes

## [v0.2.1] — 2026-04-12

### Added
- **FLV `onMetaData` Script Tag**: FLV recordings now include an `onMetaData` tag (TypeID 18) with video dimensions, codec IDs, audio sample rate, stereo flag, duration, and filesize. Duration and filesize are patched on close via `WriteAt()`.
- **AMF0 ECMA Array**: Full encoding/decoding support for AMF0 ECMA Array (marker `0x08`), used by the `onMetaData` tag.
- **H.264 SPS Parser**: Extracts video width and height from AVCDecoderConfigurationRecord sequence headers.
- **AAC AudioSpecificConfig Parser**: Extracts audio sample rate and channel configuration from AAC sequence headers.

### Fixed
- **FLV recording playback**: Players (VLC, ffplay) could only show the first frame because no `onMetaData` tag was present (`r_frame_rate=1000/1`, `avg_frame_rate=0/0`). Recordings now play correctly with proper frame rate and duration metadata.

## [v0.2.0] — 2026-04-12

### Added
- **SRT (Secure Reliable Transport) Ingest**: Accept SRT streams over UDP alongside RTMP. SRT publishers are transparently converted to RTMP format, allowing RTMP subscribers to watch SRT sources without any changes.
  - SRT v5 handshake with SYN cookie exchange and extension negotiation
  - Stream ID parsing: simple (`live/test`), prefixed (`publish:live/test`), and structured (`#!::r=live/test,m=publish`) formats
  - TSBPD (Timestamp-Based Packet Delivery) jitter buffer with configurable latency
  - ACK/NAK reliability with RTT measurement and retransmission
  - Optional AES encryption (128/192/256-bit) with PBKDF2 key derivation and AES Key Wrap (RFC 3394)
  - 31-bit circular sequence number arithmetic with wraparound handling
- **MPEG-TS Demuxer** (`internal/ts/`): Full transport stream parser with PAT/PMT table decoding, PES packet reassembly, and stream type detection (H.264, H.265, AAC)
- **Codec Converters** (`internal/codec/`): H.264/H.265 Annex B→AVCC and AAC ADTS→raw frame converters for SRT-to-RTMP bridge
  - **H.264 Support**: NALU splitter, SPS/PPS extraction, AVCDecoderConfigurationRecord builder
  - **H.265/HEVC Support**: VPS/SPS/PPS extraction, HEVCDecoderConfigurationRecord builder per ISO/IEC 14496-15
  - ADTS parser, AudioSpecificConfig builder
  - 90kHz→1ms timestamp conversion with CTS (Composition Time Offset) calculation
- **SRT-to-RTMP Bridge** (`internal/srt/bridge.go`): End-to-end pipeline converting SRT data packets through MPEG-TS demuxing and codec conversion into `chunk.Message` for the existing stream registry
  - H.265 frame handler with parameter set extraction and sequence header management
  - Support for H.264, H.265, and mixed H.264/H.265 streams with codec change detection
- **Codec-Aware Recording**: Automatic container selection based on codec — FLV for H.264/legacy codecs, MP4 for H.265/HEVC
  - MP4 recorder streams `mdat` to disk during recording (zero memory buffering), patches `mdat` size and appends `moov` atom on close
  - Lazy recorder initialization deferred until first media message for correct codec detection
- **FLV `onMetaData` Script Tag**: FLV recordings now include an `onMetaData` tag with video dimensions, codec IDs, audio sample rate, stereo flag, duration, and filesize. Duration and filesize are patched on close via `WriteAt()`. Metadata is extracted from H.264 SPS (width/height) and AAC AudioSpecificConfig (sample rate/channels). This fixes playback issues where players showed only the first frame due to missing frame rate information.
- **Ingress Abstraction** (`internal/ingress/`): Protocol-agnostic publish lifecycle manager shared by RTMP and SRT ingest paths
- **Comprehensive E2E Test Suite** (`e2e-tests/`): 25+ end-to-end tests covering RTMP publish/play, SRT ingest, RTMPS/TLS, Enhanced RTMP H.265, FLV/MP4 recording, authentication, event hooks, relay, metrics, and connection lifecycle
  - Shared test library (`_lib.sh`) with helpers for server management, stream validation, and cleanup
  - Cross-platform runners (Bash + PowerShell)
  - SRT camera integration tests with recording validation
- **SRT CLI Flags**: `-srt-listen`, `-srt-latency` (default 120ms), `-srt-passphrase`, `-srt-pbkeylen` (16/24/32)
- **SRT Metrics**: 6 new expvar counters — `srt_connections_active`, `srt_connections_total`, `srt_bytes_received`, `srt_packets_received`, `srt_packets_retransmit`, `srt_packets_dropped`
- **Comprehensive package documentation**: File-level comments and developer guide for all critical modules
- **SRT Documentation**: `docs/srt-protocol.md` technical reference with architecture diagram, codec conversion details, Stream ID format reference
- **H.265 Documentation**: `docs/H265_SUPPORT.md` with codec support matrix, bitrate comparisons, encoding/decoding guidelines, and troubleshooting

### Changed
- **MP4 recorder performance**: Streams `mdat` to disk in real-time instead of buffering in memory; patches `mdat` size via `WriteAt()` and appends `moov` atom on close
- **Allocation optimizations**: Replaced custom helpers with stdlib functions, reduced allocations in hot paths across media handling
- **Lazy recorder initialization**: Container format decision deferred until first media message arrives, enabling correct codec-aware container selection

### Fixed
- **H.265 HEVCDecoderConfigurationRecord**: Corrected builder to comply with ISO/IEC 14496-15 and switched to Enhanced RTMP signaling format
- **Flag parsing**: Fixed `-record-all` explicit bool flag parsing that caused incorrect behavior
- **Logging**: Call `slog.SetDefault()` so SRT listener and other subsystems use the configured log level
- **E2E test corrections**: Fixed three broken E2E tests (hooks and reconnect), updated Enhanced RTMP scripts for H.265 MP4 recording

### New Packages
- `internal/srt/packet/` — SRT wire protocol types
- `internal/srt/circular/` — Circular sequence number arithmetic
- `internal/srt/crypto/` — AES Key Wrap and PBKDF2
- `internal/srt/handshake/` — SRT v5 handshake FSM
- `internal/srt/conn/` — Connection state machine with reliability (TSBPD, ACK, NAK)
- `internal/ts/` — MPEG-TS demuxer
- `internal/codec/` — Video/audio codec converters (H.264, H.265, AAC)
- `internal/ingress/` — Publish lifecycle abstraction

## [v0.1.4] — 2026-04-10

### Added
- **Enhanced RTMP (E-RTMP v2) codec support**: Modern video codecs via FourCC signaling, compatible with FFmpeg 6.1+, OBS 29.1+, SRS 6.0+ ([`58005f8`](https://github.com/alxayo/rtmp-go/commit/58005f8))
  - **H.265/HEVC** (`hvc1`): High Efficiency Video Coding — 50% better compression than H.264
  - **AV1** (`av01`): AOMedia Video 1 — royalty-free next-gen codec
  - **VP9** (`vp09`): Google's open video codec
  - Automatic codec detection — no configuration or flags needed
  - `connect` command negotiation with `fourCcList` echo
  - Sequence header caching for all enhanced codecs (late-join support)
  - Legacy HEVC CodecID 12 still supported alongside enhanced `hvc1`
- **Enhanced RTMP audio signaling**: Enhanced audio tag header parsing for modern audio codecs ([`58005f8`](https://github.com/alxayo/rtmp-go/commit/58005f8))
  - Opus, FLAC, AC-3, E-AC-3, `.mp3` via E-RTMP FourCC
- **Enhanced RTMP E2E test scripts**: Paired Bash + PowerShell scripts validating H.265 end-to-end ([`1de8046`](https://github.com/alxayo/rtmp-go/commit/1de8046))
  - Build server → publish H.265+AAC via FFmpeg → verify recorded FLV (codec, duration, decode)
  - `scripts/test-enhanced-rtmp.sh` and `scripts/test-enhanced-rtmp.ps1`
- **27 new unit tests** for enhanced video/audio parsing, FourCC detection, and packet type classification ([`58005f8`](https://github.com/alxayo/rtmp-go/commit/58005f8))

### Changed
- **Shared codec helper**: Extracted `fourCC()` helper to `media/codec.go` for O(1) FourCC-to-uint32 map lookup ([`f0f3aa7`](https://github.com/alxayo/rtmp-go/commit/f0f3aa7))
- **Shared test helper**: Extracted `_tFatalf` to `media/helpers_test.go` ([`f0f3aa7`](https://github.com/alxayo/rtmp-go/commit/f0f3aa7))
- **Simplified video diagnostic logging**: Registry now uses `ParseVideoMessage()` instead of duplicating byte-level parsing ([`7ce892c`](https://github.com/alxayo/rtmp-go/commit/7ce892c))

### Fixed
- Inaccurate doc comment in `connect_response.go` (CSID was set to 3, not zero) ([`7ce892c`](https://github.com/alxayo/rtmp-go/commit/7ce892c))

---

## [v0.1.3] — 2026-04-09

### Added
- **RTMPS (TLS) support**: Encrypted RTMP connections via TLS termination at the transport layer ([`33212e4`](https://github.com/alxayo/rtmp-go/commit/33212e4))
  - New CLI flags: `-tls-listen`, `-tls-cert`, `-tls-key` for TLS-encrypted listener
  - Dual-listener architecture: plain RTMP (`-listen`) and RTMPS (`-tls-listen`) run simultaneously
  - `rtmps://` URL support in the Go client and relay destinations
  - Minimum TLS 1.2 enforced; TLS startup failure is fatal (no silent fallback to unencrypted)
  - 4 TLS integration tests with self-signed certificate helper using `crypto/x509`
- **Cross-platform E2E testing scripts**: Comprehensive test suite in `scripts/` ([`ece5ae8`](https://github.com/alxayo/rtmp-go/commit/ece5ae8))
  - 12 scripts (6 Bash + 6 PowerShell pairs) for Linux/macOS/Windows
  - 7 E2E test cases: RTMP publish/capture, RTMPS publish/capture, HLS via hooks (plain + TLS), auth allowed/rejected, RTMPS + auth
  - Helper scripts: dependency checker, TLS cert generator, parameterized server launcher, HLS hook
- **Cross-platform build scripts**: `scripts/build.sh` and `scripts/build.ps1` for local binary compilation ([`8266217`](https://github.com/alxayo/rtmp-go/commit/8266217))
- **Hugo documentation site**: Full documentation site with GitHub Pages deployment ([`8f5840d`](https://github.com/alxayo/rtmp-go/commit/8f5840d))
  - Quick Start, User Guide, CLI Reference, How-To Guides, Developer docs, Project roadmap/changelog
  - Hugo-book theme with weight-based navigation
  - GitHub Actions workflow for automatic deployment on push to main
  - Local development scripts (`serve-docs.sh`, `serve-docs.ps1`)
- **Documentation site updates**: RTMPS guide, E2E testing guide, CLI reference TLS flags, architecture diagram updates ([`ad40537`](https://github.com/alxayo/rtmp-go/commit/ad40537))

### Fixed
- **Shell hook Windows compatibility**: `NewShellHook()` detects `runtime.GOOS == "windows"` and uses `powershell.exe -ExecutionPolicy Bypass -File` instead of hardcoded `/bin/bash` ([`ece5ae8`](https://github.com/alxayo/rtmp-go/commit/ece5ae8))
- **Docs workflow Hugo version**: Bumped to Hugo 0.158.0+ for hugo-book theme compatibility ([`5490da4`](https://github.com/alxayo/rtmp-go/commit/5490da4))
- **Docs workflow Pages config**: Fixed GitHub Pages setup step and baseURL for deployment ([`bb2242d`](https://github.com/alxayo/rtmp-go/commit/bb2242d), [`74647c9`](https://github.com/alxayo/rtmp-go/commit/74647c9))

---

## [v0.1.2] — 2026-03-04

### Added
- **Expvar metrics**: Live server counters via `expvar` (HTTP `/debug/vars` endpoint) tracking connections, publishers, subscribers, streams, audio/video messages, bytes ingested, relay stats, uptime, and Go version ([`671f2a6`](https://github.com/alxayo/rtmp-go/commit/671f2a6))
- **`-metrics-addr` CLI flag**: Optional HTTP address (e.g. `:8080`) to expose the metrics endpoint; disabled by default ([`7f446c5`](https://github.com/alxayo/rtmp-go/commit/7f446c5))
- **Disconnect handlers**: Each connection fires a cleanup callback when the read loop exits, ensuring publisher/subscriber registrations are removed and relay clients are closed ([`524281f`](https://github.com/alxayo/rtmp-go/commit/524281f))
- **TCP deadline enforcement**: Read deadline (90s) and write deadline (30s) detect zombie connections and prevent resource leaks ([`524281f`](https://github.com/alxayo/rtmp-go/commit/524281f))
- **Lifecycle hook events**: `EventConnectionClose`, `EventPublishStop`, `EventPlayStop`, and `EventSubscriberCount` fire on disconnect with session metadata (duration, packet counts, codecs) ([`2ed5fd2`](https://github.com/alxayo/rtmp-go/commit/2ed5fd2))
- **Performance benchmarks**: Chunk header parsing, AMF0 number/string/object encode/decode, and strict array benchmarks ([`34058ee`](https://github.com/alxayo/rtmp-go/commit/34058ee))
- **Metrics integration test**: End-to-end test validating `/debug/vars` HTTP endpoint serves all `rtmp_*` keys ([`a22a35d`](https://github.com/alxayo/rtmp-go/commit/a22a35d))
- **Edge case tests**: Chunk writer boundary tests (chunkSize ±1) and publish handler nil-argument tests ([`7767161`](https://github.com/alxayo/rtmp-go/commit/7767161))
- **Registry tests**: Codec caching, subscriber removal, `BroadcastMessage` relay, and sequence header caching tests ([`fc4d3c7`](https://github.com/alxayo/rtmp-go/commit/fc4d3c7))
- **Spec 005 & 006**: Error handling/benchmarks and expvar metrics feature specifications ([`6274f77`](https://github.com/alxayo/rtmp-go/commit/6274f77), [`fa23693`](https://github.com/alxayo/rtmp-go/commit/fa23693))

### Fixed
- **Relay client leak**: Relay client connections are now properly closed when publisher disconnects ([`69365fe`](https://github.com/alxayo/rtmp-go/commit/69365fe))
- **Server shutdown deadlock**: Server no longer hangs during shutdown when connections are active; force exit after timeout ([`92415d0`](https://github.com/alxayo/rtmp-go/commit/92415d0), [`69365fe`](https://github.com/alxayo/rtmp-go/commit/69365fe))

### Changed
- **AMF0 decoding optimization**: Eliminated `io.MultiReader` allocations in nested value decoding by inlining payload reads in `decodeValueWithMarker`; new internal helpers `decodeObjectPayload`, `decodeStrictArrayPayload`, `decodeStringPayload` ([`a2367fa`](https://github.com/alxayo/rtmp-go/commit/a2367fa))
- **Chunk writer optimization**: Added reusable scratch buffer to `Writer` struct, eliminating per-chunk `make()` allocation in `writeChunk` ([`215aa96`](https://github.com/alxayo/rtmp-go/commit/215aa96))
- **RPC lazy-init**: `ConnectCommand.Extra` map only allocated when extra fields are present ([`b72b83a`](https://github.com/alxayo/rtmp-go/commit/b72b83a))
- **Hook manager optimization**: `TriggerEvent` pre-allocates hook slice capacity for stdio hook ([`b808da2`](https://github.com/alxayo/rtmp-go/commit/b808da2))
- **Simplified `attachCommandHandling`**: Replaced variadic `srv ...*Server` parameter with direct `*Server`, removing 7 redundant nil-checks ([`919e2a9`](https://github.com/alxayo/rtmp-go/commit/919e2a9))
- **AMF golden helpers standardized**: All golden file test helpers use `t.Helper()` + `t.Fatalf()` consistently; removed duplicate `goldenDir` constants ([`29e31f8`](https://github.com/alxayo/rtmp-go/commit/29e31f8))
- **Server test doubles consolidated**: Moved shared stubs (`stubConn`, `capturingConn`, `stubPublisher`) into `helpers_test.go` ([`d3f722f`](https://github.com/alxayo/rtmp-go/commit/d3f722f))
- **Media test helper consolidated**: Removed duplicate `_tVidFatalf` — reuses `_tFatalf` from audio_test.go; added `t.Run` subtests to error case loops ([`cf89878`](https://github.com/alxayo/rtmp-go/commit/cf89878))
- **AMF subtests**: Added `t.Run` with named subtests to `TestNumber_EdgeCases_RoundTrip` and `TestDecodeValue_UnsupportedMarkers` ([`e791988`](https://github.com/alxayo/rtmp-go/commit/e791988))

### Removed
- **Dead `bufpool` package**: `internal/bufpool/` was implemented but never imported; removed 263 lines of dead code ([`e4b37aa`](https://github.com/alxayo/rtmp-go/commit/e4b37aa))
- **Dead `ErrForbidden` sentinel**: Auth sentinel error declared but never returned by any code path ([`8eaa72e`](https://github.com/alxayo/rtmp-go/commit/8eaa72e))
- **Dead `Session` type**: Unused `Session` and `SessionState` types removed from `conn` package ([`524281f`](https://github.com/alxayo/rtmp-go/commit/524281f))
- **Dead `RunCLI` function**: Speculative future code removed from `client` package ([`919e2a9`](https://github.com/alxayo/rtmp-go/commit/919e2a9))
- **Dead `Marshal`/`Unmarshal` wrappers**: Test-only exported functions removed from `amf` package ([`919e2a9`](https://github.com/alxayo/rtmp-go/commit/919e2a9))

---

## [v0.1.1] — 2026-03-03

### Added
- **Token-based authentication** ([PR #4](https://github.com/alxayo/rtmp-go/pull/4)): Pluggable `auth.Validator` interface with four backends:
  - `TokenValidator`: In-memory map of streamKey → token pairs (CLI flag `-auth-token`)
  - `FileValidator`: JSON token file with live reload via SIGHUP (`-auth-file`)
  - `CallbackValidator`: External HTTP webhook for auth decisions (`-auth-callback`)
  - `AllowAllValidator`: Default mode, accepts all requests (`-auth-mode=none`)
- Authentication CLI flags: `-auth-mode`, `-auth-token`, `-auth-file`, `-auth-callback`, `-auth-callback-timeout` ([`f32a74a`](https://github.com/alxayo/rtmp-go/commit/f32a74a))
- URL query parameter parsing for stream names: clients pass tokens via `streamName?token=secret` ([`f32a74a`](https://github.com/alxayo/rtmp-go/commit/f32a74a))
- `EventAuthFailed` hook event when authentication is rejected ([`f32a74a`](https://github.com/alxayo/rtmp-go/commit/f32a74a))
- Auth spec document in `specs/004-token-auth/` ([`7c1fa0f`](https://github.com/alxayo/rtmp-go/commit/7c1fa0f))
- Definition of Done checklist (`docs/definition-of-done.md`) and post-feature review prompt ([`6b3e096`](https://github.com/alxayo/rtmp-go/commit/6b3e096))

### Changed
- Query parameters are stripped from stream keys before registry operations (e.g., `live/stream?token=x` → `live/stream`) ([`f32a74a`](https://github.com/alxayo/rtmp-go/commit/f32a74a))

### Fixed
- Escaped quotes in Markdown code blocks across documentation ([`bef626b`](https://github.com/alxayo/rtmp-go/commit/bef626b))
- Broken link in copilot-instructions.md ([`f92d34d`](https://github.com/alxayo/rtmp-go/commit/f92d34d))

---

## [v0.1.0] — 2025-10-18

First feature-complete release of the RTMP server. Supports end-to-end streaming from OBS/FFmpeg to subscribers with recording and relay capabilities.

### Added

#### Core RTMP Protocol
- **RTMP v3 handshake**: C0/C1/C2 ↔ S0/S1/S2 exchange with 5-second timeouts and domain-specific error types
- **Chunk streaming**: FMT 0–3 header compression, extended timestamps (≥0xFFFFFF), chunk size negotiation
- **Control messages**: Set Chunk Size, Window Acknowledgement Size, Set Peer Bandwidth, User Control (types 1–6)
- **AMF0 codec**: Number, Boolean, String, Null, Object, and Strict Array encode/decode with golden binary vector tests

#### Command Flow
- **Command dispatcher**: Routes `connect`, `createStream`, `publish`, and `play` commands
- **Connect**: Parses application name, responds with `_result` (NetConnection.Connect.Success)
- **CreateStream**: Allocates stream IDs, responds with `_result`
- **Publish/Play**: Registers publishers and subscribers in stream registry with `onStatus` responses

#### Media & Recording
- **Live relay**: Transparent forwarding from publishers to all subscribers
- **Sequence header caching**: H.264 SPS/PPS and AAC AudioSpecificConfig cached for late-joining subscribers
- **Codec detection**: Identifies audio (AAC, MP3, Speex) and video (H.264, H.265) from first media packets
- **FLV recording**: Automatic recording of all streams to FLV files (`-record-all`, `-record-dir` flags)
- **Media logging**: Per-connection bitrate stats and codec identification

#### Multi-Destination Relay
- **Relay forwarding**: Forward publisher streams to external RTMP servers (`-relay-to` flag)
- **Destination manager**: Connect, monitor, and send media to multiple downstream targets
- **Metrics tracking**: Per-destination message counts, bytes sent, and error tracking

#### Event Hooks
- **Webhook hook**: HTTP POST with JSON event payload to configured URLs
- **Shell hook**: Execute scripts with event data as environment variables
- **Stdio hook**: Print structured event data to stderr (JSON or env-var format)
- **Hook manager**: Bounded concurrency pool (default 10 workers) with configurable timeout

#### Server Infrastructure
- **TCP listener**: Accept loop with graceful shutdown support
- **Connection lifecycle**: Handshake → control burst → command exchange → media streaming
- **Stream registry**: Thread-safe map of stream keys to publisher/subscriber lists
- **Structured logging**: `log/slog` with configurable levels (debug/info/warn/error)
- **Domain errors**: Typed error wrappers (`HandshakeError`, `ChunkError`, `AMFError`, `ProtocolError`, `TimeoutError`)

#### Testing & Tooling
- **Golden binary vectors**: Exact wire-format `.bin` files for handshake, chunk headers, AMF0, and control messages
- **Integration tests**: Full publish → subscribe round-trip tests
- **RTMP test client**: Minimal client for driving integration tests (`internal/rtmp/client`)
- **CI workflow**: Automated testing with `go build`, `go vet`, `gofmt`, and `go test`
- **Stream analysis tools**: H.264 frame analyzer, RTMP stream extractor, HLS converter

#### CLI
- `-listen` — TCP address (default `:1935`)
- `-log-level` — debug/info/warn/error (default `info`)
- `-record-all` — Enable automatic FLV recording
- `-record-dir` — Recording directory (default `recordings`)
- `-chunk-size` — Outbound chunk size, 1–65536 (default 4096)
- `-relay-to` — RTMP relay destination URL (repeatable)
- `-hook-script` — Shell hook: `event_type=/path/to/script` (repeatable)
- `-hook-webhook` — Webhook: `event_type=https://url` (repeatable)
- `-hook-stdio-format` — Stdio output format: `json` or `env`
- `-hook-timeout` — Hook execution timeout (default 30s)
- `-hook-concurrency` — Max concurrent hook executions (default 10)
- `-version` — Print version and exit

---

## Pull Requests

| PR | Title | Branch | Status |
|----|-------|--------|--------|
| [#6](https://github.com/alxayo/rtmp-go/pull/6) | Expvar metrics, perf optimizations, dead code removal | `feature/006-expvar-metrics` | Merged |
| [#4](https://github.com/alxayo/rtmp-go/pull/4) | Token-based authentication | `feature/004-token-auth` | Merged |
| [#3](https://github.com/alxayo/rtmp-go/pull/3) | Set initial semantic version to v0.1.0 | `copilot/determine-semantic-version` | Merged |
| [#2](https://github.com/alxayo/rtmp-go/pull/2) | Fix server connection tracking tests | `copilot/fix-github-actions-workflow-again` | Merged |
| [#1](https://github.com/alxayo/rtmp-go/pull/1) | Fix gofmt formatting violations failing CI | `copilot/fix-github-actions-workflow` | Merged |

---

## Feature Branches

| Branch | Spec | Description |
|--------|------|-------------|
| `feature/006-expvar-metrics` | [specs/006](specs/006-expvar-metrics/spec.md) | Expvar metrics, performance optimizations, dead code removal |
| `feature/005-error-handling-benchmarks` | [specs/005](specs/005-error-handling-benchmarks/spec.md) | Error handling, connection cleanup, TCP deadlines, performance benchmarks |
| `feature/004-token-auth` | [specs/004](specs/004-token-auth/spec.md) | Token-based stream key authentication with 4 validator backends |
| `003-multi-destination-relay` | [specs/003](specs/003-multi-destination-relay/) | Multi-destination relay to external RTMP servers |
| `T001-init-go-module` | [specs/001](specs/001-rtmp-server-implementation/spec.md) | Core RTMP server implementation (handshake through media streaming) |

[v0.1.4]: https://github.com/alxayo/rtmp-go/compare/v0.1.3...v0.1.4
[v0.1.3]: https://github.com/alxayo/rtmp-go/compare/v0.1.2...v0.1.3
[v0.1.2]: https://github.com/alxayo/rtmp-go/compare/v0.1.1...v0.1.2
[v0.1.1]: https://github.com/alxayo/rtmp-go/compare/v0.1.0...v0.1.1
[v0.1.0]: https://github.com/alxayo/rtmp-go/releases/tag/v0.1.0

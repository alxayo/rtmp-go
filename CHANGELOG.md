# Changelog

All notable changes to go-rtmp are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

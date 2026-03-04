# Changelog

All notable changes to go-rtmp are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased] — feature/005-error-handling-benchmarks

### Added
- **Disconnect handlers**: Each connection fires a cleanup callback when the read loop exits, ensuring publisher/subscriber registrations are removed and relay clients are closed ([`524281f`](https://github.com/alxayo/rtmp-go/commit/524281f))
- **TCP deadline enforcement**: Read deadline (90s) and write deadline (30s) detect zombie connections and prevent resource leaks ([`524281f`](https://github.com/alxayo/rtmp-go/commit/524281f))
- **Lifecycle hook events**: `EventConnectionClose`, `EventPublishStop`, `EventPlayStop`, and `EventSubscriberCount` fire on disconnect with session metadata (duration, packet counts, codecs) ([`2ed5fd2`](https://github.com/alxayo/rtmp-go/commit/2ed5fd2))
- **Performance benchmarks**: Chunk header parsing, AMF0 number/string/object encode/decode, and strict array benchmarks ([`34058ee`](https://github.com/alxayo/rtmp-go/commit/34058ee))
- **Registry tests**: Codec caching, subscriber removal, `BroadcastMessage` relay, and sequence header caching tests ([`fc4d3c7`](https://github.com/alxayo/rtmp-go/commit/fc4d3c7))
- **Spec 005**: Error handling, connection cleanup & performance benchmarks specification ([`6274f77`](https://github.com/alxayo/rtmp-go/commit/6274f77))

### Fixed
- **Relay client leak**: Relay client connections are now properly closed when publisher disconnects ([`69365fe`](https://github.com/alxayo/rtmp-go/commit/69365fe))
- **Server shutdown deadlock**: Server no longer hangs during shutdown when connections are active; force exit after timeout ([`92415d0`](https://github.com/alxayo/rtmp-go/commit/92415d0), [`69365fe`](https://github.com/alxayo/rtmp-go/commit/69365fe))

### Changed
- **Simplified `attachCommandHandling`**: Replaced variadic `srv ...*Server` parameter with direct `*Server`, removing 7 redundant nil-checks ([`919e2a9`](https://github.com/alxayo/rtmp-go/commit/919e2a9))

### Removed
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
| [#4](https://github.com/alxayo/rtmp-go/pull/4) | Token-based authentication | `feature/004-token-auth` | Merged |
| [#3](https://github.com/alxayo/rtmp-go/pull/3) | Set initial semantic version to v0.1.0 | `copilot/determine-semantic-version` | Merged |
| [#2](https://github.com/alxayo/rtmp-go/pull/2) | Fix server connection tracking tests | `copilot/fix-github-actions-workflow-again` | Merged |
| [#1](https://github.com/alxayo/rtmp-go/pull/1) | Fix gofmt formatting violations failing CI | `copilot/fix-github-actions-workflow` | Merged |

---

## Feature Branches

| Branch | Spec | Description |
|--------|------|-------------|
| `feature/005-error-handling-benchmarks` | [specs/005](specs/005-error-handling-benchmarks/spec.md) | Error handling, connection cleanup, TCP deadlines, performance benchmarks |
| `feature/004-token-auth` | [specs/004](specs/004-token-auth/spec.md) | Token-based stream key authentication with 4 validator backends |
| `003-multi-destination-relay` | [specs/003](specs/003-multi-destination-relay/) | Multi-destination relay to external RTMP servers |
| `T001-init-go-module` | [specs/001](specs/001-rtmp-server-implementation/spec.md) | Core RTMP server implementation (handshake through media streaming) |

[Unreleased]: https://github.com/alxayo/rtmp-go/compare/v0.1.1...feature/005-error-handling-benchmarks
[v0.1.1]: https://github.com/alxayo/rtmp-go/compare/v0.1.0...v0.1.1
[v0.1.0]: https://github.com/alxayo/rtmp-go/releases/tag/v0.1.0

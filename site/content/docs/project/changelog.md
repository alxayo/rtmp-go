---
title: "Changelog"
weight: 1
---

# Changelog

All notable changes to go-rtmp are documented here.

---

## v0.1.2 (2026-03-04)

### Added
- **Expvar metrics** — live counters exposed via HTTP `/debug/vars` endpoint (`-metrics-addr` flag)
- **Disconnect handlers** — proper cleanup of publisher/subscriber registrations on connection close
- **TCP deadline enforcement** — read 90s / write 30s deadlines for zombie detection, reset on each I/O operation
- **Lifecycle hook events** — new events: `connection_close`, `publish_stop`, `play_stop`, `subscriber_count`

### Improved
- **Performance optimizations**:
  - AMF0 decode: reduced allocations in object/array parsing
  - Chunk writer: buffer reuse to avoid repeated allocation
  - RPC: lazy initialization of command dispatcher
- **Dead code removal**: removed unused `bufpool` package, `ErrForbidden` sentinel, and unused `Session` type

---

## v0.1.1 (2026-03-03)

### Added
- **Token-based authentication** with 4 backends:
  - `token` — static stream key/token pairs via CLI flags
  - `file` — JSON file with stream key/token mappings
  - `callback` — webhook URL for external auth validation
  - `none` — open access (default)
- **URL query parameter parsing** — tokens extracted from `?token=value` in stream URLs
- **`EventAuthFailed`** hook event — fires when authentication fails

---

## v0.1.0 (2025-10-18)

### Added
First feature-complete release of go-rtmp.

- **Full RTMP v3 protocol implementation**:
  - Handshake (C0/C1/C2 ↔ S0/S1/S2) with FSM and timeout enforcement
  - Chunk streaming with FMT 0-3, extended timestamps, configurable chunk size
  - AMF0 codec (Number, Boolean, String, Object, Null, Strict Array)
  - Command parsing (connect, createStream, releaseStream, FCPublish, publish, play)
  - Control messages (Set Chunk Size, Abort, Acknowledgement, Window Ack Size, Set Peer Bandwidth)
- **Live relay** — transparent pub/sub forwarding to unlimited subscribers
- **Late-join support** — H.264 SPS/PPS and AAC AudioSpecificConfig caching
- **FLV recording** — automatic recording with timestamped filenames
- **Multi-destination relay** — forward streams to external RTMP servers (`-relay-to`)
- **Event hooks**:
  - Webhook — HTTP POST to configurable URLs
  - Shell — execute scripts on lifecycle events
  - Stdio — structured output (JSON or env format) to stdout
- **Structured logging** — `log/slog` with configurable level and context fields
- **Integration tests** — end-to-end pub/sub validation through the full protocol stack
- **Golden binary vectors** — exact wire-format test data for handshake, chunks, AMF0, and control messages

# go-rtmp Optimization & Documentation Plan

**Date:** March 2, 2026  
**Scope:** Code cleanup, refactoring, documentation overhaul  
**Goal:** Make the codebase clean, simple, and beginner-friendly while maintaining production quality

---

## Codebase Snapshot

| Metric | Value |
|--------|-------|
| Go source files | 100 |
| Test files | 48 |
| Total Go lines | ~8,500 |
| Markdown docs | 75 files |
| Docs lines (docs/ only) | 7,678 |
| External dependencies | 0 (stdlib only) |
| Overall quality | 8.5/10 |

---

## Phase 1: Code Cleanup (Remove Dead Code & Debug Artifacts)

### 1.1 — Remove Debug Printf Statements from Client

**File:** `internal/rtmp/client/client.go` (383 lines)

There are **12 `fmt.Printf("DEBUG: ...")`** calls scattered through the client code. These bypass structured logging, pollute stdout, and are unfit for production.

**Action:**
- Add a `log *slog.Logger` field to the `Client` struct
- Initialize it in the constructor: `logger.Logger().With("component", "rtmp_client")`
- Replace all 12 `fmt.Printf("DEBUG: ...")` calls with `c.log.Debug(...)` calls
- Remove `"fmt"` import if no longer needed

### 1.2 — Remove Dead Code in conn.go

**File:** `internal/rtmp/conn/conn.go` (222 lines)

**Action:**
- Audit for any unreachable functions or unused helpers (e.g., `ioEOF` wrapper if present)
- Extract magic numbers into named constants:
  - `200 * time.Millisecond` → `const sendTimeout = 200 * time.Millisecond`
  - `100` (queue capacity) → `const outboundQueueSize = 100`

### 1.3 — Remove Obsolete Copilot Instructions Doc

**File:** `docs/rtmp_copilot_instructions.md` (295 lines)

This is fully superseded by `.github/copilot-instructions.md`. Delete or archive.

---

## Phase 2: Code Deduplication

### 2.1 — Extract Shared Basic Header Parsing

**Files:** `internal/rtmp/chunk/header.go` + `internal/rtmp/chunk/reader.go`

The basic header parsing (1-3 byte RTMP chunk basic header) is implemented independently in both files with ~25 lines of near-identical code.

**Action:**
- Create a shared unexported function `parseBasicHeader(r io.Reader) (fmt uint8, csid uint32, consumed int, err error)`
- Place it in `header.go` (or a new `parse.go` if cleaner)
- Update both `ParseChunkHeader()` and `Reader.ReadMessage()` to call it
- Verify with existing golden vector tests

### 2.2 — Extract Extended Timestamp Helper

**File:** `internal/rtmp/chunk/header.go` (176 lines)

The extended timestamp reading logic is copy-pasted 4 times (once per FMT type: 0, 1, 2, 3).

**Action:**
- Extract into a method: `(*ChunkHeader).readExtendedTimestamp(r io.Reader, rawTimestamp uint32) error`
- Replace all 4 instances
- This should reduce `header.go` by ~40 lines

### 2.3 — Extract FMT-Specific Parsers

**File:** `internal/rtmp/chunk/header.go`

`ParseChunkHeader` is a 150+ line function with a large switch statement. Each FMT case is 25-35 lines.

**Action:**
- Extract into private methods: `parseFMT0()`, `parseFMT1()`, `parseFMT2()`, `parseFMT3()`
- `ParseChunkHeader` becomes a ~15-line dispatcher
- Makes each FMT variant independently testable

### 2.4 — Extract Common Command Response Pattern

**File:** `internal/rtmp/server/command_integration.go` (223 lines)

The "build response → log preview → send message" pattern repeats 3 times for connect, createStream, and publish handlers.

**Action:**
- Extract `sendCmdResponse(conn, msg, cmdName, log) error` helper
- Reduces ~15 lines of duplication per handler

---

## Phase 3: Structural Refactoring

### 3.1 — Split command_integration.go

**File:** `internal/rtmp/server/command_integration.go` (223 lines)

This file mixes 4 concerns: command handling, media dispatch, recording lifecycle, and relay integration.

**Action:** Split into focused files:
1. `command_handlers.go` — RPC dispatcher wiring and command handler callbacks
2. `media_dispatch.go` — Message routing (media relay, broadcast to subscribers)
3. Keep `initRecorder()` and `cleanupRecorder()` in existing file or move to `recording.go`

### 3.2 — Add CSID/TypeID Constants to Client

**File:** `internal/rtmp/client/client.go`

Hardcoded magic numbers for chunk stream IDs (3, 6, 7) and type IDs (8, 9, 20).

**Action:**
- Define constants: `commandCSID = 3`, `audioCSID = 6`, `videoCSID = 7`
- Replace all raw integer literals
- Reference the RTMP spec in comments

### 3.3 — Consolidate Wait-For-Response Logic in Client

**File:** `internal/rtmp/client/client.go`

`waitForConnectResponse()` and `waitForCreateStreamResponse()` have nearly identical loop structures (read message → decode AMF → check command name).

**Action:**
- Extract shared `waitForCommandResponse(expectedCmd string) ([]interface{}, error)`  
- Each specific waiter becomes a thin wrapper that processes the decoded values

---

## Phase 4: Documentation Overhaul

### 4.1 — Add Package-Level Documentation

Every Go package should have a `doc.go` file with a package comment explaining what the package does, its key types, and how it fits into the architecture. This is the single most impactful change for beginner readability.

**Files to create (10 files):**

| Package | File | Description |
|---------|------|-------------|
| `internal/rtmp/handshake` | `doc.go` | RTMP v3 handshake FSM — what C0/C1/C2/S0/S1/S2 are, the state machine, timeouts |
| `internal/rtmp/chunk` | `doc.go` | Chunk layer — message fragmentation/reassembly, FMT types 0-3, extended timestamps |
| `internal/rtmp/amf` | `doc.go` | AMF0 codec — supported types, encoding/decoding, wire format |
| `internal/rtmp/control` | `doc.go` | Control messages — types 1-6, Set Chunk Size, Window Ack, Ping |
| `internal/rtmp/rpc` | `doc.go` | Command layer — connect, createStream, publish, play, deleteStream |
| `internal/rtmp/conn` | `doc.go` | Connection lifecycle — accept, read/write loops, session state |
| `internal/rtmp/server` | `doc.go` | Server — listener, stream registry, pub/sub coordination |
| `internal/rtmp/media` | `doc.go` | Media handling — audio/video parsing, codec detection, FLV recording, relay |
| `internal/rtmp/relay` | `doc.go` | Multi-destination relay — forwarding to external RTMP servers |
| `internal/rtmp/client` | `doc.go` | Minimal RTMP client for testing and integration validation |

Each `doc.go` should follow this template:
```go
// Package handshake implements the RTMP v3 simple handshake protocol.
//
// The RTMP handshake consists of three phases for each side (client/server):
//   - C0/S0: Version byte (0x03 for RTMP v3)
//   - C1/S1: 1536-byte random data with 4-byte timestamp
//   - C2/S2: 1536-byte echo of the peer's C1/S1 data
//
// Data Flow:
//
//   Client              Server
//   ──────              ──────
//   C0+C1  ──────────►
//          ◄──────────  S0+S1+S2
//   C2     ──────────►
//
// The handshake uses a state machine (see State type) with 5s timeouts
// per phase. After completion, the connection transitions to chunk-based
// communication.
//
// Usage:
//
//   // Server side
//   err := handshake.ServerHandshake(conn)
//
//   // Client side
//   err := handshake.ClientHandshake(conn)
package handshake
```

### 4.2 — Create Architecture Guide for Beginners

**File to create:** `docs/architecture.md`

A single document that explains the entire system from top to bottom:

1. **What is RTMP?** (3 paragraphs — protocol purpose, where it's used, what this project does)
2. **High-Level Architecture Diagram** (ASCII art showing the layer stack)
3. **Data Flow Walkthrough** (step-by-step: TCP connect → handshake → commands → media)
4. **Package Map** (table mapping each package to its responsibility)
5. **Key Concepts** (chunk vs message, CSID/MSID, AMF0, stream keys)
6. **Reading Order** (numbered list of files to read for full understanding)

### 4.3 — Consolidate Overlapping Documentation

The `docs/` directory has **7,678 lines across 37 files**, with significant overlap and redundancy.

**Consolidation plan:**

| Action | Source Files | Target | Lines Saved |
|--------|-------------|--------|-------------|
| **Merge** | `MEDIA_LOGGING_IMPLEMENTATION_SUMMARY.md` (240), `MEDIA_LOGGING_QUICKREF.md` (129), `media_packet_logging.md` (223), `testing_media_logging.md` (201) | `docs/features/media-logging.md` | ~400 |
| **Merge** | `RECORDING_IMPLEMENTATION.md` (242), `RECORDING_IMPLEMENTATION_SUMMARY.md` (264), `RECORDING_QUICKREF.md` (77) | `docs/features/recording.md` | ~300 |
| **Merge** | `rtmp_protocol_end_to_end_session_message_types_streams_technical_breakdown.md` (147), `rtmp_protocol_end_to_end_session_packet_structure_code_examples.md` (140), `rtmp_protocol_message_types_sessions_streams_technical_reference.md` (58) | `docs/protocol/session-and-messages.md` | ~200 |
| **Merge** | `rtmp_implementation_plan_task breakdown.md` (243), `rtmp_protocol_implementation_task_breakdown.md` (243) | Keep one, delete other | ~240 |
| **Archive** | All 14 files in `docs/fixes/` (total ~2,891 lines) | `docs/archived/fixes/` | 0 (moved) |
| **Archive** | `specs/002-rtmp-relay-feature/` (4 files), `specs/003-multi-destination-relay/` (2 files) | `docs/archived/specs/` | 0 (moved) |
| **Delete** | `docs/rtmp_copilot_instructions.md` (295 lines) | N/A (superseded by `.github/copilot-instructions.md`) | 295 |

**Net effect:** Reduce `docs/` from **37 files / 7,678 lines** to **~18 active files / ~4,000 lines** + archived folder.

### 4.4 — Reorganize docs/ Directory

**Current structure (flat, 37 files):**
```
docs/
├── 000-constitution.md
├── 000-rtmp_protocol_varians.md
├── 001-rtmp_protocol_implementation_guide.md
├── go.instructions.md
├── MEDIA_LOGGING_*.md (4 files)
├── RECORDING_*.md (3 files)
├── RTMP_*.md (6 files)
├── rtmp_*.md (5 files)
├── testing_media_logging.md
├── wireshark_rtmp_capture_guide.md
├── features/ (2 files)
└── fixes/ (14 files)
```

**Proposed structure (organized by topic):**
```
docs/
├── README.md              # Documentation index — start here
├── architecture.md        # NEW: System overview for beginners
├── 000-constitution.md    # Design principles (keep)
├── go.instructions.md     # Go conventions (keep)
│
├── protocol/              # RTMP protocol reference
│   ├── overview.md        # MERGED from RTMP_overview.md + rtmp_data_exchange.md
│   ├── handshake.md       # MOVED from RTMP_basic_handshake_deep_dive.md + handshake step-by-step
│   ├── chunks-and-messages.md  # MOVED from rtmp_audio_video_messages_chunking.md
│   ├── implementation-guide.md # KEPT from 001-rtmp_protocol_implementation_guide.md
│   └── wireshark-guide.md      # MOVED from wireshark_rtmp_capture_guide.md
│
├── features/              # Feature-specific docs
│   ├── recording.md       # MERGED (3 → 1)
│   ├── media-logging.md   # MERGED (4 → 1)
│   └── relay.md           # MOVED from feature002-rtmp-relay.md
│
└── archived/              # Historical fix notes and completed specs
    ├── README.md          # Explains what's here and why
    ├── fixes/             # MOVED from docs/fixes/
    └── specs/             # MOVED completed spec folders
```

### 4.5 — Improve README.md

The current README is good but can be enhanced:

- Add a "For Beginners" section pointing to `docs/architecture.md`
- Add a "Documentation Map" table showing where to find what
- Clean up any duplicated feature lists

### 4.6 — Add Inline Code Comments

For each core source file, add brief comments explaining non-obvious logic. Focus on:

- **Why** decisions were made (not **what** the code does)
- Protocol-specific constants and their meaning
- Concurrency patterns (why a mutex is used, what it protects)
- Wire format layouts (e.g., byte offsets in headers)

Target files (in priority order):
1. `chunk/reader.go` — The reassembly loop is the hardest code to follow
2. `chunk/writer.go` — FMT selection logic needs explanation
3. `conn/conn.go` — Read/write loop coordination
4. `server/registry.go` — Pub/sub with locking
5. `media/relay.go` — Non-blocking send pattern

---

## Phase 5: Test & Validation

### 5.1 — Run Full Test Suite After Each Phase

```bash
go test -race ./...
```

Every refactoring phase must pass all existing tests before proceeding.

### 5.2 — Verify No API Changes

- No exported function signatures change
- No package import paths change  
- `cmd/rtmp-server` binary behavior remains identical

### 5.3 — Integration Validation

After all phases complete, run the full interop test:
```bash
go build -o rtmp-server.exe ./cmd/rtmp-server
./rtmp-server.exe -listen localhost:1935 -log-level debug
# In another terminal:
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test
ffplay rtmp://localhost:1935/live/test
```

---

## Execution Order

| Step | Phase | Task | Risk |
|------|-------|------|------|
| 1 | 1.1 | Remove debug printf from client.go | Low |
| 2 | 1.2 | Extract constants in conn.go, remove dead code | Low |
| 3 | 2.1 | Extract shared basic header parsing | Medium |
| 4 | 2.2 | Extract extended timestamp helper | Low |
| 5 | 2.3 | Extract FMT-specific parsers | Low |
| 6 | 2.4 | Extract command response helper | Low |
| 7 | **TEST** | `go test -race ./...` — full green | — |
| 8 | 3.1 | Split command_integration.go | Medium |
| 9 | 3.2 | Add constants to client.go | Low |
| 10 | 3.3 | Consolidate wait-for-response logic | Medium |
| 11 | **TEST** | `go test -race ./...` — full green | — |
| 12 | 4.1 | Create 10 package doc.go files | Low |
| 13 | 4.2 | Create architecture.md | Low |
| 14 | 4.3 | Consolidate overlapping docs | Low |
| 15 | 4.4 | Reorganize docs/ directory | Low |
| 16 | 4.5 | Improve README.md | Low |
| 17 | 4.6 | Add inline code comments | Low |
| 18 | 1.3 | Archive/delete obsolete docs | Low |
| 19 | 5.1-5.3 | Final validation | — |

---

## What This Plan Does NOT Do

- **No API changes** — all exported functions keep their signatures
- **No new features** — purely cleanup and documentation
- **No dependency additions** — remains stdlib-only
- **No architecture changes** — same package structure, same data flow
- **No performance optimization** — the code is already efficient; premature optimization adds complexity

---

## Expected Outcome

| Metric | Before | After |
|--------|--------|-------|
| Debug printf calls | 12 | 0 |
| Code duplication (chunks) | ~50 duplicated lines | 0 |
| `command_integration.go` size | 223 lines (4 concerns) | ~80 lines (1 concern) per file |
| Active doc files | 37 (flat, overlapping) | ~18 (organized, deduplicated) |
| Doc lines (active) | 7,678 | ~4,000 |
| Package doc.go files | 0 | 10 |
| Beginner architecture guide | none | 1 comprehensive guide |
| Magic numbers in client | ~6 | 0 (all named constants) |

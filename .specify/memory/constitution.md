<!--
Sync Impact Report (2025-10-01)
================================
Version Change: 0.0.0 → 1.0.0 (MAJOR - Initial constitution establishment)
Rationale: First formal constitution for go-rtmp project, establishing foundational principles.

Template Alignment Status:
✓ plan-template.md: Constitution Check section will validate against all 7 principles
✓ spec-template.md: Requirements must align with Spec Compliance and Testability principles
✓ tasks-template.md: Task structure supports Protocol-First, Test-First, and Modularity principles
✓ agent-file-template.md: No direct dependencies identified

Breaking Changes: None (initial version)
Migration Required: None (initial version)
-->

# Go-RTMP Protocol Implementation Constitution

## Core Principles

### I. Protocol-First Implementation (NON-NEGOTIABLE)
All implementation must strictly adhere to the RTMP specification as documented in Adobe's RTMP specification and observed behaviors with reference tools (FFmpeg, OBS, Wirecast).

**Rules:**
- Wire format fidelity is paramount: all handshake sequences, chunk headers, message types, and AMF encoding must match the spec byte-for-byte
- State machines (handshake, chunking, RPC flow) must be explicit and documented
- No assumptions or shortcuts that deviate from protocol semantics
- Extended timestamp handling (0xFFFFFF), MSID little-endian encoding, and CSID rules must be implemented correctly
- Support RTMP version 3 simple handshake as the baseline; complex handshake is optional

**Rationale:** RTMP interoperability with industry-standard tools is the primary success criterion. Protocol violations lead to connection failures, stream corruption, or subtle bugs that are difficult to diagnose.

### II. Idiomatic Go (NON-NEGOTIABLE)
Follow Effective Go, Go Code Review Comments, and Google's Go Style Guide rigorously. Write simple, clear, idiomatic Go code.

**Rules:**
- Use standard library wherever possible; minimize external dependencies
- Favor clarity and simplicity over cleverness
- Keep the happy path left-aligned (minimize indentation); return early
- Make the zero value useful
- Use mixedCaps naming (not underscores); avoid stuttering
- Document all exported types, functions, methods, and packages
- Use `gofmt` and `goimports` for consistent formatting
- Handle errors immediately; wrap with context using `fmt.Errorf` with `%w`
- Use channels for goroutine communication; share memory by communicating
- Interfaces should be small (1-3 methods); accept interfaces, return concrete types

**Rationale:** Go's simplicity and readability are core strengths. Idiomatic code is maintainable, performant, and accessible to the Go community.

### III. Modularity and Package Discipline
The codebase is organized into focused packages mirroring protocol responsibilities. Each package must be self-contained with clear boundaries.

**Rules:**
- Package structure: `rtmp/{client,server,conn}`, `handshake`, `chunk`, `control`, `amf`, `rpc`, `media`, `internal/{bufpool,clock,logger,errors}`
- Keep `main` packages in `cmd/` directory
- Use `internal/` for packages not intended for external import
- Avoid circular dependencies
- Each package has a single, well-defined responsibility
- Package names should be lowercase, single-word, and describe what the package provides

**Rationale:** Protocol layers are naturally modular. Clear package boundaries enable independent testing, reduce coupling, and facilitate parallel development.

### IV. Test-First with Golden Vectors (NON-NEGOTIABLE)
All protocol-level code must be validated against golden test vectors derived from the RTMP specification and real-world captures.

**Rules:**
- Write tests before implementation (TDD: Red-Green-Refactor)
- Golden test vectors for: handshake sequences, chunk header formats (FMT 0-3), control messages, AMF0 encoding/decoding, interleaved chunk streams
- Unit tests for each package; integration tests for handshake → command → media flows
- Fuzz testing for parsers (chunk headers, AMF, message boundaries)
- Interop tests with FFmpeg/OBS: publish and play scenarios
- All tests must pass before code is merged
- Test coverage goal: >80% for protocol-critical paths

**Rationale:** RTMP is a binary protocol with strict format requirements. Golden vectors ensure correctness and prevent regressions. Real-world interop tests validate the implementation works with industry tools.

### V. Concurrency Safety and Backpressure
Leverage Go's concurrency primitives correctly. Ensure safe, bounded, and observable concurrency.

**Rules:**
- One goroutine per connection (readLoop + writeLoop)
- Use context for cancellation and shutdown propagation
- Bounded channels for outbound queues; implement backpressure policies (drop, disconnect, or block with timeout)
- Protect shared state with `sync.Mutex` or `sync.RWMutex`; prefer channels over mutexes when possible
- Always know how a goroutine will exit; use `sync.WaitGroup` or channels to wait for completion
- No goroutine leaks: ensure cleanup on connection close or error
- Document concurrency invariants in comments

**Rationale:** RTMP servers handle many concurrent connections. Proper concurrency discipline prevents race conditions, deadlocks, and resource exhaustion.

### VI. Observability and Debuggability
The system must be observable and debuggable in production. Emit structured logs and metrics.

**Rules:**
- Structured logging at appropriate levels (debug for protocol details, info for lifecycle events, warn/error for issues)
- Log peer addresses, stream keys, connection IDs, and relevant protocol state
- Emit metrics: connection count, message rates, chunk sizes, errors, latency
- Support debug modes for dumping raw bytes (hex) and protocol traces
- Error messages must include context (connection ID, stream, message type)
- Use deadline and timeout patterns for network I/O; log timeout events

**Rationale:** Protocol debugging requires visibility into wire-level exchanges and state transitions. Structured logs and metrics enable troubleshooting and performance analysis.

### VII. Simplicity and Incrementalism
Start with the simplest implementation that satisfies the spec. Add complexity only when required.

**Rules:**
- Implement "simple" handshake first; complex handshake is optional
- Support AMF0 before AMF3 (AMF0 is sufficient for most use cases)
- Basic commands first: `connect`, `_result`, `createStream`, `publish`, `play`, `deleteStream`
- No transcoding or muxing/demuxing beyond transparent byte forwarding
- YAGNI: avoid speculative features or optimizations
- Refactor incrementally: small, focused changes with tests
- Document trade-offs and future extension points

**Rationale:** RTMP is complex; tackling everything at once leads to bugs and delays. Incremental delivery with clear milestones enables validation and course correction.

## Technical Standards

### Language and Tooling
- **Go Version**: 1.21 or later (leveraging latest standard library improvements)
- **Dependency Management**: Go modules (`go.mod`, `go.sum`); run `go mod tidy` regularly
- **Build**: `go build ./...` must succeed; no warnings
- **Linting**: Use `golangci-lint` with standard rules; no exceptions without justification
- **Formatting**: Enforce `gofmt` and `goimports` via pre-commit hooks or CI

### Performance Targets
- **Latency**: Handshake completion <50ms (local), <200ms (over WAN)
- **Throughput**: Support 1000+ concurrent connections on commodity hardware
- **Memory**: Bounded per-connection memory (<10MB per connection under normal load)
- **Chunk Size**: Default 128 bytes; support dynamic adjustment up to 65536 bytes

### Security and Reliability
- **Input Validation**: Validate all peer inputs (version bytes, chunk sizes, message lengths, AMF data)
- **Resource Limits**: Enforce connection timeouts, message size limits, and queue depths
- **Error Handling**: Graceful degradation; log errors and close connections cleanly
- **No Panics**: Recover from panics in goroutines; log stack traces
- **Memory Safety**: No buffer overruns; use `io.ReadFull` and bounds checks

## Development Workflow

### Implementation Phases
1. **Handshake**: Simple handshake (C0/C1/C2 ↔ S0/S1/S2) with tests
2. **Chunking**: Header parsing/serialization, dechunker (reader), chunker (writer) with golden tests
3. **Control Messages**: Set Chunk Size, Window Ack Size, Set Peer Bandwidth
4. **AMF0**: Encoder/decoder for Number, Boolean, String, Object, Null, Array
5. **RPC Commands**: `connect`, `_result`, `createStream`, `publish`, `play`, `deleteStream`
6. **Media Relay**: Transparent forwarding of Audio (type 8) and Video (type 9) messages
7. **Teardown**: `deleteStream`, `closeStream`, cleanup and connection close

### Quality Gates
- All unit tests pass
- Golden vector tests pass
- Integration tests with loopback and synthetic inputs pass
- Interop tests with FFmpeg (publish + play) pass
- Fuzzing runs without crashes (1M iterations minimum)
- Code review by at least one other contributor
- Documentation updated (package docs, README, technical notes)

### Review and Approval
- Pull requests must reference a spec or task breakdown
- All CI checks must pass (build, lint, test, coverage)
- Reviewers verify: protocol compliance, Go idioms, test coverage, documentation
- Breaking changes require constitution amendment (see Governance)

## Governance

This constitution supersedes all other development practices and guidelines. It is the single source of truth for project principles and technical standards.

### Amendment Process
1. **Proposal**: Document the proposed change, rationale, and impact in an issue
2. **Review**: Discuss with maintainers; identify affected templates and code
3. **Version Bump**: Apply semantic versioning (MAJOR for new/removed principles, MINOR for refinements, PATCH for clarifications)
4. **Sync**: Update dependent templates (`plan-template.md`, `spec-template.md`, `tasks-template.md`) and code
5. **Approval**: Maintainer consensus required
6. **Commit**: Update constitution with new version and last-amended date

### Compliance
- All pull requests and code reviews must verify adherence to this constitution
- Deviations require explicit justification and approval
- Use `docs/go.instructions.md` for detailed Go coding guidance
- Use `docs/rtmp_copilot_instructions.md` for protocol-specific implementation guidance

### Exceptions
Exceptions to principles require:
- Written justification (performance, interop, spec ambiguity)
- Approval from project maintainer
- Documentation in code comments and technical notes
- Plan for future remediation if exception is temporary

**Version**: 1.0.0 | **Ratified**: 2025-10-01 | **Last Amended**: 2025-10-01
---
title: "Contributing"
weight: 6
---

# Contributing

## Workflow

1. **Fork** the repository on GitHub
2. **Create a feature branch** from `main`:
   ```bash
   git checkout -b feature/NNN-description
   ```
   Where `NNN` is the issue number (if applicable).
3. **Make your changes** — write tests first, then implementation
4. **Verify** everything passes (see below)
5. **Submit a Pull Request** back to `main`

## Branch Naming

Use the pattern `feature/NNN-description`:

```
feature/042-rtmps-support
feature/099-configurable-backpressure
fix/117-amf0-null-decode
docs/update-troubleshooting
```

## Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat(relay): add reconnection with exponential backoff
fix(chunk): handle extended timestamp in FMT 3 continuation
test: add golden vectors for H.265 sequence headers
docs: update CLI reference with new auth flags
refactor(amf): simplify object end detection
```

Scopes match package names: `handshake`, `chunk`, `amf`, `control`, `rpc`, `conn`, `server`, `auth`, `hooks`, `media`, `relay`, `metrics`.

## Definition of Done

Before submitting a PR, verify every item:

- [ ] **Compiles**: `go build ./...`
- [ ] **Vet clean**: `go vet ./...` reports no issues
- [ ] **Formatted**: `gofmt -l .` prints nothing
- [ ] **Tests pass**: `go test ./internal/... -count=1`
- [ ] **Race-free**: `go test -race ./...`
- [ ] **No dead code**: no unused functions, types, or variables
- [ ] **Doc comments**: all exported types and functions have documentation comments
- [ ] **CLI flags documented**: any new flags are documented in the CLI reference

## Verification Commands

Run all checks in one command:

```bash
go build ./... && go vet ./... && gofmt -l . && go test ./internal/... -count=1
```

For a thorough check including the race detector:

```bash
go build ./... && go vet ./... && go test -race ./... -count=1
```

## How to Add a New Feature

### 1. Create the Package

Add a new package under `internal/rtmp/` following the existing pattern:

```
internal/rtmp/yourfeature/
├── yourfeature.go        # Implementation
└── yourfeature_test.go   # Tests
```

Each package should have a single clear responsibility. Depend only on packages below you in the stack — never import a sibling or parent package.

### 2. Write Tests First

Start with golden binary vectors if your feature touches the wire format:

1. Create `.bin` files in `tests/golden/` with exact expected byte sequences
2. Write table-driven tests that decode the golden files and verify output
3. Write round-trip tests: encode → decode → compare

For non-wire-format features, write tests using `net.Pipe()` for real connection testing.

### 3. Integrate

Wire your feature into the existing flow:

- **Commands**: Add handlers in `internal/rtmp/rpc/` and integrate via the command dispatcher
- **Media processing**: Add to the media dispatch path in `internal/rtmp/conn/`
- **Server-level features**: Add configuration to `internal/rtmp/server/config.go` and initialization to `server.go`
- **CLI flags**: Add flag parsing in `cmd/rtmp-server/main.go`

### 4. Add Hook Events (If Applicable)

If your feature has lifecycle events that external systems should know about:

1. Define the event type in `internal/rtmp/server/hooks/`
2. Fire the event at the appropriate point in your code
3. Document the event payload

### 5. Document

- Add or update documentation in `site/content/docs/`
- Update the CLI reference if you added flags
- Add a changelog entry

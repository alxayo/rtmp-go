---
title: "Developer Guide"
weight: 6
bookCollapseSection: true
---

# Developer Guide

go-rtmp is a **pure Go (1.21+)** RTMP server with **zero external dependencies**. The architecture is a layered stack that mirrors the RTMP protocol itself — each layer only depends on the one below it, with no circular imports.

```
TCP → Handshake → Chunk → Control/AMF0 → RPC → Conn → Server
```

Every package under `internal/rtmp/` maps to a single protocol concern. This makes the codebase easy to navigate once you understand the protocol layers.

## Reading Order for New Contributors

If you're new to the project, read the code in this order:

1. [Architecture]({{< relref "/docs/developer/architecture" >}}) — understand the layers and data flow
2. [Design Principles]({{< relref "/docs/developer/design" >}}) — why decisions were made
3. [RTMP Protocol Reference]({{< relref "/docs/developer/protocol" >}}) — the wire format you're implementing
4. [Testing Guide]({{< relref "/docs/developer/testing" >}}) — how to validate your changes
5. [Contributing]({{< relref "/docs/developer/contributing" >}}) — workflow and conventions

## Testing Philosophy

The project follows a strict testing philosophy:

- **Golden binary vectors** — exact wire-format `.bin` files validate every encoder and decoder against known-good byte sequences
- **Table-driven tests** — every test case is a row in a struct slice, making it trivial to add new cases
- **No mocks** — tests use real `net.Pipe()` connections for end-to-end validation through the actual protocol stack
- **Race detector** — all CI runs use `go test -race` to catch concurrency bugs

This approach ensures that if the tests pass, the bytes on the wire are correct.

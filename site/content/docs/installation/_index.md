---
title: "Installation"
weight: 2
---

# Installation

## Download Pre-built Binaries

Go to the [GitHub Releases](https://github.com/alxayo/rtmp-go/releases) page and download the archive for your platform:

| Platform | Architecture | Filename |
|----------|-------------|----------|
| Linux | amd64 | `rtmp-server-linux-amd64.tar.gz` |
| Linux | arm64 | `rtmp-server-linux-arm64.tar.gz` |
| macOS | amd64 | `rtmp-server-darwin-amd64.tar.gz` |
| macOS | arm64 (Apple Silicon) | `rtmp-server-darwin-arm64.tar.gz` |
| Windows | amd64 | `rtmp-server-windows-amd64.zip` |

**Linux / macOS** — extract and make executable:

```bash
tar xzf rtmp-server-*.tar.gz
chmod +x rtmp-server
```

**Windows** — extract the `.zip` archive. The `rtmp-server.exe` binary is ready to use.

Verify the binary works:

```bash
./rtmp-server -version
```

## Build from Source

### Prerequisites

- **Go 1.21** or later ([download](https://go.dev/dl/))

### Steps

Clone the repository and build:

```bash
git clone https://github.com/alxayo/rtmp-go.git && cd rtmp-go
go build -o rtmp-server ./cmd/rtmp-server
```

### Cross-Compile for Other Platforms

Go makes it easy to build for any supported OS and architecture:

```bash
GOOS=linux GOARCH=amd64 go build -o rtmp-server-linux ./cmd/rtmp-server
GOOS=darwin GOARCH=arm64 go build -o rtmp-server-mac ./cmd/rtmp-server
GOOS=windows GOARCH=amd64 go build -o rtmp-server.exe ./cmd/rtmp-server
```

## Optimized Production Build

For the smallest, fully static binary suitable for production or containers:

```bash
CGO_ENABLED=0 go build -ldflags="-w -s" -o rtmp-server ./cmd/rtmp-server
```

| Flag | Purpose |
|------|---------|
| `CGO_ENABLED=0` | Produces a statically linked binary with no C library dependency. |
| `-w` | Omits the DWARF debug symbol table, reducing binary size. |
| `-s` | Omits the Go symbol table, further reducing binary size. |

## Verify Installation

Check the version:

```bash
./rtmp-server -version
```

Do a quick smoke test — start the server and then stop it with Ctrl+C:

```bash
./rtmp-server -listen :1935
```

## System Requirements

| Requirement | Details |
|-------------|---------|
| **Go 1.21+** | Required for building from source only. Pre-built binaries have no runtime dependency on Go. |
| **Runtime dependencies** | None — the binary is fully self-contained. |
| **FFmpeg / ffplay** | Optional. Useful for publishing test streams and playing them back. |
| **OBS Studio** | Optional. Use it for live streaming with a GUI (set the server URL to `rtmp://localhost:1935/live/stream-key`). |

# Documentation Index

## Start Here

| Document | Description |
|----------|-------------|
| [Getting Started](getting-started.md) | Build, run, and test the server in 5 minutes |
| [Architecture](architecture.md) | System overview with diagrams and package map |
| [Design](design.md) | Design principles, concurrency model, and key decisions |

## Deep Dives

| Document | Description |
|----------|-------------|
| [RTMP Protocol Reference](rtmp-protocol.md) | Wire-level protocol details: chunks, headers, AMF0, commands |
| [SRT Protocol Reference](srt-protocol.md) | SRT ingest: handshake, reliability, MPEG-TS conversion |
| [Implementation Guide](implementation.md) | Code walkthrough: connection lifecycle, data structures, media flow |
| [Testing Guide](testing-guide.md) | How to run tests, golden vectors, manual interop testing |
| [Definition of Done](definition-of-done.md) | Feature completion checklist and verification commands |

## Feature Documentation

| Document | Description |
|----------|-------------|
| [FLV Recording](features/Feature001-Auto_flv_recording.md) | Automatic stream recording to FLV files |
| [RTMP Relay](features/feature002-rtmp-relay.md) | Multi-subscriber relay with late-join support |

## Quick References

| Document | Description |
|----------|-------------|
| [Recording Quick Ref](RECORDING_QUICKREF.md) | Recording commands and troubleshooting |
| [Media Logging Quick Ref](MEDIA_LOGGING_QUICKREF.md) | Media packet logging and codec detection |
| [Wireshark Guide](wireshark_rtmp_capture_guide.md) | Capture and analyze RTMP traffic |

## Project

| Document | Description |
|----------|-------------|
| [../README.md](../README.md) | Project overview and feature summary |
| [../quick-start.md](../quick-start.md) | Original quick-start guide with OBS setup |

## Specifications

| Document | Description |
|----------|-------------|
| [../specs/001-rtmp-server-implementation/spec.md](../specs/001-rtmp-server-implementation/spec.md) | Core server specification |
| [../specs/001-rtmp-server-implementation/contracts/](../specs/001-rtmp-server-implementation/contracts/) | Wire format contracts (AMF0, chunking, commands, control, handshake, media) |
| [../specs/004-token-auth/spec.md](../specs/004-token-auth/spec.md) | Token-based authentication specification |

## Archived

Historical fix notes, debugging sessions, and completed feature implementation docs
are preserved in the [archived/](archived/) folder for reference.

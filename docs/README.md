# Documentation Index

Start here to navigate the go-rtmp documentation.

## Getting Started

| Document | Description |
|----------|-------------|
| [architecture.md](architecture.md) | **Start here** — full system overview for beginners |
| [../quick-start.md](../quick-start.md) | Step-by-step guide: build, run, stream with OBS/FFmpeg |
| [../README.md](../README.md) | Project overview, features, and build instructions |

## Design & Conventions

| Document | Description |
|----------|-------------|
| [000-constitution.md](000-constitution.md) | Core design principles and project philosophy |
| [go.instructions.md](go.instructions.md) | Go coding conventions used in this project |

## RTMP Protocol Reference

| Document | Description |
|----------|-------------|
| [RTMP_overview.md](RTMP_overview.md) | High-level protocol overview |
| [RTMP_basic_handshake_deep_dive.md](RTMP_basic_handshake_deep_dive.md) | Detailed handshake walkthrough |
| [RTMP Handshake – Step-by-Step Breakdown.md](<RTMP Handshake – Step-by-Step Breakdown.md>) | Visual step-by-step handshake |
| [rtmp_audio_video_messages_chunking.md](rtmp_audio_video_messages_chunking.md) | Chunks, audio/video message formats |
| [rtmp_data_exchange.md](rtmp_data_exchange.md) | Data exchange patterns |
| [001-rtmp_protocol_implementation_guide.md](001-rtmp_protocol_implementation_guide.md) | Full implementation guide |
| [wireshark_rtmp_capture_guide.md](wireshark_rtmp_capture_guide.md) | How to capture and analyze RTMP traffic |

## Feature Documentation

| Document | Description |
|----------|-------------|
| [features/Feature001-Auto_flv_recording.md](features/Feature001-Auto_flv_recording.md) | FLV recording feature spec |
| [features/feature002-rtmp-relay.md](features/feature002-rtmp-relay.md) | RTMP relay feature spec |
| [RECORDING_QUICKREF.md](RECORDING_QUICKREF.md) | Recording quick reference |
| [MEDIA_LOGGING_QUICKREF.md](MEDIA_LOGGING_QUICKREF.md) | Media logging quick reference |

## Specifications

| Document | Description |
|----------|-------------|
| [../specs/001-rtmp-server-implementation/spec.md](../specs/001-rtmp-server-implementation/spec.md) | Core server specification |
| [../specs/001-rtmp-server-implementation/contracts/](../specs/001-rtmp-server-implementation/contracts/) | Wire format contracts (AMF0, chunking, commands, control, handshake, media) |

## Archived

Historical fix notes, debugging sessions, and completed feature implementation docs
are preserved in the [archived/](archived/) folder for reference.

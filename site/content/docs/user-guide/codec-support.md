---
title: "Codec Support"
weight: 8
---

## Overview

go-rtmp supports multiple ingest protocols and output formats with wide codec coverage. This page provides a comprehensive reference for all supported video and audio codecs across different ingest paths and output destinations.

## Ingest Video Codecs

| Video Codec | RTMP (Legacy) | Enhanced RTMP | SRT + MPEG-TS | SRT + Matroska |
|---|:---:|:---:|:---:|:---:|
| H.264/AVC | ✅ CodecID 7 | ✅ `avc1` | ✅ 0x1B | ✅ `V_MPEG4/ISO/AVC` |
| H.265/HEVC | ✅ CodecID 12 | ✅ `hvc1` | ✅ 0x24 | ✅ `V_MPEGH/ISO/HEVC` |
| AV1 | — | ✅ `av01` | — | ✅ `V_AV1` |
| VP9 | — | ✅ `vp09` | — | ✅ `V_VP9` |
| VP8 | — | ✅ `vp08` | — | ✅ `V_VP8` |
| VVC/H.266 | — | ✅ `vvc1` | ✅ 0x33 | ✅ `V_MPEGH/ISO/VVC` |
| MPEG-2 | — | — | ⚠️ Dropped | — |

## Ingest Audio Codecs

| Audio Codec | RTMP (Legacy) | Enhanced RTMP | SRT + MPEG-TS | SRT + Matroska |
|---|:---:|:---:|:---:|:---:|
| AAC | ✅ SoundFormat 10 | ✅ `mp4a` | ✅ ADTS + LATM | ✅ `A_AAC` |
| MP3 | ✅ SoundFormat 2 | ✅ `.mp3` | ✅ 0x03/0x04 | ✅ `A_MP3` |
| Opus | — | ✅ `Opus` | — | ✅ `A_OPUS` |
| FLAC | — | ✅ `fLaC` | — | ✅ `A_FLAC` |
| AC-3 | — | ✅ `ac-3` | ✅ 0x81 | ✅ `A_AC3` |
| E-AC-3 | — | ✅ `ec-3` | ✅ 0x87 | ✅ `A_EAC3` |
| Speex | ✅ SoundFormat 11 | — | — | — |
| Vorbis | — | — | — | ⚠️ Dropped |

## Output: RTMP Relay

RTMP relay is pure passthrough—any accepted codec is forwarded to subscribers byte-for-byte. No transcoding or format conversion occurs. This ensures maximum performance and minimal latency for live streaming scenarios where subscribers are expecting the same codec stream as the source.

Relay supports all ingest codecs across all ingest protocols:
- **RTMP Legacy** sources relay to RTMP subscribers
- **Enhanced RTMP** sources relay to Enhanced RTMP subscribers
- **SRT sources** relay based on their original codec representation

## Output: Recording

Recording format is selected automatically based on the ingest video codec. Two recording containers are supported: FLV and MP4.

### Container Selection

| Video Codec | Recording Format |
|---|---|
| H.264 (or audio-only) | FLV |
| H.265, AV1, VP9, VP8, VVC | MP4 |

### FLV Recording Support

FLV recording is optimized for H.264 video streams and includes support for several audio codecs.

| Codec | Supported |
|---|:---:|
| H.264 video | ✅ |
| AAC audio | ✅ |
| MP3 audio | ✅ |
| Speex audio | ✅ |

### MP4 Recording Support

MP4 recording supports modern video codecs and a wide range of audio formats. Each codec is stored using its appropriate ISO Base Media File Format box structure.

| Video Codec | Box | Audio Codec | Box |
|---|---|---|---|
| H.264 | `avc1`/`avcC` | AAC | `mp4a`/`esds` |
| H.265 | `hvc1`/`hvcC` | Opus | `Opus`/`dOps` |
| AV1 | `av01`/`av1C` | FLAC | `fLaC`/`dfLa` |
| VP9 | `vp09`/`vpcC` | AC-3 | `ac-3`/`dac3` |
| VP8 | `vp08`/`vpcC` | E-AC-3 | `ec-3`/`dec3` |
| VVC | `vvc1`/`vvcC` | MP3 | `.mp3`/`esds` |

## Common End-to-End Combinations

The following table shows popular real-world setups and their expected output format combinations.

| Use Case | Ingest | Video | Audio | Relay | Recording |
|---|---|---|---|:---:|---|
| OBS + RTMP | RTMP | H.264 | AAC | ✅ | FLV |
| OBS + Enhanced RTMP | E-RTMP | HEVC | AAC | ✅ | MP4 |
| FFmpeg → SRT | SRT/TS | H.264 | AAC | ✅ | FLV |
| IP Camera → SRT | SRT/TS | H.265 | AAC | ✅ | MP4 |
| WebM via SRT | SRT/MKV | VP9 | Opus | ✅ | MP4 |
| AV1 via SRT | SRT/MKV | AV1 | Opus | ✅ | MP4 |
| Next-gen codec | E-RTMP | VVC | AC-3 | ✅ | MP4 |

## Unsupported Codecs

The following codecs are not supported and will trigger warnings during processing.

| Codec | Path | Behavior |
|---|---|---|
| MPEG-2 Video | SRT/TS | Warning logged, frames dropped (no RTMP representation) |
| Vorbis | SRT/MKV | Warning logged, frames dropped (use Opus instead) |
| Speex | MP4 recording | Warning logged, not recorded (relays fine, records to FLV) |

## Notes

- **Enhanced RTMP**: Extended RTMP protocol with FourCC-based codec identification, enabling support for modern codecs beyond the legacy protocol.
- **SRT Container Format**: Automatically detected from the first bytes (0x47 = MPEG-TS, EBML = Matroska). No configuration needed.
- **Sequence Headers**: All video and audio sequence headers are cached for late-joining subscribers, ensuring smooth playback upon connection.
- **Recording Format Selection**: Recording format is selected automatically based on ingest video codec—no configuration required. Audio-only streams default to FLV.

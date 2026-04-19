# Codec & Protocol Support Matrix

This document describes all supported codec/protocol/container combinations in go-rtmp.

## Ingest Protocols

go-rtmp accepts media via three ingest paths:

- **RTMP** (TCP) — Legacy RTMP with limited codec support (H.264, H.265, AAC, MP3, Speex)
- **Enhanced RTMP** (TCP) — Extended RTMP with FourCC-based codec identification (all modern codecs)
- **SRT** (UDP) — Secure Reliable Transport carrying either MPEG-TS or Matroska containers (auto-detected)

## Video Codec Support

| Video Codec | RTMP (Legacy) | Enhanced RTMP | SRT + MPEG-TS | SRT + Matroska |
|---|:---:|:---:|:---:|:---:|
| H.264/AVC | ✅ CodecID 7 | ✅ `avc1` | ✅ StreamType 0x1B | ✅ `V_MPEG4/ISO/AVC` |
| H.265/HEVC | ✅ CodecID 12 | ✅ `hvc1` | ✅ StreamType 0x24 | ✅ `V_MPEGH/ISO/HEVC` |
| AV1 | — | ✅ `av01` | — | ✅ `V_AV1` |
| VP9 | — | ✅ `vp09` | — | ✅ `V_VP9` |
| VP8 | — | ✅ `vp08` | — | ✅ `V_VP8` |
| VVC/H.266 | — | ✅ `vvc1` | ✅ StreamType 0x33 | ✅ `V_MPEGH/ISO/VVC` |
| MPEG-2 Video | — | — | ⚠️ Warning + drop | — |

## Audio Codec Support

| Audio Codec | RTMP (Legacy) | Enhanced RTMP | SRT + MPEG-TS | SRT + Matroska |
|---|:---:|:---:|:---:|:---:|
| AAC | ✅ SoundFormat 10 | ✅ `mp4a` | ✅ ADTS (0x0F) + LATM (0x11) | ✅ `A_AAC` |
| MP3 | ✅ SoundFormat 2 | ✅ `.mp3` | ✅ StreamType 0x03/0x04 | ✅ `A_MP3` |
| Opus | — | ✅ `Opus` | — | ✅ `A_OPUS` |
| FLAC | — | ✅ `fLaC` | — | ✅ `A_FLAC` |
| AC-3 (Dolby Digital) | — | ✅ `ac-3` | ✅ StreamType 0x81 | ✅ `A_AC3` |
| E-AC-3 (Dolby Digital+) | — | ✅ `ec-3` | ✅ StreamType 0x87 | ✅ `A_EAC3` |
| Speex | ✅ SoundFormat 11 | — | — | — |
| Vorbis | — | — | — | ⚠️ Warning + drop |

## Output: RTMP Relay

RTMP relay to subscribers is **pure passthrough** — every accepted codec is forwarded byte-for-byte to all connected subscribers. No codec filtering or transcoding occurs. Sequence headers are cached for late-joining subscribers.

## Output: Recording

### Container Selection

The recording format is selected automatically based on the detected video codec:

| Video Codec | Recording Format |
|---|---|
| H.264/AVC (or audio-only) | FLV |
| H.265, AV1, VP9, VP8, VVC | MP4 |

### FLV Recording

| Codec | Supported |
|---|:---:|
| H.264 video | ✅ |
| AAC audio | ✅ |
| MP3 audio | ✅ |
| Speex audio | ✅ |

FLV records raw RTMP tags — any legacy audio format is preserved as-is.

### MP4 Recording

| Video Codec | Sample Entry | Config Box |
|---|---|---|
| H.264/AVC | `avc1` | `avcC` |
| H.265/HEVC | `hvc1` | `hvcC` |
| AV1 | `av01` | `av1C` |
| VP9 | `vp09` | `vpcC` |
| VP8 | `vp08` | `vpcC` |
| VVC/H.266 | `vvc1` | `vvcC` |

| Audio Codec | Sample Entry | Config Box |
|---|---|---|
| AAC | `mp4a` | `esds` |
| Opus | `Opus` | `dOps` |
| FLAC | `fLaC` | `dfLa` |
| AC-3 | `ac-3` | `dac3` |
| E-AC-3 | `ec-3` | `dec3` |
| MP3 | `.mp3` | `esds` |
| Speex | ⚠️ Not supported | Warning logged, frames dropped |

## Common End-to-End Combinations

| Use Case | Ingest | Video | Audio | Relay | Recording |
|---|---|---|---|:---:|---|
| OBS Studio (default) | RTMP | H.264 | AAC | ✅ | FLV |
| OBS Studio (HEVC) | Enhanced RTMP | H.265 | AAC | ✅ | MP4 |
| FFmpeg → SRT (TS) | SRT/MPEG-TS | H.264 | AAC | ✅ | FLV |
| IP Camera → SRT | SRT/MPEG-TS | H.265 | AAC | ✅ | MP4 |
| FFmpeg → SRT (WebM) | SRT/Matroska | VP9 | Opus | ✅ | MP4 |
| AV1 low-latency | SRT/Matroska | AV1 | Opus | ✅ | MP4 |
| Broadcast (AC-3) | SRT/MPEG-TS | H.265 | AC-3 | ✅ | MP4 |
| Next-gen (VVC) | Enhanced RTMP | VVC | AAC | ✅ | MP4 |
| ISDB/DVB (LATM) | SRT/MPEG-TS | H.264 | AAC (LATM) | ✅ | FLV |
| Surround sound | Enhanced RTMP | H.264 | E-AC-3 | ✅ | MP4 |

## Unsupported Codecs

These codecs are recognized but cannot be forwarded due to protocol limitations:

| Codec | Ingest Path | Behavior |
|---|---|---|
| MPEG-2 Video | SRT/MPEG-TS | Warning logged once, frames dropped. No RTMP representation exists. |
| Vorbis | SRT/Matroska | Warning logged once, frames dropped. Use Opus instead. |
| Speex | MP4 recording | Warning logged once, not recorded to MP4. Relays to RTMP subscribers fine; records to FLV. |

## Technical Notes

- **Enhanced RTMP** extends legacy RTMP with FourCC-based codec identification (negotiated via `fourCcList` in the `connect` command)
- **SRT container auto-detection**: First bytes determine format — `0x47` sync byte = MPEG-TS, EBML magic (`0x1A45DFA3`) = Matroska
- **Sequence header caching**: All video/audio decoder configuration records are cached per stream for late-joining subscribers
- **No transcoding**: go-rtmp is a relay server — codecs pass through unchanged. Ingest codec = output codec.
- **Recording format is automatic**: Based on video codec detection, no user configuration needed
- **AAC-LATM**: Some broadcast encoders (ISDB, DVB) use LATM framing instead of ADTS. Both are transparently converted to raw AAC for RTMP.

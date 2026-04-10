// Package ts implements an MPEG-TS (Transport Stream) demuxer.
//
// # What is MPEG-TS?
//
// MPEG Transport Stream (MPEG-TS) is a container format defined by the ISO/IEC 13818-1
// standard. It was designed to carry multiple audio and video streams over unreliable
// transports like broadcast television, satellite links, and — more recently — the SRT
// protocol used for live video contribution over the internet.
//
// The key design idea behind MPEG-TS is resilience: the stream is divided into small,
// fixed-size 188-byte packets. If some packets are lost or corrupted during transmission,
// the receiver can quickly resynchronize and continue decoding. Each packet starts with a
// sync byte (0x47) that makes it easy to find packet boundaries even in the middle of a
// damaged stream.
//
// # How MPEG-TS is structured
//
// An MPEG-TS stream contains several layers:
//
//   - Transport packets: Fixed 188-byte packets identified by a 13-bit PID
//     (Packet Identifier). Different PIDs carry different types of data.
//
//   - Program-Specific Information (PSI): Metadata tables that describe what's
//     in the stream. The PAT (Program Association Table, always on PID 0) lists
//     available programs. Each program has a PMT (Program Map Table) that lists
//     the audio and video streams and their PIDs.
//
//   - PES (Packetized Elementary Stream): Raw codec data (H.264 video, AAC audio,
//     etc.) is wrapped in PES packets, which are then split across one or more TS
//     packets. PES headers carry timestamps (PTS/DTS) that tell the player when
//     to display or decode each frame.
//
// # What this demuxer does
//
// This package parses MPEG-TS packets, reads the PAT and PMT to discover which
// streams are present, reassembles PES packets from individual TS packets, and
// delivers complete media frames (with timestamps) to a callback function. It is
// designed to work with the SRT ingest pipeline, converting incoming MPEG-TS data
// into individual H.264 and AAC elementary stream frames suitable for re-muxing
// into RTMP or other formats.
package ts

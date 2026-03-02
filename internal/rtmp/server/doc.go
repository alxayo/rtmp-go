// Package server provides the RTMP server: listener, stream registry, and
// pub/sub coordination.
//
// # Architecture
//
// The server accepts TCP connections, performs handshakes via the conn package,
// and wires up command handling so real RTMP clients (OBS, FFmpeg, etc.) can
// publish and subscribe to live streams.
//
//	┌─────────────────────────────────────────────────────┐
//	│                     Server                          │
//	│                                                     │
//	│  Accept Loop ──► conn.Accept ──► attachCommandHandling
//	│                                                     │
//	│  Registry (stream map)                              │
//	│    └─ Stream                                        │
//	│         ├─ Publisher   (single conn per stream)      │
//	│         ├─ Subscribers (multiple play clients)       │
//	│         ├─ Recorder   (optional FLV recording)       │
//	│         └─ CachedHeaders (sequence headers for late join)│
//	│                                                     │
//	│  DestinationManager (optional external relay)       │
//	└─────────────────────────────────────────────────────┘
//
// # Key Components
//
//   - [Server]: Listener + accept loop + connection tracking + graceful shutdown.
//   - [Registry]: Thread-safe map of stream keys to [Stream] objects. Manages
//     publisher uniqueness, subscriber lists, and codec metadata.
//   - [Config]: Server settings (listen address, chunk size, recording, relay).
//   - [MediaLogger]: Per-connection audio/video statistics and codec detection.
//
// # Media Flow
//
// When a publisher sends audio/video messages, the message handler in
// command_integration.go routes them through media_dispatch.go which:
//  1. Logs statistics via MediaLogger
//  2. Writes to the FLV Recorder (if recording is enabled)
//  3. Broadcasts to all local subscribers
//  4. Forwards to external relay destinations (if configured)
package server

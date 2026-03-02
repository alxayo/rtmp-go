// Package chunk implements RTMP chunk-level framing: message fragmentation,
// reassembly, and header compression.
//
// RTMP does not send whole messages over the wire. Instead, each message is
// split into fixed-size chunks (default 128 bytes) that can be interleaved
// with chunks from other streams. This package handles both directions:
//
//   - [Writer] fragments outbound messages into chunks with FMT-based header
//     compression (FMT 0-3) for wire efficiency.
//   - [Reader] reassembles inbound chunks back into complete messages,
//     maintaining per-CSID state for header decompression.
//
// # Key Concepts
//
//   - CSID (Chunk Stream ID): Identifies a logical stream of chunks.
//     Each CSID maintains independent header compression state.
//   - FMT (Format): The 2-bit header type (0 = full, 1 = delta + length/type,
//     2 = delta only, 3 = continuation). Lower FMT values carry more header
//     data; higher values compress by reusing previous header fields.
//   - Extended Timestamp: When a timestamp value reaches 0xFFFFFF, a 4-byte
//     extended timestamp follows the message header.
//
// # Wire Layout
//
//	┌──────────────┬──────────────────┬─────────────────────┬──────────┐
//	│ Basic Header │  Message Header  │ Extended Timestamp?  │ Payload  │
//	│  (1-3 bytes) │ (0/3/7/11 bytes) │    (0 or 4 bytes)   │ (N bytes)│
//	└──────────────┴──────────────────┴─────────────────────┴──────────┘
//
// # Thread Safety
//
// Neither Reader nor Writer is safe for concurrent use. The expected pattern
// is one goroutine per direction (readLoop / writeLoop).
package chunk

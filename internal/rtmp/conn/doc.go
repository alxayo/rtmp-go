// Package conn manages the lifecycle of a single RTMP connection.
//
// After a TCP connection is accepted, this package performs the RTMP handshake,
// sends the initial control burst (Set Chunk Size, Window Ack Size, Set Peer
// Bandwidth), and manages the read/write goroutine pair for chunk I/O.
//
// # Connection Lifecycle
//
//  1. Accept(listener) → handshake → control burst → Connection
//  2. SetMessageHandler(fn)  – install the dispatch callback
//  3. Start()                – begin the read loop
//  4. Close()                – cancel context, close TCP, wait for goroutines
//
// # Concurrency Model
//
// Each connection runs two goroutines:
//   - readLoop: reads chunks from the TCP socket, reassembles messages, and
//     calls the installed message handler callback.
//   - writeLoop: drains the outbound message queue and writes chunks.
//
// The outbound queue is bounded (see [outboundQueueSize]) to provide
// backpressure. [SendMessage] blocks briefly (see [sendTimeout]) and returns
// an error if the queue is full.
//
// # Session State
//
// Per-connection metadata (app name, stream key, state machine) is tracked
// by the [Session] type in session.go.
package conn

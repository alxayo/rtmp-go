// Package conn implements the SRT connection state machine and lifecycle.
//
// # Overview
//
// An SRT connection represents a bidirectional UDP flow between client and
// server for live audio/video streaming. Unlike TCP, SRT is built on UDP
// and adds its own reliability layer (retransmissions, congestion control,
// timing buffers).
//
// The Conn type models the full lifecycle: initial handshake negotiation,
// active data exchange, graceful shutdown, and cleanup. All I/O is non-blocking
// and event-driven (via tickers and channel events).
//
// # Connection States
//
// A connection progresses through three states:
//   - StateConnected: Normal operation, data is flowing
//   - StateClosing: Shutdown initiated (either locally or by peer)
//   - StateClosed: Fully closed, all resources released
//
// # Concurrency Model
//
// The connection is NOT safe for concurrent use. Designed for a single
// readLoop goroutine per connection. That goroutine:
//   1. Reads UDP packets from the socket
//   2. Feeds them through the state machine (ACK, NAK, data handling)
//   3. Sends responses via the outbound queue
//
// Shared state (e.g., stream registry, Publisher interface) is protected
// by the parent listener's sync.RWMutex.
//
// # Lifecycle
//
// 1. NewConn(socket, config): Create connection, negotiate with peer
// 2. HandlePacket(pkt): Main loop processes incoming packets
// 3. SendPacket(pkt): Outbound queue for responses (ACKs, keepalives)
// 4. Close(): Graceful shutdown, flush buffers
// 5. Destroy(): Force close (on error or timeout)
//
// # Key Components
//
// ConnConfig: Negotiated parameters (MTU, TSBPD delay, congestion window)
// Shared across all packets for the lifetime of the connection.
//
// Timers: Run in parallel:
//   - ACK (every 10ms): Send selective ACKs for received packets
//   - NAK (every 20ms): Detect and retransmit lost packets
//   - TSBPD (every 1ms): Buffer management for playout timing
//   - Keepalive (every 1s): Detect dead peer or stalled connection
//
// Buffer: Circular buffer (internal/srt/circular) holds received packets
// in arrival order, even if out-of-sequence. TSBPD timer drains packets
// at the negotiated playback delay.
//
// Congestion Control: Tracks RTT, packet loss rate, and bitrate.
// Adjusts the congestion window to match network conditions.
//
// # Integration Points
//
// - handshake package: Initial negotiation (INDUCTION → CONCLUSION)
// - packet package: Parse/serialize SRT protocol packets
// - circular package: Packet reordering buffer
// - crypto package: Optional AES-128-CTR encryption
// - Publisher (ingress package): Callback when data arrives
//
// # Example: Receiving a Packet
//
//	for pkt := range packetChannel {
//	    if err := conn.HandlePacket(pkt); err != nil {
//	        log.Error("handle packet", "err", err)
//	        conn.Destroy()
//	        break
//	    }
//	}
//
// # Shutdown
//
// Close() initiates graceful shutdown: stop accepting new packets,
// drain buffers, send final messages. Destroy() is the emergency exit
// (timeout or fatal error), killing the connection immediately.
package conn

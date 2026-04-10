package srt

import (
	"fmt"
	"log/slog"
	"net"
	"sync"

	srtconn "github.com/alxayo/go-rtmp/internal/srt/conn"
)

// --- UDP Multiplexing Concept ---
//
// Unlike TCP, where each accepted connection gets its own dedicated socket,
// SRT runs over UDP — and ALL SRT connections share a SINGLE UDP socket.
//
// This means we need a way to figure out which connection each incoming
// packet belongs to. We do this by looking at two things:
//   1. The remote address (IP:port) of the sender
//   2. The SRT "destination socket ID" in the packet header
//
// Together, these form a unique key for each connection.
//
// There's one wrinkle: during the initial handshake, the socket ID is 0
// (because neither side has assigned one yet). So for handshake packets,
// we identify the connection by remote address alone. Once the handshake
// completes and both sides have socket IDs, we switch to keying by
// (remoteAddr, socketID) for all subsequent packets.

// connKey uniquely identifies an SRT connection within the listener.
// During handshake, socketID is 0 and we rely on just the address.
// After handshake, socketID is set to the peer's assigned socket ID.
type connKey struct {
	addr     string // Remote IP:port as a string (e.g., "192.168.1.5:12345")
	socketID uint32 // Peer's SRT socket ID (0 during initial handshake)
}

// ConnRequest represents a pending SRT connection that has completed
// the handshake and is waiting for the server to accept or reject it.
//
// The server application can inspect fields like StreamID (which carries
// the requested stream key in SRT) and decide whether to allow the
// connection.
type ConnRequest struct {
	// streamID is the SRT stream ID sent by the caller during handshake.
	// In live streaming, this typically carries the stream key or path
	// (e.g., "live/mystream").
	streamID string

	// peerAddr is the remote UDP address of the connecting client.
	peerAddr *net.UDPAddr

	// socketID is the SRT socket ID assigned to this connection.
	socketID uint32

	// conn is the established SRT connection. Set when the handshake
	// completes and the connection transitions to the connected state.
	conn *srtconn.Conn

	// accepted is signaled (closed) when the server accepts this connection.
	accepted chan struct{}

	// rejected carries a rejection reason code when the server rejects
	// this connection. The code is sent back to the peer in the
	// handshake rejection response.
	rejected chan uint32
}

// StreamID returns the SRT stream ID from the connection request.
// This is typically the stream key or path the client wants to access.
func (r *ConnRequest) StreamID() string { return r.streamID }

// PeerAddr returns the remote UDP address of the connecting client.
func (r *ConnRequest) PeerAddr() *net.UDPAddr { return r.peerAddr }

// Accept accepts the SRT connection and returns the established Conn.
// The connection is ready for reading data immediately after Accept returns.
func (r *ConnRequest) Accept() (*srtconn.Conn, error) {
	// Signal the handshake FSM that the application accepted
	close(r.accepted)

	// Return the established connection
	if r.conn == nil {
		return nil, fmt.Errorf("SRT connection not established for socket %d", r.socketID)
	}
	return r.conn, nil
}

// Reject rejects the SRT connection with the given reason code.
// The reason code is sent back to the peer in the handshake rejection.
func (r *ConnRequest) Reject(reason uint32) {
	select {
	case r.rejected <- reason:
	default:
	}
}

// SRT rejection reason codes. These are sent back to the peer to explain
// why their connection was refused.
const (
	// RejectBadRequest indicates the connection was rejected because the
	// request was malformed (e.g., invalid stream ID format).
	RejectBadRequest uint32 = 1400

	// RejectUnauthorized indicates authentication failed.
	RejectUnauthorized uint32 = 1401

	// RejectForbidden indicates the stream key is not allowed.
	RejectForbidden uint32 = 1403

	// RejectNotFound indicates the requested resource doesn't exist.
	RejectNotFound uint32 = 1404

	// RejectConflict indicates the stream key is already in use.
	RejectConflict uint32 = 1409
)

// srtHeaderMinBytes is the minimum size of an SRT packet header.
// Every SRT packet (both data and control) has at least a 16-byte header.
// Anything smaller is not a valid SRT packet and should be discarded.
const srtHeaderMinBytes = 16

// Listener accepts SRT connections on a single UDP port.
//
// It binds one UDP socket and multiplexes all SRT connections through it.
// Incoming packets are dispatched to the correct connection handler based
// on the sender's address and SRT socket ID. New connections go through
// a handshake process, and once complete, they appear on the accept channel.
type Listener struct {
	// udpConn is the single UDP socket shared by all SRT connections.
	udpConn *net.UDPConn

	// mu protects mutable state (like the closing flag) from concurrent
	// access by the readLoop goroutine and external callers.
	mu sync.RWMutex

	// config holds the SRT configuration (latency, MTU, etc.).
	config Config

	// closing is set to true when Close() is called. The readLoop
	// checks this flag to know when to stop gracefully instead of
	// logging errors about a closed socket.
	closing bool

	// acceptChan delivers completed handshakes to the Accept() caller.
	// It's buffered (capacity 16) so the handshake process doesn't block
	// if the server is slightly slow to call Accept().
	acceptChan chan *ConnRequest

	// log is the structured logger for this listener, tagged with
	// component="srt_listener" for easy filtering in logs.
	log *slog.Logger
}

// Listen creates and starts an SRT listener on the given UDP address.
//
// The address format is "host:port" (e.g., ":10080" to listen on all
// interfaces, or "127.0.0.1:10080" for localhost only).
//
// This function:
//  1. Applies default config values for any zero-valued fields
//  2. Resolves the UDP address
//  3. Opens a UDP socket
//  4. Starts the background read loop to process incoming packets
//
// The returned Listener is ready to accept connections via Accept().
func Listen(addr string, cfg Config) (*Listener, error) {
	// Fill in defaults for any fields the caller didn't set.
	cfg.applyDefaults()

	// Resolve the address string into a structured UDP address.
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}

	// Open the UDP socket. This is the single socket that ALL SRT
	// connections on this listener will share.
	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, err
	}

	l := &Listener{
		udpConn:    udpConn,
		config:     cfg,
		acceptChan: make(chan *ConnRequest, 16),
		log:        slog.Default().With("component", "srt_listener"),
	}

	// Start the read loop in a background goroutine. This goroutine
	// runs for the lifetime of the listener, reading UDP packets and
	// dispatching them to the appropriate connection handler.
	go l.readLoop()

	return l, nil
}

// readLoop continuously reads UDP packets from the shared socket and
// dispatches them to the correct connection handler.
//
// This is the "heart" of the multiplexer — it runs in its own goroutine
// and processes every single UDP packet that arrives on our port.
//
// The buffer is reused across reads for efficiency, but we copy the data
// out before dispatching so the buffer can be reused immediately.
func (l *Listener) readLoop() {
	// Allocate a reusable read buffer sized to the configured MTU.
	// We never expect a packet larger than this.
	buf := make([]byte, l.config.MTU)

	for {
		// ReadFromUDP blocks until a packet arrives or the socket is closed.
		// It returns the number of bytes read and the sender's address.
		n, remoteAddr, err := l.udpConn.ReadFromUDP(buf)
		if err != nil {
			// Check if we're shutting down. If so, the error is expected
			// (the socket was closed by Close()) and we exit cleanly.
			l.mu.RLock()
			closing := l.closing
			l.mu.RUnlock()
			if closing {
				return
			}
			// Unexpected error — log it and keep trying. UDP is lossy
			// by nature, so one bad read shouldn't kill the listener.
			l.log.Warn("UDP read error", "error", err)
			continue
		}

		// Make a copy of the received data. We must copy because the
		// buf slice is reused on the next ReadFromUDP call, and the
		// dispatch handler may process the data asynchronously.
		data := make([]byte, n)
		copy(data, buf[:n])

		// Route the packet to the appropriate connection handler.
		l.dispatch(data, remoteAddr)
	}
}

// dispatch routes an incoming UDP packet to the correct SRT connection.
//
// Currently (Phase 4), this is a placeholder that just validates the
// minimum packet size. The full dispatch logic — looking up connections
// by (remoteAddr, socketID), handling handshake packets, and forwarding
// data packets — will be wired in Phase 5 when the handshake FSM is added.
//
// The general approach will be:
//  1. Parse the SRT header to extract the destination socket ID
//  2. If socketID == 0, it's a handshake → look up by remoteAddr only
//  3. Otherwise, look up by (remoteAddr, socketID)
//  4. If found, forward to that connection's packet handler
//  5. If not found and it's a handshake, start a new handshake FSM
func (l *Listener) dispatch(data []byte, from *net.UDPAddr) {
	// Every valid SRT packet has at least a 16-byte header.
	// Discard anything smaller — it's either corrupted or not SRT.
	if len(data) < srtHeaderMinBytes {
		return
	}

	// Phase 5 will add:
	// - SRT header parsing to extract socketID and packet type
	// - Connection lookup in a map keyed by connKey{addr, socketID}
	// - Handshake initiation for new connections
	// - Packet forwarding for established connections
}

// Accept blocks until a new SRT connection completes its handshake,
// then returns a ConnRequest that the server can inspect and accept.
//
// This follows the same pattern as net.Listener.Accept() — the caller
// loops calling Accept() to handle incoming connections one at a time.
//
// Returns net.ErrClosed if the listener has been closed.
func (l *Listener) Accept() (*ConnRequest, error) {
	// Block until a handshake completes and a ConnRequest is available,
	// or until the channel is closed (meaning the listener is shutting down).
	req, ok := <-l.acceptChan
	if !ok {
		// Channel closed — listener was shut down.
		return nil, net.ErrClosed
	}
	return req, nil
}

// Addr returns the local UDP address the listener is bound to.
// This is useful for finding the actual port when binding to ":0"
// (which lets the OS pick a random available port).
func (l *Listener) Addr() net.Addr {
	return l.udpConn.LocalAddr()
}

// Close shuts down the listener gracefully.
//
// It sets the closing flag (so readLoop knows to exit quietly),
// closes the accept channel (so Accept() returns an error), and
// closes the underlying UDP socket (which causes readLoop's
// ReadFromUDP to return an error and exit).
func (l *Listener) Close() error {
	// Set the closing flag so readLoop doesn't log an error when
	// ReadFromUDP fails due to the socket being closed.
	l.mu.Lock()
	l.closing = true
	l.mu.Unlock()

	// Close the accept channel so any goroutine blocked on Accept()
	// will get net.ErrClosed.
	close(l.acceptChan)

	// Close the UDP socket. This will cause readLoop's ReadFromUDP
	// to return an error, and since closing==true, it will exit cleanly.
	return l.udpConn.Close()
}

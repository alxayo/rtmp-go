// File: listener.go
// Purpose: Implements the UDP listener for SRT ingest. Unlike RTMP (TCP),
// SRT uses UDP and multiplexes multiple connections over a single socket.
// This file handles connection acceptance, handshake negotiation, packet routing,
// and stateful connection tracking.
//
// Key Types:
//   - Listener: UDP socket + connection tracker
//   - connKey: Unique identifier for connection (remote addr + socket ID)
//   - pendingHandshake: In-progress handshake state
//   - Connection: Established SRT flow with packet/ACK/NAK timers
//
// Key Functions:
//   - NewListener(addr, config): Create UDP listener
//   - (l *Listener) Accept(ctx): Accept one connection (blocks until handshake complete)
//   - (l *Listener) Close(): Gracefully shutdown listener and all connections
//   - handlePacket(): Route incoming packet to correct connection or handshake handler
//
// Dependencies:
//   - internal/srt/conn: SRT connection state machine
//   - internal/srt/handshake: INDUCTION/CONCLUSION exchange
//   - internal/srt/packet: Packet parsing
//   - internal/ingress: Publisher interface for media dispatch
//   - log/slog: Structured logging
//   - net: UDP socket operations
//   - sync: Mutex for connection tracking
//
// Design Note: UDP multiplexing requires custom connection tracking.
// During handshake (socket ID = 0), connections are identified by remote address.
// After handshake, connections are identified by (remote address, socket ID) pair.
// This allows the same peer to initiate multiple SRT flows on different socket IDs.
package srt

import (
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"

	srtconn "github.com/alxayo/go-rtmp/internal/srt/conn"
	"github.com/alxayo/go-rtmp/internal/srt/handshake"
	"github.com/alxayo/go-rtmp/internal/srt/packet"
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

// pendingHandshake tracks a connection that is in the middle of the two-phase
// SRT handshake. After we send the Induction response (Phase 1), we store
// the state here. When the Conclusion arrives (Phase 2), we look it up to
// finish the handshake and create the connection.
type pendingHandshake struct {
	// hsListener is the per-connection handshake FSM. Each connection gets
	// its own instance because each has a unique local socket ID.
	hsListener *handshake.Listener

	// localSID is the unique local socket ID assigned to this connection.
	// The peer uses this as DestSocketID in all packets sent to us.
	localSID uint32

	// remoteAddr is the string form of the peer's UDP address (e.g.,
	// "192.168.1.5:12345"). Used as a map key for retransmitted Inductions
	// where DestSocketID is still 0.
	remoteAddr string
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

	// --- Connection Tracking ---
	//
	// These maps track SRT connections through their lifecycle:
	//   Induction → pendingByAddr + pendingBySID → Conclusion → conns
	//
	// All maps are protected by connsMu. The readLoop goroutine is the
	// primary writer (during handshakes), but Close() also modifies them.

	// connsMu protects conns, pendingByAddr, and pendingBySID maps.
	connsMu sync.RWMutex

	// conns maps our local socket ID → established SRT connection.
	// Once a handshake completes, the connection is stored here so
	// incoming data/control packets can be routed to it.
	conns map[uint32]*srtconn.Conn

	// pendingByAddr maps remote address string → pending handshake.
	// Used when we receive an Induction (DestSocketID is 0), so we can
	// only identify the connection by the sender's IP:port.
	pendingByAddr map[string]*pendingHandshake

	// pendingBySID maps our local socket ID → pending handshake.
	// Used when we receive a Conclusion (DestSocketID is the local SID
	// we assigned during Induction).
	pendingBySID map[uint32]*pendingHandshake

	// nextSocketID generates unique local socket IDs for new connections.
	// Each new handshake gets the next value. Starts at 1 because 0 is
	// reserved for handshake packets (DestSocketID=0 means "new connection").
	nextSocketID atomic.Uint32
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

	log := slog.Default().With("component", "srt_listener")

	// Log the actual bound address at DEBUG for troubleshooting
	log.Debug("SRT UDP socket opened",
		"requested_addr", addr,
		"bound_addr", udpConn.LocalAddr().String(),
		"network", udpConn.LocalAddr().Network(),
	)

	l := &Listener{
		udpConn:       udpConn,
		config:        cfg,
		acceptChan:    make(chan *ConnRequest, 16),
		log:           log,
		conns:         make(map[uint32]*srtconn.Conn),
		pendingByAddr: make(map[string]*pendingHandshake),
		pendingBySID:  make(map[uint32]*pendingHandshake),
	}

	// Socket IDs start at 1. Zero is reserved for handshake packets
	// (DestSocketID=0 means "this is a new connection, not yet assigned").
	l.nextSocketID.Store(1)

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
	l.log.Debug("SRT read loop started", "mtu", l.config.MTU, "local_addr", l.udpConn.LocalAddr().String())

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
				l.log.Debug("SRT read loop exiting (listener closing)")
				return
			}
			// Unexpected error — log it and keep trying. UDP is lossy
			// by nature, so one bad read shouldn't kill the listener.
			l.log.Warn("UDP read error", "error", err)
			continue
		}

		l.log.Debug("SRT UDP packet received",
			"remote", remoteAddr.String(),
			"bytes", n,
			"hex", fmt.Sprintf("%x", buf[:n]),
		)

		// Make a copy of the received data. We must copy because the
		// buf slice is reused on the next ReadFromUDP call, and the
		// dispatch handler may process the data asynchronously.
		data := make([]byte, n)
		copy(data, buf[:n])

		// Route the packet to the appropriate connection handler.
		l.dispatch(data, remoteAddr)
	}
}

// dispatch routes an incoming UDP packet to the correct SRT connection
// or handshake handler.
//
// The routing logic works as follows:
//
//  1. Parse the 16-byte SRT header to get DestSocketID and the control flag.
//  2. If DestSocketID == 0, this is a new connection starting a handshake
//     (Induction phase). Route to handleInduction().
//  3. If DestSocketID != 0, look up the connection:
//     a. First check established connections (conns map) → forward the packet
//     b. Then check pending handshakes (pendingBySID map) → handle Conclusion
//     c. If neither found, discard the packet (stale or misrouted)
func (l *Listener) dispatch(data []byte, from *net.UDPAddr) {
	// Every valid SRT packet has at least a 16-byte header.
	// Discard anything smaller — it's either corrupted or not SRT.
	if len(data) < srtHeaderMinBytes {
		l.log.Debug("SRT packet too small (discarded)",
			"remote", from.String(),
			"bytes", len(data),
			"min_required", srtHeaderMinBytes,
		)
		return
	}

	// Step 1: Parse the common 16-byte SRT header.
	// This tells us whether it's a control or data packet, and gives us
	// the DestSocketID which is how we route it to the right connection.
	hdr, err := packet.ParseHeader(data)
	if err != nil {
		l.log.Debug("SRT header parse failed",
			"remote", from.String(),
			"error", err,
		)
		return
	}

	// Step 2: Route based on DestSocketID.
	if hdr.DestSocketID == 0 {
		// DestSocketID == 0 means this is either:
		//   a) An Induction packet from a new client (first contact)
		//   b) A Conclusion packet from a client that just completed Induction
		//
		// In the SRT v5 handshake, the Induction response echoes the caller's
		// CIF SocketID back (not the listener's SID). So the caller doesn't
		// know our local SID yet and sends the Conclusion with DestSocketID=0.
		// We route based on the CIF handshake type to distinguish the two.
		l.handleHandshakePacket(data, from)
		return
	}

	// Step 3a: Check if this belongs to an established connection.
	// This is the hot path for media data — just a map lookup and forward.
	l.connsMu.RLock()
	conn, found := l.conns[hdr.DestSocketID]
	l.connsMu.RUnlock()

	if found {
		// Forward the raw packet to the connection's receive handler.
		// It will parse the full packet internally and handle data vs
		// control (ACK, NAK, keepalive, etc.) appropriately.
		conn.RecvPacket(data)
		return
	}

	// Step 3b: Check if this is a Conclusion for a pending handshake.
	// After Induction, the peer sends its Conclusion with DestSocketID
	// set to the local SID we assigned. We need to look it up in the
	// pending handshakes map.
	l.connsMu.RLock()
	pending, found := l.pendingBySID[hdr.DestSocketID]
	l.connsMu.RUnlock()

	if found {
		l.handleConclusion(data, from, pending)
		return
	}

	// Neither an established connection nor a pending handshake.
	// This could be a stale packet from a connection that was already
	// closed, or a misrouted packet. Just discard it.
	l.log.Debug("SRT packet for unknown socket ID (discarded)",
		"remote", from.String(),
		"dest_socket_id", hdr.DestSocketID,
		"is_control", hdr.IsControl,
	)
}

// handleHandshakePacket routes a handshake packet (DestSocketID=0) to the
// correct handler based on its CIF type: Induction or Conclusion.
//
// In SRT v5, both Induction AND Conclusion can arrive with DestSocketID=0
// because the Induction response echoes the caller's CIF SocketID (not the
// listener's SID). The caller doesn't learn our SID until the Conclusion
// response. So we need to parse the CIF to tell them apart.
func (l *Listener) handleHandshakePacket(data []byte, from *net.UDPAddr) {
	remoteAddr := from.String()

	// Parse the raw bytes as an SRT control packet.
	ctrl, err := packet.UnmarshalControlPacket(data)
	if err != nil {
		l.log.Debug("SRT failed to parse control packet",
			"remote", remoteAddr,
			"error", err,
		)
		return
	}

	// Verify it's a Handshake control packet (type 0x0000).
	if ctrl.Type != packet.CtrlHandshake {
		l.log.Debug("SRT expected Handshake control packet, got different type",
			"remote", remoteAddr,
			"control_type", ctrl.Type,
		)
		return
	}

	// Parse the Handshake CIF to determine the handshake phase.
	hs, err := packet.UnmarshalHandshakeCIF(ctrl.CIF)
	if err != nil {
		l.log.Debug("SRT failed to parse Handshake CIF",
			"remote", remoteAddr,
			"error", err,
		)
		return
	}

	// Route based on the handshake type in the CIF.
	switch hs.Type {
	case packet.HSTypeInduction:
		// First contact from a new client. Process the Induction.
		l.handleInduction(hs, ctrl, from)

	case packet.HSTypeConclusion:
		// Second phase — the client is responding to our Induction with
		// a Conclusion. Look up the pending handshake by source address.
		l.connsMu.RLock()
		pending, found := l.pendingByAddr[remoteAddr]
		l.connsMu.RUnlock()

		if !found {
			l.log.Debug("SRT Conclusion from unknown address (no pending handshake)",
				"remote", remoteAddr,
			)
			return
		}
		l.handleConclusion(data, from, pending)

	default:
		l.log.Debug("SRT unexpected handshake type with DestSocketID=0",
			"remote", remoteAddr,
			"handshake_type", hs.Type,
		)
	}
}

// handleInduction processes the first phase of the SRT handshake.
//
// When a new client wants to connect, it sends an Induction packet with
// DestSocketID=0. We:
//  1. Assign a unique local socket ID for this new connection
//  2. Create a per-connection handshake FSM
//  3. Process the Induction and generate a response with a SYN cookie
//  4. Send the response back to the client
//  5. Store the pending handshake so we can handle the Conclusion later
//
// The CIF and control packet are already parsed by handleHandshakePacket.
func (l *Listener) handleInduction(hs *packet.HandshakeCIF, _ *packet.ControlPacket, from *net.UDPAddr) {
	remoteAddr := from.String()

	// Check if we already have a pending handshake from this address.
	// If so, this is a retransmitted Induction (the client didn't receive
	// our response and is trying again). We reuse the existing state.
	l.connsMu.RLock()
	existing, isRetransmit := l.pendingByAddr[remoteAddr]
	l.connsMu.RUnlock()

	l.log.Debug("SRT Induction received",
		"remote", remoteAddr,
		"peer_socket_id", hs.SocketID,
		"version", hs.Version,
		"is_retransmit", isRetransmit,
	)

	// Determine the handshake FSM to use. Reuse if retransmit, create new otherwise.
	var ph *pendingHandshake
	if isRetransmit {
		ph = existing
	} else {
		// Assign a new unique local socket ID for this connection.
		// This ID is what the peer will use as DestSocketID in future packets.
		localSID := l.nextSocketID.Add(1)

		// Create a per-connection handshake FSM with our assigned socket ID.
		hsListener := handshake.NewListener(
			localSID,
			uint16(l.config.Latency),
			uint32(l.config.MTU),
			uint32(l.config.FlowWindow),
			l.log,
		)

		ph = &pendingHandshake{
			hsListener: hsListener,
			localSID:   localSID,
			remoteAddr: remoteAddr,
		}

		// Store in both lookup maps:
		// - pendingByAddr: for retransmitted Inductions (DestSocketID still 0)
		// - pendingBySID: for the Conclusion (DestSocketID = our local SID)
		l.connsMu.Lock()
		l.pendingByAddr[remoteAddr] = ph
		l.pendingBySID[localSID] = ph
		l.connsMu.Unlock()
	}

	// Process the Induction through the handshake FSM.
	// This generates a SYN cookie and builds the Induction response CIF.
	respCIF, err := ph.hsListener.HandleInduction(hs, from)
	if err != nil {
		l.log.Warn("SRT Induction handling failed",
			"remote", remoteAddr,
			"error", err,
		)
		// Clean up the pending handshake on failure
		l.connsMu.Lock()
		delete(l.pendingByAddr, remoteAddr)
		delete(l.pendingBySID, ph.localSID)
		l.connsMu.Unlock()
		return
	}

	// Send the Induction response back to the client.
	// DestSocketID in the response header MUST be the caller's socket ID
	// (from the CIF, not the header — during Induction the header DestSID
	// is 0). The caller uses the header DestSocketID to route the response
	// back to the correct socket in its internal multiplexer.
	if err := l.sendHandshakeResponse(respCIF, hs.SocketID, from); err != nil {
		l.log.Warn("SRT failed to send Induction response",
			"remote", remoteAddr,
			"error", err,
		)
		return
	}

	l.log.Debug("SRT Induction response sent",
		"remote", remoteAddr,
		"local_socket_id", ph.localSID,
		"cookie", respCIF.SYNCookie,
	)
}

// handleConclusion processes the second phase of the SRT handshake.
//
// After receiving our Induction response (with a SYN cookie), the client
// sends a Conclusion that echoes the cookie and includes extensions
// (HSREQ for capabilities, SID for stream ID). We:
//  1. Parse the handshake CIF from the packet
//  2. Validate the cookie and negotiate parameters via the handshake FSM
//  3. Create an SRT connection with the negotiated settings
//  4. Send the Conclusion response back to the client
//  5. Push a ConnRequest to the accept channel for the server to handle
func (l *Listener) handleConclusion(data []byte, from *net.UDPAddr, ph *pendingHandshake) {
	remoteAddr := from.String()

	// Parse the control packet and handshake CIF.
	ctrl, err := packet.UnmarshalControlPacket(data)
	if err != nil {
		l.log.Debug("SRT failed to parse control packet during Conclusion",
			"remote", remoteAddr,
			"error", err,
		)
		return
	}

	if ctrl.Type != packet.CtrlHandshake {
		l.log.Debug("SRT expected Handshake during Conclusion, got different type",
			"remote", remoteAddr,
			"control_type", ctrl.Type,
		)
		return
	}

	hs, err := packet.UnmarshalHandshakeCIF(ctrl.CIF)
	if err != nil {
		l.log.Debug("SRT failed to parse Conclusion CIF",
			"remote", remoteAddr,
			"error", err,
		)
		return
	}

	// The Conclusion should have Type=HSTypeConclusion, but we also
	// handle retransmitted Inductions (which arrive at the same SID).
	if hs.Type == packet.HSTypeInduction {
		l.log.Debug("SRT received retransmitted Induction on pending SID, re-handling",
			"remote", remoteAddr,
			"local_sid", ph.localSID,
		)
		l.handleInduction(hs, ctrl, from)
		return
	}

	if hs.Type != packet.HSTypeConclusion {
		l.log.Debug("SRT expected Conclusion handshake, got different type",
			"remote", remoteAddr,
			"handshake_type", hs.Type,
		)
		return
	}

	l.log.Debug("SRT Conclusion received",
		"remote", remoteAddr,
		"peer_socket_id", hs.SocketID,
		"local_socket_id", ph.localSID,
		"cookie", hs.SYNCookie,
		"num_extensions", len(hs.Extensions),
	)

	// Process the Conclusion through the handshake FSM.
	// This validates the SYN cookie, parses extensions (HSREQ, SID),
	// negotiates parameters (TSBPD, MTU, flags), and builds the response.
	respCIF, result, err := ph.hsListener.HandleConclusion(hs, from)
	if err != nil {
		l.log.Warn("SRT Conclusion handling failed",
			"remote", remoteAddr,
			"error", err,
		)
		// Clean up the pending handshake
		l.connsMu.Lock()
		delete(l.pendingByAddr, ph.remoteAddr)
		delete(l.pendingBySID, ph.localSID)
		l.connsMu.Unlock()
		return
	}

	// Build the connection configuration from the negotiated handshake result.
	connCfg := srtconn.ConnConfig{
		MTU:            result.MTU,
		FlowWindow:     result.FlowWindow,
		TSBPDDelay:     uint32(result.LocalTSBPD),
		PeerTSBPDDelay: uint32(result.PeerTSBPD),
		InitialSeqNum:  result.InitialSeqNum,
		PayloadSize:    result.MTU - 16, // SRT header is 16 bytes
	}

	// Create the SRT connection. It shares the listener's UDP socket
	// for sending packets back to the peer.
	conn := srtconn.New(
		ph.localSID,
		result.PeerSocketID,
		from,
		l.udpConn,
		result.StreamID,
		connCfg,
		l.log,
	)

	// Move from pending → established in the connection maps.
	l.connsMu.Lock()
	delete(l.pendingByAddr, ph.remoteAddr)
	delete(l.pendingBySID, ph.localSID)
	l.conns[ph.localSID] = conn
	l.connsMu.Unlock()

	// Send the Conclusion response back to the client.
	// DestSocketID is the peer's socket ID (they told us in the CIF).
	if err := l.sendHandshakeResponse(respCIF, result.PeerSocketID, from); err != nil {
		l.log.Warn("SRT failed to send Conclusion response",
			"remote", remoteAddr,
			"error", err,
		)
		// Remove the connection we just added since handshake didn't complete
		l.connsMu.Lock()
		delete(l.conns, ph.localSID)
		l.connsMu.Unlock()
		conn.Close()
		return
	}

	l.log.Info("SRT handshake completed",
		"remote", remoteAddr,
		"local_socket_id", ph.localSID,
		"peer_socket_id", result.PeerSocketID,
		"stream_id", result.StreamID,
		"mtu", result.MTU,
		"flow_window", result.FlowWindow,
	)

	// Create a ConnRequest and push it to the accept channel.
	// The server's accept loop will pick it up and wire the connection
	// into the media pipeline (SRT→RTMP bridge, recording, relay, etc.).
	req := &ConnRequest{
		streamID: result.StreamID,
		peerAddr: from,
		socketID: ph.localSID,
		conn:     conn,
		accepted: make(chan struct{}),
		rejected: make(chan uint32, 1),
	}

	// Use a non-blocking send to avoid stalling the readLoop if the
	// accept channel is full (which would block ALL SRT packet processing).
	select {
	case l.acceptChan <- req:
		l.log.Debug("SRT ConnRequest queued for accept",
			"remote", remoteAddr,
			"stream_id", result.StreamID,
		)
	default:
		l.log.Warn("SRT accept channel full, dropping connection",
			"remote", remoteAddr,
			"stream_id", result.StreamID,
		)
		l.connsMu.Lock()
		delete(l.conns, ph.localSID)
		l.connsMu.Unlock()
		conn.Close()
	}
}

// sendHandshakeResponse wraps a handshake CIF in an SRT control packet
// and sends it back to the peer via the shared UDP socket.
//
// Parameters:
//   - cif: The handshake CIF to send (built by the handshake FSM)
//   - destSocketID: The DestSocketID for the control packet header.
//     For Induction: the caller's CIF SocketID (so libsrt can route the reply).
//     For Conclusion: the peer's socket ID.
//   - to: The peer's UDP address to send to
func (l *Listener) sendHandshakeResponse(cif *packet.HandshakeCIF, destSocketID uint32, to *net.UDPAddr) error {
	// Step 1: Serialize the handshake CIF to binary.
	cifBytes, err := cif.MarshalBinary()
	if err != nil {
		return fmt.Errorf("marshal handshake CIF: %w", err)
	}

	// Step 2: Wrap the CIF in an SRT control packet.
	// Control packets have the F bit set to 1, with Type=Handshake (0x0000).
	ctrl := &packet.ControlPacket{
		Header: packet.Header{
			IsControl:    true,
			Timestamp:    0, // Handshake packets use timestamp 0
			DestSocketID: destSocketID,
		},
		Type: packet.CtrlHandshake,
		CIF:  cifBytes,
	}

	// Step 3: Serialize the complete control packet to wire format.
	ctrlBytes, err := ctrl.MarshalBinary()
	if err != nil {
		return fmt.Errorf("marshal control packet: %w", err)
	}

	// Log the hex bytes for debugging handshake wire format
	l.log.Debug("SRT sending handshake response (hex)",
		"dest_socket_id", destSocketID,
		"total_bytes", len(ctrlBytes),
		"cif_bytes", len(cifBytes),
		"hex", fmt.Sprintf("%x", ctrlBytes),
		"cif_version", cif.Version,
		"cif_ext_field", fmt.Sprintf("0x%04x", cif.ExtensionField),
		"cif_type", fmt.Sprintf("0x%08x", uint32(cif.Type)),
		"cif_socket_id", cif.SocketID,
		"cif_cookie", cif.SYNCookie,
	)

	// Step 4: Send the packet via the shared UDP socket.
	_, err = l.udpConn.WriteToUDP(ctrlBytes, to)
	if err != nil {
		return fmt.Errorf("send handshake response to %s: %w", to.String(), err)
	}

	return nil
}

// RemoveConn removes an established connection from the listener's
// connection registry. Call this after a connection has been closed
// to free the map entry and allow the socket ID to be reused.
func (l *Listener) RemoveConn(localSID uint32) {
	l.connsMu.Lock()
	delete(l.conns, localSID)
	l.connsMu.Unlock()
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

	// Close all established connections gracefully.
	l.connsMu.Lock()
	for sid, conn := range l.conns {
		conn.Close()
		delete(l.conns, sid)
	}
	// Clear pending handshakes (they'll never complete).
	for addr := range l.pendingByAddr {
		delete(l.pendingByAddr, addr)
	}
	for sid := range l.pendingBySID {
		delete(l.pendingBySID, sid)
	}
	l.connsMu.Unlock()

	// Close the accept channel so any goroutine blocked on Accept()
	// will get net.ErrClosed.
	close(l.acceptChan)

	// Close the UDP socket. This will cause readLoop's ReadFromUDP
	// to return an error, and since closing==true, it will exit cleanly.
	return l.udpConn.Close()
}

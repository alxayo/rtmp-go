package conn

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"

	"github.com/alxayo/go-rtmp/internal/srt/circular"
	"github.com/alxayo/go-rtmp/internal/srt/crypto"
	"github.com/alxayo/go-rtmp/internal/srt/packet"
)

// State represents the lifecycle state of an SRT connection.
// Connections always progress forward: Connected → Closing → Closed.
type State uint8

const (
	// StateConnected means the connection is established and actively
	// exchanging data packets. This is the normal operating state.
	StateConnected State = iota

	// StateClosing means shutdown has been initiated (either locally or
	// by the peer). Buffers are being drained and no new data is accepted.
	StateClosing

	// StateClosed means the connection is fully closed. All resources
	// have been released and no more I/O operations are possible.
	StateClosed
)

// String returns a human-readable name for the connection state.
func (s State) String() string {
	switch s {
	case StateConnected:
		return "Connected"
	case StateClosing:
		return "Closing"
	case StateClosed:
		return "Closed"
	default:
		return "Unknown"
	}
}

// ConnConfig holds the negotiated connection parameters.
// These values are determined during the SRT handshake and remain
// constant for the lifetime of the connection.
type ConnConfig struct {
	// MTU is the Maximum Transmission Unit — the largest packet size (in bytes)
	// that can be sent without fragmentation. Typically 1500 for Ethernet.
	MTU uint32

	// FlowWindow is the maximum number of unacknowledged data packets
	// allowed in flight. This provides flow control so a fast sender
	// doesn't overwhelm a slow receiver.
	FlowWindow uint32

	// TSBPDDelay is our TSBPD (Timestamp-Based Packet Delivery) latency
	// in milliseconds. Packets are held in the receive buffer for this
	// duration to smooth out network jitter before delivery.
	TSBPDDelay uint32

	// PeerTSBPDDelay is the peer's TSBPD delay in milliseconds.
	// The sender uses this to know how long the receiver will hold packets.
	PeerTSBPDDelay uint32

	// InitialSeqNum is the starting sequence number for data packets,
	// negotiated during the handshake. Sequence numbers start here and
	// wrap around at 2^31 - 1.
	InitialSeqNum uint32

	// PayloadSize is the maximum payload bytes per data packet.
	// Calculated as MTU - 16 (SRT data header size).
	PayloadSize uint32

	// KeySet holds the even/odd AES-CTR ciphers for decrypting incoming
	// data packets. nil means no encryption (plaintext mode). During key
	// rotation, both even and odd slots may be populated simultaneously.
	KeySet *crypto.KeySet

	// Passphrase is the shared secret used for PBKDF2 key derivation.
	// Stored here so post-handshake KMREQ messages (key rotation) can
	// derive new KEKs from new salts using the same passphrase.
	// Empty string means no encryption.
	Passphrase string

	// PbKeyLen is the expected AES key length in bytes (16/24/32) for
	// post-handshake key rotation. Matches the initial handshake key size.
	PbKeyLen int
}

// Conn represents an established SRT connection.
//
// Unlike TCP where each connection has its own OS socket, SRT connections
// share a single UDP socket managed by the Listener. Each Conn sends
// packets by writing to the shared UDP socket addressed to its specific peer.
//
// The connection lifecycle is:
//  1. Created by the Listener after a successful handshake
//  2. Receives incoming packets via RecvPacket() called by the Listener
//  3. Application reads data via Read() (implements io.Reader)
//  4. Closed via Close() or when the peer sends a Shutdown control packet
type Conn struct {
	// --- Identity (immutable after creation) ---

	// localSocketID uniquely identifies this connection on our side.
	// The peer includes this in every packet so the Listener can route it.
	localSocketID uint32

	// peerSocketID is the peer's socket identifier.
	// We include this in every packet we send so the peer can route it.
	peerSocketID uint32

	// peerAddr is the peer's UDP address (IP + port).
	peerAddr *net.UDPAddr

	// udpConn is the shared UDP socket owned by the Listener.
	// All SRT connections on this listener share this single socket.
	udpConn *net.UDPConn

	// streamID is the SRT Stream ID requested by the caller.
	// Used for routing (e.g., which stream to publish to).
	streamID string

	// --- State (protected by mu) ---

	// mu protects the state field from concurrent access.
	mu sync.RWMutex

	// state tracks where we are in the connection lifecycle.
	state State

	// --- Lifecycle management ---

	// cancel is called to signal all goroutines that the connection is done.
	cancel func()

	// done is closed when the connection has fully shut down.
	// Read() and other operations check this to know when to stop.
	done chan struct{}

	// --- Send and receive subsystems ---

	// sender manages outgoing packets: buffering, retransmission, RTT tracking.
	sender *Sender

	// receiver manages incoming packets: reordering, delivery, loss detection.
	receiver *Receiver

	// ackState tracks ACK generation timing and ACKACK-based RTT measurement.
	ackState *ACKState

	// tsbpd manages timestamp-based packet delivery scheduling.
	// It determines when buffered packets should be delivered to the application.
	tsbpd *TSBPDManager

	// --- Configuration ---

	// config holds the negotiated connection parameters from the handshake.
	config ConnConfig

	// --- Callbacks ---

	// onDisconnect is called when the connection closes. The Listener uses
	// this to remove the connection from its registry and clean up resources.
	onDisconnect func()

	// log is the structured logger with connection-specific context fields.
	log *slog.Logger
}

// New creates a new SRT connection with the given parameters.
// This is called by the Listener after a successful handshake completes.
//
// Parameters:
//   - localSID:  our socket ID (identifies this connection locally)
//   - peerSID:   the peer's socket ID (included in outgoing packets)
//   - peerAddr:  the peer's UDP address
//   - udpConn:   the shared UDP socket for sending packets
//   - streamID:  the SRT stream identifier
//   - cfg:       negotiated connection parameters
//   - log:       structured logger
func New(localSID, peerSID uint32, peerAddr *net.UDPAddr, udpConn *net.UDPConn, streamID string, cfg ConnConfig, log *slog.Logger) *Conn {
	c := &Conn{
		localSocketID: localSID,
		peerSocketID:  peerSID,
		peerAddr:      peerAddr,
		udpConn:       udpConn,
		streamID:      streamID,
		state:         StateConnected,
		done:          make(chan struct{}),
		config:        cfg,
		log:           log.With("conn_id", localSID, "peer", peerAddr.String()),
	}

	// Create send and receive subsystems with the negotiated initial sequence number
	c.sender = NewSender(circular.New(cfg.InitialSeqNum), cfg, log)
	c.receiver = NewReceiver(circular.New(cfg.InitialSeqNum), cfg, log)

	// Create ACK state for tracking acknowledgement timing and RTT measurement
	c.ackState = NewACKState()

	// Create TSBPD manager for timestamp-based packet delivery
	c.tsbpd = NewTSBPDManager(cfg.TSBPDDelay)

	// Set up a cancel function that transitions state when called
	c.cancel = func() {
		c.mu.Lock()
		alreadyClosed := c.state == StateClosed
		if !alreadyClosed {
			c.state = StateClosed
		}
		c.mu.Unlock()

		if !alreadyClosed {
			close(c.done)
		}
	}

	return c
}

// LocalSocketID returns the local SRT socket identifier.
func (c *Conn) LocalSocketID() uint32 { return c.localSocketID }

// PeerSocketID returns the peer's SRT socket identifier.
func (c *Conn) PeerSocketID() uint32 { return c.peerSocketID }

// PeerAddr returns the peer's network address.
func (c *Conn) PeerAddr() *net.UDPAddr { return c.peerAddr }

// StreamID returns the SRT stream ID for this connection.
func (c *Conn) StreamID() string { return c.streamID }

// State returns the current connection state (thread-safe).
func (c *Conn) State() State {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

// SetDisconnectHandler sets a function that will be called when the connection
// closes. This is typically used by the Listener to clean up its connection
// registry when a connection goes away.
func (c *Conn) SetDisconnectHandler(fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onDisconnect = fn
}

// Read reads delivered data from the receive buffer.
// It blocks until data is available or the connection closes.
// This implements the io.Reader interface so the connection can be used
// anywhere a reader is expected (e.g., io.Copy, bufio.Scanner).
func (c *Conn) Read(buf []byte) (int, error) {
	// Check if the connection is already closed
	c.mu.RLock()
	if c.state == StateClosed {
		c.mu.RUnlock()
		return 0, io.EOF
	}
	c.mu.RUnlock()

	// Wait for data from the receiver's delivery channel, or connection close.
	// Using select lets us respond to whichever event happens first.
	select {
	case data, ok := <-c.receiver.DeliveryChan():
		if !ok {
			// Channel was closed — connection is shutting down
			return 0, io.EOF
		}

		// Copy the delivered data into the caller's buffer
		n := copy(buf, data)
		return n, nil

	case <-c.done:
		// Connection was closed while we were waiting for data
		return 0, io.EOF
	}
}

// Close initiates graceful shutdown of the connection.
// It sends a Shutdown control packet to the peer, transitions the state,
// fires the disconnect handler, and releases resources.
func (c *Conn) Close() error {
	c.mu.Lock()

	// Don't close twice
	if c.state == StateClosed {
		c.mu.Unlock()
		return nil
	}

	// Transition to Closing state while we clean up
	c.state = StateClosing
	c.mu.Unlock()

	c.log.Info("closing SRT connection")

	// Send a Shutdown control packet to tell the peer we're done.
	// We do this best-effort — if it fails, the peer will time out eventually.
	shutdownPkt := &packet.ControlPacket{
		Header: packet.Header{
			IsControl:    true,
			DestSocketID: c.peerSocketID,
		},
		Type: packet.CtrlShutdown,
	}
	if err := c.sendControl(shutdownPkt); err != nil {
		c.log.Warn("failed to send shutdown packet", "error", err)
	}

	// Cancel the connection context to signal all goroutines
	c.cancel()

	// Fire the disconnect handler so the Listener can clean up
	c.mu.RLock()
	handler := c.onDisconnect
	c.mu.RUnlock()

	if handler != nil {
		handler()
	}

	return nil
}

// RecvPacket is called by the Listener when a UDP packet arrives for this
// connection. It parses the packet header and routes it to the appropriate
// handler based on whether it's a data packet or a control packet.
func (c *Conn) RecvPacket(data []byte) {
	// Need at least a full header (16 bytes) to parse
	if len(data) < packet.HeaderSize {
		c.log.Warn("received packet too small", "size", len(data))
		return
	}

	// Parse the common header to determine if this is data or control
	hdr, err := packet.ParseHeader(data)
	if err != nil {
		c.log.Warn("failed to parse packet header", "error", err)
		return
	}

	if hdr.IsControl {
		// Control packet — parse the full control packet and handle by type
		c.handleControlPacket(data)
	} else {
		// Data packet — parse and deliver to the receiver
		c.handleDataPacket(data)
	}
}

// handleDataPacket parses a raw data packet and passes it to the receiver
// for buffering and in-order delivery.
func (c *Conn) handleDataPacket(data []byte) {
	// Parse the full data packet from the raw bytes.
	pkt, err := packet.UnmarshalDataPacket(data)
	if err != nil {
		c.log.Warn("failed to parse data packet", "error", err)
		return
	}

	// If this connection uses encryption, decrypt the payload using the
	// appropriate key (even or odd) from the KeySet. The KK field in the
	// data packet header tells us which key was used to encrypt.
	if c.config.KeySet != nil {
		if pkt.Encryption == packet.EncryptionNone {
			// Plaintext packet on an encrypted connection — drop it.
			// Accepting plaintext would weaken the security contract.
			c.log.Warn("dropping unencrypted packet on encrypted connection",
				"seq", pkt.SequenceNumber,
			)
			return
		}
		if err := c.config.KeySet.DecryptPayload(pkt.Payload, pkt.SequenceNumber, uint8(pkt.Encryption)); err != nil {
			c.log.Warn("failed to decrypt data packet",
				"seq", pkt.SequenceNumber,
				"encryption", pkt.Encryption,
				"error", err,
			)
			return // Drop packets that fail decryption
		}
	}

	// Hand it off to the receiver for buffering and delivery.
	c.receiver.OnData(pkt)

	// Track the received packet for ACK interval calculation.
	c.ackState.OnDataReceived()
}

// handleControlPacket parses a raw control packet and dispatches it
// to the appropriate handler based on the control type.
func (c *Conn) handleControlPacket(data []byte) {
	// Parse the full control packet
	ctrl, err := packet.UnmarshalControlPacket(data)
	if err != nil {
		c.log.Warn("failed to parse control packet", "error", err)
		return
	}

	// Dispatch based on control packet type
	switch ctrl.Type {
	case packet.CtrlACK:
		// ACK: the peer is acknowledging received packets
		c.handleACK(ctrl)

	case packet.CtrlNAK:
		// NAK: the peer is reporting lost packets that need retransmission
		c.HandleIncomingNAK(ctrl)

	case packet.CtrlACKACK:
		// ACKACK: response to our ACK — used for RTT measurement
		c.HandleACKACK(ctrl.TypeSpecific)

	case packet.CtrlShutdown:
		// Shutdown: the peer wants to close the connection
		c.log.Info("received shutdown from peer")
		c.Close()

	case packet.CtrlKeepAlive:
		// Keepalive: the peer is still alive, nothing to do
		c.log.Debug("received keepalive")

	case packet.CtrlUserDefined:
		// User-defined control messages carry a subtype in TypeSpecific.
		// Currently only KMREQ (key rotation) is handled.
		switch ctrl.TypeSpecific {
		case packet.UserSubtypeKMREQ:
			c.handleKMREQ(ctrl)
		default:
			c.log.Debug("received unknown user-defined control message",
				"subtype", ctrl.TypeSpecific,
			)
		}

	default:
		c.log.Debug("received unhandled control packet",
			"type", ctrl.Type,
		)
	}
}

// handleACK processes an ACK control packet from the peer.
// The ACK contains information about what the peer has received and
// its receive buffer status.
func (c *Conn) handleACK(ctrl *packet.ControlPacket) {
	// The ACK CIF (Control Information Field) contains detailed info
	if len(ctrl.CIF) < packet.ACKCIFSize {
		c.log.Warn("ACK packet too small", "cif_size", len(ctrl.CIF))
		return
	}

	// Parse the ACK CIF to get the acknowledged sequence number and stats
	ackCIF, err := packet.UnmarshalACKCIF(ctrl.CIF)
	if err != nil {
		c.log.Warn("failed to parse ACK CIF", "error", err)
		return
	}

	// Tell the sender that everything before this sequence number is received
	c.sender.OnACK(circular.New(ackCIF.LastACKPacketSeq))

	// Send ACKACK back so the peer can measure RTT.
	// The TypeSpecific field of the ACK contains the ACK sequence number;
	// we echo it back in the ACKACK.
	ackack := &packet.ControlPacket{
		Header: packet.Header{
			IsControl:    true,
			Timestamp:    ctrl.Timestamp,
			DestSocketID: c.peerSocketID,
		},
		Type:         packet.CtrlACKACK,
		TypeSpecific: ctrl.TypeSpecific,
	}

	if err := c.sendControl(ackack); err != nil {
		c.log.Warn("failed to send ACKACK", "error", err)
	}
}

// handleNAK processes a NAK control packet from the peer.
// The NAK contains ranges of sequence numbers that the peer hasn't received.
func (c *Conn) handleNAK(ctrl *packet.ControlPacket) {
	// Decode the loss ranges from the NAK's CIF
	ranges := packet.DecodeLossRanges(ctrl.CIF)
	if len(ranges) == 0 {
		return
	}

	// Tell the sender to queue these packets for retransmission
	c.sender.OnNAK(ranges)
}

// handleKMREQ processes a post-handshake Key Material Request from the peer.
// The sender sends this during key rotation to deliver a new Stream Encrypting
// Key (SEK). We derive the KEK from our passphrase and the new salt, unwrap
// the SEK(s), install them in the KeySet, and send back a KMRSP to confirm.
func (c *Conn) handleKMREQ(ctrl *packet.ControlPacket) {
	// 1. Verify we have encryption configured. If the connection was
	//    established without a passphrase, we can't process key material.
	if c.config.KeySet == nil || c.config.Passphrase == "" {
		c.log.Warn("received KMREQ but connection is not encrypted")
		return
	}

	// 2. Parse the KM message from the control packet's CIF.
	km, err := crypto.ParseKMMsg(ctrl.CIF)
	if err != nil {
		c.log.Warn("failed to parse post-handshake KMREQ", "error", err)
		return
	}

	// 3. Validate the KM message carries a supported crypto profile.
	//    We only support AES-CTR with no authentication, MPEG-TS/SRT
	//    encapsulation, and the default passphrase-derived KEK (KEKI=0).
	if km.Cipher != crypto.CipherAESCTR {
		c.log.Warn("rejecting KMREQ: unsupported cipher", "cipher", km.Cipher)
		return
	}
	if km.Auth != 0 {
		c.log.Warn("rejecting KMREQ: unsupported auth type", "auth", km.Auth)
		return
	}
	if km.SE != crypto.SELiveSRT {
		c.log.Warn("rejecting KMREQ: unsupported stream encapsulation", "se", km.SE)
		return
	}
	if km.KEKI != 0 {
		c.log.Warn("rejecting KMREQ: unsupported KEKI (only passphrase-derived KEK supported)", "keki", km.KEKI)
		return
	}

	// 4. Validate the key length matches the negotiated size.
	keyLen := int(km.KLen)
	if c.config.PbKeyLen != 0 && keyLen != c.config.PbKeyLen {
		c.log.Warn("rejecting KMREQ: key length mismatch",
			"km_key_len", keyLen,
			"negotiated_key_len", c.config.PbKeyLen,
		)
		return
	}

	// 5. Derive the KEK using PBKDF2 with the new salt from this KMREQ.
	//    Per SRT spec §6.2.1, only the last 8 bytes (LSB 64 bits) of the
	//    16-byte salt are used for PBKDF2 derivation.
	kek, err := crypto.DeriveKey(c.config.Passphrase, km.Salt[len(km.Salt)-8:], keyLen)
	if err != nil {
		c.log.Warn("failed to derive KEK for key rotation", "error", err)
		return
	}

	// 6. Unwrap the SEK(s) using AES Key Wrap (RFC 3394). If the passphrase
	//    is wrong, Unwrap returns an integrity check error.
	plaintext, err := crypto.Unwrap(kek, km.WrappedKey)
	if err != nil {
		c.log.Warn("failed to unwrap SEK during key rotation (wrong passphrase?)", "error", err)
		return
	}

	// 7. Install the unwrapped SEK(s) into the KeySet. For KKEven or KKOdd
	//    the plaintext is a single key; for KKBoth it's even||odd concatenated.
	if err := c.config.KeySet.InstallKey(km.KK, plaintext, km.Salt, keyLen); err != nil {
		c.log.Warn("failed to install rotated key", "kk", km.KK, "error", err)
		return
	}

	c.log.Info("key rotation completed",
		"kk", km.KK,
		"key_len", keyLen,
		"cipher", "AES-CTR",
	)

	// 8. Send KMRSP back to the peer to confirm receipt.
	//    The response echoes the same KM message content.
	c.sendKMRSP(ctrl.CIF)
}

// sendKMRSP sends a Key Material Response back to the peer, confirming
// that we received and installed the new encryption key(s). The response
// echoes the KMREQ content to indicate success.
func (c *Conn) sendKMRSP(kmData []byte) {
	resp := &packet.ControlPacket{
		Header: packet.Header{
			IsControl:    true,
			DestSocketID: c.peerSocketID,
		},
		Type:         packet.CtrlUserDefined,
		TypeSpecific: packet.UserSubtypeKMRSP,
		CIF:          kmData,
	}

	if err := c.sendControl(resp); err != nil {
		c.log.Warn("failed to send KMRSP", "error", err)
	}
}

// sendPacket sends raw bytes to the peer via the shared UDP socket.
// This is the lowest-level send function — it just puts bytes on the wire.
func (c *Conn) sendPacket(data []byte) error {
	_, err := c.udpConn.WriteToUDP(data, c.peerAddr)
	return err
}

// sendControl marshals a control packet to bytes and sends it to the peer.
// This is a convenience wrapper around sendPacket for control messages.
func (c *Conn) sendControl(ctrl *packet.ControlPacket) error {
	// Serialize the control packet to its wire format
	data, err := ctrl.MarshalBinary()
	if err != nil {
		return fmt.Errorf("marshal control packet: %w", err)
	}

	// Send the raw bytes to the peer
	return c.sendPacket(data)
}

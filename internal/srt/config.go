package srt

// Config holds all SRT-specific listener configuration settings.
// These values control how the SRT listener behaves, including
// network parameters, latency buffering, and optional encryption.
//
// Zero values are replaced with sensible defaults by applyDefaults().
type Config struct {
	// ListenAddr is the UDP address to bind to (e.g., ":10080").
	// Unlike TCP, SRT uses UDP, so this opens a single UDP socket.
	ListenAddr string

	// Latency is the TSBPD (Time Stamp Based Packet Delivery) latency
	// in milliseconds. This is the amount of time the receiver buffers
	// packets before delivering them, which absorbs jitter and allows
	// time for retransmissions. Default: 120ms.
	Latency int

	// MTU is the Maximum Transmission Unit in bytes. This is the largest
	// UDP packet size we will send or expect to receive. It must fit
	// within the network path's MTU to avoid IP fragmentation.
	// Default: 1500 bytes (standard Ethernet MTU).
	MTU int

	// FlowWindow is the maximum number of packets that can be "in-flight"
	// (sent but not yet acknowledged) at any given time. This provides
	// flow control so a fast sender doesn't overwhelm a slow receiver.
	// Default: 8192 packets.
	FlowWindow int

	// Passphrase is the encryption passphrase. When non-empty, SRT will
	// use AES encryption to protect the media stream. Both sides must
	// use the same passphrase. Empty string means no encryption.
	Passphrase string

	// PbKeyLen is the AES key size in bytes for encryption.
	// Valid values: 0 (no encryption), 16 (AES-128), 24 (AES-192),
	// or 32 (AES-256). Default: 0 (no encryption).
	PbKeyLen int
}

// applyDefaults fills in zero-valued fields with sensible default values.
// This is called automatically when creating a listener, so users only
// need to set the fields they want to customize.
func (c *Config) applyDefaults() {
	// 120ms latency is a good balance between low delay and jitter
	// absorption for most live-streaming use cases.
	if c.Latency == 0 {
		c.Latency = 120
	}

	// 1500 bytes matches standard Ethernet MTU, which works on
	// virtually all networks without causing IP fragmentation.
	if c.MTU == 0 {
		c.MTU = 1500
	}

	// 8192 packets gives plenty of room for high-bitrate streams
	// while still providing backpressure if the receiver falls behind.
	if c.FlowWindow == 0 {
		c.FlowWindow = 8192
	}
}

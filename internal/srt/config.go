package srt

import "fmt"

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

// Validate checks the Config for invalid or unsupported values.
// Call this before creating a listener to get clear error messages
// instead of cryptic failures later.
func (c *Config) Validate() error {
	// SRT spec recommends passphrases be 10-79 characters.
	// libsrt enforces a minimum of 10 characters. We follow the same rule
	// so operators get a clear error instead of a silent mismatch.
	if c.Passphrase != "" {
		if len(c.Passphrase) < 10 {
			return fmt.Errorf("srt passphrase too short: %d characters (minimum 10)", len(c.Passphrase))
		}
		if len(c.Passphrase) > 79 {
			return fmt.Errorf("srt passphrase too long: %d characters (maximum 79)", len(c.Passphrase))
		}
	}

	// PbKeyLen must be a valid AES key size or 0 (no encryption).
	if c.PbKeyLen != 0 && c.PbKeyLen != 16 && c.PbKeyLen != 24 && c.PbKeyLen != 32 {
		return fmt.Errorf("srt pbkeylen must be 0, 16, 24, or 32, got %d", c.PbKeyLen)
	}

	return nil
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

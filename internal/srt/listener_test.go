package srt

import (
	"net"
	"testing"
	"time"
)

// TestConfigDefaults verifies that applyDefaults fills in sensible values
// for any fields left at their zero value, while preserving explicitly
// set values.
func TestConfigDefaults(t *testing.T) {
	// --- Sub-test: all defaults ---
	// When no fields are set, applyDefaults should fill in the standard
	// values for Latency, MTU, and FlowWindow.
	t.Run("all defaults", func(t *testing.T) {
		cfg := Config{}
		cfg.applyDefaults()

		if cfg.Latency != 120 {
			t.Errorf("Latency: got %d, want 120", cfg.Latency)
		}
		if cfg.MTU != 1500 {
			t.Errorf("MTU: got %d, want 1500", cfg.MTU)
		}
		if cfg.FlowWindow != 8192 {
			t.Errorf("FlowWindow: got %d, want 8192", cfg.FlowWindow)
		}
	})

	// --- Sub-test: custom values preserved ---
	// When the user explicitly sets values, applyDefaults should NOT
	// overwrite them — only zero values get defaults.
	t.Run("custom values preserved", func(t *testing.T) {
		cfg := Config{
			Latency:    200,
			MTU:        1200,
			FlowWindow: 4096,
		}
		cfg.applyDefaults()

		if cfg.Latency != 200 {
			t.Errorf("Latency: got %d, want 200", cfg.Latency)
		}
		if cfg.MTU != 1200 {
			t.Errorf("MTU: got %d, want 1200", cfg.MTU)
		}
		if cfg.FlowWindow != 4096 {
			t.Errorf("FlowWindow: got %d, want 4096", cfg.FlowWindow)
		}
	})

	// --- Sub-test: string fields left alone ---
	// String fields like Passphrase don't have defaults — empty string
	// means "no encryption" which is a valid choice, not a missing value.
	t.Run("string fields untouched", func(t *testing.T) {
		cfg := Config{Passphrase: "secret"}
		cfg.applyDefaults()

		if cfg.Passphrase != "secret" {
			t.Errorf("Passphrase: got %q, want %q", cfg.Passphrase, "secret")
		}
		if cfg.PbKeyLen != 0 {
			t.Errorf("PbKeyLen: got %d, want 0", cfg.PbKeyLen)
		}
	})
}

// TestListenerListenAndClose verifies that we can create a listener on a
// random port and shut it down cleanly without errors.
func TestListenerListenAndClose(t *testing.T) {
	// Use ":0" to let the OS pick a random available port.
	// This avoids port conflicts when running tests in parallel.
	l, err := Listen(":0", Config{})
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}

	// Close should return without error.
	if err := l.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

// TestListenerAddr verifies that Addr() returns the actual bound address,
// which is especially useful when binding to ":0" (random port).
func TestListenerAddr(t *testing.T) {
	l, err := Listen(":0", Config{})
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	defer l.Close()

	// Addr() should return a non-nil address.
	addr := l.Addr()
	if addr == nil {
		t.Fatal("Addr() returned nil")
	}

	// The address should be a valid UDP address with a real port
	// (not port 0, since the OS should have assigned one).
	udpAddr, ok := addr.(*net.UDPAddr)
	if !ok {
		t.Fatalf("Addr() returned %T, want *net.UDPAddr", addr)
	}
	if udpAddr.Port == 0 {
		t.Error("Addr() returned port 0; expected a real assigned port")
	}
}

// TestListenerShortPacketDiscarded verifies that packets shorter than the
// minimum SRT header size (16 bytes) are silently discarded without
// causing errors or panics.
func TestListenerShortPacketDiscarded(t *testing.T) {
	l, err := Listen(":0", Config{})
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	defer l.Close()

	// Get the address the listener is bound to so we can send to it.
	laddr := l.Addr().(*net.UDPAddr)

	// Open a client UDP socket to send packets to the listener.
	clientConn, err := net.DialUDP("udp", nil, laddr)
	if err != nil {
		t.Fatalf("DialUDP failed: %v", err)
	}
	defer clientConn.Close()

	// Send a packet that's too short to be a valid SRT packet.
	// The listener should silently discard it (no crash, no error).
	shortPacket := []byte{0x01, 0x02, 0x03} // Only 3 bytes, need 16
	_, err = clientConn.Write(shortPacket)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Give the readLoop a moment to process the packet.
	time.Sleep(50 * time.Millisecond)

	// If we get here without a panic or hang, the short packet was
	// correctly discarded. There's no observable side effect to check,
	// since discarding is silent by design.
}

// TestListenerAcceptAfterClose verifies that calling Accept() on a closed
// listener returns net.ErrClosed immediately instead of blocking forever.
func TestListenerAcceptAfterClose(t *testing.T) {
	l, err := Listen(":0", Config{})
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}

	// Close the listener first.
	if err := l.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Accept should return an error since the listener is closed.
	_, err = l.Accept()
	if err == nil {
		t.Fatal("Accept() returned nil error after Close; expected net.ErrClosed")
	}
	if err != net.ErrClosed {
		t.Fatalf("Accept() error = %v; want net.ErrClosed", err)
	}
}

// TestListenerAcceptBlocksUntilClose verifies that Accept() blocks when
// there are no pending connections, and unblocks when Close() is called.
func TestListenerAcceptBlocksUntilClose(t *testing.T) {
	l, err := Listen(":0", Config{})
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}

	// Run Accept() in a goroutine since it blocks.
	errCh := make(chan error, 1)
	go func() {
		_, err := l.Accept()
		errCh <- err
	}()

	// Give Accept() a moment to start blocking.
	time.Sleep(50 * time.Millisecond)

	// Close the listener, which should unblock Accept().
	l.Close()

	// Wait for Accept() to return (with a timeout to avoid hanging tests).
	select {
	case err := <-errCh:
		if err != net.ErrClosed {
			t.Fatalf("Accept() error = %v; want net.ErrClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Accept() did not unblock after Close(); test timed out")
	}
}

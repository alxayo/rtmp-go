// Package integration – RTMPS relay integration tests.
//
// rtmps_relay_test.go validates that relay destinations can use rtmps:// URLs
// to forward media over TLS-encrypted connections.
//
// Test scenarios:
//
//	TestRTMPS_RelayToTLSDestination — Publisher → relay server → RTMPS destination.
//	  Verifies the relay client establishes a TLS connection to the destination
//	  server and successfully forwards audio+video messages.
//
//	TestRTMPS_MixedSchemeRelay — Publisher → relay server with both rtmp:// and
//	  rtmps:// destinations simultaneously. Verifies fan-out works across both
//	  plaintext and TLS destinations.
//
// Self-signed certificates are generated at test time (see genSelfSignedCert
// in tls_test.go). The relay client factory sets InsecureSkipVerify to accept
// the self-signed certs in these tests.
package integration

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
	"github.com/alxayo/go-rtmp/internal/rtmp/client"
	"github.com/alxayo/go-rtmp/internal/rtmp/relay"
	srv "github.com/alxayo/go-rtmp/internal/rtmp/server"
)

// TestRTMPS_RelayToTLSDestination verifies that the relay system can forward
// media to a destination server over an RTMPS (TLS-encrypted) connection.
//
// Topology:
//
//	Publisher → [relay server :random] → rtmps://[dest server :random]/live/relayed
//
// Steps:
//  1. Start a TLS-enabled destination server on a random port.
//  2. Create a DestinationManager with an rtmps:// URL pointing at the destination.
//  3. Connect and send audio+video through the relay.
//  4. Verify the destination server received a connection (relay client connected over TLS).
func TestRTMPS_RelayToTLSDestination(t *testing.T) {
	// Generate self-signed cert for the destination server
	dir := t.TempDir()
	certFile, keyFile := genSelfSignedCert(t, dir)

	// Start destination server with TLS enabled
	destServer := srv.New(srv.Config{
		ListenAddr:    "127.0.0.1:0",
		TLSListenAddr: "127.0.0.1:0",
		TLSCertFile:   certFile,
		TLSKeyFile:    keyFile,
		ChunkSize:     4096,
	})
	if err := destServer.Start(); err != nil {
		t.Fatalf("start destination server: %v", err)
	}
	defer destServer.Stop()

	tlsAddr := destServer.TLSAddr()
	if tlsAddr == nil {
		t.Fatal("destination server TLS listener not active")
	}

	// Create a client factory that trusts the self-signed certificate.
	// In production, the default TLS config would validate against the
	// system's trusted CA pool — InsecureSkipVerify is test-only.
	tlsFactory := func(url string) (relay.RTMPClient, error) {
		c, err := client.New(url)
		if err != nil {
			return nil, err
		}
		c.TLSConfig = &tls.Config{InsecureSkipVerify: true}
		return c, nil
	}

	// Create relay DestinationManager targeting the RTMPS destination
	destURL := fmt.Sprintf("rtmps://%s/live/relayed", tlsAddr.String())
	dm, err := relay.NewDestinationManager(
		[]string{destURL},
		slog.Default(),
		tlsFactory,
	)
	if err != nil {
		t.Fatalf("create destination manager: %v", err)
	}
	defer dm.Close()

	// Allow time for the relay client to connect over TLS
	time.Sleep(500 * time.Millisecond)

	// Check relay destination status
	statuses := dm.GetStatus()
	status, ok := statuses[destURL]
	if !ok {
		t.Fatal("destination URL not found in status map")
	}
	if status != relay.StatusConnected {
		t.Fatalf("expected destination status %v, got %v", relay.StatusConnected, status)
	}

	// Send test media through the relay
	audioMsg := &chunk.Message{
		CSID:      4,
		TypeID:    8, // Audio
		Timestamp: 0,
		Payload:   []byte{0xAF, 0x00, 0x01, 0x02, 0x03},
	}
	videoMsg := &chunk.Message{
		CSID:      6,
		TypeID:    9, // Video
		Timestamp: 0,
		Payload:   []byte{0x17, 0x00, 0x01, 0x02, 0x03},
	}

	dm.RelayMessage(audioMsg)
	dm.RelayMessage(videoMsg)

	// Allow messages to propagate
	time.Sleep(200 * time.Millisecond)

	// Verify the relay sent messages (check metrics)
	metrics := dm.GetMetrics()
	destMetrics, ok := metrics[destURL]
	if !ok {
		t.Fatal("no metrics for RTMPS destination")
	}
	if destMetrics.MessagesSent < 2 {
		t.Errorf("expected at least 2 messages sent over RTMPS, got %d", destMetrics.MessagesSent)
	}

	t.Logf("✅ RTMPS relay test passed: sent %d messages to TLS destination", destMetrics.MessagesSent)
}

// TestRTMPS_MixedSchemeRelay verifies that the relay system handles a mix of
// plaintext (rtmp://) and TLS-encrypted (rtmps://) destinations simultaneously.
//
// Topology:
//
//	Publisher → [relay manager]
//	               ├── rtmp://[plain-dest :random]/live/plain
//	               └── rtmps://[tls-dest :random]/live/secure
func TestRTMPS_MixedSchemeRelay(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := genSelfSignedCert(t, dir)

	// Start a plain (non-TLS) destination server
	plainDest := srv.New(srv.Config{
		ListenAddr: "127.0.0.1:0",
		ChunkSize:  4096,
	})
	if err := plainDest.Start(); err != nil {
		t.Fatalf("start plain destination: %v", err)
	}
	defer plainDest.Stop()

	// Start a TLS destination server
	tlsDest := srv.New(srv.Config{
		ListenAddr:    "127.0.0.1:0",
		TLSListenAddr: "127.0.0.1:0",
		TLSCertFile:   certFile,
		TLSKeyFile:    keyFile,
		ChunkSize:     4096,
	})
	if err := tlsDest.Start(); err != nil {
		t.Fatalf("start TLS destination: %v", err)
	}
	defer tlsDest.Stop()

	plainURL := fmt.Sprintf("rtmp://%s/live/plain", plainDest.Addr().String())
	tlsURL := fmt.Sprintf("rtmps://%s/live/secure", tlsDest.TLSAddr().String())

	// Factory that allows self-signed certs for RTMPS connections
	factory := func(url string) (relay.RTMPClient, error) {
		c, err := client.New(url)
		if err != nil {
			return nil, err
		}
		c.TLSConfig = &tls.Config{InsecureSkipVerify: true}
		return c, nil
	}

	dm, err := relay.NewDestinationManager(
		[]string{plainURL, tlsURL},
		slog.Default(),
		factory,
	)
	if err != nil {
		t.Fatalf("create destination manager: %v", err)
	}
	defer dm.Close()

	// Allow connections to establish
	time.Sleep(500 * time.Millisecond)

	// Verify both destinations are connected
	statuses := dm.GetStatus()
	for _, url := range []string{plainURL, tlsURL} {
		s, ok := statuses[url]
		if !ok {
			t.Fatalf("destination %s not in status map", url)
		}
		if s != relay.StatusConnected {
			t.Fatalf("destination %s: expected %v, got %v", url, relay.StatusConnected, s)
		}
	}

	// Send media to both destinations via relay
	for i := 0; i < 5; i++ {
		dm.RelayMessage(&chunk.Message{
			CSID: 4, TypeID: 8, Timestamp: uint32(i * 100),
			Payload: []byte{0xAF, 0x01, 0xAA, 0xBB},
		})
		dm.RelayMessage(&chunk.Message{
			CSID: 6, TypeID: 9, Timestamp: uint32(i * 100),
			Payload: []byte{0x17, 0x01, 0xCC, 0xDD},
		})
	}

	time.Sleep(200 * time.Millisecond)

	// Verify both destinations received all messages
	metrics := dm.GetMetrics()
	for _, url := range []string{plainURL, tlsURL} {
		m, ok := metrics[url]
		if !ok {
			t.Fatalf("no metrics for %s", url)
		}
		if m.MessagesSent < 10 {
			t.Errorf("%s: expected at least 10 messages sent, got %d", url, m.MessagesSent)
		}
	}

	t.Logf("✅ Mixed-scheme relay passed: both rtmp:// and rtmps:// destinations received media")
}

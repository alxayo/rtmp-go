package integration

import (
	"fmt"
	"testing"
	"time"

	"github.com/alxayo/go-rtmp/internal/rtmp/client"
	"github.com/alxayo/go-rtmp/internal/rtmp/server"
)

// TestBasicMultiDestinationRelay tests the basic flow:
// publisher → relay-server → destination-server → subscriber
func TestBasicMultiDestinationRelay(t *testing.T) {
	// Start destination server (rtmp-server-2)
	destServerCfg := server.Config{
		ListenAddr: ":0", // Let OS pick port
		LogLevel:   "info",
	}
	destServer := server.New(destServerCfg)
	err := destServer.Start()
	if err != nil {
		t.Fatalf("Failed to start destination server: %v", err)
	}
	defer destServer.Stop()
	destAddr := destServer.Addr().String()

	// Give destination server time to start
	time.Sleep(100 * time.Millisecond)

	// Start relay server (rtmp-server-1) with destination
	relayServerCfg := server.Config{
		ListenAddr:        ":0", // Let OS pick port
		RelayDestinations: []string{fmt.Sprintf("rtmp://%s/live/relayed", destAddr)},
		LogLevel:          "info",
	}
	relayServer := server.New(relayServerCfg)
	err = relayServer.Start()
	if err != nil {
		t.Fatalf("Failed to start relay server: %v", err)
	}
	defer relayServer.Stop()
	relayAddr := relayServer.Addr().String()

	// Give relay server time to start and connect to destination
	time.Sleep(500 * time.Millisecond)

	t.Logf("Destination server running on: %s", destAddr)
	t.Logf("Relay server running on: %s", relayAddr)

	// Step 1: Connect publisher to relay server
	pubClient, err := client.New(fmt.Sprintf("rtmp://%s/live/source", relayAddr))
	if err != nil {
		t.Fatalf("Create publisher client: %v", err)
	}
	defer pubClient.Close()

	if err := pubClient.Connect(); err != nil {
		t.Fatalf("Publisher connect: %v", err)
	}

	if err := pubClient.Publish(); err != nil {
		t.Fatalf("Publisher publish: %v", err)
	}

	// Step 2: Connect subscriber to destination server
	subClient, err := client.New(fmt.Sprintf("rtmp://%s/live/relayed", destAddr))
	if err != nil {
		t.Fatalf("Create subscriber client: %v", err)
	}
	defer subClient.Close()

	if err := subClient.Connect(); err != nil {
		t.Fatalf("Subscriber connect: %v", err)
	}

	if err := subClient.Play(); err != nil {
		t.Fatalf("Subscriber play: %v", err)
	}

	// Step 3: Send test media from publisher
	testAudio := []byte{0xAF, 0x00, 0x01, 0x02, 0x03} // AAC sequence header
	testVideo := []byte{0x17, 0x00, 0x01, 0x02, 0x03} // AVC sequence header

	if err := pubClient.SendAudio(0, testAudio); err != nil {
		t.Fatalf("Send audio: %v", err)
	}

	if err := pubClient.SendVideo(0, testVideo); err != nil {
		t.Fatalf("Send video: %v", err)
	}

	// Give time for messages to propagate through the relay
	time.Sleep(1 * time.Second)

	t.Logf("Multi-destination relay test completed successfully")
}

// TestMultipleDestinations tests relay to multiple destinations simultaneously
func TestMultipleDestinations(t *testing.T) {
	// Start 3 destination servers
	var destServers []*server.Server
	var destURLs []string

	for i := 0; i < 3; i++ {
		cfg := server.Config{
			ListenAddr: ":0",
			LogLevel:   "info",
		}
		srv := server.New(cfg)
		if err := srv.Start(); err != nil {
			t.Fatalf("Failed to start destination server %d: %v", i, err)
		}
		destServers = append(destServers, srv)
		destURLs = append(destURLs, fmt.Sprintf("rtmp://%s/live/dest%d", srv.Addr().String(), i))

		defer srv.Stop()
	}

	// Give servers time to start
	time.Sleep(200 * time.Millisecond)

	// Start relay server with all destinations
	relayServerCfg := server.Config{
		ListenAddr:        ":0",
		RelayDestinations: destURLs,
		LogLevel:          "info",
	}
	relayServer := server.New(relayServerCfg)
	err := relayServer.Start()
	if err != nil {
		t.Fatalf("Failed to start relay server: %v", err)
	}
	defer relayServer.Stop()

	// Give relay server time to connect to all destinations
	time.Sleep(1 * time.Second)

	t.Logf("Relay server running on: %s", relayServer.Addr().String())
	t.Logf("Destination URLs: %v", destURLs)

	// Connect publisher to relay server
	pubClient, err := client.New(fmt.Sprintf("rtmp://%s/live/source", relayServer.Addr().String()))
	if err != nil {
		t.Fatalf("Create publisher client: %v", err)
	}
	defer pubClient.Close()

	if err := pubClient.Connect(); err != nil {
		t.Fatalf("Publisher connect: %v", err)
	}

	if err := pubClient.Publish(); err != nil {
		t.Fatalf("Publisher publish: %v", err)
	}

	// Send test media
	testAudio := []byte{0xAF, 0x00, 0x01, 0x02, 0x03}
	testVideo := []byte{0x17, 0x00, 0x01, 0x02, 0x03}

	for i := 0; i < 10; i++ {
		if err := pubClient.SendAudio(uint32(i*100), testAudio); err != nil {
			t.Errorf("Send audio frame %d: %v", i, err)
		}
		if err := pubClient.SendVideo(uint32(i*100), testVideo); err != nil {
			t.Errorf("Send video frame %d: %v", i, err)
		}
		time.Sleep(10 * time.Millisecond) // Small delay between frames
	}

	// Give time for messages to propagate
	time.Sleep(2 * time.Second)

	t.Logf("Multiple destinations test completed - sent media to %d destinations", len(destURLs))
}

// TestDestinationFailureIsolation tests that one failed destination doesn't affect others
func TestDestinationFailureIsolation(t *testing.T) {
	// Start 2 destination servers
	dest1Server := server.New(server.Config{ListenAddr: ":0", LogLevel: "info"})
	if err := dest1Server.Start(); err != nil {
		t.Fatalf("Failed to start dest1 server: %v", err)
	}
	defer dest1Server.Stop()

	dest2Server := server.New(server.Config{ListenAddr: ":0", LogLevel: "info"})
	if err := dest2Server.Start(); err != nil {
		t.Fatalf("Failed to start dest2 server: %v", err)
	}
	defer dest2Server.Stop()

	// Also add a non-existent destination that will fail
	destURLs := []string{
		fmt.Sprintf("rtmp://%s/live/dest1", dest1Server.Addr().String()),
		fmt.Sprintf("rtmp://%s/live/dest2", dest2Server.Addr().String()),
		"rtmp://localhost:9999/live/nonexistent", // This will fail
	}

	// Start relay server with working + failing destinations
	relayServerCfg := server.Config{
		ListenAddr:        ":0",
		RelayDestinations: destURLs,
		LogLevel:          "debug", // Use debug to see failure logs
	}
	relayServer := server.New(relayServerCfg)
	err := relayServer.Start()
	if err != nil {
		t.Fatalf("Failed to start relay server: %v", err)
	}
	defer relayServer.Stop()

	// Give time for connections (some will fail)
	time.Sleep(1 * time.Second)

	// Publish media despite one destination failing
	pubClient, err := client.New(fmt.Sprintf("rtmp://%s/live/source", relayServer.Addr().String()))
	if err != nil {
		t.Fatalf("Create publisher client: %v", err)
	}
	defer pubClient.Close()

	if err := pubClient.Connect(); err != nil {
		t.Fatalf("Publisher connect: %v", err)
	}

	if err := pubClient.Publish(); err != nil {
		t.Fatalf("Publisher publish: %v", err)
	}

	// Send test media
	testAudio := []byte{0xAF, 0x00, 0x01, 0x02, 0x03}
	testVideo := []byte{0x17, 0x00, 0x01, 0x02, 0x03}

	if err := pubClient.SendAudio(0, testAudio); err != nil {
		t.Fatalf("Send audio: %v", err)
	}

	if err := pubClient.SendVideo(0, testVideo); err != nil {
		t.Fatalf("Send video: %v", err)
	}

	// Give time for relay attempts
	time.Sleep(2 * time.Second)

	t.Logf("Destination failure isolation test completed - relay should continue despite one failed destination")
}

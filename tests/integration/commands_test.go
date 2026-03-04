// Package integration – end-to-end integration tests for the RTMP server.
//
// commands_test.go validates the full RTMP command exchange sequence through
// a real server and client:
//
//  1. connect       → _result (NetConnection.Connect.Success)
//  2. createStream  → _result with stream ID
//  3. publish       → onStatus (NetStream.Publish.Start)
//  4. play          → subscriber connected
//
// The test starts a real server on an ephemeral port, uses the RTMP client
// to perform the full handshake + command sequence, and verifies that
// publish and play complete without errors.
package integration

import (
	"fmt"
	"testing"
	"time"

	"github.com/alxayo/go-rtmp/internal/rtmp/client"
	srv "github.com/alxayo/go-rtmp/internal/rtmp/server"
)

// TestCommandsFlow exercises the full command exchange through a live server.
// It verifies connect → createStream → publish (and play) work end-to-end.
func TestCommandsFlow(t *testing.T) {
	s := srv.New(srv.Config{
		ListenAddr: "127.0.0.1:0",
		ChunkSize:  4096,
	})
	if err := s.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	defer s.Stop()

	addr := s.Addr().String()

	t.Run("connect_createStream_publish", func(t *testing.T) {
		c, err := client.New(fmt.Sprintf("rtmp://%s/live/test_publish", addr))
		if err != nil {
			t.Fatalf("client new: %v", err)
		}
		defer c.Close()

		// Connect performs: TCP dial → handshake → connect command → createStream
		if err := c.Connect(); err != nil {
			t.Fatalf("connect: %v", err)
		}

		// Publish sends the publish command and expects onStatus success
		if err := c.Publish(); err != nil {
			t.Fatalf("publish: %v", err)
		}

		// Verify server registered the stream
		time.Sleep(50 * time.Millisecond)
		if s.ConnectionCount() < 1 {
			t.Fatalf("expected at least 1 connection, got %d", s.ConnectionCount())
		}
	})

	t.Run("connect_createStream_play", func(t *testing.T) {
		// First, set up a publisher so play has something to subscribe to
		pub, err := client.New(fmt.Sprintf("rtmp://%s/live/test_play", addr))
		if err != nil {
			t.Fatalf("publisher new: %v", err)
		}
		defer pub.Close()
		if err := pub.Connect(); err != nil {
			t.Fatalf("publisher connect: %v", err)
		}
		if err := pub.Publish(); err != nil {
			t.Fatalf("publisher publish: %v", err)
		}

		// Now connect a subscriber
		sub, err := client.New(fmt.Sprintf("rtmp://%s/live/test_play", addr))
		if err != nil {
			t.Fatalf("subscriber new: %v", err)
		}
		defer sub.Close()
		if err := sub.Connect(); err != nil {
			t.Fatalf("subscriber connect: %v", err)
		}
		if err := sub.Play(); err != nil {
			t.Fatalf("subscriber play: %v", err)
		}
	})
}

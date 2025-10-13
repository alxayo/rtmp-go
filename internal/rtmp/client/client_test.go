package client

import (
	"fmt"
	"testing"
	"time"

	"github.com/alxayo/go-rtmp/internal/rtmp/server"
)

// TestConnectFlow dials a real in-process server and exercises handshake + connect + createStream.
func TestConnectFlow(t *testing.T) {
	s := server.New(server.Config{ListenAddr: ":0"})
	if err := s.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer s.Stop()
	addr := s.Addr().String()
	c, err := New(fmt.Sprintf("rtmp://%s/app/stream", addr))
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if err := c.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	_ = c.Close()
}

// TestPublishFlow ensures Publish command can be sent after connect.
func TestPublishFlow(t *testing.T) {
	s := server.New(server.Config{ListenAddr: ":0"})
	if err := s.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer s.Stop()
	addr := s.Addr().String()
	c, err := New(fmt.Sprintf("rtmp://%s/live/testpub", addr))
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if err := c.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := c.Publish(); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := c.SendAudio(0, []byte{0xAF, 0x00}); err != nil {
		t.Fatalf("send audio: %v", err)
	}
	if err := c.SendVideo(0, []byte{0x17, 0x00}); err != nil {
		t.Fatalf("send video: %v", err)
	}
	_ = c.Close()
}

// TestPlayFlow ensures Play command can be sent.
func TestPlayFlow(t *testing.T) {
	s := server.New(server.Config{ListenAddr: ":0"})
	if err := s.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer s.Stop()
	addr := s.Addr().String()
	c, err := New(fmt.Sprintf("rtmp://%s/live/testplay", addr))
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if err := c.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := c.Play(); err != nil {
		t.Fatalf("play: %v", err)
	}
	// Allow small delay for any async server handling (handshake already done).
	time.Sleep(50 * time.Millisecond)
	_ = c.Close()
}

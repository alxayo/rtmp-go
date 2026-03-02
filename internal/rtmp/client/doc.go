// Package client provides a minimal RTMP client for tests and integration
// validation.
//
// This client implements just enough of the RTMP protocol to drive the
// server through its connection lifecycle:
//
//	1. TCP dial + RTMP handshake
//	2. connect command → wait for _result
//	3. createStream command → wait for _result (stream ID)
//	4. publish or play command
//	5. Send/receive audio and video messages
//
// # Limitations
//
// This client is intentionally minimal. It does not support:
//   - Bandwidth negotiation or flow control
//   - Extended timestamps
//   - AMF3 encoding
//   - Reconnection or retry logic
//
// The primary consumer is the integration test suite in tests/integration/.
//
// # Usage
//
//	c, err := client.New("rtmp://localhost:1935/live/stream")
//	if err != nil { ... }
//	if err := c.Connect(); err != nil { ... }
//	if err := c.Publish(); err != nil { ... }
//	_ = c.SendVideo(0, videoData)
//	_ = c.SendAudio(0, audioData)
//	c.Close()
package client

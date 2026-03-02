// Package integration – end-to-end integration tests for the RTMP server.
//
// quickstart_test.go is the top-level “quickstart” scenario from the
// project documentation.  It specifies the full lifecycle:
//
//	Server startup → publisher connect → handshake → connect command →
//	createStream → publish → send AVC/AAC sequence headers →
//	server detects codecs → server logs expected milestones.
//
// The test is structured in five sequential phases:
//  1. Server bootstrap (listener + goroutines)
//  2. Mock publisher connection + handshake
//  3. Command sequence (connect/createStream/publish)
//  4. Media initiation (video + audio messages)
//  5. Log assertions (expected milestone substrings)
//
// Currently all phases are TODO stubs and the test deliberately fails
// via t.Fatalf to serve as a TDD driver.  As protocol layers are
// implemented (handshake, chunking, control, AMF0, RPC, media), each
// phase will be replaced with real code.
//
// Run with:
//
//	go test -run TestQuickstartScenario ./tests/integration -v
package integration

// Integration test scaffold for T012 (Quickstart end-to-end scenario).
// This test is intentionally a high-level specification-driven harness that will
// evolve as lower protocol layers (handshake, chunking, control, AMF0, RPC, media)
// become implemented. For now it encodes the EXPECTED sequence, required
// assertions, and logging checkpoints from `quickstart.md` so future tasks can
// progressively replace TODO/failure points with working logic.
//
// Scope & Goals (from task requirements):
//   - Server startup listening on :1935
//   - Connection acceptance + handshake
//   - connect command processing → _result NetConnection.Connect.Success
//   - createStream + publish command (stream key live/test)
//   - Codec detection (H.264 AVC, AAC)
//   - Mock FFmpeg publisher sending: handshake, connect, createStream, publish,
//     audio & video messages (H.264 SPS/PPS, AAC AudioSpecificConfig + frames)
//   - Assertions that server logs contain expected milestones
//
// Current Status:
//   - No underlying implementation exists yet; dependent packages are stubs.
//   - This test therefore FAILS deliberately (TDD) to drive upcoming tasks.
//   - Replace placeholder sections incrementally as features land:
//       * T013-T016: Handshake FSM → replace handshake placeholder
//       * T017-T021: Chunk reader/writer → actual media/control message exchange
//       * T022-T025: Control burst verification (post-handshake)
//       * T026-T032: AMF0 encode/decode → real command message crafting
//       * T033-T040: RPC command dispatcher & responses
//       * Media layer tasks (T041+) for codec parsing/detection
//
// Usage Notes:
//   Run with: go test -run TestQuickstartScenario ./tests/integration -v
//   Expect failure until end-to-end stack implemented.

import (
	"testing"
	// Future (uncomment as implementations become available):
	// "net"
	// "time"
	// "github.com/alxayo/go-rtmp/internal/logger"
	// "github.com/alxayo/go-rtmp/internal/rtmp/handshake"
	// "github.com/alxayo/go-rtmp/internal/rtmp/chunk"
	// "github.com/alxayo/go-rtmp/internal/rtmp/rpc"
)

// expectedServerLogKeys lists the log substrings the fully-implemented
// test will eventually assert appear (in order) in captured server logs.
// Each entry maps to a protocol milestone from the quickstart scenario.
var expectedServerLogKeys = []string{
	"RTMP server starting", // startup
	"Server listening",     // listening on :1935
	"Connection accepted",  // inbound connection
	"Handshake completed",  // handshake success
	"connect command",      // connect parsed
	"createStream",         // stream allocation
	"publish command",      // publish accepted
	"Codec detected",       // H.264 + AAC identified
	// Future (playback client) entries could include: play command, Subscriber added
}

// TestQuickstartScenario is the master end-to-end acceptance test.
//
// It fails immediately until the full protocol stack is wired up.
// The t.Fatalf message lists all blocking task groups so developers
// know exactly which components to implement first.
func TestQuickstartScenario(t *testing.T) {
	t.Helper()

	// PHASE 1: Server bootstrap (placeholder)
	// TODO: Implement real server bootstrap (listener on :1935, goroutines, logger setup).
	// Example (future): go server.Run()

	// PHASE 2: Mock publisher connection + handshake
	// TODO: Dial :1935, perform ClientHandshake, verify server logs handshake completion.

	// PHASE 3: Command sequence (connect → _result, createStream → _result, publish → onStatus)
	// TODO: Use AMF0 encoder to craft command messages and chunk.Writer to send them.

	// PHASE 4: Media initiation (send AVC sequence header & AAC AudioSpecificConfig)
	// TODO: Send video (typeID=9) + audio (typeID=8) messages; trigger codec detection.

	// PHASE 5: Assertions – gather captured logs and check presence/order of expected keys.
	// TODO: Implement log capture harness in logger package (maybe in-memory handler for tests).

	// For now, force failure with informative guidance so developers know what to implement next.
	t.Fatalf("quickstart end-to-end scenario not implemented yet; implement handshake (T013-T016), chunking (T017-T021), control (T022-T025), AMF0 (T026-T032), RPC (T033-T040), and media parsing (T041+) to satisfy this test")
}

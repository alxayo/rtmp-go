package integration

import (
	"net"
	"testing"
	// Future imports (will be used once implementations exist):
	// "github.com/alxayo/go-rtmp/internal/rtmp/handshake"
	// "github.com/alxayo/go-rtmp/internal/rtmp/chunk"
	// "github.com/alxayo/go-rtmp/internal/rtmp/rpc"
)

// TestCommandsFlow is the integration test scaffold for T011.
// It defines the end-to-end command workflow expectations:
//  1. connect        -> server replies _result (NetConnection.Connect.Success)
//  2. createStream   -> server replies _result with stream ID (e.g., 1)
//  3. publish        -> server sends onStatus NetStream.Publish.Start
//  4. play           -> server sends onStatus NetStream.Play.Start
//
// Implementation Notes (to be satisfied by later tasks T032-T040):
// - Handshake already covered by T009; this test begins AFTER a successful handshake.
// - AMF0 generic encoder/decoder (T032) will provide helpers to build/parse command payloads.
// - Command dispatcher (T040) will route messages based on first AMF0 string in payload.
// - Stream ID allocation expected to start at 1.
// - onStatus messages must include level="status", code matching the scenario, and description.
//
// Current State:
//   - No RPC/command implementation yet; this test intentionally fails (TDD) via t.Fatal placeholders.
//   - When implementing, replace the placeholders with real client/server harness using chunk.Reader/Writer
//     to exchange AMF0 command messages over an in-memory net.Pipe().
func TestCommandsFlow(t *testing.T) {
	// Use subtests so individual flows can be debugged independently.

	// 1. connect flow
	// Expected message sequence (logical):
	//   C->S:  command(connect, tx=1, obj{app, tcUrl, objectEncoding=0})
	//   S->C:  command(_result, tx=1, properties{fmsVer,capabilities,mode}, info{code=NetConnection.Connect.Success})
	// Future assertions: verify properties + info fields and AMF0 types.
	// Failure driver for now:
	serverConn1, clientConn1 := net.Pipe()
	_ = serverConn1.Close()
	_ = clientConn1.Close()
	if true { // placeholder branch; remove once implemented
		// NOTE: This forces the test to fail until connect handling is implemented.
		// Replace with real harness invoking handshake + command exchange.
		// Keep failure message descriptive to guide implementation.
		t.Fatal("connect flow not implemented (awaiting AMF0 + RPC layers T026-T040)")
	}

	// 2. createStream flow
	// Sequence (after successful connect):
	//   C->S: command(createStream, tx=4, null)
	//   S->C: command(_result, tx=4, null, 1.0)  // stream ID 1
	// Placeholder failure:
	serverConn2, clientConn2 := net.Pipe()
	_ = serverConn2.Close()
	_ = clientConn2.Close()
	if true {
		t.Fatal("createStream flow not implemented (awaiting RPC dispatcher & response builder T033-T036)")
	}

	// 3. publish flow
	// Prerequisites: stream ID allocated.
	// Sequence:
	//   C->S: command(publish, tx=0, null, "streamKey", "live") on MSID=1
	//   S->C: command(onStatus, 0, null, {code: NetStream.Publish.Start}) on MSID=1
	serverConn3, clientConn3 := net.Pipe()
	_ = serverConn3.Close()
	_ = clientConn3.Close()
	if true {
		t.Fatal("publish flow not implemented (awaiting publish parser and onStatus builder T037-T039)")
	}

	// 4. play flow
	// Sequence:
	//   C->S: command(play, tx=0, null, "streamKey", -2, -1, true) on MSID=1
	//   S->C: command(onStatus, 0, null, {code: NetStream.Play.Start}) on MSID=1
	serverConn4, clientConn4 := net.Pipe()
	_ = serverConn4.Close()
	_ = clientConn4.Close()
	if true {
		t.Fatal("play flow not implemented (awaiting play parser and onStatus builder T038-T039)")
	}
}

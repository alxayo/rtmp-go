// Package handshake implements the RTMP v3 simple handshake protocol.
//
// The RTMP handshake is the first step in establishing a connection. Both sides
// exchange version bytes and random data to verify connectivity before any
// application data (chunks/messages) is transmitted.
//
// # Handshake Phases
//
// Each side sends three pieces of data:
//
//   - C0/S0 (1 byte): Protocol version. Must be 0x03 for RTMP v3.
//   - C1/S1 (1536 bytes): Random data with a 4-byte timestamp at the start.
//   - C2/S2 (1536 bytes): Echo of the peer's C1/S1 data for verification.
//
// The exchange follows this sequence:
//
//	Client              Server
//	──────              ──────
//	C0+C1  ──────────►
//	       ◄──────────  S0+S1+S2
//	C2     ──────────►
//
// # Timeouts
//
// Each read/write phase uses a 5-second timeout to prevent hung connections.
// Timeout errors are wrapped as [errors.TimeoutError].
//
// # Usage
//
//	// Server side (after net.Listener.Accept):
//	err := handshake.ServerHandshake(conn)
//
//	// Client side (after net.Dial):
//	err := handshake.ClientHandshake(conn)
//
// After a successful handshake, the connection transitions to the chunk layer
// for all further communication.
package handshake

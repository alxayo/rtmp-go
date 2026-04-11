// Package handshake implements the SRT connection negotiation protocol.
//
// # Overview
//
// SRT is built on UDP, but it needs to establish shared parameters
// (buffer sizes, delays, encryption keys, etc.) before data can flow.
// The handshake is a 1.5-RTT exchange: client sends INDUCTION, server
// replies with INDUCTION, client sends CONCLUSION with final parameters,
// server confirms with CONCLUSION (or rejects).
//
// # Message Types
//
// There are two handshake message types:
//
// INDUCTION (0x0000): Client or server proposes initial parameters.
//   - Sent first by client, replied to by server
//   - Contains protocol version, MTU, TSBPD delay, KM frequency, etc.
//   - Unsigned (no authentication yet)
//
// CONCLUSION (0x0001): Client or server commits to final parameters.
//   - Sent second by client, replied to by server
//   - Client copies server's proposed values (with possible overrides)
//   - Both sides must agree on critical fields (MTU, TSBPD)
//   - If server rejects, it responds with a CONCLUSION containing an error code
//
// # State Machine
//
// CLIENT                                SERVER
// ------                                ------
// new conn
// send INDUCTION (version, MTU, TSBPD) →
//                                       ← recv INDUCTION, reply INDUCTION
// recv server INDUCTION
// send CONCLUSION (final params)        →
//                                       ← recv CONCLUSION, validate & reply
// Connected!                            Connected!
//
// # Key Parameters (Exchanged During Handshake)
//
// - Version: SRT protocol version (1 = SRT 1.4.x)
// - MTU: Maximum Transmission Unit (1200-1500 bytes typical)
// - TSBPD: Time-based sequence buffer delay (buffer duration on receiver)
// - Encryption: Key mode (none, AES-128, custom), KM frequency (every N packets)
// - Congestion Control: LOSSMAX (allowed packet loss %), RTT thresholds
// - Socket ID: Unique identifier for this connection (for multiplexing over same UDP port)
//
// # Asymmetries
//
// Client and server have slightly different roles:
// - Server can REJECT a CONCLUSION by setting error_code > 0
// - Server uses initial INDUCTION to size buffers before CONCLUSION arrives
// - Client waits for server's CONCLUSION before committing to buffers
//
// # Encryption Handshake
//
// If both sides support encryption:
// 1. Client sends preferred encryption mode (AES-128-CTR) in INDUCTION
// 2. Server agrees or proposes alternative in its INDUCTION
// 3. During CONCLUSION, both exchange Key Material (KM) packets
// 4. KM contains the actual AES key (encrypted with a shared passphrase)
// 5. After CONCLUSION, all data packets are encrypted
//
// See internal/srt/crypto for key derivation and AES-CTR implementation.
//
// # Integration Points
//
// - packet package: Parses/serializes handshake packet format
// - conn package: Calls handshake to establish connection
// - crypto package: Handles KM (Key Material) exchange if encryption enabled
// - listener package: Server-side handshake acceptance
//
// # Example: Server-Side Handshake
//
//	// Receive client's INDUCTION
//	hs := handshake.NewServer()
//	pkt, _ := packet.DecodeHandshake(buf)
//	hs.HandleClientInduction(pkt)
//
//	// Send server INDUCTION back
//	response := hs.ServerInduction()
//	conn.Send(response)
//
//	// Receive client CONCLUSION
//	pkt, _ := packet.DecodeHandshake(buf)
//	if err := hs.HandleClientConclusion(pkt); err != nil {
//	    response := hs.RejectConclusion(err)
//	    conn.Send(response)
//	    return
//	}
//
//	// Send acceptance CONCLUSION
//	response := hs.ServerConclusion()
//	conn.Send(response)
//	// Connection is now established
//
// # Timeout & Retransmission
//
// Handshake is NOT automatic. The listener or connection loop must:
// 1. Implement retransmission (if no response after 100ms, resend)
// 2. Implement timeout (if no response after ~5 seconds, give up)
// 3. Implement state machine (don't send CONCLUSION until after receiving server INDUCTION)
//
// See internal/srt/listener for a complete example.
package handshake

// Package control implements RTMP protocol control messages (types 1-6).
//
// Control messages manage the chunk-level transport: negotiating chunk size,
// flow control via window acknowledgements, and stream lifecycle signals.
// They always use CSID 2 and Message Stream ID 0.
//
// # Message Types
//
//   - Type 1 – Set Chunk Size: Changes the maximum chunk payload size.
//   - Type 2 – Abort Message: Discards a partially received message.
//   - Type 3 – Acknowledgement: Reports bytes received for flow control.
//   - Type 5 – Window Acknowledgement Size: Sets the ack window.
//   - Type 6 – Set Peer Bandwidth: Constrains the peer's output rate.
//
// # User Control Events (Type 4)
//
// User Control messages carry a 2-byte event type. Supported events:
//   - StreamBegin (0): Signals a stream is ready for use.
//   - PingRequest (6): Server-initiated liveness check.
//   - PingResponse (7): Client response to a ping.
//
// # Usage
//
//	// Encode a control message:
//	msg := control.EncodeSetChunkSize(4096)
//
//	// Decode a received control message:
//	result, err := control.Decode(msg.TypeID, msg.Payload)
//
//	// Handle control messages on a connection:
//	err := control.Handle(ctx, msg)
package control

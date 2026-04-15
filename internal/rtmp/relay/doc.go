// Package relay implements multi-destination RTMP relay for forwarding live
// streams to external RTMP or RTMPS (TLS-encrypted) servers.
//
// When configured with one or more relay destination URLs (e.g.,
// rtmp://cdn.example.com/live/key or rtmps://secure-cdn.example.com/live/key),
// the relay system connects to each destination and forwards audio/video
// messages in real time. TLS destinations use the RTMP client's built-in
// tls.Dialer for encrypted outbound connections.
//
// # Components
//
//   - [Destination]: Represents a single relay target. Manages connection
//     state (Disconnected/Connecting/Connected/Error) and tracks metrics
//     (messages sent, bytes sent, dropped messages, reconnect count).
//   - [DestinationManager]: Coordinates multiple destinations. Its
//     [DestinationManager.RelayMessage] method fans out each media message
//     to all connected destinations in parallel.
//
// # Usage
//
// The relay is typically configured via the server's -relay-to CLI flag:
//
//	./rtmp-server -relay-to rtmp://dest1/live/key -relay-to rtmps://secure-dest2/live/key
//
// The server creates a DestinationManager during startup and passes it to
// the media dispatch layer, which calls RelayMessage for every audio/video
// message received from the publisher.
//
// # Interface
//
// [RTMPClient] defines the interface that relay destinations use to connect
// and send messages. In production this is implemented by the client package;
// tests can substitute mock implementations via the client factory function.
package relay

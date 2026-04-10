package ingress

// Publisher represents a media source that pushes audio/video into the server.
// Both RTMP connections and SRT connections can implement this interface.
// It provides the minimal identity information needed for the publish lifecycle.
//
// Any ingest protocol can satisfy Publisher by providing four read-only
// identity methods plus a Close method for disconnection.
type Publisher interface {
	// ID returns a unique identifier for this publisher.
	// Each publisher should have a globally unique ID so the server can
	// distinguish between different connections. Examples:
	//   "rtmp-abc123"   (an RTMP connection)
	//   "srt-xyz789"    (an SRT connection)
	ID() string

	// Protocol returns the name of the ingest protocol this publisher
	// uses. The value is a short lowercase string such as "rtmp" or "srt".
	// It is used for logging and metrics so operators can see which
	// protocol each stream arrives on.
	Protocol() string

	// RemoteAddr returns the peer's network address as a string.
	// This is typically the IP and port of the client, for example
	// "192.168.1.100:54321". It is used for logging and access control.
	RemoteAddr() string

	// StreamKey returns the stream key used for routing. The stream key
	// determines which logical stream the media belongs to, for example
	// "live/mystream". Two publishers with the same stream key are
	// considered to be competing for the same channel; only one may be
	// active at a time.
	StreamKey() string

	// Close disconnects the publisher, releasing any underlying network
	// resources (sockets, buffers, etc.). After Close returns the
	// publisher must not push any more media.
	Close() error
}

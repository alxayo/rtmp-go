package chunk

// Message represents a fully reassembled RTMP message after chunk-level
// reassembly. RTMP splits large messages into smaller "chunks" for
// transmission; the Reader reassembles them back into complete Messages.
//
// Think of it like receiving a letter that was split across multiple
// envelopes — this struct is the fully reconstructed letter.
type Message struct {
	CSID            uint32 // Chunk Stream ID — identifies which logical stream this chunk belongs to
	Timestamp       uint32 // Message timestamp in milliseconds (absolute or accumulated from deltas)
	MessageLength   uint32 // Total length of the Payload in bytes
	TypeID          uint8  // Message type: 1-6=control, 8=audio, 9=video, 20=AMF0 command
	MessageStreamID uint32 // Identifies the application-level stream (0=control, 1+=media)
	Payload         []byte // The actual message data (audio frame, video frame, command, etc.)
}

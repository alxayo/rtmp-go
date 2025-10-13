package chunk

// NOTE: This is a temporary stub to allow integration tests (T010) to compile.
// Real implementation will be provided by tasks T017-T021.
// Do not rely on behavior here; all methods intentionally return "not implemented" errors.

// Message represents a fully reassembled RTMP message (post-dechunking).
// Field naming follows the chunking contract; exported to allow integration tests to assert values.
type Message struct {
	CSID            uint32
	Timestamp       uint32
	MessageLength   uint32
	TypeID          uint8
	MessageStreamID uint32
	Payload         []byte
}

// NOTE: Reader implementation provided in reader.go (T020). Writer implementation now lives in writer.go (T021).

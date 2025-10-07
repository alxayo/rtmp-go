package chunk

// NOTE: This is a temporary stub to allow integration tests (T010) to compile.
// Real implementation will be provided by tasks T017-T021.
// Do not rely on behavior here; all methods intentionally return "not implemented" errors.

import (
	"errors"
	"io"
)

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

// Reader will dechunk byte streams into Messages. Stub only.
type Reader struct {
	chunkSize uint32
	br        io.Reader
}

// NewReader creates a new dechunker with an initial inbound chunk size.
func NewReader(r io.Reader, chunkSize uint32) *Reader {
	return &Reader{br: r, chunkSize: chunkSize}
}

// SetChunkSize updates the reader's active chunk size (e.g., after receiving a Set Chunk Size control message).
func (r *Reader) SetChunkSize(size uint32) { r.chunkSize = size }

// ReadMessage reads and reassembles the next complete message from the stream.
func (r *Reader) ReadMessage() (*Message, error) {
	return nil, errors.New("chunk.Reader not implemented (T017-T021 pending)")
}

// Writer fragments Messages into chunks. Stub only.
type Writer struct {
	chunkSize uint32
	w         io.Writer
}

// NewWriter creates a writer with the given outbound chunk size.
func NewWriter(w io.Writer, chunkSize uint32) *Writer { return &Writer{w: w, chunkSize: chunkSize} }

// WriteMessage writes the message in chunked form.
func (w *Writer) WriteMessage(msg *Message) error {
	return errors.New("chunk.Writer not implemented (T018/T021 pending)")
}

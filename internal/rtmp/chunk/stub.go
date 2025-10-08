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

// NOTE: Reader implementation provided in reader.go (T020). This stub file retains Writer stub only.

// Writer fragments Messages into chunks. Stub only.
type Writer struct {
	chunkSize uint32
	w         io.Writer
}

// NewWriter creates a writer with the given outbound chunk size.
func NewWriter(w io.Writer, chunkSize uint32) *Writer { return &Writer{w: w, chunkSize: chunkSize} }

// EncodeHeaderOnly writes just the chunk header for the provided header spec h (T018 helper for tests).
// prev is the previous header on the same CSID (required for correct FMT3 + extended timestamp emission).
// Returns number of bytes written or error.
func (w *Writer) EncodeHeaderOnly(h *ChunkHeader, prev *ChunkHeader) (int, error) {
	b, err := EncodeChunkHeader(h, prev)
	if err != nil {
		return 0, err
	}
	n, err := w.w.Write(b)
	return n, err
}

// WriteMessage writes the message in chunked form.
func (w *Writer) WriteMessage(msg *Message) error {
	return errors.New("chunk.Writer not implemented (T018/T021 pending)")
}

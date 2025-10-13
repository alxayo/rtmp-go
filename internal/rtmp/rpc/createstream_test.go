package rpc

import (
	"testing"

	"github.com/alxayo/go-rtmp/internal/rtmp/amf"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

func buildCreateStreamMessage(payload []byte) *chunk.Message {
	return &chunk.Message{TypeID: 20, Payload: payload}
}

func TestParseCreateStreamCommand_Valid(t *testing.T) {
	payload, err := amf.EncodeAll(
		"createStream", // command name
		2.0,            // transaction ID
		nil,            // null per spec
	)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	cmd, err := ParseCreateStreamCommand(buildCreateStreamMessage(payload))
	if err != nil {
		t.Fatalf("ParseCreateStreamCommand returned error: %v", err)
	}
	if cmd.TransactionID != 2.0 {
		t.Fatalf("unexpected transaction id: %+v", cmd)
	}
}
